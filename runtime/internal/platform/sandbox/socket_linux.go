//go:build linux && (amd64 || arm64)

package sandbox

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"path"
	"runtime"
	"strconv"
	"strings"
	"unsafe"

	"golang.org/x/sys/unix"
)

// All untrusted bind/connect calls are emulated on a pinned descriptor and a
// copied address. Never use USER_NOTIF_CONTINUE: another thread could replace
// either argument after validation. Abstract endpoints stay in the execution's
// private network namespace; pathname endpoints must resolve in its private tmpfs.
type connectNotification struct {
	ID         uint64
	PID, Flags uint32
	Number     int32
	Arch       uint32
	IP         uint64
	Args       [6]uint64
}
type connectResponse struct {
	ID    uint64
	Value int64
	Error int32
	Flags uint32
}

const notifyReceive = 0xc0502100
const notifySend = 0xc0182101

func notifyIOCTL(fd int, request uintptr, p unsafe.Pointer) error {
	_, _, e := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), request, uintptr(p))
	if e != 0 {
		return e
	}
	return nil
}
func restrictSocketShell(channel int) error {
	f := socketFilters(true, true)
	prog := unix.SockFprog{Len: uint16(len(f)), Filter: &f[0]}
	if err := unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0); err != nil {
		return err
	}
	fd, _, e := unix.Syscall(unix.SYS_SECCOMP, unix.SECCOMP_SET_MODE_FILTER, unix.SECCOMP_FILTER_FLAG_NEW_LISTENER, uintptr(unsafe.Pointer(&prog)))
	runtime.KeepAlive(f)
	if e != 0 {
		return e
	}
	defer unix.Close(int(fd))
	return unix.Sendmsg(channel, []byte{1}, unix.UnixRights(int(fd)), nil, 0)
}
func receiveSocketListener(channel int) (int, error) {
	payload := make([]byte, 1)
	control := make([]byte, unix.CmsgSpace(4))
	n, oob, flags, _, err := unix.Recvmsg(channel, payload, control, 0)
	if err != nil {
		return -1, err
	}
	if n != 1 || flags&unix.MSG_CTRUNC != 0 {
		return -1, unix.EINVAL
	}
	messages, err := unix.ParseSocketControlMessage(control[:oob])
	if err != nil {
		return -1, err
	}
	if len(messages) != 1 {
		return -1, unix.EINVAL
	}
	fds, err := unix.ParseUnixRights(&messages[0])
	if err != nil {
		return -1, err
	}
	if len(fds) != 1 {
		for _, fd := range fds {
			unix.Close(fd)
		}
		return -1, unix.EINVAL
	}
	unix.CloseOnExec(fds[0])
	return fds[0], nil
}
func serveSockets(listener int) {
	slots := make(chan struct{}, 32)
	for {
		var n connectNotification
		if err := notifyIOCTL(listener, notifyReceive, unsafe.Pointer(&n)); err != nil {
			if errors.Is(err, unix.EINTR) || errors.Is(err, unix.ENOENT) {
				continue
			}
			return
		}
		select {
		case slots <- struct{}{}:
			go func() { defer func() { <-slots }(); answerSocket(listener, n) }()
		default:
			response := connectResponse{ID: n.ID, Error: -int32(unix.EAGAIN)}
			_ = notifyIOCTL(listener, notifySend, unsafe.Pointer(&response))
		}
	}
}
func answerSocket(listener int, n connectNotification) {
	err := emulateSocket(listener, n)
	response := connectResponse{ID: n.ID}
	if err != nil {
		e := unix.EPERM
		var value unix.Errno
		if errors.As(err, &value) {
			e = value
		}
		response.Error = -int32(e)
	}
	_ = notifyIOCTL(listener, notifySend, unsafe.Pointer(&response))
}
func validNotification(listener int, id uint64) error {
	return notifyIOCTL(listener, unix.SECCOMP_IOCTL_NOTIF_ID_VALID, unsafe.Pointer(&id))
}
func connectAddressAllowed(domain int, address []byte) bool {
	if len(address) < 2 {
		return false
	}
	family := int(binary.NativeEndian.Uint16(address))
	switch domain {
	case unix.AF_UNIX:
		return family == unix.AF_UNIX && len(address) > 2 && address[2] == 0
	case unix.AF_INET:
		return (family == unix.AF_INET && len(address) >= 16) || family == unix.AF_UNSPEC
	case unix.AF_INET6:
		return (family == unix.AF_INET6 && len(address) >= 28) || family == unix.AF_UNSPEC
	}
	return false
}
func emulateSocket(listener int, n connectNotification) error {
	if (n.Number != unix.SYS_CONNECT && n.Number != unix.SYS_BIND) || n.Args[0] > 1<<31 || n.Args[2] < 2 || n.Args[2] > 128 {
		return unix.EINVAL
	}
	if err := validNotification(listener, n.ID); err != nil {
		return err
	}
	// Notifications identify the calling thread; pidfd_open needs its process ID.
	status, err := os.ReadFile("/proc/" + strconv.Itoa(int(n.PID)) + "/status")
	if err != nil {
		return unix.ESRCH
	}
	tgid := 0
	for _, line := range strings.Split(string(status), "\n") {
		if strings.HasPrefix(line, "Tgid:") {
			tgid, _ = strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "Tgid:")))
		}
	}
	if tgid <= 0 {
		return unix.ESRCH
	}
	pidfd, err := unix.PidfdOpen(tgid, 0)
	if err != nil {
		return err
	}
	defer unix.Close(pidfd)
	socket, err := unix.PidfdGetfd(pidfd, int(n.Args[0]), 0)
	if err != nil {
		return err
	}
	defer unix.Close(socket)
	address := make([]byte, int(n.Args[2]))
	local := unix.Iovec{Base: &address[0]}
	local.SetLen(len(address))
	read, err := unix.ProcessVMReadv(int(n.PID), []unix.Iovec{local}, []unix.RemoteIovec{{Base: uintptr(n.Args[1]), Len: len(address)}}, 0)
	if err != nil {
		return err
	}
	if read != len(address) {
		return unix.EFAULT
	}
	domain, err := unix.GetsockoptInt(socket, unix.SOL_SOCKET, unix.SO_DOMAIN)
	if err != nil {
		return err
	}
	if domain == unix.AF_UNIX && len(address) > 2 && address[2] != 0 {
		translated, closePath, err := privateSocketAddress(address, n.Number == unix.SYS_BIND)
		if err != nil {
			return err
		}
		defer closePath()
		address = translated
	} else if !connectAddressAllowed(domain, address) {
		return unix.EACCES
	}
	if err := validNotification(listener, n.ID); err != nil {
		return err
	}
	_, _, e := unix.Syscall(uintptr(n.Number), uintptr(socket), uintptr(unsafe.Pointer(&address[0])), uintptr(len(address)))
	runtime.KeepAlive(address)
	if e != 0 {
		return e
	}
	return nil
}

// Resolve against the execution's immutable /tmp mount, never a caller's cwd.
// openat2 refuses symlinks, magic links, mount crossings and '..' escapes. Pin
// the existing socket for connect, or its parent for atomic bind creation. The
// /proc/self/fd address refers to this supervisor's pinned inode, not to mutable
// caller memory. Inodes from granted host directories cannot be hard-linked into
// this tmpfs. A bind leaf swapped for a symlink fails with EADDRINUSE.
func privateSocketAddress(address []byte, bind bool) ([]byte, func(), error) {
	if len(address) < 3 || binary.NativeEndian.Uint16(address) != unix.AF_UNIX {
		return nil, nil, unix.EACCES
	}
	name := string(bytes.SplitN(address[2:], []byte{0}, 2)[0])
	if !strings.HasPrefix(name, "/tmp/") {
		return nil, nil, unix.EACCES
	}
	relative := strings.TrimPrefix(name, "/tmp/")
	root, err := unix.Open("/tmp", unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, nil, err
	}
	defer unix.Close(root)
	how := unix.OpenHow{Flags: unix.O_PATH | unix.O_CLOEXEC, Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_XDEV}
	target := relative
	leaf := ""
	if bind {
		// Reject noncanonical names before splitting; path.Dir would clean '..'.
		if relative == "" || path.Clean(relative) != relative || strings.HasPrefix(relative, "../") {
			return nil, nil, unix.EACCES
		}
		target, leaf = path.Dir(relative), path.Base(relative)
		if leaf == "." || leaf == ".." {
			return nil, nil, unix.EACCES
		}
		how.Flags |= unix.O_DIRECTORY
	}
	fd, err := unix.Openat2(root, target, &how)
	if err != nil {
		return nil, nil, unix.EACCES
	}
	closePath := func() { unix.Close(fd) }
	if !bind {
		var st unix.Stat_t
		if err = unix.Fstat(fd, &st); err != nil || st.Mode&unix.S_IFMT != unix.S_IFSOCK {
			closePath()
			return nil, nil, unix.EACCES
		}
	}
	pinned := "/proc/self/fd/" + strconv.Itoa(fd)
	if bind {
		pinned += "/" + leaf
	}
	if len(pinned) >= 108 {
		closePath()
		return nil, nil, unix.ENAMETOOLONG
	}
	result := make([]byte, 2+len(pinned)+1)
	binary.NativeEndian.PutUint16(result, unix.AF_UNIX)
	copy(result[2:], pinned)
	return result, closePath, nil
}
