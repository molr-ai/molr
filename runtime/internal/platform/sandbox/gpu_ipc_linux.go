//go:build linux && (amd64 || arm64)

package sandbox

import (
	"encoding/binary"
	"errors"
	"os"
	"runtime"
	"strconv"
	"strings"
	"unsafe"

	"golang.org/x/sys/unix"
)

// GPU drivers need abstract Unix SEQPACKET sockets. All bind/connect syscalls in a
// GPU shell are emulated by its trusted supervisor. Never use USER_NOTIF_CONTINUE:
// another thread could replace the descriptor or sockaddr after validation.
// The supervisor runs in the same private network namespace, so abstract Unix
// sockets remain execution-local. Filesystem Unix sockets are always denied.
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
func restrictGPUShell(channel int) error {
	f := filtersGPU(true, true)
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
func receiveGPUListener(channel int) (int, error) {
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
func serveGPUSockets(listener int) {
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
			go func() { defer func() { <-slots }(); answerGPUSocket(listener, n) }()
		default:
			response := connectResponse{ID: n.ID, Error: -int32(unix.EAGAIN)}
			_ = notifyIOCTL(listener, notifySend, unsafe.Pointer(&response))
		}
	}
}
func answerGPUSocket(listener int, n connectNotification) {
	err := emulateGPUSocket(listener, n)
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
func gpuConnectAddressAllowed(domain int, address []byte) bool {
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
func emulateGPUSocket(listener int, n connectNotification) error {
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
	if !gpuConnectAddressAllowed(domain, address) {
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
