//go:build linux || darwin

package installation

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

// Credentials live only for this installation. Existing accounts are never reset.
func (e *Engine) prepareAdministrator(p *Plan) error {
	if p.Record.Mode == "sandbox" || e.Options.AutoInitAdmin {
		return nil
	}
	if p.Previous != nil && p.Previous.State != "installing" {
		if _, err := os.Stat(filepath.Join(p.Record.DataDir, "runtime.db")); err == nil {
			return nil
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	var password []byte
	var err error
	if e.Options.InitOwner != "" {
		password, err = io.ReadAll(io.LimitReader(e.Input, 1026))
		password = bytes.TrimSuffix(bytes.TrimSuffix(password, []byte("\n")), []byte("\r"))
	} else if e.UI.interactive() {
		fmt.Fprint(e.UI.Terminal, "Administrator username [owner]: ")
		name, readErr := e.UI.reader.ReadString('\n')
		if readErr != nil {
			return readErr
		}
		e.Options.InitOwner = strings.TrimSpace(name)
		if e.Options.InitOwner == "" {
			e.Options.InitOwner = "owner"
		}
		password, err = e.UI.readPassword("Administrator password (at least 12 bytes): ")
		if err == nil {
			confirmation, confirmErr := e.UI.readPassword("Confirm password: ")
			equal := bytes.Equal(password, confirmation)
			clear(confirmation)
			err = confirmErr
			if err == nil && !equal {
				err = fmt.Errorf("passwords do not match")
			}
		}
	} else {
		return fmt.Errorf("set administrator credentials with --init-owner NAME and password on stdin, or explicitly use --auto-init-admin")
	}
	if err == nil && (strings.TrimSpace(e.Options.InitOwner) != e.Options.InitOwner || len(e.Options.InitOwner) > 128 || strings.ContainsAny(e.Options.InitOwner, "\r\n")) {
		err = fmt.Errorf("administrator username must be 1 to 128 bytes without surrounding whitespace")
	}
	if err == nil && (len(password) < 12 || len(password) > 1024 || bytes.ContainsAny(password, "\r\n")) {
		err = fmt.Errorf("administrator password must be 12 to 1024 bytes on one line")
	}
	if err != nil {
		clear(password)
		return err
	}
	e.initPassword = password
	return nil
}

func (u *UI) readPassword(prompt string) ([]byte, error) {
	fd := int(u.Terminal.Fd())
	old, err := unix.IoctlGetTermios(fd, terminalGet)
	if err != nil {
		return nil, err
	}
	next := *old
	next.Lflag &^= unix.ECHO
	if err = unix.IoctlSetTermios(fd, terminalSet, &next); err != nil {
		return nil, err
	}
	defer unix.IoctlSetTermios(fd, terminalSet, old)
	fmt.Fprint(u.Terminal, prompt)
	value, err := u.reader.ReadBytes('\n')
	fmt.Fprintln(u.Terminal)
	return bytes.TrimSuffix(bytes.TrimSuffix(value, []byte("\n")), []byte("\r")), err
}
