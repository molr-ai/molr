package sandbox

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"syscall"
)

// Each trusted guardian owns one process group. Only Runtime owns the write end
// of stdin; EOF (including Runtime SIGKILL) kills the entire group. The child
// receives /dev/null, never the liveness pipe or Runtime descriptors.
type browserChildConfig struct {
	Program       string
	Args, Env     []string
	Profile, Work string
}

func browserChildMain() int {
	// Refuse to signal a process group we did not create.
	if syscall.Getpgrp() != os.Getpid() {
		return 125
	}
	input := bufio.NewReaderSize(os.Stdin, 256<<10)
	line, err := input.ReadSlice('\n')
	if err != nil {
		return 125
	}
	var cfg browserChildConfig
	if json.Unmarshal(line, &cfg) != nil || cfg.Program == "" || cfg.Profile == "" {
		return 125
	}
	clear(line)
	cmd := exec.Command("/bin/sh", append([]string{"-c", seatbeltCloseDescriptors, "molr-browser", "/usr/bin/sandbox-exec", "-p", cfg.Profile, cfg.Program}, cfg.Args...)...)
	cmd.Dir, cmd.Env = cfg.Work, cfg.Env
	cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
	// Share the guardian's session/group; a detached Chrome launch is prohibited.
	if err = cmd.Start(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 125
	}
	defer syscall.Kill(-os.Getpid(), syscall.SIGKILL)
	if json.NewEncoder(os.Stdout).Encode(cmd.Process.Pid) != nil {
		return 125
	}
	gone := make(chan struct{})
	go func() { io.Copy(io.Discard, input); close(gone) }()
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	select {
	case <-gone:
	case <-exited:
	}
	return 0
}
