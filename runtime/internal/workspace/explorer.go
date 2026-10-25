package workspace

import (
	"bytes"
	"context"
	"io"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/textfiles"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

const textLimit = 1 << 20

func isText(data []byte) bool { return utf8.Valid(data) && !bytes.ContainsRune(data, 0) }

type Explorer struct {
	root      func(context.Context, core.Principal, core.ID, bool, int64) (string, error)
	maxUpload int64
}

func New(root func(context.Context, core.Principal, core.ID, bool, int64) (string, error), maxUpload int64) *Explorer {
	return &Explorer{root: root, maxUpload: maxUpload}
}
func workspacePath(name string) (string, error) {
	if name == "" {
		return ".", nil
	}
	if !utf8.ValidString(name) || !filepath.IsLocal(name) || path.Clean(name) != name || strings.ContainsAny(name, "\\\x00\r\n") || len(name) > 4096 {
		return "", core.Invalid("path", "canonical workspace-relative path required")
	}
	return name, nil
}
func workspaceStat(root, name string) (Entry, error) {
	f, err := textfiles.Open(root, name)
	if err != nil {
		f, err = textfiles.OpenDirectory(root, name)
	}
	if err != nil {
		return Entry{}, err
	}
	defer f.Close()
	return workspaceFileInfo(f, name)
}
func workspaceFileInfo(f *os.File, name string) (Entry, error) {
	st, err := f.Stat()
	if err != nil {
		return Entry{}, err
	}
	version, err := textfiles.Version(f)
	if err != nil {
		return Entry{}, err
	}
	e := Entry{Name: path.Base(name), Path: name, Size: st.Size(), ModifiedAt: st.ModTime(), Version: version, Type: "file"}
	if st.IsDir() {
		e.Type = "directory"
		e.Size = 0
	}
	return e, nil
}
func (s *Explorer) Query(ctx context.Context, p core.Principal, id core.ID, q Request) (any, error) {
	name, err := workspacePath(q.Path)
	if err != nil {
		return nil, err
	}
	write := q.Action != "list" && q.Action != "stat" && q.Action != "read"
	root, err := s.root(ctx, p, id, write, int64(len(q.Text)))
	if err != nil {
		return nil, err
	}
	check := func() error { _, err := s.root(ctx, p, id, write, int64(len(q.Text))); return err }
	switch q.Action {
	case "list":
		if q.Limit == 0 {
			q.Limit = 100
		}
		if q.Offset < 0 || q.Offset > 100000 || q.Limit < 1 || q.Limit > 200 || (q.Offset > 0 && q.ExpectedVersion == "") {
			return nil, core.Invalid("page", "bounded page and version required")
		}
		dir, err := textfiles.OpenDirectory(root, name)
		if err != nil {
			return nil, err
		}
		defer dir.Close()
		version, err := textfiles.Version(dir)
		if err != nil {
			return nil, err
		}
		if q.ExpectedVersion != "" && version != q.ExpectedVersion {
			return nil, textfiles.Changed()
		}
		out := Listing{Entries: []Entry{}, Version: version}
		position := 0
		for position < q.Offset+q.Limit+1 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			entries, err := dir.ReadDir(min(128, q.Offset+q.Limit+1-position))
			for _, entry := range entries {
				if position >= q.Offset {
					if len(out.Entries) == q.Limit {
						out.Truncated = true
						break
					}
					child := path.Join(name, entry.Name())
					e, statErr := workspaceStat(root, child)
					if statErr != nil {
						e = Entry{Name: entry.Name(), Path: child, Type: "unavailable"}
					}
					out.Entries = append(out.Entries, e)
				}
				position++
			}
			if out.Truncated {
				out.NextOffset = q.Offset + len(out.Entries)
				break
			}
			if err == io.EOF {
				break
			}
			if err != nil {
				return nil, err
			}
		}
		current, err := textfiles.Version(dir)
		if err != nil {
			return nil, err
		}
		if current != version {
			return nil, textfiles.Changed()
		}
		if err = check(); err != nil {
			return nil, err
		}
		return out, nil
	case "stat":
		return workspaceStat(root, name)
	case "read":
		f, err := textfiles.Open(root, name)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		e, err := workspaceFileInfo(f, name)
		if err != nil {
			return nil, err
		}
		if q.ExpectedVersion != "" && q.ExpectedVersion != e.Version {
			return nil, textfiles.Changed()
		}
		data, err := io.ReadAll(io.LimitReader(f, textLimit+1))
		if err != nil {
			return nil, err
		}
		e.MediaType = http.DetectContentType(data)
		truncated := len(data) > textLimit
		if truncated {
			data = data[:textLimit]
			for len(data) > 0 && !utf8.Valid(data) && len(data) > textLimit-4 {
				data = data[:len(data)-1]
			}
		}
		out := Text{Entry: e, Truncated: truncated}
		if utf8.Valid(data) && !bytes.ContainsRune(data, 0) && (strings.HasPrefix(e.MediaType, "text/") || e.MediaType == "application/octet-stream") {
			out.Text = string(data)
			out.Editable = !truncated
			e.MediaType = "text/plain; charset=utf-8"
			out.MediaType = e.MediaType
		}
		version, err := textfiles.Version(f)
		if err != nil {
			return nil, err
		}
		if version != e.Version {
			return nil, textfiles.Changed()
		}
		if err = check(); err != nil {
			return nil, err
		}
		return out, nil
	case "write":
		if len(q.Text) > textLimit || !isText([]byte(q.Text)) {
			return nil, core.Invalid("text", "UTF-8 text up to 1 MiB required")
		}
		version, err := textfiles.Replace(ctx, root, name, q.Text, q.ExpectedVersion != "", q.ExpectedVersion, check)
		if err != nil {
			return nil, err
		}
		return Entry{Path: name, Name: path.Base(name), Type: "file", Version: version, Size: int64(len(q.Text))}, nil
	case "mkdir":
		if name == "." {
			return nil, core.Invalid("path", "new directory required")
		}
		if err = textfiles.MakeDirectory(ctx, root, name, check); err != nil {
			return nil, err
		}
		return workspaceStat(root, name)
	case "move", "delete":
		if name == "." || q.ExpectedVersion == "" {
			return nil, core.Invalid("path", "entry and version required")
		}
		e, err := workspaceStat(root, name)
		if err != nil {
			return nil, err
		}
		if e.Version != q.ExpectedVersion {
			return nil, textfiles.Changed()
		}
		if q.Action == "move" {
			to, err := workspacePath(q.ToPath)
			if err != nil {
				return nil, err
			}
			if to == "." || to == name || strings.HasPrefix(to, name+"/") {
				return nil, core.Invalid("to_path", "distinct destination outside source required")
			}
			version, err := textfiles.Move(ctx, root, name, to, q.ExpectedVersion, e.Type == "directory", check)
			if err != nil {
				return nil, err
			}
			e.Path = to
			e.Name = path.Base(to)
			e.Version = version
			return e, nil
		}
		if e.Type == "directory" {
			err = textfiles.RemoveDirectory(ctx, root, name, q.ExpectedVersion, check)
		} else {
			err = textfiles.Remove(ctx, root, name, q.ExpectedVersion, check)
		}
		return nil, err
	default:
		return nil, core.Invalid("action", "unknown workspace action")
	}
}
func (s *Explorer) Upload(ctx context.Context, p core.Principal, id core.ID, q Request, body io.Reader) (Entry, error) {
	name, err := workspacePath(q.Path)
	if err != nil {
		return Entry{}, err
	}
	root, err := s.root(ctx, p, id, true, 0)
	if err != nil {
		return Entry{}, err
	}
	limit := min(s.maxUpload, 64<<20)
	data, err := io.ReadAll(io.LimitReader(body, limit+1))
	if err != nil {
		return Entry{}, core.Invalid("body", "upload incomplete")
	}
	if int64(len(data)) > limit {
		return Entry{}, core.Invalid("body", "upload too large")
	}
	check := func() error { _, err := s.root(ctx, p, id, true, int64(len(data))); return err }
	if err = check(); err != nil {
		return Entry{}, err
	}
	version, err := textfiles.Replace(ctx, root, name, string(data), q.ExpectedVersion != "", q.ExpectedVersion, check)
	return Entry{Path: name, Name: path.Base(name), Type: "file", Size: int64(len(data)), Version: version}, err
}
func (s *Explorer) Download(ctx context.Context, p core.Principal, id core.ID, q Request) (Content, error) {
	name, err := workspacePath(q.Path)
	if err != nil {
		return Content{}, err
	}
	root, err := s.root(ctx, p, id, false, 0)
	if err != nil {
		return Content{}, err
	}
	f, err := textfiles.Open(root, name)
	if err != nil {
		return Content{}, err
	}
	e, err := workspaceFileInfo(f, name)
	if err != nil {
		f.Close()
		return Content{}, err
	}
	if q.ExpectedVersion != "" && q.ExpectedVersion != e.Version {
		f.Close()
		return Content{}, textfiles.Changed()
	}
	return Content{ReadSeekCloser: f, Entry: e}, nil
}
