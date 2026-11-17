package execenv

import (
	"os"

	"golang.org/x/sys/windows"
)

func checkOwner(os.FileInfo) error         { return nil }
func setOwner(*os.File, os.FileInfo) error { return nil }
func privateLock(f *os.File, info, _ os.FileInfo) bool {
	var metadata windows.ByHandleFileInformation
	return info.Mode().IsRegular() && windows.GetFileInformationByHandle(windows.Handle(f.Fd()), &metadata) == nil && metadata.NumberOfLinks == 1 && metadata.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT == 0
}
func lockFile(f *os.File) error {
	return windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &windows.Overlapped{})
}
func unlockFile(f *os.File) {
	_ = windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, &windows.Overlapped{})
}
func syncDirectory(*os.File) error { return nil }
