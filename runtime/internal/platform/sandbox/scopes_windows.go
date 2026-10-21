package sandbox

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/platform"
)

// Reuse two file capabilities while a directory has live users. Joining an
// existing lease never reapplies inheritable ACLs underneath running code.
// Different/nested roots cannot be mutated concurrently: shared ancestor file
// locks and an exclusive root lock enforce the rule across Runtime processes.
var windowsScopes = struct {
	sync.Mutex
	runners map[*Runner]map[string]*windowsScopeLease
}{runners: make(map[*Runner]map[string]*windowsScopeLease)}

type windowsScopeRecord struct {
	Scope             containerScope
	ReadSID, WriteSID string
}
type windowsScopeLease struct {
	record      windowsScopeRecord
	read, write *windows.SID
	pins, locks []windows.Handle
	journal     string
	users       int
}

func randomContainerName() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "molr-" + hex.EncodeToString(b), nil
}
func fileCapability(name string) (*windows.SID, error) {
	proc := windows.NewLazySystemDLL("kernelbase.dll").NewProc("DeriveCapabilitySidsFromName")
	if err := proc.Find(); err != nil {
		return nil, err
	}
	n, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return nil, err
	}
	var groups, caps **windows.SID
	var ng, nc uint32
	ok, _, err := proc.Call(uintptr(unsafe.Pointer(n)), uintptr(unsafe.Pointer(&groups)), uintptr(unsafe.Pointer(&ng)), uintptr(unsafe.Pointer(&caps)), uintptr(unsafe.Pointer(&nc)))
	free := func(p **windows.SID, count uint32) {
		if p != nil {
			for _, sid := range unsafe.Slice(p, int(count)) {
				windows.LocalFree(windows.Handle(unsafe.Pointer(sid)))
			}
			windows.LocalFree(windows.Handle(unsafe.Pointer(p)))
		}
	}
	defer free(groups, ng)
	defer free(caps, nc)
	if ok == 0 {
		return nil, err
	}
	if nc != 1 {
		return nil, fmt.Errorf("unexpected file capability count")
	}
	return (*caps).Copy()
}
func closeScopeLocks(handles []windows.Handle) {
	for i := len(handles) - 1; i >= 0; i-- {
		windows.CloseHandle(handles[i])
	}
}
func lockScope(path string) ([]windows.Handle, error) {
	base := filepath.Join(os.Getenv("LOCALAPPDATA"), "Molr", "sandbox-locks")
	if !filepath.IsAbs(base) {
		return nil, unavailable("sandbox_private_directory_required")
	}
	if err := os.MkdirAll(base, 0700); err != nil {
		return nil, err
	}
	if _, err := windowsScope(base); err != nil {
		return nil, err
	}
	if err := platform.PrivateDirectory(base); err != nil {
		return nil, err
	}
	var paths []string
	for current := filepath.Clean(path); ; current = filepath.Dir(current) {
		paths = append(paths, current)
		if filepath.Dir(current) == current {
			break
		}
	}
	var handles []windows.Handle
	for i := len(paths) - 1; i >= 0; i-- {
		digest := sha256.Sum256([]byte(strings.ToLower(paths[i])))
		name, _ := windows.UTF16PtrFromString(filepath.Join(base, hex.EncodeToString(digest[:])+".lock"))
		h, err := windows.CreateFile(name, windows.GENERIC_READ|windows.GENERIC_WRITE, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_ALWAYS, windows.FILE_ATTRIBUTE_NORMAL|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
		if err != nil {
			closeScopeLocks(handles)
			return nil, err
		}
		var info windows.ByHandleFileInformation
		err = windows.GetFileInformationByHandle(h, &info)
		if err == nil && (info.FileAttributes&(windows.FILE_ATTRIBUTE_DIRECTORY|windows.FILE_ATTRIBUTE_REPARSE_POINT) != 0 || info.NumberOfLinks != 1) {
			err = core.Invalid("scope", "invalid scope lock file")
		}
		flags := uint32(windows.LOCKFILE_FAIL_IMMEDIATELY)
		if i == 0 {
			flags |= windows.LOCKFILE_EXCLUSIVE_LOCK
		}
		if err == nil {
			err = windows.LockFileEx(h, flags, 0, 1, 0, &windows.Overlapped{})
		}
		if err != nil {
			windows.CloseHandle(h)
			closeScopeLocks(handles)
			if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
				return nil, unavailable("sandbox_file_scope_busy")
			}
			return nil, err
		}
		handles = append(handles, h)
	}
	return handles, nil
}
func pinScopeTree(ctx context.Context, path string) ([]windows.Handle, []windows.Handle, string, error) {
	pins, id, err := pinScope(path)
	if err != nil {
		return nil, nil, "", err
	}
	var children []windows.Handle
	err = filepath.WalkDir(path, func(p string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if p == path {
			return nil
		}
		if len(children) >= 100000 {
			return core.Invalid("path", "AppContainer scope exceeds 100000 entries")
		}
		name, _ := windows.UTF16PtrFromString(p)
		h, err := windows.CreateFile(name, windows.FILE_READ_ATTRIBUTES|windows.FILE_READ_DATA, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
		if err != nil {
			return err
		}
		children = append(children, h)
		var info windows.ByHandleFileInformation
		if err = windows.GetFileInformationByHandle(h, &info); err != nil {
			return err
		}
		if info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 || info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY == 0 && info.NumberOfLinks != 1 {
			return core.Invalid("path", "AppContainer scopes cannot contain reparse points or hard links")
		}
		return nil
	})
	if err != nil {
		closeScopeLocks(children)
		closeScopeLocks(pins)
		return nil, nil, "", err
	}
	return pins, children, id, nil
}
func (r *Runner) acquireScope(ctx context.Context, path string) (*windowsScopeLease, error) {
	windowsScopes.Lock()
	defer windowsScopes.Unlock()
	path, err := windowsScope(path)
	if err != nil {
		return nil, err
	}
	key := strings.ToLower(path)
	cache := windowsScopes.runners[r]
	if cache == nil {
		cache = make(map[string]*windowsScopeLease)
	}
	if lease := cache[key]; lease != nil {
		lease.users++
		return lease, nil
	}
	locks, err := lockScope(path)
	if err != nil {
		return nil, err
	}
	pins, children, id, err := pinScopeTree(ctx, path)
	if err != nil {
		closeScopeLocks(locks)
		return nil, err
	}
	defer closeScopeLocks(children)
	success := false
	defer func() {
		if !success {
			closeScopeLocks(pins)
			closeScopeLocks(locks)
		}
	}()
	name, err := randomContainerName()
	if err != nil {
		return nil, err
	}
	read, err := fileCapability(name + "-read")
	if err != nil {
		return nil, err
	}
	write, err := fileCapability(name + "-write")
	if err != nil {
		return nil, err
	}
	lease := &windowsScopeLease{record: windowsScopeRecord{Scope: containerScope{Path: path, FileID: id}, ReadSID: read.String(), WriteSID: write.String()}, read: read, write: write, pins: pins, locks: locks, journal: filepath.Join(r.containerDirectory(), "scope-"+name+".json"), users: 1}
	raw, _ := json.Marshal(lease.record)
	if err = os.WriteFile(lease.journal, raw, 0600); err != nil {
		return nil, err
	}
	root := pins[len(pins)-1]
	if err = updateContainerACL(root, read, false, false); err == nil {
		err = updateContainerACL(root, write, true, false)
	}
	if err != nil {
		rollback := errors.Join(updateContainerACL(root, read, false, true), updateContainerACL(root, write, false, true))
		if rollback == nil {
			os.Remove(lease.journal)
		}
		return nil, errors.Join(err, rollback)
	}
	success = true
	windowsScopes.runners[r] = cache
	cache[key] = lease
	return lease, nil
}
func (r *Runner) releaseScope(lease *windowsScopeLease) error {
	windowsScopes.Lock()
	defer windowsScopes.Unlock()
	lease.users--
	if lease.users > 0 {
		return nil
	}
	delete(windowsScopes.runners[r], strings.ToLower(lease.record.Scope.Path))
	if len(windowsScopes.runners[r]) == 0 {
		delete(windowsScopes.runners, r)
	}
	defer closeScopeLocks(lease.pins)
	defer closeScopeLocks(lease.locks)
	root := lease.pins[len(lease.pins)-1]
	if err := errors.Join(updateContainerACL(root, lease.read, false, true), updateContainerACL(root, lease.write, false, true)); err != nil {
		return err
	}
	return os.Remove(lease.journal)
}
func (r *Runner) recoverScope(path string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var record windowsScopeRecord
	if len(raw) > 65536 || json.Unmarshal(raw, &record) != nil {
		return unavailable("sandbox_appcontainer_journal_invalid")
	}
	locks, err := lockScope(record.Scope.Path)
	if err != nil {
		return err
	}
	defer closeScopeLocks(locks)
	pins, id, err := pinScope(record.Scope.Path)
	if err != nil {
		return err
	}
	defer closeScopeLocks(pins)
	if id != record.Scope.FileID {
		return unavailable("sandbox_appcontainer_scope_moved")
	}
	for _, value := range []string{record.ReadSID, record.WriteSID} {
		sid, err := windows.StringToSid(value)
		if err != nil {
			return err
		}
		if err = updateContainerACL(pins[len(pins)-1], sid, false, true); err != nil {
			return err
		}
	}
	return os.Remove(path)
}
