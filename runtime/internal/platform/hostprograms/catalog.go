// Package hostprograms discovers launch entries without executing host programs.
package hostprograms

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"molr.ai/runtime/internal/core"
)

type Program struct {
	Name, Command, Category          string
	Executable, ResolvedPath, Source string
	Available                        bool
	Reason                           string
}
type Request struct {
	Command string
	Refresh bool
}
type Snapshot struct {
	Programs  []Program
	CheckedAt time.Time
}
type description struct{ command, name, category string }

var known = []description{
	{"codex", "Codex", "agent"}, {"claude", "Claude Code", "agent"},
	{"opencode", "OpenCode", "agent"}, {"kimi", "Kimi Code", "agent"},
	{"docker", "Docker", "containers"}, {"podman", "Podman", "containers"},
	{"kubectl", "kubectl", "operations"}, {"helm", "Helm", "operations"},
	{"terraform", "Terraform", "operations"}, {"aws", "AWS CLI", "operations"},
	{"az", "Azure CLI", "operations"}, {"gcloud", "Google Cloud CLI", "operations"},
	{"adb", "Android Debug Bridge", "system"}, {"xcodebuild", "Xcode Build", "system"},
}
var commandName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.+-]{0,79}$`)

// Describe uses only an already-authorized path; it never scans another machine.
func Describe(executable string) (name, category string) {
	base := strings.ToLower(filepath.Base(strings.ReplaceAll(executable, `\`, "/")))
	for _, ext := range []string{".exe", ".cmd", ".ps1", ".bat"} {
		base = strings.TrimSuffix(base, ext)
	}
	for _, d := range known {
		if base == d.command {
			return d.name, d.category
		}
	}
	return base, "other"
}

type Catalog struct {
	mu          sync.Mutex
	excluded    []string
	cache       map[string]Snapshot
	environment string
	// Injectable filesystem scope keeps tests independent of the real host.
	directories func() []searchDirectory
}
type searchDirectory struct{ path, source string }

func New(excluded []string) *Catalog {
	return &Catalog{excluded: slices.Clone(excluded), cache: map[string]Snapshot{}, directories: systemDirectories}
}

// List is demand-driven. The cache expires lazily, with no background timer or
// subprocess. Concurrent refreshes that overlap share the completed scan.
func (c *Catalog) List(ctx context.Context, q Request) (Snapshot, error) {
	if q.Command != "" && !commandName.MatchString(q.Command) {
		return Snapshot{}, core.Invalid("command", "a command name, without a path, is required")
	}
	started := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	environment := os.Getenv("PATH") + "\x00" + os.Getenv("HOME") + "\x00" + os.Getenv("USERPROFILE")
	if environment != c.environment {
		c.cache = map[string]Snapshot{}
		c.environment = environment
	}
	if old, ok := c.cache[q.Command]; ok && (old.CheckedAt.After(started) || !q.Refresh && time.Since(old.CheckedAt) < time.Minute) {
		old.Programs = slices.Clone(old.Programs)
		return old, nil
	}
	descriptions := known
	if q.Command != "" {
		name, category := Describe(q.Command)
		descriptions = []description{{q.Command, name, category}}
	}
	out := Snapshot{Programs: []Program{}}
	seen := map[string]bool{}
	installations := map[string]bool{}
	for _, directory := range c.directories() {
		if err := ctx.Err(); err != nil {
			return Snapshot{}, err
		}
		if time.Since(started) > 2*time.Second {
			return Snapshot{}, core.NewError(core.Unavailable, "program discovery deadline exceeded")
		}
		if !filepath.IsAbs(directory.path) {
			continue
		}
		root, err := filepath.EvalSymlinks(directory.path)
		if err != nil || c.blocked(root) {
			continue
		}
		for _, d := range descriptions {
			for _, file := range candidateNames(d.command) {
				entry := filepath.Join(directory.path, file)
				key := entry
				if runtime.GOOS == "windows" {
					key = strings.ToLower(key)
				}
				if seen[key] {
					continue
				}
				seen[key] = true
				actual, err := filepath.EvalSymlinks(entry)
				if err != nil || c.blocked(actual) {
					continue
				}
				installation := d.command + "\x00" + actual
				if runtime.GOOS == "windows" {
					installation = strings.ToLower(installation)
				}
				if installations[installation] {
					continue
				}
				info, err := os.Stat(actual)
				if err != nil || !info.Mode().IsRegular() {
					continue
				}
				installations[installation] = true
				p := Program{Name: d.name, Command: d.command, Category: d.category, Executable: entry, ResolvedPath: actual, Source: directory.source, Available: true}
				if runtime.GOOS == "windows" {
					if !strings.EqualFold(filepath.Ext(entry), ".exe") {
						p.Available = false
						p.Reason = "launcher_unsupported"
					}
				} else if info.Mode().Perm()&0111 == 0 {
					p.Available = false
					p.Reason = "not_executable"
				}
				out.Programs = append(out.Programs, p)
				if len(out.Programs) >= 128 {
					break
				}
			}
			if len(out.Programs) >= 128 {
				break
			}
		}
		if len(out.Programs) >= 128 {
			break
		}
	}
	out.CheckedAt = time.Now()
	if len(c.cache) >= 32 {
		c.cache = map[string]Snapshot{}
	}
	c.cache[q.Command] = out
	out.Programs = slices.Clone(out.Programs)
	return out, nil
}
func (c *Catalog) blocked(path string) bool {
	for _, root := range c.excluded {
		if root == "" {
			continue
		}
		if actual, err := filepath.EvalSymlinks(root); err == nil {
			root = actual
		}
		rel, err := filepath.Rel(root, path)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return true
		}
	}
	return false
}
func candidateNames(command string) []string {
	if runtime.GOOS != "windows" || filepath.Ext(command) != "" {
		return []string{command}
	}
	return []string{command + ".exe", command + ".cmd", command + ".ps1", command + ".bat"}
}
func systemDirectories() []searchDirectory {
	out := []searchDirectory{}
	seen := map[string]bool{}
	add := func(path, source string) {
		if !filepath.IsAbs(path) || len(out) >= 64 {
			return
		}
		path = filepath.Clean(path)
		key := path
		if runtime.GOOS == "windows" {
			key = strings.ToLower(key)
		}
		if !seen[key] {
			seen[key] = true
			out = append(out, searchDirectory{path, source})
		}
	}
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		add(dir, "path")
	}
	home, _ := os.UserHomeDir()
	if home != "" {
		for _, rel := range []string{".local/bin", "bin", ".npm-global/bin", ".npm/bin", ".bun/bin", ".opencode/bin", ".cargo/bin", "AppData/Roaming/npm", "scoop/shims"} {
			add(filepath.Join(home, filepath.FromSlash(rel)), "installation")
		}
		// One bounded level of Node version directories; never recurse over HOME.
		base := filepath.Join(home, ".nvm", "versions", "node")
		if f, err := os.Open(base); err == nil {
			entries, _ := f.ReadDir(32)
			_ = f.Close()
			for _, e := range entries {
				if e.IsDir() {
					add(filepath.Join(base, e.Name(), "bin"), "installation")
				}
			}
		}
	}
	if runtime.GOOS != "windows" {
		for _, dir := range []string{"/usr/local/bin", "/usr/bin", "/bin", "/opt/homebrew/bin", "/opt/local/bin"} {
			add(dir, "system")
		}
	}
	return out
}
