package sandbox

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"golang.org/x/sys/windows"
	"io/fs"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/platform"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"unsafe"
)

var containerACL sync.Mutex

// Journals contain only a unique package identity and the exact granted paths.
// They are private Runtime state; recovery revokes stale SIDs before new work.
type containerScope struct {
	Path   string
	FileID string
	Write  bool
}
type containerRecord struct {
	Name   string
	Scopes []containerScope
}

func (r *Runner) check() error {
	if !r.prepared {
		return unavailable("sandbox_not_prepared")
	}
	return r.prepareErr
}
func (r *Runner) start(ctx context.Context, e core.Execution, c platform.Command) (*process, error) {
	return r.startHost(ctx, e, c)
}
func windowsScope(path string) (string, error) {
	if !filepath.IsAbs(path) || strings.HasPrefix(path, `\\`) || strings.ContainsAny(strings.TrimPrefix(path, filepath.VolumeName(path)), ":\x00") {
		return "", core.Invalid("path", "local absolute Windows path required")
	}
	actual, e := filepath.EvalSymlinks(path)
	if e != nil {
		return "", e
	}
	if !strings.EqualFold(filepath.Clean(path), actual) {
		return "", core.Invalid("path", "reparse aliases are not allowed")
	}
	current := filepath.VolumeName(actual) + string(filepath.Separator)
	for _, part := range strings.Split(strings.TrimPrefix(actual, current), string(filepath.Separator)) {
		if part == "" {
			continue
		}
		current = filepath.Join(current, part)
		st, e := os.Lstat(current)
		if e != nil {
			return "", e
		}
		if st.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 {
			return "", core.Invalid("path", "reparse points are not allowed")
		}
	}
	return actual, nil
}
func windowsOverlap(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	a = strings.ToLower(filepath.Clean(a))
	b = strings.ToLower(filepath.Clean(b))
	return a == b || strings.HasPrefix(a, b+`\`) || strings.HasPrefix(b, a+`\`)
}
func (r *Runner) externalScope(path string) (string, error) {
	p, e := windowsScope(path)
	if e != nil {
		return "", e
	}
	if filepath.Dir(p) == p {
		return "", core.Invalid("path", "volume root cannot be granted")
	}
	for _, protected := range append([]string{r.options.PrivateRoot, r.options.WorkRoot}, r.options.ProtectedRoots...) {
		if windowsOverlap(p, protected) {
			return "", core.Invalid("path", "scope overlaps Runtime private data")
		}
	}
	return p, nil
}

// Keep every ancestor open without FILE_SHARE_DELETE until ACL revocation.
// This prevents rename/reparse replacement while granting and running commands.
func pinScope(path string) ([]windows.Handle, string, error) {
	actual, err := windowsScope(path)
	if err != nil {
		return nil, "", err
	}
	var handles []windows.Handle
	fail := func(err error) ([]windows.Handle, string, error) {
		for _, h := range handles {
			windows.CloseHandle(h)
		}
		return nil, "", err
	}
	current := filepath.VolumeName(actual) + string(filepath.Separator)
	parts := strings.Split(strings.TrimPrefix(actual, current), string(filepath.Separator))
	for index, part := range append([]string{""}, parts...) {
		if index > 0 {
			current = filepath.Join(current, part)
		}
		// Attribute-only handles do not enforce sharing restrictions. Directory
		// read access keeps ancestors pinned against rename until ACL revocation.
		access := uint32(windows.FILE_READ_ATTRIBUTES | windows.FILE_LIST_DIRECTORY)
		if index == len(parts) {
			access |= windows.READ_CONTROL | windows.WRITE_DAC
		}
		p, _ := windows.UTF16PtrFromString(current)
		h, e := windows.CreateFile(p, access, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
		if e != nil {
			return fail(e)
		}
		handles = append(handles, h)
		var info windows.ByHandleFileInformation
		if e = windows.GetFileInformationByHandle(h, &info); e != nil {
			return fail(e)
		}
		if info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
			return fail(core.Invalid("path", "reparse ancestor prohibited"))
		}
		buf := make([]uint16, 32768)
		n, e := windows.GetFinalPathNameByHandle(h, &buf[0], uint32(len(buf)), 0)
		if e != nil || n >= uint32(len(buf)) {
			return fail(fmt.Errorf("cannot verify scope handle: %v", e))
		}
		final := strings.TrimPrefix(windows.UTF16ToString(buf[:n]), `\\?\`)
		if !strings.EqualFold(filepath.Clean(final), filepath.Clean(current)) {
			return fail(core.Invalid("path", "scope identity changed"))
		}
		if index == len(parts) {
			return handles, fmt.Sprintf("%d:%d:%d", info.VolumeSerialNumber, info.FileIndexHigh, info.FileIndexLow), nil
		}
	}
	return fail(core.Invalid("path", "invalid scope"))
}
func releaseScopes(handles [][]windows.Handle) {
	for _, set := range handles {
		for i := len(set) - 1; i >= 0; i-- {
			windows.CloseHandle(set[i])
		}
	}
}
func updateContainerACL(h windows.Handle, sid *windows.SID, write, revoke bool) error {
	containerACL.Lock()
	defer containerACL.Unlock()
	descriptor, e := windows.GetSecurityInfo(h, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if e != nil {
		return e
	}
	acl, _, e := descriptor.DACL()
	if e != nil || acl == nil {
		return fmt.Errorf("scope must have an explicit ACL: %v", e)
	}
	access := windows.EXPLICIT_ACCESS{AccessPermissions: windows.FILE_GENERIC_READ | windows.FILE_GENERIC_EXECUTE, AccessMode: windows.GRANT_ACCESS, Inheritance: windows.SUB_CONTAINERS_AND_OBJECTS_INHERIT, Trustee: windows.TRUSTEE{TrusteeForm: windows.TRUSTEE_IS_SID, TrusteeType: windows.TRUSTEE_IS_UNKNOWN, TrusteeValue: windows.TrusteeValueFromSID(sid)}}
	if write {
		access.AccessPermissions |= windows.FILE_GENERIC_WRITE | windows.DELETE | 0x0040 /* FILE_DELETE_CHILD */
	}
	if revoke {
		access.AccessMode = windows.REVOKE_ACCESS
		access.AccessPermissions = 0
	}
	var pin runtime.Pinner
	pin.Pin(sid)
	defer pin.Unpin()
	merged, e := windows.ACLFromEntries([]windows.EXPLICIT_ACCESS{access}, acl)
	if e != nil {
		return e
	}
	return windows.SetSecurityInfo(h, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION, nil, nil, merged, nil)
}
func (r *Runner) containerDirectory() string {
	return filepath.Join(r.options.PrivateRoot, "appcontainers")
}
func (r *Runner) cleanupContainer(record containerRecord) error {
	sid, e := profileSID(record.Name)
	if e != nil {
		return e
	}
	for _, scope := range record.Scopes {
		handles, id, err := pinScope(scope.Path)
		if err != nil {
			return err
		}
		if id != scope.FileID {
			releaseScopes([][]windows.Handle{handles})
			return unavailable("sandbox_appcontainer_scope_moved")
		}
		err = updateContainerACL(handles[len(handles)-1], sid, false, true)
		releaseScopes([][]windows.Handle{handles})
		if err != nil {
			return err
		}
	}
	if e = deleteProfile(record.Name); e != nil {
		return e
	}
	return os.Remove(filepath.Join(r.containerDirectory(), record.Name+".json"))
}
func (r *Runner) recover() error {
	release, err := acquireContainerLease()
	if err != nil {
		return err
	}
	defer release()
	entries, e := os.ReadDir(r.containerDirectory())
	if errors.Is(e, os.ErrNotExist) {
		return nil
	}
	if e != nil {
		return e
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), "molr-") || !strings.HasSuffix(entry.Name(), ".json") {
			return unavailable("sandbox_appcontainer_journal_invalid")
		}
		raw, e := os.ReadFile(filepath.Join(r.containerDirectory(), entry.Name()))
		if e != nil {
			return e
		}
		var record containerRecord
		if len(raw) > 65536 || json.Unmarshal(raw, &record) != nil || record.Name+".json" != entry.Name() {
			return unavailable("sandbox_appcontainer_journal_invalid")
		}
		if e = r.cleanupContainer(record); e != nil {
			return e
		}
	}
	return nil
}
func (r *Runner) configureContainer(ctx context.Context, cmd *exec.Cmd, work string, c platform.Command) (func() error, error) {
	// Windows ACL inheritance updates existing descendants. A per-user kernel
	// mutex serializes controlled tasks across Runtime instances while temporary
	// grants exist; a dedicated thread owns it until all processes are gone.
	releaseLease, err := acquireContainerLease()
	if err != nil {
		return nil, err
	}
	leaseOwned := true
	defer func() {
		if leaseOwned {
			releaseLease()
		}
	}()

	if c.TTY {
		return nil, unavailable("terminal_platform_unsupported")
	}
	if len(c.ForwardPorts) > 0 {
		return nil, unavailable("forwarding_platform_unsupported")
	}
	work, e := windowsScope(work)
	if e != nil {
		return nil, e
	}
	if !r.options.NoWorkspace {
		if e = platform.EnsureWorkspace(work); e != nil {
			return nil, e
		}
	}
	temp, e := os.MkdirTemp(r.options.PrivateRoot, "appcontainer-tmp-")
	if e != nil {
		return nil, e
	}
	scopes := []containerScope{{Path: work, Write: true}, {Path: temp, Write: true}}
	global := platform.UserGlobal(r.options.PrivateRoot, "")
	// startHost passes the current owner's global path through this fixed variable.
	for _, env := range cmd.Env {
		if strings.HasPrefix(env, "MOLR_GLOBAL=") {
			global = strings.TrimPrefix(env, "MOLR_GLOBAL=")
		}
	}
	if !r.options.NoWorkspace {
		if _, e = os.Stat(global); e == nil {
			p, e := windowsScope(global)
			if e != nil {
				return nil, e
			}
			scopes = append(scopes, containerScope{Path: p})
		}
	}
	resourceEnv := []string{}
	for _, mount := range c.Mounts {
		source, e := r.externalScope(mount.Source)
		if e != nil {
			os.RemoveAll(temp)
			return nil, e
		}
		scopes = append(scopes, containerScope{Path: source, Write: !mount.ReadOnly})
		if mount.Destination != "" {
			resourceEnv = append(resourceEnv, platform.ResourceEnvironment(mount.Destination)+"="+source)
			if c.Directory == mount.Destination || strings.HasPrefix(c.Directory, mount.Destination+"/") {
				cmd.Dir = filepath.Join(source, strings.TrimPrefix(c.Directory, mount.Destination))
			}
		}
	}
	for _, tool := range r.options.ToolDirectories {
		p, e := r.externalScope(tool)
		if e != nil {
			os.RemoveAll(temp)
			return nil, e
		}
		scopes = append(scopes, containerScope{Path: p})
	}
	if c.Directory == "/tmp" || strings.HasPrefix(c.Directory, "/tmp/") {
		cmd.Dir = filepath.Join(temp, strings.TrimPrefix(c.Directory, "/tmp"))
	}
	token := make([]byte, 16)
	if _, e = rand.Read(token); e != nil {
		return nil, e
	}
	record := containerRecord{Name: "molr-" + hex.EncodeToString(token), Scopes: scopes}
	var pinned [][]windows.Handle
	var descendants []windows.Handle
	releasePinned := func() {
		for _, handle := range descendants {
			windows.CloseHandle(handle)
		}
		releaseScopes(pinned)
	}
	for i := range record.Scopes {
		handles, id, err := pinScope(record.Scopes[i].Path)
		if err != nil {
			releasePinned()
			os.RemoveAll(temp)
			return nil, err
		}
		pinned = append(pinned, handles)
		record.Scopes[i].FileID = id
		err = filepath.WalkDir(record.Scopes[i].Path, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			if len(descendants) >= 100000 {
				return core.Invalid("path", "AppContainer scope exceeds 100000 entries")
			}
			if path == record.Scopes[i].Path {
				return nil
			}
			name, _ := windows.UTF16PtrFromString(path)
			h, err := windows.CreateFile(name, windows.FILE_READ_ATTRIBUTES|windows.FILE_READ_DATA, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
			if err != nil {
				return err
			}
			descendants = append(descendants, h)
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
			releasePinned()
			os.RemoveAll(temp)
			return nil, err
		}
	}
	sid, e := createProfile(record.Name)
	if e != nil {
		releasePinned()
		os.RemoveAll(temp)
		return nil, e
	}
	journal := filepath.Join(r.containerDirectory(), record.Name+".json")
	raw, _ := json.Marshal(record)
	if e = os.WriteFile(journal, raw, 0600); e != nil {
		deleteProfile(record.Name)
		releasePinned()
		os.RemoveAll(temp)
		return nil, e
	}
	leaseOwned = false
	cleanup := func() error {
		defer releaseLease()
		var failure error
		for _, handles := range pinned {
			if err := updateContainerACL(handles[len(handles)-1], sid, false, true); err != nil {
				failure = errors.Join(failure, err)
			}
		}
		releasePinned()
		if failure != nil {
			return failure
		}
		if err := deleteProfile(record.Name); err != nil {
			return err
		}
		if err := os.Remove(journal); err != nil {
			return err
		}
		return os.RemoveAll(temp)
	}
	for i, scope := range record.Scopes {
		if e = updateContainerACL(pinned[i][len(pinned[i])-1], sid, scope.Write, false); e != nil {
			if cleanErr := cleanup(); cleanErr != nil {
				r.closed = true
				e = errors.Join(e, cleanErr)
			}
			return nil, e
		}
	}
	// Existing entries are held only during inheritance updates. Release them
	// before user code so ordinary child-file rename/delete remains possible.
	for _, handle := range descendants {
		windows.CloseHandle(handle)
	}
	descendants = nil
	// CreateProcess requires LOCALAPPDATA for AppContainer startup. Keep it in
	// the granted temporary scope rather than inheriting the host profile.
	environment := []string{"SystemRoot=" + os.Getenv("SystemRoot"), "WINDIR=" + os.Getenv("SystemRoot"), "PATH=" + filepath.Join(os.Getenv("SystemRoot"), "System32"), "USERPROFILE=" + work, "LOCALAPPDATA=" + temp, "TEMP=" + temp, "TMP=" + temp}
	if !r.options.NoWorkspace {
		environment = append(environment, "MOLR_WORKSPACE="+work, "MOLR_GLOBAL="+global)
	}
	environment = append(environment, resourceEnv...)
	spec := containerLaunch{SID: sid.String(), Script: c.Script, Directory: cmd.Dir, Environment: environment, Network: c.NetworkAccess}
	data, _ := json.Marshal(spec)
	helper := r.options.Helper
	if helper == "" {
		helper, e = os.Executable()
		if e != nil {
			if cleanErr := cleanup(); cleanErr != nil {
				r.closed = true
				e = errors.Join(e, cleanErr)
			}
			return nil, e
		}
	}
	cmd.Path = helper
	cmd.Args = []string{helper, "sandbox-helper", "windows-appcontainer"}
	cmd.Stdin = bytes.NewReader(data)
	cmd.Env = environment
	leaseOwned = false
	return cleanup, nil
}
func psLiteral(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }
func (r *Runner) prepare(ctx context.Context) error {
	if !filepath.IsAbs(r.options.PrivateRoot) {
		return unavailable("sandbox_private_directory_required")
	}
	if e := r.checkDirect(); e != nil {
		return e
	}
	if e := createContainer.Find(); e != nil {
		return unavailable("sandbox_appcontainer_unavailable")
	}
	if e := os.MkdirAll(r.containerDirectory(), 0700); e != nil {
		return e
	}
	if e := platform.PrivateDirectory(r.containerDirectory()); e != nil {
		return e
	}
	if e := r.recover(); e != nil {
		if core.ReasonOf(e) == "sandbox_file_scope_busy" {
			return e
		}
		return unavailable("sandbox_appcontainer_recovery_failed")
	}
	r.recovered = true
	probe, e := os.MkdirTemp(r.options.PrivateRoot, "appcontainer-probe-")
	if e != nil {
		return e
	}
	defer os.RemoveAll(probe)
	work := filepath.Join(probe, "work")
	if e = os.Mkdir(work, 0700); e != nil {
		return e
	}
	secret := filepath.Join(probe, "private")
	if e = os.WriteFile(secret, []byte("private"), 0600); e != nil {
		return e
	}
	readonly := filepath.Join(probe, "readonly")
	if e = os.Mkdir(readonly, 0700); e != nil {
		return e
	}
	readFile := filepath.Join(readonly, "read.txt")
	if e = os.WriteFile(readFile, []byte("read"), 0600); e != nil {
		return e
	}
	probeOptions := r.options
	probeOptions.NoWorkspace = false
	probeRunner := New(probeOptions)
	// Windows PowerShell 5.1 uses .NET Framework, whose Socket has no
	// ConnectAsync(IPAddress, int) overload. Use its supported APM methods.
	for _, network := range []bool{false, true} {
		script := `$ErrorActionPreference='Stop'; [IO.File]::WriteAllText(` + psLiteral(filepath.Join(work, "result")) + `, 'probe'); try { [IO.File]::ReadAllText(` + psLiteral(secret) + `); exit 11 } catch [UnauthorizedAccessException] {} ; try { [IO.File]::WriteAllText(` + psLiteral(secret) + `, 'changed'); exit 12 } catch [UnauthorizedAccessException] {} ; $s=[Net.Sockets.Socket]::new([Net.Sockets.AddressFamily]::InterNetwork,[Net.Sockets.SocketType]::Stream,[Net.Sockets.ProtocolType]::Tcp); $denied=$false; try { $pending=$s.BeginConnect([Net.IPAddress]::Parse('192.0.2.1'),9,$null,$null); $wait=$pending.AsyncWaitHandle; try { if($wait.WaitOne(1200)){$s.EndConnect($pending)} } finally { $wait.Close() } } catch { $x=$_.Exception; while($x.InnerException){$x=$x.InnerException}; $denied=($x -is [Net.Sockets.SocketException] -and $x.SocketErrorCode -eq [Net.Sockets.SocketError]::AccessDenied) } finally { $s.Dispose() }; `
		if network {
			script += `if($denied){exit 14}`
		} else {
			script += `if(-not $denied){exit 13}`
		}
		script += `; if([IO.File]::ReadAllText(` + psLiteral(readFile) + `) -ne 'read'){exit 15}; try{[IO.File]::WriteAllText(` + psLiteral(readFile) + `,'changed');exit 16}catch [UnauthorizedAccessException]{}; exit 0`
		cmd := exec.CommandContext(ctx, windowsShell())
		cmd.Dir = work
		cmd.Env = []string{"MOLR_GLOBAL=" + readonly}
		cleanup, e := probeRunner.configureContainer(ctx, cmd, work, platform.Command{Script: script, NetworkAccess: network})
		if e != nil {
			return e
		}
		job, e := startWindowsJob(cmd)
		if e == nil {
			done := make(chan error, 1)
			go func() { done <- cmd.Wait() }()
			select {
			case e = <-done:
			case <-ctx.Done():
				terminateWindowsJob(job)
				e = ctx.Err()
				<-done
			}
			stopErr := terminateWindowsJob(job)
			closeWindowsJob(job)
			if stopErr != nil {
				return unavailable("sandbox_appcontainer_cleanup_failed")
			}
		}
		cleanErr := cleanup()
		if e != nil || cleanErr != nil {
			return unavailable("sandbox_appcontainer_probe_failed")
		}
	}
	return nil
}

func acquireContainerLease() (func(), error) {
	type acquired struct {
		release func()
		err     error
	}
	ready := make(chan acquired, 1)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		token, err := windows.OpenCurrentProcessToken()
		if err != nil {
			ready <- acquired{err: err}
			return
		}
		user, err := token.GetTokenUser()
		token.Close()
		if err != nil {
			ready <- acquired{err: err}
			return
		}
		name, _ := windows.UTF16PtrFromString(`Global\Molr-AppContainer-` + user.User.Sid.String())
		descriptor, err := windows.SecurityDescriptorFromString("D:P(A;;GA;;;" + user.User.Sid.String() + ")(A;;GA;;;SY)")
		if err != nil {
			ready <- acquired{err: err}
			return
		}
		attributes := windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), SecurityDescriptor: descriptor}
		mutex, err := windows.CreateMutex(&attributes, false, name)
		if err != nil && err != windows.ERROR_ALREADY_EXISTS {
			ready <- acquired{err: err}
			return
		}
		defer windows.CloseHandle(mutex)
		status, err := windows.WaitForSingleObject(mutex, 0)
		if err != nil || status != windows.WAIT_OBJECT_0 && status != windows.WAIT_ABANDONED {
			ready <- acquired{err: unavailable("sandbox_file_scope_busy")}
			return
		}
		done := make(chan struct{})
		released := make(chan struct{})
		var once sync.Once
		ready <- acquired{release: func() { once.Do(func() { close(done) }); <-released }}
		<-done
		windows.ReleaseMutex(mutex)
		close(released)
	}()
	result := <-ready
	return result.release, result.err
}
