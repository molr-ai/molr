package sandbox

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"golang.org/x/sys/windows"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/platform"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"
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
	entries, e := os.ReadDir(r.containerDirectory())
	if errors.Is(e, os.ErrNotExist) {
		return nil
	}
	if e != nil {
		return e
	}
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasPrefix(entry.Name(), "scope-molr-") && strings.HasSuffix(entry.Name(), ".json") {
			if err := r.recoverScope(filepath.Join(r.containerDirectory(), entry.Name())); err != nil {
				return err
			}
			continue
		}
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
	if len(c.ForwardPorts) > 0 {
		return nil, unavailable("forwarding_platform_unsupported")
	}
	work, err := windowsScope(work)
	if err != nil {
		return nil, err
	}
	if !r.options.NoWorkspace {
		if err = platform.EnsureWorkspace(work); err != nil {
			return nil, err
		}
	}
	temp, err := os.MkdirTemp(r.options.PrivateRoot, "appcontainer-tmp-")
	if err != nil {
		return nil, err
	}
	scopes := []containerScope{{Path: work, Write: true}}
	global := platform.UserGlobal(r.options.PrivateRoot, "")
	for _, value := range cmd.Env {
		if strings.HasPrefix(value, "MOLR_GLOBAL=") {
			global = strings.TrimPrefix(value, "MOLR_GLOBAL=")
		}
	}
	if !r.options.NoWorkspace {
		if _, err = os.Stat(global); err == nil {
			scopes = append(scopes, containerScope{Path: global})
		}
	}
	resourceEnv := []string{}
	fail := func(err error) (func() error, error) { os.RemoveAll(temp); return nil, err }
	for _, mount := range c.Mounts {
		source, err := r.externalScope(mount.Source)
		if err != nil {
			return fail(err)
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
		source, err := r.externalScope(tool)
		if err != nil {
			return fail(err)
		}
		scopes = append(scopes, containerScope{Path: source})
	}
	if c.Directory == "/tmp" || strings.HasPrefix(c.Directory, "/tmp/") {
		cmd.Dir = filepath.Join(temp, strings.TrimPrefix(c.Directory, "/tmp"))
	}
	var leases []*windowsScopeLease
	release := func() error {
		var failure error
		for i := len(leases) - 1; i >= 0; i-- {
			failure = errors.Join(failure, r.releaseScope(leases[i]))
		}
		leases = nil
		return failure
	}
	fileCaps := []string{}
	for _, scope := range scopes {
		lease, err := r.acquireScope(ctx, scope.Path)
		if err != nil {
			return fail(errors.Join(err, release()))
		}
		leases = append(leases, lease)
		value := lease.read.String()
		if scope.Write {
			value = lease.write.String()
		}
		if !slices.Contains(fileCaps, value) {
			fileCaps = append(fileCaps, value)
		}
	}
	pins, id, err := pinScope(temp)
	if err != nil {
		return fail(errors.Join(err, release()))
	}
	name, err := randomContainerName()
	if err != nil {
		closeScopeLocks(pins)
		return fail(errors.Join(err, release()))
	}
	sid, err := createProfile(name)
	if err != nil {
		closeScopeLocks(pins)
		return fail(errors.Join(err, release()))
	}
	record := containerRecord{Name: name, Scopes: []containerScope{{Path: temp, FileID: id, Write: true}}}
	journal := filepath.Join(r.containerDirectory(), name+".json")
	raw, _ := json.Marshal(record)
	cleanup := func() error {
		failure := updateContainerACL(pins[len(pins)-1], sid, false, true)
		closeScopeLocks(pins)
		if failure == nil {
			failure = deleteProfile(name)
		}
		if failure == nil {
			failure = os.Remove(journal)
		}
		failure = errors.Join(failure, release())
		if failure == nil {
			failure = os.RemoveAll(temp)
		}
		return failure
	}
	if err = os.WriteFile(journal, raw, 0600); err != nil {
		deleteProfile(name)
		closeScopeLocks(pins)
		return fail(errors.Join(err, release()))
	}
	if err = updateContainerACL(pins[len(pins)-1], sid, true, false); err != nil {
		return nil, errors.Join(err, cleanup())
	}
	environment := []string{"SystemRoot=" + os.Getenv("SystemRoot"), "WINDIR=" + os.Getenv("SystemRoot"), "PATH=" + filepath.Join(os.Getenv("SystemRoot"), "System32"), "USERPROFILE=" + work, "LOCALAPPDATA=" + temp, "TEMP=" + temp, "TMP=" + temp, "PSModulePath=" + filepath.Join(filepath.Dir(windowsShell()), "Modules")}
	if !r.options.NoWorkspace {
		environment = append(environment, "MOLR_WORKSPACE="+work, "MOLR_GLOBAL="+global)
	}
	environment = append(environment, resourceEnv...)
	spec := containerLaunch{SID: sid.String(), FileCapabilities: fileCaps, Script: c.Script, Directory: cmd.Dir, Environment: environment, Network: c.NetworkAccess, TTY: c.TTY}
	data, _ := json.Marshal(spec)
	helper := r.options.Helper
	if helper == "" {
		helper, err = os.Executable()
		if err != nil {
			return nil, errors.Join(err, cleanup())
		}
	}
	cmd.Path = helper
	cmd.Args = []string{helper, "sandbox-helper", "windows-appcontainer"}
	cmd.Stdin = bytes.NewReader(data)
	cmd.Env = environment
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
		var probeOutput bytes.Buffer
		cmd.Stdout, cmd.Stderr = &probeOutput, &probeOutput
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
			return fmt.Errorf("%w: network=%t, process=%v, output=%q, cleanup=%v", unavailable("sandbox_appcontainer_probe_failed"), network, e, probeOutput.String(), cleanErr)
		}
	}
	r.terminalReason = "terminal_platform_unsupported"
	if terminalAPIAvailable() {
		if err := probeRunner.probeWindowsTerminal(ctx, work, readonly); err == nil {
			r.terminalReason = ""
		} else {
			r.terminalReason = "terminal_start_failed"
			r.terminalProbeErr = err
			if probeRunner.closed {
				return unavailable("sandbox_appcontainer_cleanup_failed")
			}
		}
	}
	return nil
}

func (r *Runner) probeWindowsTerminal(parent context.Context, work, global string) (result error) {
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()
	cmd := exec.Command(windowsShell())
	cmd.Dir = work
	cmd.Env = []string{"MOLR_GLOBAL=" + global}
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	cleanup, err := r.configureContainer(ctx, cmd, work, platform.Command{TTY: true, Script: "Write-Output 'molr-terminal-ready'; exit 0"})
	if err != nil {
		return err
	}
	safeCleanup := true
	defer func() {
		if !safeCleanup {
			r.closed = true
			return
		}
		if err := cleanup(); err != nil {
			r.closed = true
			result = errors.Join(result, err)
		}
	}()
	initialize, closePipe, err := terminalPipe(cmd, &process{done: make(chan struct{})})
	if err != nil {
		return err
	}
	defer closePipe()
	job, err := startWindowsJob(cmd)
	if err != nil {
		return err
	}
	defer closeWindowsJob(job)
	safeCleanup = false
	if err = initialize(); err != nil {
		safeCleanup = terminateWindowsJob(job) == nil
		cmd.Wait()
		return err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err = <-done:
	case <-ctx.Done():
		terminateWindowsJob(job)
		<-done
		err = ctx.Err()
	}
	stopErr := terminateWindowsJob(job)
	safeCleanup = stopErr == nil
	err = errors.Join(err, stopErr)
	if err != nil {
		return fmt.Errorf("terminal probe: %w; output=%q", err, output.String())
	}
	if !strings.Contains(output.String(), "molr-terminal-ready") {
		return fmt.Errorf("terminal probe missing output")
	}
	return nil
}
