package platform

import (
	"fmt"
	"golang.org/x/sys/windows"
	"os"
	"path/filepath"
	"strings"
	"unsafe"
)

// Windows chmod does not enforce privacy. Pin a protected, inheritable DACL to
// the current account and SYSTEM before writing database, keys or credentials.
func PrivateDirectory(path string) error {
	if !filepath.IsAbs(path) || filepath.Dir(filepath.Clean(path)) == filepath.Clean(path) || strings.HasPrefix(path, `\\`) {
		return fmt.Errorf("dedicated local Windows directory required")
	}
	token, err := windows.OpenCurrentProcessToken()
	if err != nil {
		return err
	}
	defer token.Close()
	user, err := token.GetTokenUser()
	if err != nil {
		return err
	}
	sd, err := windows.SecurityDescriptorFromString("D:P(A;OICI;FA;;;" + user.User.Sid.String() + ")(A;OICI;FA;;;SY)")
	if err != nil {
		return err
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return err
	}
	return windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil)
}
func PrivateFile(path string, info os.FileInfo) bool {
	if !info.Mode().IsRegular() {
		return false
	}
	// The caller has secured the containing directory; ensure this file does not
	// carry an explicit ACL inherited from outside it.
	token, err := windows.OpenCurrentProcessToken()
	if err != nil {
		return false
	}
	defer token.Close()
	user, err := token.GetTokenUser()
	if err != nil {
		return false
	}
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return false
	}
	acl, _, err := sd.DACL()
	if err != nil || acl == nil {
		return false
	}
	system, _ := windows.CreateWellKnownSid(windows.WinLocalSystemSid)
	for i := uint32(0); i < uint32(acl.AceCount); i++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if windows.GetAce(acl, i, &ace) != nil {
			return false
		}
		if ace.Header.AceType == windows.ACCESS_DENIED_ACE_TYPE {
			continue
		}
		if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE {
			return false
		}
		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
		if !sid.Equals(user.User.Sid) && !sid.Equals(system) {
			return false
		}
	}
	return true
}
