package sandbox

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"molr.ai/runtime/internal/core"
)

type terminalCommand struct {
	Data       string
	Rows, Cols uint16
}

func terminalAPIAvailable() bool {
	dll := windows.NewLazySystemDLL("kernel32.dll")
	for _, name := range []string{"CreatePseudoConsole", "ResizePseudoConsole", "ClosePseudoConsole"} {
		if dll.NewProc(name).Find() != nil {
			return false
		}
	}
	return true
}
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
	if p == nil || p.operation != string(e.Principal.ExecutionID)+"/"+string(e.OperationID) || p.terminalControl == nil {
		return core.NewError(core.NotFound, "terminal is not running")
	}
	return p.terminalControl(ctx, data, rows, cols)
}
func terminalPipe(cmd *exec.Cmd, p *process) (func() error, func(), error) {
	spec, err := io.ReadAll(io.LimitReader(cmd.Stdin, (256<<10)+1))
	if err != nil || len(spec) > 256<<10 {
		return nil, nil, fmt.Errorf("invalid terminal startup: %v", err)
	}
	reader, writer, err := terminalControlPipe()
	if err != nil {
		return nil, nil, err
	}
	cmd.Stdin = reader
	// A fresh hidden console clears the ignored Ctrl-C state inherited from SSH.
	// The helper resets it explicitly before creating the ConPTY child.
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_CONSOLE, HideWindow: true}
	var mu sync.Mutex
	p.terminalControl = func(ctx context.Context, data string, rows, cols uint16) error {
		mu.Lock()
		defer mu.Unlock()
		if err := ctx.Err(); err != nil {
			return err
		}
		select {
		case <-p.done:
			return core.NewError(core.NotFound, "terminal has ended")
		default:
		}
		raw, err := json.Marshal(terminalCommand{Data: data, Rows: rows, Cols: cols})
		if err != nil {
			return err
		}
		return writer.write(ctx, append(raw, '\n'), time.Second)
	}
	initialize := func() error {
		reader.Close()
		return writer.write(context.Background(), append(spec, '\n'), 5*time.Second)
	}
	closePipe := func() { reader.Close(); writer.close() }
	return initialize, closePipe, nil
}
func runTerminalContainer(spec containerLaunch, commands *bufio.Reader) (int, error) {
	if !terminalAPIAvailable() {
		return 125, unavailable("terminal_platform_unsupported")
	}
	var inputRead, inputWrite, outputRead, outputWrite windows.Handle
	if err := windows.CreatePipe(&inputRead, &inputWrite, nil, 0); err != nil {
		return 125, err
	}
	defer func() {
		if inputRead != 0 {
			windows.CloseHandle(inputRead)
		}
	}()
	input := os.NewFile(uintptr(inputWrite), "conpty-input")
	defer input.Close()
	if err := windows.CreatePipe(&outputRead, &outputWrite, nil, 0); err != nil {
		return 125, err
	}
	defer func() {
		if outputWrite != 0 {
			windows.CloseHandle(outputWrite)
		}
	}()
	output := os.NewFile(uintptr(outputRead), "conpty-output")
	defer output.Close()
	var console windows.Handle
	if err := windows.CreatePseudoConsole(windows.Coord{X: 80, Y: 24}, inputRead, outputWrite, 0, &console); err != nil {
		return 125, err
	}
	outputDone := make(chan error, 1)
	go func() { _, err := io.Copy(os.Stdout, output); outputDone <- err }()
	defer func() {
		input.Close()
		if console != 0 {
			windows.ClosePseudoConsole(console)
		}
	}()
	process, dispose, err := createContainerProcess(spec, console)
	if err != nil {
		return 125, err
	}
	windows.CloseHandle(inputRead)
	inputRead = 0
	windows.CloseHandle(outputWrite)
	outputWrite = 0
	controlDone := make(chan error, 1)
	var terminalMu sync.Mutex
	closed := false
	go func() {
		for {
			raw, err := commands.ReadSlice('\n')
			if err != nil {
				controlDone <- err
				return
			}
			var command terminalCommand
			if len(raw) > 64<<10 || json.Unmarshal(raw, &command) != nil || len(command.Data) > 8192 || command.Rows > 500 || command.Cols > 500 || (command.Rows == 0) != (command.Cols == 0) {
				controlDone <- fmt.Errorf("invalid terminal control frame")
				return
			}
			terminalMu.Lock()
			if closed {
				terminalMu.Unlock()
				return
			}
			if command.Rows != 0 {
				err = windows.ResizePseudoConsole(console, windows.Coord{X: int16(command.Cols), Y: int16(command.Rows)})
			}
			terminalMu.Unlock()
			if err != nil {
				controlDone <- err
				return
			}
			if command.Data != "" {
				if _, err = input.WriteString(command.Data); err != nil {
					controlDone <- err
					return
				}
			}
		}
	}()
	outputFinished := false
	var code uint32
	poll := time.NewTicker(50 * time.Millisecond)
	defer poll.Stop()
wait:
	for {
		select {
		case <-poll.C:
			err = windows.GetExitCodeProcess(process.Process, &code)
			if err != nil || code != 259 {
				break wait
			}
		case err = <-controlDone:
			break wait
		case err = <-outputDone:
			outputFinished = true
			break wait
		}
	}
	if err == nil {
		err = windows.GetExitCodeProcess(process.Process, &code)
	}
	if code == 259 {
		code = 125
	}
	// Stop the entire inner job before closing ConPTY. Detached grandchildren
	// cannot keep its pipes alive and prevent the helper from exiting.
	err = errors.Join(err, dispose())
	os.Stdin.Close()
	input.Close()
	terminalMu.Lock()
	closed = true
	windows.ClosePseudoConsole(console)
	console = 0
	terminalMu.Unlock()
	if !outputFinished {
		select {
		case e := <-outputDone:
			err = errors.Join(err, e)
		case <-time.After(5 * time.Second):
			windows.CancelIoEx(outputRead, nil)
			output.Close()
			err = errors.Join(err, fmt.Errorf("terminal output did not close"))
		}
	}
	return int(code), err
}

// The parent endpoint uses overlapped writes to bound a stalled helper.
// ConPTY itself still receives separate synchronous pipes.
func terminalControlPipe() (*os.File, *terminalWriter, error) {
	name, err := randomContainerName()
	if err != nil {
		return nil, nil, err
	}
	token, err := windows.OpenCurrentProcessToken()
	if err != nil {
		return nil, nil, err
	}
	defer token.Close()
	user, err := token.GetTokenUser()
	if err != nil {
		return nil, nil, err
	}
	descriptor, err := windows.SecurityDescriptorFromString("D:P(A;;GA;;;" + user.User.Sid.String() + ")(A;;GA;;;SY)")
	if err != nil {
		return nil, nil, err
	}
	security := windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), SecurityDescriptor: descriptor}
	path, _ := windows.UTF16PtrFromString(`\\.\pipe\` + name)
	server, err := windows.CreateNamedPipe(path, windows.PIPE_ACCESS_OUTBOUND|windows.FILE_FLAG_OVERLAPPED|windows.FILE_FLAG_FIRST_PIPE_INSTANCE, windows.PIPE_TYPE_BYTE|windows.PIPE_WAIT|windows.PIPE_REJECT_REMOTE_CLIENTS, 1, 65536, 65536, 0, &security)
	if err != nil {
		return nil, nil, err
	}
	client, err := windows.CreateFile(path, windows.GENERIC_READ, 0, nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		windows.CloseHandle(server)
		return nil, nil, err
	}
	if err = windows.ConnectNamedPipe(server, nil); err != nil && err != windows.ERROR_PIPE_CONNECTED {
		windows.CloseHandle(server)
		windows.CloseHandle(client)
		return nil, nil, err
	}
	return os.NewFile(uintptr(client), "terminal-control-read"), &terminalWriter{handle: server}, nil
}

// os.NewFile does not register Windows named pipes with the Go poller.
// Manage overlapped operations directly, and finish cancellation before the
// buffer or OVERLAPPED can leave scope.
type terminalWriter struct {
	mu     sync.Mutex
	handle windows.Handle
}

func (w *terminalWriter) close() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.handle != 0 {
		windows.CloseHandle(w.handle)
		w.handle = 0
	}
}
func (w *terminalWriter) write(ctx context.Context, data []byte, timeout time.Duration) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.handle == 0 {
		return os.ErrClosed
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	event, err := windows.CreateEvent(nil, 1, 0, nil)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(event)
	for len(data) > 0 {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := windows.ResetEvent(event); err != nil {
			return err
		}
		overlap := windows.Overlapped{HEvent: event}
		var written uint32
		err := windows.WriteFile(w.handle, data, &written, &overlap)
		if err == windows.ERROR_IO_PENDING {
			for {
				status, waitErr := windows.WaitForSingleObject(event, 25)
				if waitErr != nil || ctx.Err() != nil {
					windows.CancelIoEx(w.handle, &overlap)
					windows.GetOverlappedResult(w.handle, &overlap, &written, true)
					return errors.Join(waitErr, ctx.Err())
				}
				if status == windows.WAIT_OBJECT_0 {
					err = windows.GetOverlappedResult(w.handle, &overlap, &written, false)
					break
				}
			}
		}
		if err != nil {
			return err
		}
		if written == 0 {
			return io.ErrShortWrite
		}
		data = data[written:]
	}
	return nil
}
