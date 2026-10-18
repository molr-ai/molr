package platform

import (
	"golang.org/x/sys/windows"
	"os"
	"path/filepath"
	"testing"
)

func TestWindowsPrivateFileRejectsExtraAndComplexAllow(t *testing.T) {
	directory := t.TempDir()
	if err := PrivateDirectory(directory); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(directory, "key")
	if err := os.WriteFile(file, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	if !PrivateFile(file, info) {
		t.Fatal("private owner file rejected")
	}
	token, err := windows.OpenCurrentProcessToken()
	if err != nil {
		t.Fatal(err)
	}
	defer token.Close()
	user, err := token.GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	for _, extra := range []string{"(A;;FR;;;WD)", "(OA;;FR;;;WD)"} {
		descriptor, err := windows.SecurityDescriptorFromString("D:P(A;;FA;;;" + user.User.Sid.String() + ")" + extra)
		if err != nil {
			t.Fatal(err)
		}
		acl, _, err := descriptor.DACL()
		if err != nil {
			t.Fatal(err)
		}
		if err = windows.SetNamedSecurityInfo(file, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, acl, nil); err != nil {
			t.Fatal(err)
		}
		if PrivateFile(file, info) {
			t.Fatal("unsafe explicit allow accepted", extra)
		}
	}
}
