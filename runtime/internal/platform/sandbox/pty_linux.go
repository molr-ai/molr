//go:build linux && (amd64 || arm64)

package sandbox

import (
	"context"
	"fmt"
	"golang.org/x/sys/unix"
	"molr.ai/runtime/internal/core"
	"os"
	"time"
)

func openPTY() (*os.File, *os.File, error) {
	fd, err := unix.Open("/dev/ptmx", unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOCTTY|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, nil, err
	}
	master := os.NewFile(uintptr(fd), "pty")
	if err = unix.IoctlSetPointerInt(fd, unix.TIOCSPTLCK, 0); err != nil {
		master.Close()
		return nil, nil, err
	}
	n, err := unix.IoctlGetInt(fd, unix.TIOCGPTN)
	if err != nil {
		master.Close()
		return nil, nil, err
	}
	slave, err := os.OpenFile(fmt.Sprintf("/dev/pts/%d", n), os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		master.Close()
		return nil, nil, err
	}
	if err = unix.IoctlSetWinsize(fd, unix.TIOCSWINSZ, &unix.Winsize{Row: 24, Col: 100}); err != nil {
		master.Close()
		slave.Close()
		return nil, nil, err
	}
	return master, slave, nil
}

// Terminal controls only this execution's PTY. Interrupt sends the terminal's
// interrupt character to the foreground process group; Cancel kills the cgroup.
func (r *Runner) Terminal(ctx context.Context, e core.Execution, data string, rows, cols uint16) error {
	if err := e.Validate(); err != nil {
		return err
	}
	if len(data) > 8192 || rows > 500 || cols > 500 || (rows == 0) != (cols == 0) {
		return core.Invalid("terminal", "invalid bounded input or size")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	p := r.active[key(e)]
	r.mu.Unlock()
	if p == nil || p.operation != string(e.Principal.ExecutionID)+"/"+string(e.OperationID) || p.terminal == nil {
		return core.NewError(core.NotFound, "terminal is not running")
	}
	if rows > 0 && cols > 0 {
		if err := unix.IoctlSetWinsize(int(p.terminal.Fd()), unix.TIOCSWINSZ, &unix.Winsize{Row: rows, Col: cols}); err != nil {
			return err
		}
	}
	// Bound writes even when the application does not consume input.
	p.terminal.SetWriteDeadline(time.Now().Add(time.Second))
	_, err := p.terminal.WriteString(data)
	return err
}
