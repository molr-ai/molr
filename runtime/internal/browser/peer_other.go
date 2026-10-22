//go:build !darwin && !linux

package browser

import (
	"fmt"
	"net"
)

func verifyPeer(_ net.Conn, expectedPID int) error {
	if expectedPID != 0 {
		return fmt.Errorf("daemon peer verification unavailable on this platform")
	}
	return nil
}
