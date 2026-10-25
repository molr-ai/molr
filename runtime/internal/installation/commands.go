//go:build linux || darwin

package installation

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
	"molr.ai/runtime/internal/installrecord"
)

var commandNames = []string{"molr", "molr-setup"}

func commandDirectory(r *installrecord.Record) (string, error) {
	if r.System {
		return "/usr/local/bin", nil
	}
	if err := checkPath(r.Home); err != nil {
		return "", err
	}
	return filepath.Join(r.Home, ".local", "bin"), nil
}

// All link operations use a pinned directory. Existing entries are never replaced.
func installCommandLinks(r *installrecord.Record, directory string) error {
	if err := checkManaged(directory, r.OwnerUID, nil); err != nil {
		return err
	}
	dir, err := openDir(directory, true)
	if err != nil {
		return err
	}
	defer dir.Close()
	fd := int(dir.Fd())
	for _, name := range commandNames {
		target, err := readCommandLink(fd, name)
		if errors.Is(err, unix.ENOENT) {
			continue
		}
		if err != nil || target != filepath.Join(r.Paths.Program, "bin", name) {
			return fmt.Errorf("command already exists; leaving both commands unchanged: %s", filepath.Join(directory, name))
		}
	}
	created := []string{}
	for _, name := range commandNames {
		target := filepath.Join(r.Paths.Program, "bin", name)
		if existing, err := readCommandLink(fd, name); err == nil && existing == target {
			continue
		}
		if err := unix.Symlinkat(target, fd, name); err != nil {
			for _, createdName := range created {
				_ = removeCommandLink(fd, createdName, filepath.Join(r.Paths.Program, "bin", createdName))
			}
			return err
		}
		created = append(created, name)
	}
	return dir.Sync()
}

func readCommandLink(fd int, name string) (string, error) {
	buf := make([]byte, 4096)
	n, err := unix.Readlinkat(fd, name, buf)
	if err != nil {
		return "", err
	}
	if n == len(buf) {
		return "", fmt.Errorf("command link target too long")
	}
	return string(buf[:n]), nil
}

func removeCommandLink(fd int, name, target string) error {
	actual, err := readCommandLink(fd, name)
	if errors.Is(err, unix.ENOENT) || errors.Is(err, unix.EINVAL) {
		return nil
	}
	if err != nil {
		return err
	}
	if actual != target {
		return nil
	}
	return unix.Unlinkat(fd, name, 0)
}

func removeCommandLinks(r *installrecord.Record) error {
	directory, err := commandDirectory(r)
	if err != nil {
		return err
	}
	if err = checkManaged(directory, r.OwnerUID, nil); err != nil {
		return err
	}
	dir, err := openDir(directory, false)
	if errors.Is(err, unix.ENOENT) {
		return nil
	}
	if err != nil {
		return err
	}
	defer dir.Close()
	for _, name := range commandNames {
		if err := removeCommandLink(int(dir.Fd()), name, filepath.Join(r.Paths.Program, "bin", name)); err != nil {
			return err
		}
	}
	return dir.Sync()
}

const pathMarker = "# Molr command PATH"
const shellPath = pathMarker + `
case ":$PATH:" in
  *":$HOME/.local/bin:"*) ;;
  *) export PATH="$HOME/.local/bin:$PATH" ;;
esac
# End Molr command PATH
`
const fishPath = pathMarker + `
if not contains -- "$HOME/.local/bin" $PATH
  set -gx PATH "$HOME/.local/bin" $PATH
end
# End Molr command PATH
`

func shellProfiles(home, shell, zdotdir, configHome string) ([]string, string, error) {
	switch filepath.Base(shell) {
	case "bash":
		login := filepath.Join(home, ".profile")
		for _, name := range []string{".bash_profile", ".bash_login"} {
			path := filepath.Join(home, name)
			if _, err := os.Lstat(path); err == nil {
				login = path
				break
			} else if !os.IsNotExist(err) {
				return nil, "", err
			}
		}
		return []string{login, filepath.Join(home, ".bashrc")}, shellPath, nil
	case "zsh":
		if zdotdir == "" {
			zdotdir = home
		}
		return []string{filepath.Join(zdotdir, ".zprofile"), filepath.Join(zdotdir, ".zshrc")}, shellPath, nil
	case "fish":
		if configHome == "" {
			configHome = filepath.Join(home, ".config")
		}
		return []string{filepath.Join(configHome, "fish", "config.fish")}, fishPath, nil
	case "sh", "dash", "ksh":
		return []string{filepath.Join(home, ".profile")}, shellPath, nil
	default:
		return nil, "", fmt.Errorf("configure PATH in your shell startup file: unsupported shell %q", shell)
	}
}

// Append without replacing user files, following symlinks or rewriting their permissions.
func appendShellPath(path, block string, uid int) error {
	if err := checkManaged(path, uid, nil); err != nil {
		return err
	}
	dir, err := openDir(filepath.Dir(path), true)
	if err != nil {
		return err
	}
	defer dir.Close()
	fd, err := unix.Openat(int(dir.Fd()), filepath.Base(path), unix.O_RDWR|unix.O_APPEND|unix.O_CREAT|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0644)
	if err != nil {
		return err
	}
	f := os.NewFile(uintptr(fd), path)
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	st := info.Sys().(*syscall.Stat_t)
	if !info.Mode().IsRegular() || info.Size() > 1<<20 || int(st.Uid) != uid || st.Nlink != 1 || info.Mode().Perm()&0022 != 0 {
		return fmt.Errorf("shell profile must be a protected regular file owned by the installation user: %s", path)
	}
	if err = unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		return err
	}
	data, err := io.ReadAll(io.LimitReader(f, (1<<20)+1))
	if err != nil {
		return err
	}
	if len(data) > 1<<20 {
		return fmt.Errorf("shell profile too large: %s", path)
	}
	if strings.Contains(string(data), block) {
		return nil
	}
	if strings.Contains(string(data), pathMarker) {
		return fmt.Errorf("edited Molr PATH block retained: %s", path)
	}
	if _, err = io.WriteString(f, "\n"+block); err != nil {
		return err
	}
	return f.Sync()
}

func (e *Engine) installCommands(r *installrecord.Record) {
	if e.Options.DryRun {
		return
	}
	directory, err := commandDirectory(r)
	if err == nil {
		err = installCommandLinks(r, directory)
	}
	if err != nil {
		e.UI.print("Command shortcuts unavailable: %v\nUse the instance programs in %s", err, filepath.Join(r.Paths.Program, "bin"))
		return
	}
	e.UI.print("Commands: %s and %s -> %s", filepath.Join(directory, "molr"), filepath.Join(directory, "molr-setup"), filepath.Join(r.Paths.Program, "bin"))
	for _, entry := range filepath.SplitList(os.Getenv("PATH")) {
		if entry == directory {
			return
		}
	}
	if r.System {
		e.UI.print("Add %s to your shell PATH to use these commands.", directory)
		return
	}
	paths, block, err := shellProfiles(r.Home, os.Getenv("SHELL"), os.Getenv("ZDOTDIR"), os.Getenv("XDG_CONFIG_HOME"))
	if err == nil {
		for _, path := range paths {
			if err = appendShellPath(path, block, r.OwnerUID); err != nil {
				break
			}
		}
	}
	if err != nil {
		e.UI.print("PATH setup incomplete: %v\nAdd %s to your shell PATH, or use the full command paths above.", err, directory)
		return
	}
	e.UI.print("PATH configured. Open a new terminal to use molr and molr-setup. This terminal keeps its current PATH.")
}
