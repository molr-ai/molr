package browser

import (
	"fmt"
	"golang.org/x/sys/unix"
	"net"
)

func verifyPeer(conn net.Conn, expectedPID int) error {
	if expectedPID == 0 {
		return nil
	}
	socket, ok := conn.(*net.UnixConn)
	if !ok {
		return fmt.Errorf("private daemon socket required")
	}
	raw, err := socket.SyscallConn()
	if err != nil {
		return err
	}
	var peer *unix.Ucred
	var inspect error
	err = raw.Control(func(fd uintptr) { peer, inspect = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED) })
	if err != nil {
		return err
	}
	if inspect != nil {
		return inspect
	}
	if peer == nil || int(peer.Pid) != expectedPID {
		return fmt.Errorf("browser daemon identity changed")
	}
	return nil
}
