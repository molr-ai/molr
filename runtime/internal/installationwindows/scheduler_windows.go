package installationwindows

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"golang.org/x/sys/windows"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"unicode/utf16"
)

var getOEMCP = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetOEMCP")

func schedulerCodePage() uint32 {
	if page, err := windows.GetConsoleOutputCP(); err == nil && page != 0 {
		return page
	}
	// Electron normally supplies pipes without an attached console. Legacy
	// Windows console tools use the system OEM page in that case.
	page, _, _ := getOEMCP.Call()
	return uint32(page)
}
func codePageText(raw []byte, page uint32) ([]byte, error) {
	if len(raw) == 0 {
		return raw, nil
	}
	count, err := windows.MultiByteToWideChar(page, 0, &raw[0], int32(len(raw)), nil, 0)
	if err != nil {
		return nil, err
	}
	units := make([]uint16, count)
	if _, err = windows.MultiByteToWideChar(page, 0, &raw[0], int32(len(raw)), &units[0], count); err != nil {
		return nil, err
	}
	return []byte(string(utf16.Decode(units))), nil
}
func scheduler(ctx context.Context, args ...string) ([]byte, error) {
	if len(args) == 4 && args[0] == "/Query" && args[1] == "/TN" && args[3] == "/XML" {
		// schtasks can replace characters outside the OEM code page before its
		// output reaches us. Read the scheduler's Unicode XML through COM instead.
		script := "$ErrorActionPreference='Stop'; $ProgressPreference='SilentlyContinue'; [Console]::OutputEncoding=[Text.UTF8Encoding]::new($false); $service=New-Object -ComObject Schedule.Service; $service.Connect(); [Console]::Write($service.GetFolder('\\').GetTask('" + strings.ReplaceAll(args[2], "'", "''") + "').Xml)"
		units := utf16.Encode([]rune(script))
		encoded := make([]byte, len(units)*2)
		for i, unit := range units {
			binary.LittleEndian.PutUint16(encoded[i*2:], unit)
		}
		exe := filepath.Join(os.Getenv("SystemRoot"), "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
		out, err := command(ctx, exe, "-NoLogo", "-NoProfile", "-NonInteractive", "-EncodedCommand", base64.StdEncoding.EncodeToString(encoded))
		if err != nil {
			return nil, err
		}
		return utf16Declaration.ReplaceAll(out, []byte(`${1}UTF-8${2}`)), nil
	}
	exe := filepath.Join(os.Getenv("SystemRoot"), "System32", "schtasks.exe")
	child := exec.CommandContext(ctx, exe, args...)
	child.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	var stdout, stderr bytes.Buffer
	child.Stdout, child.Stderr = &stdout, &stderr
	runErr := child.Run()
	decode := func(raw []byte) ([]byte, error) { return codePageText(raw, schedulerCodePage()) }
	out, outErr := schedulerText(stdout.Bytes(), decode)
	diagnostic, diagnosticErr := schedulerText(stderr.Bytes(), decode)
	if err := errors.Join(outErr, diagnosticErr); err != nil {
		return nil, fmt.Errorf("schtasks output decoding: %w", errors.Join(runErr, err))
	}
	if runErr != nil {
		return nil, fmt.Errorf("schtasks.exe: %w: %s", runErr, strings.TrimSpace(string(out)+string(diagnostic)))
	}
	return out, nil
}
