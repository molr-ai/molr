//go:build linux

package installation

import (
	"debug/elf"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

func staticELF(path, arch string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 || info.Mode().Perm()&0022 != 0 {
		return fmt.Errorf("protected executable required: %s", path)
	}
	f, err := elf.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	if f.Type != elf.ET_EXEC {
		return fmt.Errorf("static executable required: %s", path)
	}
	if arch != "" && ((arch == "amd64" && f.Machine != elf.EM_X86_64) || (arch == "arm64" && f.Machine != elf.EM_AARCH64)) {
		return fmt.Errorf("wrong executable architecture")
	}
	for _, p := range f.Progs {
		if p.Type == elf.PT_INTERP {
			return fmt.Errorf("dynamically linked executable: %s", path)
		}
	}
	libs, err := f.ImportedLibraries()
	if err != nil {
		return err
	}
	if len(libs) > 0 {
		return fmt.Errorf("dynamic dependencies in %s", path)
	}
	return nil
}

func checkExecutable(path, arch string) error { return staticELF(path, arch) }
func checkTree(path string, uid int) error {
	if err := checkPath(path); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() || int(info.Sys().(*syscall.Stat_t).Uid) != uid {
		return fmt.Errorf("directory ownership changed: %s", path)
	}
	data, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil {
		return err
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}
		mount := fields[4]
		for _, v := range []string{"040", "011", "012", "134"} {
			n, _ := strconv.ParseInt(v, 8, 8)
			mount = strings.ReplaceAll(mount, "\\"+v, string(rune(n)))
		}
		if mount == path || strings.HasPrefix(mount, path+"/") {
			return fmt.Errorf("unmount filesystem before removal: %s", mount)
		}
	}
	return nil
}
func removeTree(path string) error {
	dir, err := openDir(filepath.Dir(path), false)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer dir.Close()
	// The /proc descriptor refers to the pinned directory, not a mutable pathname.
	root, err := os.OpenRoot(fmt.Sprintf("/proc/self/fd/%d", dir.Fd()))
	if err != nil {
		return err
	}
	defer root.Close()
	return root.RemoveAll(filepath.Base(path))
}
