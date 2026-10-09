package textfiles

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"molr.ai/runtime/internal/core"
)

type SearchOptions struct {
	Pattern              string
	Regex, CaseSensitive bool
	Glob, ExcludeGlob    []string
	Mode                 string
	ContextLines         int
	IncludeHidden        bool
	MaxResults           int
}
type SearchLine struct {
	Line int
	Text string
}
type SearchMatch struct {
	Path          string
	Line          int          `json:",omitempty"`
	Text          string       `json:",omitempty"`
	Before, After []SearchLine `json:",omitempty"`
	Version       string
}
type SearchResult struct {
	TargetKind                                 string
	Matches                                    []SearchMatch
	Complete                                   bool
	Truncated                                  bool
	StopReason                                 string `json:",omitempty"`
	EntriesScanned, FilesScanned, BytesScanned int
	Skipped                                    map[string]int
}

func (o SearchOptions) Validate() error {
	if o.Mode != "" && o.Mode != "content" && o.Mode != "files" {
		return core.Invalid("mode", "content or files required")
	}
	if (o.Mode != "files" && o.Pattern == "") || len(o.Pattern) > 2048 || !utf8.ValidString(o.Pattern) || strings.ContainsRune(o.Pattern, 0) {
		return core.Invalid("pattern", "bounded UTF-8 pattern required")
	}
	if o.ContextLines < 0 || o.ContextLines > 5 || o.MaxResults < 0 || o.MaxResults > 200 || len(o.Glob)+len(o.ExcludeGlob) > 32 {
		return core.Invalid("search", "search limits exceeded")
	}
	if o.Regex {
		if _, err := regexp.Compile(o.Pattern); err != nil {
			return core.Invalid("pattern", "invalid RE2 regular expression")
		}
	}
	for _, g := range append(append([]string{}, o.Glob...), o.ExcludeGlob...) {
		if g == "" || len(g) > 512 || strings.HasPrefix(g, "/") || strings.Contains(g, "\\") || strings.Contains(g, "..") {
			return core.Invalid("glob", "relative glob required")
		}
		for _, part := range strings.Split(g, "/") {
			if part != "**" {
				if _, err := path.Match(part, ""); err != nil {
					return core.Invalid("glob", "invalid glob")
				}
			}
		}
	}
	return nil
}

type pathGlob []string

func compileGlobs(patterns []string) []pathGlob {
	out := make([]pathGlob, len(patterns))
	for i, pattern := range patterns {
		out[i] = strings.Split(pattern, "/")
	}
	return out
}

func (pattern pathGlob) matches(parts []string) bool {
	i, j, star, retry := 0, 0, -1, 0
	for j < len(parts) {
		if i < len(pattern) && pattern[i] == "**" {
			// A directory wildcard first consumes nothing. On a later mismatch,
			// extend the latest wildcard by one component and retry its suffix.
			star, retry = i, j
			i++
			continue
		}
		if i < len(pattern) {
			if ok, _ := path.Match(pattern[i], parts[j]); ok {
				i++
				j++
				continue
			}
		}
		if star < 0 {
			return false
		}
		retry++
		i, j = star+1, retry
	}
	for i < len(pattern) && pattern[i] == "**" {
		i++
	}
	return i == len(pattern)
}

func matchGlobs(patterns []pathGlob, parts []string) bool {
	for _, pattern := range patterns {
		if pattern.matches(parts) {
			return true
		}
	}
	return false
}

// Search reads through the same confined descriptors as file tools. It never
// executes a command, follows discovered links, or interprets .gitignore files.
// authorize checks every visited path before opening it.
func Search(ctx context.Context, root, name string, o SearchOptions, authorize func(string) error) (SearchResult, error) {
	out := SearchResult{Matches: []SearchMatch{}, Complete: true, Skipped: map[string]int{}}
	if err := o.Validate(); err != nil {
		return out, err
	}
	if o.MaxResults == 0 {
		o.MaxResults = 50
	}
	if o.Mode == "" {
		o.Mode = "content"
	}
	deadline := time.Now().Add(5 * time.Second)
	outputBytes := 0
	stop := func(reason string) {
		out.Complete = false
		out.Truncated = true
		if out.StopReason == "" {
			out.StopReason = reason
		}
	}
	check := func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if time.Now().After(deadline) {
			stop("time_limit")
		}
		return nil
	}
	pattern := o.Pattern
	if !o.Regex {
		pattern = regexp.QuoteMeta(pattern)
	}
	if !o.CaseSensitive {
		pattern = "(?i)" + pattern
	}
	re := regexp.MustCompile(pattern)
	includeGlobs, excludeGlobs := compileGlobs(o.Glob), compileGlobs(o.ExcludeGlob)
	excluded := func(rel string) bool {
		return len(excludeGlobs) > 0 && matchGlobs(excludeGlobs, strings.Split(rel, "/"))
	}
	selected := func(rel string) bool {
		if len(includeGlobs) == 0 && len(excludeGlobs) == 0 {
			return true
		}
		parts := strings.Split(rel, "/")
		return !matchGlobs(excludeGlobs, parts) && (len(includeGlobs) == 0 || matchGlobs(includeGlobs, parts))
	}
	add := func(m SearchMatch) {
		encoded, _ := json.Marshal(m)
		b := len(encoded) + 1
		if outputBytes+b > (32<<10)-1024 {
			stop("output_limit")
			return
		}
		out.Matches = append(out.Matches, m)
		outputBytes += b
		if len(out.Matches) >= o.MaxResults {
			stop("result_limit")
		}
	}
	var file func(string, string) error
	file = func(current, relative string) error {
		if !selected(relative) {
			return nil
		}
		if err := check(); err != nil {
			return err
		}
		if out.Truncated {
			return nil
		}
		if err := authorize(current); err != nil {
			return err
		}
		f, err := Open(root, current)
		if err != nil {
			out.Skipped["unreadable_or_link"]++
			out.Complete = false
			return nil
		}
		defer f.Close()
		version, err := Version(f)
		if err != nil {
			return err
		}
		info, err := f.Stat()
		if err != nil {
			return err
		}
		out.FilesScanned++
		if o.Mode == "files" && o.Pattern == "" {
			add(SearchMatch{Path: current, Version: version})
			return nil
		}
		if info.Size() > 2<<20 {
			out.Skipped["large_file"]++
			out.Complete = false
			return nil
		}
		if int64(out.BytesScanned)+info.Size() > 64<<20 {
			stop("byte_limit")
			return nil
		}
		data, err := io.ReadAll(io.LimitReader(f, (2<<20)+1))
		if err != nil {
			out.Skipped["read_error"]++
			out.Complete = false
			return nil
		}
		out.BytesScanned += len(data)
		after, err := Version(f)
		if err != nil {
			return err
		}
		if version != after {
			out.Skipped["changed_file"]++
			out.Complete = false
			return nil
		}
		if len(data) > 2<<20 {
			out.Skipped["large_file"]++
			out.Complete = false
			return nil
		}
		if !utf8.Valid(data) || bytes.IndexByte(data, 0) >= 0 {
			out.Skipped["binary"]++
			return nil
		}
		lines := strings.Split(string(data), "\n")
		if len(lines) > 0 && lines[len(lines)-1] == "" {
			lines = lines[:len(lines)-1]
		}
		for i, line := range lines {
			line = strings.TrimSuffix(line, "\r")
			if err := check(); err != nil {
				return err
			}
			if out.Truncated {
				return nil
			}
			if len(line) > 4096 {
				out.Skipped["long_line"]++
				out.Complete = false
				continue
			}
			if !re.MatchString(line) {
				continue
			}
			if o.Mode == "files" {
				add(SearchMatch{Path: current, Version: version})
				return nil
			}
			m := SearchMatch{Path: current, Line: i + 1, Text: strings.TrimSuffix(line, "\r"), Version: version}
			for n := max(0, i-o.ContextLines); n < i; n++ {
				if len(lines[n]) <= 4096 {
					m.Before = append(m.Before, SearchLine{n + 1, strings.TrimSuffix(lines[n], "\r")})
				}
			}
			for n := i + 1; n < min(len(lines), i+1+o.ContextLines); n++ {
				if len(lines[n]) <= 4096 {
					m.After = append(m.After, SearchLine{n + 1, strings.TrimSuffix(lines[n], "\r")})
				}
			}
			add(m)
			if out.Truncated {
				return nil
			}
		}
		return nil
	}
	var walk func(string, string, int) error
	walk = func(current, relative string, depth int) error {
		if err := check(); err != nil {
			return err
		}
		if out.Truncated {
			return nil
		}
		if depth > 32 {
			stop("depth_limit")
			return nil
		}
		if err := authorize(current); err != nil {
			return err
		}
		dir, err := OpenDirectory(root, current)
		if err != nil {
			return err
		}
		before, err := Version(dir)
		if err != nil {
			dir.Close()
			return err
		}
		entries, err := dir.ReadDir(10001 - out.EntriesScanned)
		dir.Close()
		if err != nil && err != io.EOF {
			return err
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
		for _, entry := range entries {
			out.EntriesScanned++
			if out.EntriesScanned > 10000 {
				stop("entry_limit")
				break
			}
			if !o.IncludeHidden && strings.HasPrefix(entry.Name(), ".") {
				out.Skipped["hidden"]++
				continue
			}
			child, rel := path.Join(current, entry.Name()), path.Join(relative, entry.Name())
			if excluded(rel) {
				continue
			}
			if entry.IsDir() {
				err = walk(child, rel, depth+1)
			} else if entry.Type().IsRegular() {
				err = file(child, rel)
			} else {
				out.Skipped["link_or_special"]++
			}
			if err != nil {
				return err
			}
			if out.Truncated {
				break
			}
		}
		afterDir, e := OpenDirectory(root, current)
		if e != nil {
			out.Complete = false
			out.Skipped["changed_directory"]++
		} else {
			after, e := Version(afterDir)
			afterDir.Close()
			if e != nil || after != before {
				out.Complete = false
				out.Skipped["changed_directory"]++
			}
		}
		return nil
	}
	if err := authorize(name); err != nil {
		return out, err
	}
	if f, err := Open(root, name); err == nil {
		out.TargetKind = "file"
		f.Close()
		err = file(name, path.Base(name))
		return out, err
	}
	out.TargetKind = "directory"
	err := walk(name, "", 0)
	return out, err
}
