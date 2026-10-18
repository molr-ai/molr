//go:build !windows

package platform

import "os"

func PrivateDirectory(path string) error { return os.Chmod(path, 0700) }
func PrivateFile(path string, info os.FileInfo) bool {
	return info.Mode().IsRegular() && info.Mode().Perm()&0077 == 0
}
