//go:build linux || darwin

package installation

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/sys/unix"
)

type UI struct {
	Out, Err     io.Writer
	Terminal     *os.File
	reader       *bufio.Reader
	Stage, State string
	Yes          bool
}

func newUI(mode string, dry bool, out, diagnostics io.Writer) (*UI, error) {
	u := &UI{Out: out, Err: diagnostics, Stage: "Read options", State: "No installation changes made."}
	if mode != "non-interactive" && !dry {
		if f, err := os.OpenFile("/dev/tty", os.O_RDWR, 0); err == nil {
			if _, err = unix.IoctlGetTermios(int(f.Fd()), terminalGet); err == nil {
				u.Terminal = f
				u.reader = bufio.NewReader(f)
			} else {
				f.Close()
			}
		}
	}
	if mode == "interactive" && !dry && u.Terminal == nil {
		return nil, fmt.Errorf("--interactive requires a controlling terminal; use --non-interactive for automation")
	}
	return u, nil
}
func (u *UI) close() {
	if u.Terminal != nil {
		u.Terminal.Close()
	}
}
func (u *UI) interactive() bool                { return u.Terminal != nil }
func (u *UI) print(format string, args ...any) { fmt.Fprintf(u.Out, format+"\n", args...) }
func (u *UI) step(n int, title string)         { u.Stage = title; u.print("\n[%d/5] %s", n, title) }
func (u *UI) answer(prompt string) (string, error) {
	if !u.interactive() {
		return "", fmt.Errorf("a choice is required; supply explicit options for non-interactive operation")
	}
	fmt.Fprint(u.Terminal, prompt)
	s, err := u.reader.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("no terminal response received: %w", err)
	}
	return strings.ToLower(strings.TrimSpace(s)), nil
}
func (u *UI) confirm(prompt string) (bool, error) {
	if u.Yes || !u.interactive() {
		return true, nil
	}
	s, err := u.answer(prompt + " [y/N] ")
	return s == "y" || s == "yes", err
}
func (u *UI) directory(path string) (string, error) {
	if !u.interactive() {
		return "", fmt.Errorf("directory permissions need a choice for %s; use --directory-permissions fix|ignore", path)
	}
	for {
		s, err := u.answer("Fix permissions [f, recommended], ignore and continue [i], or quit [q]? ")
		if err != nil {
			return "", err
		}
		switch s {
		case "f", "fix":
			return "fix", nil
		case "i", "ignore":
			return "ignore", nil
		case "q", "quit":
			return "", fmt.Errorf("cancelled; no directory permissions changed")
		}
	}
}
