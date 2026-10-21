package sandbox

import (
	"bufio"
	"encoding/json"
	"fmt"
	"golang.org/x/sys/windows"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"syscall"
	"unsafe"
)

var userenv = windows.NewLazySystemDLL("userenv.dll")
var createContainer = userenv.NewProc("CreateAppContainerProfile")
var deleteContainer = userenv.NewProc("DeleteAppContainerProfile")
var deriveContainer = userenv.NewProc("DeriveAppContainerSidFromAppContainerName")

type containerLaunch struct {
	SID, Script, Directory string
	Environment            []string
	Network                bool
	TTY, FullAccess        bool
	FileCapabilities       []string
}
type securityCapabilities struct {
	AppContainerSID           *windows.SID
	Capabilities              *windows.SIDAndAttributes
	CapabilityCount, Reserved uint32
}

func createProfile(name string) (*windows.SID, error) {
	p, e := windows.UTF16PtrFromString(name)
	if e != nil {
		return nil, e
	}
	var sid *windows.SID
	code, _, _ := createContainer.Call(uintptr(unsafe.Pointer(p)), uintptr(unsafe.Pointer(p)), uintptr(unsafe.Pointer(p)), 0, 0, uintptr(unsafe.Pointer(&sid)))
	if int32(code) < 0 {
		return nil, fmt.Errorf("CreateAppContainerProfile: HRESULT 0x%x", code)
	}
	copy, e := sid.Copy()
	windows.FreeSid(sid)
	return copy, e
}
func deleteProfile(name string) error {
	p, e := windows.UTF16PtrFromString(name)
	if e != nil {
		return e
	}
	code, _, _ := deleteContainer.Call(uintptr(unsafe.Pointer(p)))
	if int32(code) < 0 {
		return fmt.Errorf("DeleteAppContainerProfile: HRESULT 0x%x", code)
	}
	return nil
}
func profileSID(name string) (*windows.SID, error) {
	p, e := windows.UTF16PtrFromString(name)
	if e != nil {
		return nil, e
	}
	var sid *windows.SID
	code, _, _ := deriveContainer.Call(uintptr(unsafe.Pointer(p)), uintptr(unsafe.Pointer(&sid)))
	if int32(code) < 0 {
		return nil, fmt.Errorf("DeriveAppContainerSid: HRESULT 0x%x", code)
	}
	copy, e := sid.Copy()
	windows.FreeSid(sid)
	return copy, e
}

func HelperMain(args []string) int {
	if len(args) != 1 || args[0] != "windows-appcontainer" {
		return 125
	}
	reader := bufio.NewReaderSize(os.Stdin, 256<<10)
	raw, err := reader.ReadSlice('\n')
	if err != nil && err != io.EOF || len(raw) > 256<<10 {
		return 125
	}
	var spec containerLaunch
	if json.Unmarshal(raw, &spec) != nil {
		return 125
	}
	var code int
	if spec.TTY {
		code, err = runTerminalContainer(spec, reader)
	} else {
		code, err = runContainer(spec)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "AppContainer launch:", err)
		return 125
	}
	return code
}
func SupervisorMain() int { return 125 }
func runContainer(spec containerLaunch) (int, error) {
	process, dispose, err := createContainerProcess(spec, 0)
	if err != nil {
		return 125, err
	}
	defer dispose()
	if _, err = windows.WaitForSingleObject(process.Process, windows.INFINITE); err != nil {
		return 125, err
	}
	var code uint32
	err = windows.GetExitCodeProcess(process.Process, &code)
	return int(code), err
}
func containerCapabilities(spec containerLaunch) (*windows.SID, []windows.SIDAndAttributes, error) {
	if spec.FullAccess {
		return nil, nil, nil
	}
	sid, err := windows.StringToSid(spec.SID)
	if err != nil {
		return nil, nil, err
	}
	var caps []windows.SIDAndAttributes
	for _, value := range spec.FileCapabilities {
		if !strings.HasPrefix(value, "S-1-15-3-") {
			return nil, nil, fmt.Errorf("invalid file capability")
		}
		cap, err := windows.StringToSid(value)
		if err != nil {
			return nil, nil, err
		}
		caps = append(caps, windows.SIDAndAttributes{Sid: cap, Attributes: windows.SE_GROUP_ENABLED})
	}
	if spec.Network {
		for _, kind := range []windows.WELL_KNOWN_SID_TYPE{windows.WinCapabilityInternetClientServerSid, windows.WinCapabilityPrivateNetworkClientServerSid} {
			cap, err := windows.CreateWellKnownSid(kind)
			if err != nil {
				return nil, nil, err
			}
			caps = append(caps, windows.SIDAndAttributes{Sid: cap, Attributes: windows.SE_GROUP_ENABLED})
		}
	}
	return sid, caps, nil
}
func checkContainerToken(process windows.Handle, caps []windows.SIDAndAttributes) error {
	var token windows.Token
	if err := windows.OpenProcessToken(process, windows.TOKEN_QUERY, &token); err != nil {
		return err
	}
	defer token.Close()
	var isContainer, needed uint32
	if err := windows.GetTokenInformation(token, 29, (*byte)(unsafe.Pointer(&isContainer)), 4, &needed); err != nil || isContainer != 1 {
		return fmt.Errorf("child is not AppContainer: %v", err)
	}
	var size uint32
	_ = windows.GetTokenInformation(token, 30, nil, 0, &size)
	if size == 0 || size > 65536 {
		return fmt.Errorf("invalid child capabilities")
	}
	buffer := make([]byte, size)
	if err := windows.GetTokenInformation(token, 30, &buffer[0], size, &needed); err != nil {
		return err
	}
	groups := (*windows.Tokengroups)(unsafe.Pointer(&buffer[0]))
	if groups.GroupCount != uint32(len(caps)) || uintptr(size) < unsafe.Offsetof(windows.Tokengroups{}.Groups)+uintptr(groups.GroupCount)*unsafe.Sizeof(windows.SIDAndAttributes{}) {
		return fmt.Errorf("child capability count mismatch")
	}
	if groups.GroupCount > 0 {
		for _, actual := range unsafe.Slice(&groups.Groups[0], int(groups.GroupCount)) {
			if !slices.ContainsFunc(caps, func(expected windows.SIDAndAttributes) bool { return actual.Sid.Equals(expected.Sid) }) {
				return fmt.Errorf("unexpected child capability")
			}
		}
	}
	return nil
}
func createContainerProcess(spec containerLaunch, hpc windows.Handle) (result windows.ProcessInformation, dispose func() error, err error) {
	sid, capabilities, err := containerCapabilities(spec)
	if err != nil {
		return result, nil, err
	}
	attributes, err := windows.NewProcThreadAttributeList(2)
	if err != nil {
		return result, nil, err
	}
	defer attributes.Delete()
	caps := securityCapabilities{AppContainerSID: sid, CapabilityCount: uint32(len(capabilities))}
	if len(capabilities) > 0 {
		caps.Capabilities = &capabilities[0]
	}
	if !spec.FullAccess {
		if err = attributes.Update(0x00020009, unsafe.Pointer(&caps), unsafe.Sizeof(caps)); err != nil {
			return result, nil, err
		}
	}
	startup := windows.StartupInfoEx{ProcThreadAttributeList: attributes.List()}
	startup.Cb = uint32(unsafe.Sizeof(startup))
	startup.Flags = windows.STARTF_USESTDHANDLES
	desktopClose := func() {}
	if hpc != 0 {
		if err = attributes.Update(windows.PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE, unsafe.Pointer(uintptr(hpc)), unsafe.Sizeof(hpc)); err != nil {
			return result, nil, err
		}
	} else {
		handles := []windows.Handle{}
		for _, file := range []*os.File{os.Stdin, os.Stdout, os.Stderr} {
			var handle windows.Handle
			err = windows.DuplicateHandle(windows.CurrentProcess(), windows.Handle(file.Fd()), windows.CurrentProcess(), &handle, 0, true, windows.DUPLICATE_SAME_ACCESS)
			if err != nil {
				return result, nil, err
			}
			defer windows.CloseHandle(handle)
			handles = append(handles, handle)
		}
		if err = attributes.Update(windows.PROC_THREAD_ATTRIBUTE_HANDLE_LIST, unsafe.Pointer(&handles[0]), uintptr(len(handles))*unsafe.Sizeof(handles[0])); err != nil {
			return result, nil, err
		}
		startup.StdInput, startup.StdOutput, startup.StdErr = handles[0], handles[1], handles[2]
	}
	if !spec.FullAccess {
		startup.Desktop, desktopClose, err = containerDesktop(sid)
		if err != nil {
			return result, nil, err
		}
	}
	success := false
	defer func() {
		if !success {
			desktopClose()
		}
	}()
	script := "[Console]::OutputEncoding=[System.Text.UTF8Encoding]::new(); "
	if !spec.FullAccess {
		module := filepath.Join(filepath.Dir(windowsShell()), "Modules", "Microsoft.PowerShell.Management", "Microsoft.PowerShell.Management.psd1")
		utility := filepath.Join(filepath.Dir(windowsShell()), "Modules", "Microsoft.PowerShell.Utility", "Microsoft.PowerShell.Utility.psd1")
		script += "try { Import-Module -Name " + psLiteral(module) + "," + psLiteral(utility) + " -ErrorAction Stop; New-PSDrive -Name Molr -PSProvider FileSystem -Root " + psLiteral(spec.Directory) + " -Scope Global -ErrorAction Stop | Out-Null; Set-Location -LiteralPath 'Molr:\\' -ErrorAction Stop } catch { [Console]::Error.WriteLine($_.Exception.Message); exit 125 }; "
	}
	script += spec.Script
	args := []string{windowsShell(), "-NoLogo", "-NoProfile"}
	if spec.TTY {
		if strings.TrimSpace(spec.Script) == "" {
			args = append(args, "-NoExit")
		}
	} else {
		args = append(args, "-NonInteractive")
	}
	args = append(args, "-Command", script)
	for i := range args {
		args[i] = syscall.EscapeArg(args[i])
	}
	command, err := windows.UTF16PtrFromString(strings.Join(args, " "))
	if err != nil {
		return result, nil, err
	}
	application, _ := windows.UTF16PtrFromString(windowsShell())
	directory, err := windows.UTF16PtrFromString(spec.Directory)
	if err != nil {
		return result, nil, err
	}
	var environment []uint16
	for _, entry := range spec.Environment {
		value, e := windows.UTF16FromString(entry)
		if e != nil {
			return result, nil, e
		}
		environment = append(environment, value...)
	}
	environment = append(environment, 0)
	flags := uint32(windows.EXTENDED_STARTUPINFO_PRESENT | windows.CREATE_UNICODE_ENVIRONMENT | windows.CREATE_SUSPENDED)
	if hpc == 0 {
		flags |= windows.CREATE_NO_WINDOW
	}
	if spec.TTY {
		if err = resetConsoleInterrupt(); err != nil {
			return result, nil, err
		}
	}
	if err = windows.CreateProcess(application, command, nil, nil, hpc == 0, flags, &environment[0], directory, &startup.StartupInfo, &result); err != nil {
		return result, nil, err
	}
	var job windows.Handle
	dispose = func() error {
		var failure error
		if job != 0 {
			failure = terminateWindowsJob(job)
			closeWindowsJob(job)
			job = 0
		}
		windows.CloseHandle(result.Process)
		windows.CloseHandle(result.Thread)
		desktopClose()
		return failure
	}
	defer func() {
		if !success {
			if job != 0 {
				terminateWindowsJob(job)
				closeWindowsJob(job)
			}
			windows.TerminateProcess(result.Process, 125)
			windows.WaitForSingleObject(result.Process, 5000)
			windows.CloseHandle(result.Process)
			windows.CloseHandle(result.Thread)
		}
	}()
	if !spec.FullAccess {
		if err = checkContainerToken(result.Process, capabilities); err != nil {
			return result, nil, err
		}
	}
	if spec.TTY {
		job, err = newWindowsJob()
		if err != nil {
			return result, nil, err
		}
		if err = windows.AssignProcessToJobObject(job, result.Process); err != nil {
			closeWindowsJob(job)
			job = 0
			return result, nil, err
		}
	}
	if _, err = windows.ResumeThread(result.Thread); err != nil {
		return result, nil, err
	}
	runtime.KeepAlive(capabilities)
	runtime.KeepAlive(sid)
	success = true
	return result, dispose, nil
}
