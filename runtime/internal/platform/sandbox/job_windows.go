package sandbox

import (
	"fmt"
	"golang.org/x/sys/windows"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
	"unsafe"
)

func windowsShell() string {
	return filepath.Join(os.Getenv("SystemRoot"), "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
}

// Start suspended: no user code can create a child before Job membership is set.
// Closing the non-inheritable Job handle also kills descendants on Runtime exit.
func startWindowsJob(cmd *exec.Cmd) (windows.Handle, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return 0, err
	}
	ok := false
	defer func() {
		if !ok {
			windows.CloseHandle(job)
		}
	}()
	limits := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	limits.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err = windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&limits)), uint32(unsafe.Sizeof(limits))); err != nil {
		return 0, err
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_SUSPENDED | windows.CREATE_NO_WINDOW, HideWindow: true}
	if err = cmd.Start(); err != nil {
		return 0, err
	}
	defer func() {
		if !ok {
			cmd.Process.Kill()
			cmd.Wait()
		}
	}()
	process, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(cmd.Process.Pid))
	if err != nil {
		return 0, err
	}
	defer windows.CloseHandle(process)
	if err = windows.AssignProcessToJobObject(job, process); err != nil {
		return 0, err
	}
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPTHREAD, 0)
	if err != nil {
		return 0, err
	}
	defer windows.CloseHandle(snapshot)
	entry := windows.ThreadEntry32{Size: uint32(unsafe.Sizeof(windows.ThreadEntry32{}))}
	for err = windows.Thread32First(snapshot, &entry); err == nil; err = windows.Thread32Next(snapshot, &entry) {
		if entry.OwnerProcessID != uint32(cmd.Process.Pid) {
			continue
		}
		thread, err := windows.OpenThread(windows.THREAD_SUSPEND_RESUME, false, entry.ThreadID)
		if err != nil {
			return 0, err
		}
		_, err = windows.ResumeThread(thread)
		windows.CloseHandle(thread)
		if err != nil {
			return 0, err
		}
		ok = true
		return job, nil
	}
	return 0, fmt.Errorf("suspended process thread unavailable: %w", err)
}
func terminateWindowsJob(job windows.Handle) error {
	if err := windows.TerminateJobObject(job, 125); err != nil {
		return err
	}
	var info struct {
		TotalUserTime, TotalKernelTime, ThisPeriodUserTime, ThisPeriodKernelTime       int64
		TotalPageFaultCount, TotalProcesses, ActiveProcesses, TotalTerminatedProcesses uint32
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		if err := windows.QueryInformationJobObject(job, windows.JobObjectBasicAccountingInformation, uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)), nil); err != nil {
			return err
		}
		if info.ActiveProcesses == 0 {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("Windows Job still has active descendants")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
func closeWindowsJob(job windows.Handle) { windows.CloseHandle(job) }
