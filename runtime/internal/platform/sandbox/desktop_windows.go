package sandbox

import (
	"fmt"
	"golang.org/x/sys/windows"
	"runtime"
	"unsafe"
)

var user32 = windows.NewLazySystemDLL("user32.dll")

// The helper owns one launch, so changing its window station cannot affect
// unrelated Runtime jobs. Standard desktop sessions only need a private desktop;
// elevated noninteractive hosts also get a private window station.
func containerDesktop(sid *windows.SID) (*uint16, func(), error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	token, err := windows.OpenCurrentProcessToken()
	if err != nil {
		return nil, nil, err
	}
	defer token.Close()
	user, err := token.GetTokenUser()
	if err != nil {
		return nil, nil, err
	}
	descriptor, err := windows.SecurityDescriptorFromString("D:P(A;;GA;;;" + user.User.Sid.String() + ")(A;;GA;;;SY)(A;;GA;;;" + sid.String() + ")S:(ML;;NW;;;LW)")
	if err != nil {
		return nil, nil, err
	}
	security := windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), SecurityDescriptor: descriptor}
	name, err := randomContainerName()
	if err != nil {
		return nil, nil, err
	}
	wide, _ := windows.UTF16PtrFromString(name)
	var station uintptr
	old, _, _ := user32.NewProc("GetProcessWindowStation").Call()
	if token.IsElevated() {
		var e error
		station, _, e = user32.NewProc("CreateWindowStationW").Call(uintptr(unsafe.Pointer(wide)), 1, 0xf037f, uintptr(unsafe.Pointer(&security)))
		if station == 0 {
			return nil, nil, fmt.Errorf("private window station: %w", e)
		}
		ok, _, e := user32.NewProc("SetProcessWindowStation").Call(station)
		if ok == 0 {
			user32.NewProc("CloseWindowStation").Call(station)
			return nil, nil, e
		}
	}
	desktop, _, err := user32.NewProc("CreateDesktopW").Call(uintptr(unsafe.Pointer(wide)), 0, 0, 0, 0xf01ff, uintptr(unsafe.Pointer(&security)))
	if station != 0 {
		user32.NewProc("SetProcessWindowStation").Call(old)
	}
	if desktop == 0 {
		if station != 0 {
			user32.NewProc("CloseWindowStation").Call(station)
		}
		return nil, nil, fmt.Errorf("private desktop: %w", err)
	}
	path := name
	if station != 0 {
		path = name + "\\" + name
	}
	wide, _ = windows.UTF16PtrFromString(path)
	return wide, func() {
		user32.NewProc("CloseDesktop").Call(desktop)
		if station != 0 {
			user32.NewProc("CloseWindowStation").Call(station)
		}
	}, nil
}
func resetConsoleInterrupt() error {
	ok, _, err := windows.NewLazySystemDLL("kernel32.dll").NewProc("SetConsoleCtrlHandler").Call(0, 0)
	if ok == 0 {
		return fmt.Errorf("reset console interrupt: %w", err)
	}
	return nil
}
