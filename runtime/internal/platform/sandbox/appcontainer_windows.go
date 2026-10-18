package sandbox

import (
	"encoding/json"
	"fmt"
	"golang.org/x/sys/windows"
	"io"
	"os"
	"runtime"
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
	raw, e := io.ReadAll(io.LimitReader(os.Stdin, (256<<10)+1))
	if e != nil || len(raw) > 256<<10 {
		return 125
	}
	var spec containerLaunch
	if json.Unmarshal(raw, &spec) != nil {
		return 125
	}
	code, e := runContainer(spec)
	if e != nil {
		fmt.Fprintln(os.Stderr, "AppContainer launch:", e)
		return 125
	}
	return code
}
func SupervisorMain() int { return 125 }
func runContainer(spec containerLaunch) (int, error) {
	sid, e := windows.StringToSid(spec.SID)
	if e != nil {
		return 125, e
	}
	caps := securityCapabilities{AppContainerSID: sid}
	var capabilities []windows.SIDAndAttributes
	if spec.Network {
		for _, kind := range []windows.WELL_KNOWN_SID_TYPE{windows.WinCapabilityInternetClientServerSid, windows.WinCapabilityPrivateNetworkClientServerSid} {
			s, e := windows.CreateWellKnownSid(kind)
			if e != nil {
				return 125, e
			}
			capabilities = append(capabilities, windows.SIDAndAttributes{Sid: s, Attributes: windows.SE_GROUP_ENABLED})
		}
		caps.Capabilities = &capabilities[0]
		caps.CapabilityCount = uint32(len(capabilities))
	}
	attributes, e := windows.NewProcThreadAttributeList(2)
	if e != nil {
		return 125, e
	}
	defer attributes.Delete()
	const securityCapabilitiesAttribute = 0x00020009
	if e = attributes.Update(securityCapabilitiesAttribute, unsafe.Pointer(&caps), unsafe.Sizeof(caps)); e != nil {
		return 125, e
	}
	// Duplicate only stdio. No database, key, socket or control-event handles enter
	// the low-privilege child, even if another library made them inheritable.
	handles := []windows.Handle{}
	for _, f := range []*os.File{os.Stdin, os.Stdout, os.Stderr} {
		var h windows.Handle
		e = windows.DuplicateHandle(windows.CurrentProcess(), windows.Handle(f.Fd()), windows.CurrentProcess(), &h, 0, true, windows.DUPLICATE_SAME_ACCESS)
		if e != nil {
			return 125, e
		}
		defer windows.CloseHandle(h)
		handles = append(handles, h)
	}
	if e = attributes.Update(windows.PROC_THREAD_ATTRIBUTE_HANDLE_LIST, unsafe.Pointer(&handles[0]), uintptr(len(handles))*unsafe.Sizeof(handles[0])); e != nil {
		return 125, e
	}
	startup := windows.StartupInfoEx{ProcThreadAttributeList: attributes.List()}
	startup.Cb = uint32(unsafe.Sizeof(startup))
	startup.Flags = windows.STARTF_USESTDHANDLES
	startup.StdInput = handles[0]
	startup.StdOutput = handles[1]
	startup.StdErr = handles[2]
	// PowerShell's volume-rooted location resolver reads ancestor attributes,
	// which are deliberately outside the granted scope. Root its working drive
	// at the authorized directory; ProviderPath remains the native Windows path.
	script := "[Console]::OutputEncoding = [System.Text.UTF8Encoding]::new(); try { New-PSDrive -Name Molr -PSProvider FileSystem -Root " + psLiteral(spec.Directory) + " -Scope Global -ErrorAction Stop | Out-Null; Set-Location -LiteralPath 'Molr:\\' -ErrorAction Stop } catch { [Console]::Error.WriteLine($_.Exception.Message); exit 125 }; " + spec.Script
	args := []string{windowsShell(), "-NoLogo", "-NoProfile", "-NonInteractive", "-Command", script}
	for i := range args {
		args[i] = syscall.EscapeArg(args[i])
	}
	command, e := windows.UTF16PtrFromString(strings.Join(args, " "))
	if e != nil {
		return 125, e
	}
	application, _ := windows.UTF16PtrFromString(windowsShell())
	directory, e := windows.UTF16PtrFromString(spec.Directory)
	if e != nil {
		return 125, e
	}
	var environment []uint16
	for _, entry := range spec.Environment {
		v, e := windows.UTF16FromString(entry)
		if e != nil {
			return 125, e
		}
		environment = append(environment, v...)
	}
	environment = append(environment, 0)
	var process windows.ProcessInformation
	if e = windows.CreateProcess(application, command, nil, nil, true, windows.EXTENDED_STARTUPINFO_PRESENT|windows.CREATE_UNICODE_ENVIRONMENT|windows.CREATE_NO_WINDOW|windows.CREATE_SUSPENDED, &environment[0], directory, &startup.StartupInfo, &process); e != nil {
		return 125, e
	}
	defer windows.CloseHandle(process.Process)
	defer windows.CloseHandle(process.Thread)
	resumed := false
	defer func() {
		if !resumed {
			windows.TerminateProcess(process.Process, 125)
		}
	}()
	// Verify the actual child token before allowing the first user instruction.
	var token windows.Token
	if e = windows.OpenProcessToken(process.Process, windows.TOKEN_QUERY, &token); e != nil {
		return 125, e
	}
	var isContainer, needed uint32
	e = windows.GetTokenInformation(token, 29 /* TokenIsAppContainer */, (*byte)(unsafe.Pointer(&isContainer)), 4, &needed)
	defer token.Close()
	if e != nil || isContainer != 1 {
		return 125, fmt.Errorf("child is not AppContainer: %v", e)
	}
	var capabilitySize uint32
	_ = windows.GetTokenInformation(token, 30 /* TokenCapabilities */, nil, 0, &capabilitySize)
	if capabilitySize == 0 || capabilitySize > 65536 {
		return 125, fmt.Errorf("invalid child capability information")
	}
	capabilityBuffer := make([]byte, capabilitySize)
	if e = windows.GetTokenInformation(token, 30, &capabilityBuffer[0], capabilitySize, &needed); e != nil {
		return 125, e
	}
	groups := (*windows.Tokengroups)(unsafe.Pointer(&capabilityBuffer[0]))
	if groups.GroupCount != uint32(len(capabilities)) {
		return 125, fmt.Errorf("child network capability count mismatch")
	}
	if groups.GroupCount > 0 {
		if uintptr(capabilitySize) < unsafe.Offsetof(windows.Tokengroups{}.Groups)+uintptr(groups.GroupCount)*unsafe.Sizeof(windows.SIDAndAttributes{}) {
			return 125, fmt.Errorf("truncated child capabilities")
		}
		for _, actual := range unsafe.Slice(&groups.Groups[0], int(groups.GroupCount)) {
			matched := false
			for _, expected := range capabilities {
				if actual.Sid.Equals(expected.Sid) {
					matched = true
				}
			}
			if !matched {
				return 125, fmt.Errorf("unexpected child capability")
			}
		}
	}
	if _, e = windows.ResumeThread(process.Thread); e != nil {
		return 125, e
	}
	resumed = true
	runtime.KeepAlive(capabilities)
	runtime.KeepAlive(sid)
	if _, e = windows.WaitForSingleObject(process.Process, windows.INFINITE); e != nil {
		return 125, e
	}
	var code uint32
	e = windows.GetExitCodeProcess(process.Process, &code)
	return int(code), e
}
