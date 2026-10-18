package platform

import (
	"context"
	"crypto/sha256"
	"fmt"
	"golang.org/x/sys/windows"
	"path/filepath"
	"strings"
	"unsafe"
)

func serviceEvent(directory string) *uint16 {
	sum := sha256.Sum256([]byte(strings.ToLower(filepath.Clean(directory))))
	name, _ := windows.UTF16PtrFromString(fmt.Sprintf("Local\\Molr-%x", sum[:16]))
	return name
}

// A named event provides graceful shutdown without a TCP management endpoint.
// Only the owner and SYSTEM can open it, matching the private data directory.
func ServiceContext(parent context.Context, directory string) (context.Context, func(), error) {
	token, err := windows.OpenCurrentProcessToken()
	if err != nil {
		return nil, nil, err
	}
	defer token.Close()
	user, err := token.GetTokenUser()
	if err != nil {
		return nil, nil, err
	}
	sd, err := windows.SecurityDescriptorFromString("D:P(A;;GA;;;" + user.User.Sid.String() + ")(A;;GA;;;SY)")
	if err != nil {
		return nil, nil, err
	}
	attributes := windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), SecurityDescriptor: sd}
	event, err := windows.CreateEvent(&attributes, 1, 0, serviceEvent(directory))
	if err != nil {
		return nil, nil, err
	}
	ctx, cancel := context.WithCancel(parent)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			result, err := windows.WaitForSingleObject(event, 200)
			if err != nil || result == windows.WAIT_OBJECT_0 {
				cancel()
				return
			}
			if ctx.Err() != nil {
				return
			}
		}
	}()
	return ctx, func() { cancel(); <-done; windows.CloseHandle(event) }, nil
}
func ServiceRunning(directory string) bool {
	event, err := windows.OpenEvent(windows.SYNCHRONIZE, false, serviceEvent(directory))
	if err != nil {
		return false
	}
	windows.CloseHandle(event)
	return true
}
func StopService(directory string) error {
	event, err := windows.OpenEvent(windows.EVENT_MODIFY_STATE, false, serviceEvent(directory))
	if err != nil {
		return err
	}
	defer windows.CloseHandle(event)
	return windows.SetEvent(event)
}
