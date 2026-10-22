//go:build (linux && (amd64 || arm64)) || darwin

package sandbox

import (
	"context"
	"golang.org/x/sys/unix"
	"molr.ai/runtime/internal/core"
	"time"
)

// Terminal controls only this execution's PTY. Interrupt sends the terminal's
// interrupt character to the foreground process group; Cancel uses the selected execution backend.
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

func directTerminalAvailable() bool {
	master, slave, err := openPTY()
	if err != nil {
		return false
	}
	master.Close()
	slave.Close()
	return true
}
