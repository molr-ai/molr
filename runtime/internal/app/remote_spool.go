package app

import (
	"io"
	"molr.ai/runtime/internal/core"
	sm "molr.ai/runtime/internal/sandboxmanager"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// A bounded recovery spool, not user storage. Receipts survive expired logs so
// missing output must never cause a command to be executed again.
func (b *linuxSandboxBackend) prepareOperationSpool(i sm.Identity) (string, error) {
	b.spoolMu.Lock()
	defer b.spoolMu.Unlock()
	root := filepath.Join(b.cfg.DataDir, "operations")
	if err := os.MkdirAll(root, 0700); err != nil {
		return "", err
	}
	active := map[string]bool{}
	b.shellOutputs.Range(func(_, v any) bool {
		active[filepath.Base(b.operationRoot(v.(shellRemoteOutput).Identity))] = true
		return true
	})
	dir, err := openResourceDirectory(root, ".")
	if err != nil {
		return "", err
	}
	entries, err := dir.ReadDir(129)
	dir.Close()
	if err != nil && err != io.EOF {
		return "", err
	}
	type oldSpool struct {
		name string
		at   time.Time
	}
	var completed []oldSpool
	retained := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		child, err := openResourceDirectory(root, entry.Name())
		if err != nil {
			return "", err
		}
		st, err := child.Stat()
		child.Close()
		if err != nil {
			return "", err
		}
		if !active[entry.Name()] && time.Since(st.ModTime()) > 7*24*time.Hour {
			if err = os.RemoveAll(filepath.Join(root, entry.Name())); err != nil {
				return "", err
			}
		} else {
			retained++
			if !active[entry.Name()] {
				completed = append(completed, oldSpool{entry.Name(), st.ModTime()})
			}
		}
	}
	sort.Slice(completed, func(i, j int) bool { return completed[i].at.Before(completed[j].at) })
	for _, old := range completed {
		if retained < 128 {
			break
		}
		if err = os.RemoveAll(filepath.Join(root, old.name)); err != nil {
			return "", err
		}
		retained--
	}
	if retained >= 128 {
		return "", core.NewError(core.RateLimited, "remote operation spool full; collect or expire completed output before starting more commands")
	}
	path := b.operationRoot(i)
	if err = os.Mkdir(path, 0700); err != nil {
		return "", err
	}
	return path, nil
}
