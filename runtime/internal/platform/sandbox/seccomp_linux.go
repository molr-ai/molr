//go:build linux && (amd64 || arm64)

package sandbox

import (
	"bytes"
	"encoding/binary"
	"golang.org/x/sys/unix"
	"runtime"
	"unsafe"
)

func filters(shell bool) []unix.SockFilter {
	arch := uint32(unix.AUDIT_ARCH_X86_64)
	if runtime.GOARCH == "arm64" {
		arch = unix.AUDIT_ARCH_AARCH64
	}
	load := func(offset uint32) unix.SockFilter {
		return unix.SockFilter{Code: unix.BPF_LD | unix.BPF_W | unix.BPF_ABS, K: offset}
	}
	eq := func(n uint32) unix.SockFilter {
		return unix.SockFilter{Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, Jf: 1, K: n}
	}
	deny := unix.SockFilter{Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_ERRNO | uint32(unix.EPERM)}
	f := []unix.SockFilter{load(4), {Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, Jt: 1, K: arch}, {Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_KILL_PROCESS}, load(0)}
	// Reject x32/unknown syscall ABI instead of letting alternate numbers bypass rules.
	if runtime.GOARCH == "amd64" {
		f = append(f, unix.SockFilter{Code: unix.BPF_JMP | unix.BPF_JGE | unix.BPF_K, Jf: 1, K: 0x40000000}, deny)
	}
	for _, n := range []uint32{unix.SYS_QUOTACTL, unix.SYS_QUOTACTL_FD, unix.SYS_MOUNT, unix.SYS_UMOUNT2, unix.SYS_PIVOT_ROOT, unix.SYS_PTRACE, unix.SYS_PROCESS_VM_READV, unix.SYS_PROCESS_VM_WRITEV, unix.SYS_PIDFD_GETFD, unix.SYS_BPF, unix.SYS_PERF_EVENT_OPEN, unix.SYS_USERFAULTFD, unix.SYS_IO_URING_SETUP, unix.SYS_IO_URING_ENTER, unix.SYS_IO_URING_REGISTER, unix.SYS_SETNS, unix.SYS_UNSHARE, unix.SYS_OPEN_BY_HANDLE_AT, unix.SYS_NAME_TO_HANDLE_AT, unix.SYS_FSOPEN, unix.SYS_FSCONFIG, unix.SYS_FSMOUNT, unix.SYS_MOVE_MOUNT, unix.SYS_OPEN_TREE, unix.SYS_MOUNT_SETATTR, unix.SYS_KEXEC_LOAD, unix.SYS_REBOOT, unix.SYS_INIT_MODULE, unix.SYS_FINIT_MODULE, unix.SYS_DELETE_MODULE, unix.SYS_ADD_KEY, unix.SYS_REQUEST_KEY, unix.SYS_KEYCTL} {
		f = append(f, eq(n), deny)
	}
	// Prevent a workspace owner from clearing project inheritance or moving an
	// inode to another quota project through filesystem ioctls.
	f = append(f, unix.SockFilter{Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, Jf: 8, K: unix.SYS_IOCTL}, load(24), eq(0x401c5820), deny, eq(0x40086602), deny, eq(0x40046602), deny, load(0))
	// clone3's pointer arguments cannot be safely inspected by classic BPF. ENOSYS
	// lets libc fall back to clone, whose namespace flags we can inspect.
	f = append(f, eq(unix.SYS_CLONE3), unix.SockFilter{Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_ERRNO | uint32(unix.ENOSYS)})
	f = append(f, unix.SockFilter{Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, Jf: 4, K: unix.SYS_CLONE}, load(16), unix.SockFilter{Code: unix.BPF_JMP | unix.BPF_JSET | unix.BPF_K, Jf: 1, K: unix.CLONE_NEWNS | unix.CLONE_NEWUSER | unix.CLONE_NEWNET | unix.CLONE_NEWPID | unix.CLONE_NEWIPC | unix.CLONE_NEWUTS | unix.CLONE_NEWCGROUP}, deny, load(0))
	// Restrict address families, not just Unix sockets: e.g. VSOCK must not
	// become an alternate host transport outside the isolated IP namespace.
	for _, n := range []uint32{unix.SYS_SOCKET, unix.SYS_SOCKETPAIR} {
		families := []uint32{unix.AF_INET, unix.AF_INET6}
		// Connected anonymous stream pairs support libcurl resolver notification.
		// socket(AF_UNIX) stays forbidden. Datagram pairs must also be denied:
		// sendto on them could address a filesystem socket outside the pair.
		if !shell || n == unix.SYS_SOCKETPAIR {
			families = append(families, unix.AF_UNIX)
		}
		if shell && n == unix.SYS_SOCKETPAIR {
			families = []uint32{unix.AF_UNIX}
		}
		block := []unix.SockFilter{load(16)}
		for i, family := range families {
			block = append(block, unix.SockFilter{Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, Jt: uint8(len(families) - i), K: family})
		}
		block = append(block, deny)
		if shell && n == unix.SYS_SOCKETPAIR {
			block = append(block, load(24),
				unix.SockFilter{Code: unix.BPF_ALU | unix.BPF_AND | unix.BPF_K, K: ^uint32(unix.SOCK_CLOEXEC | unix.SOCK_NONBLOCK)},
				unix.SockFilter{Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, Jt: 1, K: unix.SOCK_STREAM}, deny)
		}
		block = append(block, load(0))
		f = append(f, unix.SockFilter{Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, Jf: uint8(len(block)), K: n})
		f = append(f, block...)
	}
	f = append(f, unix.SockFilter{Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_ALLOW})
	return f
}
func filterBytes() []byte {
	var b bytes.Buffer
	_ = binary.Write(&b, binary.LittleEndian, filters(false))
	return b.Bytes()
}
func restrictShell() error {
	f := filters(true)
	p := unix.SockFprog{Len: uint16(len(f)), Filter: &f[0]}
	if err := unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0); err != nil {
		return err
	}
	_, _, errno := unix.Syscall(unix.SYS_PRCTL, unix.PR_SET_SECCOMP, unix.SECCOMP_MODE_FILTER, uintptr(unsafe.Pointer(&p)))
	runtime.KeepAlive(f)
	if errno != 0 {
		return errno
	}
	return nil
}
