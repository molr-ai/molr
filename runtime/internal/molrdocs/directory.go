package molrdocs

import (
	"io"
	"io/fs"
	"path"
	"time"
)

type ancestor struct {
	name, child string
	done        bool
}

func (d *ancestor) Stat() (fs.FileInfo, error) { return dirInfo{path.Base(d.name)}, nil }
func (d *ancestor) Close() error               { return nil }
func (d *ancestor) Read([]byte) (int, error) {
	return 0, &fs.PathError{Op: "read", Path: d.name, Err: fs.ErrInvalid}
}
func (d *ancestor) ReadDir(n int) ([]fs.DirEntry, error) {
	if d.done {
		if n > 0 {
			return nil, io.EOF
		}
		return []fs.DirEntry{}, nil
	}
	d.done = true
	return []fs.DirEntry{fs.FileInfoToDirEntry(dirInfo{d.child})}, nil
}

type dirInfo struct{ name string }

func (d dirInfo) Name() string     { return d.name }
func (dirInfo) Size() int64        { return 0 }
func (dirInfo) Mode() fs.FileMode  { return fs.ModeDir | 0555 }
func (dirInfo) ModTime() time.Time { return time.Time{} }
func (dirInfo) IsDir() bool        { return true }
func (dirInfo) Sys() any           { return nil }
