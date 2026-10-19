// Package instancebackup stores portable, offline instance archives. Service and
// installation lifecycle decisions belong to the platform installer.
package instancebackup

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"molr.ai/runtime/internal/installrecord"
	"molr.ai/runtime/internal/platform"
	"molr.ai/runtime/internal/storage/sqlite"
)

const formatVersion = 1
const maxManifest = 32 << 20

type Entry struct {
	Path   string `json:"path"`
	Kind   string `json:"kind"`
	Mode   uint32 `json:"mode"`
	Size   int64  `json:"size,omitempty"`
	SHA256 string `json:"sha256,omitempty"`
	Link   string `json:"link,omitempty"`
}
type Manifest struct {
	Schema     sqlite.Schema       `json:"database_schema"`
	Format     int                 `json:"format"`
	CreatedAt  time.Time           `json:"created_at"`
	Mode       string              `json:"mode"`
	Build      installrecord.Build `json:"build"`
	DataDir    string              `json:"data_dir"`
	WorkRoot   string              `json:"work_root,omitempty"`
	ConfigDir  string              `json:"config_dir"`
	Config     map[string]any      `json:"config"`
	References map[string]string   `json:"references"`
	Excluded   []string            `json:"excluded_directories"`
	Files      []Entry             `json:"files"`
}

// Add copies a tree into an empty private staging area without following links.
// filter selects relative names; excluding a directory excludes its descendants.
func Add(ctx context.Context, stage, source, name string, filter func(string) bool) error {
	if !safeName(name) {
		return fmt.Errorf("invalid archive root %q", name)
	}
	rootInfo, err := os.Lstat(source)
	if err != nil {
		return err
	}
	if rootInfo.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("backup root must not be a symlink: %s", source)
	}
	directories := map[string]os.FileMode{}
	err = filepath.WalkDir(source, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		rel, err := filepath.Rel(source, p)
		if err != nil {
			return err
		}
		if filter != nil && !filter(filepath.ToSlash(rel)) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		target := filepath.Join(stage, filepath.FromSlash(name), rel)
		info, err := d.Info()
		if err != nil {
			return err
		}
		if d.IsDir() {
			directories[target] = info.Mode().Perm()
			return os.MkdirAll(target, 0700)
		}
		if err = os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			link, err := os.Readlink(p)
			if err != nil {
				return err
			}
			return os.Symlink(link, target)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("cannot back up special file: %s", p)
		}
		in, err := os.Open(p)
		if err != nil {
			return err
		}
		defer in.Close()
		actual, err := in.Stat()
		if err != nil {
			return err
		}
		if !os.SameFile(info, actual) {
			return fmt.Errorf("backup source changed: %s", p)
		}
		out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return err
		}
		_, err = io.Copy(out, contextReader{ctx, in})
		if err == nil {
			err = out.Chmod(info.Mode().Perm())
		}
		if err == nil {
			err = out.Sync()
		}
		closeErr := out.Close()
		if err != nil {
			return err
		}
		return closeErr
	})
	if err != nil {
		return err
	}
	paths := make([]string, 0, len(directories))
	for p := range directories {
		paths = append(paths, p)
	}
	sort.Slice(paths, func(i, j int) bool { return len(paths[i]) > len(paths[j]) })
	for _, target := range paths {
		mode := directories[target]
		if err = os.Chmod(target, mode); err != nil {
			return err
		}
	}
	return nil
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(p)
}
func safeName(s string) bool {
	return s != "" && s != "." && path.Clean(s) == s && !strings.ContainsAny(s, "\\:\x00") && !strings.HasPrefix(s, "/") && s != ".." && !strings.HasPrefix(s, "../") && filepath.IsLocal(filepath.FromSlash(s))
}

// Seal inventories the staged files, including empty directories and links.
func Seal(ctx context.Context, stage string, m *Manifest) error {
	m.Format = formatVersion
	m.CreatedAt = time.Now().UTC()
	m.Files = nil
	err := filepath.WalkDir(stage, func(p string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if p == stage {
			return nil
		}
		rel, _ := filepath.Rel(stage, p)
		rel = filepath.ToSlash(rel)
		if rel == "backup.json" {
			return fmt.Errorf("staging already sealed")
		}
		if !safeName(rel) {
			return fmt.Errorf("nonportable file name: %s", rel)
		}
		info, e := d.Info()
		if e != nil {
			return e
		}
		entry := Entry{Path: rel, Mode: uint32(info.Mode().Perm())}
		switch {
		case d.IsDir():
			entry.Kind = "directory"
		case info.Mode()&os.ModeSymlink != 0:
			entry.Kind = "symlink"
			entry.Link, e = os.Readlink(p)
		case info.Mode().IsRegular():
			entry.Kind = "file"
			entry.Size = info.Size()
			f, err := os.Open(p)
			if err != nil {
				return err
			}
			h := sha256.New()
			_, e = io.Copy(h, contextReader{ctx, f})
			f.Close()
			entry.SHA256 = hex.EncodeToString(h.Sum(nil))
		default:
			return fmt.Errorf("unsupported backup file %s", rel)
		}
		if e != nil {
			return e
		}
		m.Files = append(m.Files, entry)
		return nil
	})
	if err != nil {
		return err
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	if len(b) > maxManifest {
		return fmt.Errorf("backup manifest too large")
	}
	return os.WriteFile(filepath.Join(stage, "backup.json"), b, 0600)
}

// Pack publishes a complete archive without overwriting an existing destination.
func Pack(ctx context.Context, stage, output string, m *Manifest) (resultErr error) {
	privateDir, err := os.MkdirTemp(filepath.Dir(output), ".molr-backup-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(privateDir)
	if err = platform.PrivateDirectory(privateDir); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(privateDir, "archive"), os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	entries := append([]Entry{{Path: "backup.json", Kind: "file", Mode: 0600}}, m.Files...)
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		p := filepath.Join(stage, filepath.FromSlash(entry.Path))
		info, err := os.Lstat(p)
		if err != nil {
			return err
		}
		h, err := tar.FileInfoHeader(info, entry.Link)
		if err != nil {
			return err
		}
		h.Name = entry.Path
		h.Uid = 0
		h.Gid = 0
		h.Uname = ""
		h.Gname = ""
		if err = tw.WriteHeader(h); err != nil {
			return err
		}
		if info.Mode().IsRegular() {
			in, err := os.Open(p)
			if err != nil {
				return err
			}
			_, err = io.Copy(tw, contextReader{ctx, in})
			in.Close()
			if err != nil {
				return err
			}
		}
	}
	if err = tw.Close(); err != nil {
		return err
	}
	if err = gz.Close(); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	// Link provides exclusive publication on the destination filesystem.
	if err = os.Link(f.Name(), output); err != nil {
		return err
	}
	return nil
}

// Unpack validates every declared file before returning a private staging tree.
// Links are created last, so archive content can never write through a link.
func Unpack(ctx context.Context, archive, parent string) (stage string, m *Manifest, resultErr error) {
	f, err := os.Open(archive)
	if err != nil {
		return "", nil, err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return "", nil, err
	}
	defer gz.Close()
	tr := tar.NewReader(contextReader{ctx, gz})
	h, err := tr.Next()
	if err != nil {
		return "", nil, err
	}
	if h.Name != "backup.json" || h.Typeflag != tar.TypeReg || h.Size > maxManifest || h.Size < 1 {
		return "", nil, fmt.Errorf("backup.json must be first")
	}
	m = &Manifest{}
	decoder := json.NewDecoder(io.LimitReader(tr, maxManifest+1))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(m); err != nil {
		return "", nil, err
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return "", nil, fmt.Errorf("trailing manifest content")
	}
	if m.Format != formatVersion || (m.Mode != "server" && m.Mode != "sandbox") || m.Build.Version == "" || m.Build.Commit == "" || m.Config == nil {
		return "", nil, fmt.Errorf("unsupported or incomplete backup")
	}
	expected := map[string]Entry{}
	folded := map[string]string{}
	for _, e := range m.Files {
		if !safeName(e.Path) || e.Path == "backup.json" || e.Size < 0 || e.Mode > 0777 {
			return "", nil, fmt.Errorf("invalid backup entry %q", e.Path)
		}
		if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
			key := strings.ToLower(e.Path)
			if old, ok := folded[key]; ok && old != e.Path {
				return "", nil, fmt.Errorf("case-insensitive path collision: %s / %s", old, e.Path)
			}
			folded[key] = e.Path
		}
		first, _, _ := strings.Cut(e.Path, "/")
		if first != "data" && first != "config" && first != "program" && first != "work" {
			return "", nil, fmt.Errorf("unknown backup section")
		}
		if _, ok := expected[e.Path]; ok {
			return "", nil, fmt.Errorf("duplicate backup path")
		}
		if e.Kind != "file" && e.Kind != "directory" && e.Kind != "symlink" {
			return "", nil, fmt.Errorf("unsupported backup file type")
		}
		if e.Kind == "file" {
			hash, err := hex.DecodeString(e.SHA256)
			if err != nil || len(hash) != sha256.Size {
				return "", nil, fmt.Errorf("invalid checksum")
			}
		}
		if e.Kind == "symlink" && !strings.HasPrefix(e.Path, "work/") && !strings.HasPrefix(e.Path, "data/sandbox/work/") && !strings.HasPrefix(e.Path, "data/users/") {
			return "", nil, fmt.Errorf("symlinks are allowed only in managed user files: %s", e.Path)
		}
		expected[e.Path] = e
	}
	for key, name := range m.References {
		if key != "http.cert_file" && key != "http.key_file" && key != "pricing.rules_file" && key != "pricing.exchange_rates_file" {
			return "", nil, fmt.Errorf("unknown configuration reference")
		}
		if name != "config/references/"+strings.ReplaceAll(key, ".", "-") || expected[name].Kind != "file" {
			return "", nil, fmt.Errorf("invalid configuration reference")
		}
	}
	for name := range expected {
		for p := path.Dir(name); p != "."; p = path.Dir(p) {
			e, ok := expected[p]
			if !ok || e.Kind != "directory" {
				return "", nil, fmt.Errorf("file ancestor is not a declared directory: %s", p)
			}
		}
	}
	stage, err = os.MkdirTemp(parent, ".molr-restore-*")
	if err != nil {
		return "", nil, err
	}
	defer func() {
		if resultErr != nil {
			os.RemoveAll(stage)
		}
	}()
	if err = platform.PrivateDirectory(stage); err != nil {
		return stage, nil, err
	}
	seen := map[string]bool{}
	for {
		h, err = tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return stage, nil, err
		}
		e, ok := expected[h.Name]
		if !ok || seen[h.Name] {
			return stage, nil, fmt.Errorf("unexpected/duplicate archive entry %q", h.Name)
		}
		seen[h.Name] = true
		p := filepath.Join(stage, filepath.FromSlash(e.Path))
		if err = os.MkdirAll(filepath.Dir(p), 0700); err != nil {
			return stage, nil, err
		}
		switch e.Kind {
		case "directory":
			if h.Typeflag != tar.TypeDir {
				return stage, nil, fmt.Errorf("entry type mismatch")
			}
			err = os.MkdirAll(p, 0700)
		case "symlink":
			if h.Typeflag != tar.TypeSymlink || h.Linkname != e.Link {
				return stage, nil, fmt.Errorf("link mismatch")
			}
		case "file":
			if h.Typeflag != tar.TypeReg || h.Size != e.Size {
				return stage, nil, fmt.Errorf("entry size/type mismatch")
			}
			out, x := os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
			if x != nil {
				return stage, nil, x
			}
			hash := sha256.New()
			_, err = io.Copy(io.MultiWriter(out, hash), tr)
			if err == nil {
				err = out.Sync()
			}
			closeErr := out.Close()
			if err == nil {
				err = closeErr
			}
			if err == nil && hex.EncodeToString(hash.Sum(nil)) != e.SHA256 {
				err = fmt.Errorf("checksum mismatch: %s", e.Path)
			}
		}
		if err != nil {
			return stage, nil, err
		}
	}
	if len(seen) != len(expected) {
		return stage, nil, fmt.Errorf("incomplete archive")
	}
	// Consume the gzip trailer to verify CRC and reject extra tar payload.
	tail, err := io.Copy(io.Discard, gz)
	if err != nil {
		return stage, nil, err
	}
	if tail != 0 {
		return stage, nil, fmt.Errorf("trailing archive payload")
	}
	for _, e := range m.Files {
		p := filepath.Join(stage, filepath.FromSlash(e.Path))
		if e.Kind == "symlink" {
			if err = os.Symlink(e.Link, p); err != nil {
				return stage, nil, fmt.Errorf("cannot restore symlink %s: %w", e.Path, err)
			}
		} else if e.Kind == "file" {
			if err = os.Chmod(p, os.FileMode(e.Mode)); err != nil {
				return stage, nil, err
			}
		}
	}
	dirs := []Entry{}
	for _, e := range m.Files {
		if e.Kind == "directory" {
			dirs = append(dirs, e)
		}
	}
	sort.Slice(dirs, func(i, j int) bool { return len(dirs[i].Path) > len(dirs[j].Path) })
	for _, e := range dirs {
		if err = os.Chmod(filepath.Join(stage, filepath.FromSlash(e.Path)), os.FileMode(e.Mode)); err != nil {
			return stage, nil, err
		}
	}
	return stage, m, nil
}
