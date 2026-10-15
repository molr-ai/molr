//go:build linux && (amd64 || arm64)

package sandbox

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// The connected socket retains its network namespace when transferred to the
// parent. No caller-controlled path, address, or PID enters this helper.
func forwardDialHelper(value string) int {
	port, err := strconv.Atoi(value)
	if err != nil || port < 1 || port > 65535 {
		return 125
	}
	if err := restrictShell(); err != nil {
		return 125
	}
	conn, err := net.DialTimeout("tcp4", net.JoinHostPort("127.0.0.1", value), 3*time.Second)
	if err != nil {
		return 125
	}
	defer conn.Close()
	f, err := conn.(*net.TCPConn).File()
	if err != nil {
		return 125
	}
	defer f.Close()
	if unix.Sendmsg(3, []byte("TCP"), unix.UnixRights(int(f.Fd())), nil, 0) != nil {
		return 125
	}
	return 0
}

func (n *networkSession) duplicateNamespaces() (*os.File, *os.File, error) {
	n.forwardMu.Lock()
	defer n.forwardMu.Unlock()
	if n.closed.Load() {
		return nil, nil, fmt.Errorf("execution network closed")
	}
	u, err := unix.FcntlInt(n.user.Fd(), unix.F_DUPFD_CLOEXEC, 0)
	if err != nil {
		return nil, nil, err
	}
	v, err := unix.FcntlInt(n.network.Fd(), unix.F_DUPFD_CLOEXEC, 0)
	if err != nil {
		unix.Close(u)
		return nil, nil, err
	}
	return os.NewFile(uintptr(u), "forward-user"), os.NewFile(uintptr(v), "forward-net"), nil
}

func (n *networkSession) dialForward(ctx context.Context, helper string, port int) (net.Conn, error) {
	user, network, err := n.duplicateNamespaces()
	if err != nil {
		return nil, err
	}
	defer user.Close()
	defer network.Close()
	a, b, err := socketPair()
	if err != nil {
		return nil, err
	}
	defer b.Close()
	control, err := net.FileConn(a)
	a.Close()
	if err != nil {
		return nil, err
	}
	defer control.Close()
	run, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	cmd := exec.CommandContext(run, "/usr/bin/nsenter", "--user=/proc/self/fd/4", "--net=/proc/self/fd/5", "--preserve-credentials", "--", helper, "sandbox-helper", "forward-dial", strconv.Itoa(port))
	cmd.ExtraFiles = []*os.File{b, user, network}
	cmd.Env = []string{"GOMAXPROCS=2"}
	// Use the same bounded auxiliary cgroup as the network keeper.
	n.forwardMu.Lock()
	if n.closed.Load() {
		n.forwardMu.Unlock()
		return nil, fmt.Errorf("execution network closed")
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{UseCgroupFD: true, CgroupFD: int(n.group.file.Fd()), Pdeathsig: syscall.SIGKILL}
	err = cmd.Start()
	n.forwardMu.Unlock()
	if err != nil {
		return nil, err
	}
	if err = cmd.Wait(); err != nil {
		return nil, err
	}
	control.SetReadDeadline(time.Now().Add(time.Second))
	data, rights := make([]byte, 8), make([]byte, unix.CmsgSpace(4))
	count, size, flags, _, err := control.(*net.UnixConn).ReadMsgUnix(data, rights)
	if err != nil {
		return nil, err
	}
	messages, err := unix.ParseSocketControlMessage(rights[:size])
	if err != nil {
		return nil, err
	}
	var fds []int
	for _, message := range messages {
		received, err := unix.ParseUnixRights(&message)
		if err != nil {
			for _, fd := range fds {
				unix.Close(fd)
			}
			return nil, err
		}
		fds = append(fds, received...)
	}
	defer func() {
		for _, fd := range fds {
			unix.Close(fd)
		}
	}()
	if len(fds) != 1 || flags&(unix.MSG_CTRUNC|unix.MSG_TRUNC) != 0 || string(data[:count]) != "TCP" {
		return nil, fmt.Errorf("invalid forward socket")
	}
	unix.CloseOnExec(fds[0])
	f := os.NewFile(uintptr(fds[0]), "forward-tcp")
	conn, err := net.FileConn(f)
	f.Close()
	fds = nil
	return conn, err
}
