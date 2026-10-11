//go:build linux && (amd64 || arm64)

package sandbox

import (
	"encoding/json"
	"fmt"
	"golang.org/x/sys/unix"
	"io"
	"net"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"syscall"
)

type helperConfig struct {
	GPU               bool
	NoWorkspace       bool
	TTY               bool
	HostTools         bool
	Script, Directory string
	MaxFileBytes      int64
	CPUPercent        int
}

// HelperMain is a private, non-privileged entry point. Calling it on the host
// never installs a sandbox or grants authority; only Runner constructs one.
func HelperMain(args []string) int {
	runtime.GOMAXPROCS(2)
	if len(args) == 2 && args[0] == "forward-dial" {
		return forwardDialHelper(args[1])
	}
	if len(args) == 3 && args[0] == "network-probe" {
		return networkProbeClient(args[1], args[2])
	}
	if len(args) != 1 {
		return 125
	}
	if unix.Setrlimit(unix.RLIMIT_CORE, &unix.Rlimit{Cur: 0, Max: 0}) != nil || unix.Setrlimit(unix.RLIMIT_NOFILE, &unix.Rlimit{Cur: 1024, Max: 1024}) != nil {
		return 125
	}
	if args[0] == "network-keeper" || args[0] == "network-setup" || args[0] == "network-gateway" {
		return networkHelper(args[0])
	}
	data, err := os.ReadFile("/molr/launch.json")
	if err != nil {
		return 125
	}
	var config helperConfig
	if len(data) > 128<<10 || json.Unmarshal(data, &config) != nil {
		return 125
	}
	if config.MaxFileBytes <= 0 || unix.Setrlimit(unix.RLIMIT_FSIZE, &unix.Rlimit{Cur: uint64(config.MaxFileBytes), Max: uint64(config.MaxFileBytes)}) != nil {
		return 125
	}
	switch args[0] {
	case "exec":
		runtime.LockOSThread()
		if config.GPU {
			err = restrictGPUShell(3)
			_ = unix.Close(3)
		} else {
			err = restrictShell()
		}
		if err != nil {
			return 125
		}
		path := "/usr/bin:/bin"
		if config.HostTools {
			path = hostToolPath
		}
		home := "/work"
		if config.NoWorkspace {
			home = "/tmp"
		}
		env := []string{"PATH=" + path, "HOME=" + home, "TMPDIR=/tmp", "LANG=C.UTF-8", "GOMAXPROCS=" + strconv.Itoa(max(1, (config.CPUPercent+99)/100))}
		shell := "/bin/sh"
		if config.HostTools {
			var appletsFirst bool
			shell, appletsFirst, err = selectHostShell(env)
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				return 125
			}
			if appletsFirst {
				fmt.Fprintln(os.Stderr, "molr: using BusyBox shell fallback; system-tool priority is not guaranteed: built-in applets may take precedence over PATH. Use absolute paths for host tools.")
			}
		}
		argv := []string{"sh", "-c", config.Script}
		if config.TTY {
			env = append(env, "TERM=xterm-256color", "ENV="+home+"/.profile")
			argv = []string{"sh", "-i"}
			if config.Script != "exec /bin/sh -i" {
				argv = append(argv, "-c", config.Script)
			}
		}
		if syscall.Exec(shell, argv, env) != nil {
			return 125
		}
	case "supervise":
		cmd := exec.Command("/molr/helper", "sandbox-helper", "exec")
		cmd.Dir = config.Directory
		cmd.Env = []string{}
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if config.TTY {
			cmd.Stdin = os.Stdin
			// The controlling terminal was acquired before entering the PID
			// namespace; its inherited process group may appear here as 0.
			// Foreground creates a child process group in this namespace and
			// assigns the terminal before exec, with SIGTTOU still blocked.
			// This gives every shell a valid initial group to restore on exit.
			cmd.SysProcAttr = &syscall.SysProcAttr{Foreground: true, Ctty: int(os.Stdin.Fd())}
		}
		if config.GPU {
			pair, socketErr := unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM|unix.SOCK_CLOEXEC, 0)
			if socketErr != nil {
				return 125
			}
			parent := os.NewFile(uintptr(pair[0]), "gpu-listener-parent")
			child := os.NewFile(uintptr(pair[1]), "gpu-listener-child")
			defer parent.Close()
			defer child.Close()
			cmd.ExtraFiles = []*os.File{child}
			if err = cmd.Start(); err != nil {
				return 125
			}
			child.Close()
			listener, receiveErr := receiveGPUListener(int(parent.Fd()))
			parent.Close()
			if receiveErr != nil {
				_ = cmd.Process.Kill()
				_ = cmd.Wait()
				return 125
			}
			defer unix.Close(listener)
			go serveGPUSockets(listener)
			err = cmd.Wait()
		} else {
			err = cmd.Run()
		}
		if err != nil {
			if x, ok := err.(*exec.ExitError); ok {
				return x.ExitCode()
			}
			fmt.Fprintln(os.Stderr, "sandbox shell startup failed")
			return 125
		}
		return 0
	}
	return 125
}
func pipe(a, b net.Conn) {
	done := make(chan struct{})
	copyHalf := func(dst, src net.Conn) {
		if _, err := io.Copy(dst, src); err != nil {
			a.Close()
			b.Close()
			return
		}
		if half, ok := dst.(interface{ CloseWrite() error }); ok {
			_ = half.CloseWrite()
		} else {
			dst.Close()
		}
	}
	go func() { copyHalf(a, b); close(done) }()
	copyHalf(b, a)
	<-done
	a.Close()
	b.Close()
}
