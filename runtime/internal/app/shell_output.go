package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"sync"

	"molr.ai/runtime/internal/agent/tools"
	"molr.ai/runtime/internal/config"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/platform"
	"molr.ai/runtime/internal/policy"
)

// Disk capture is separate from the in-memory preview budget. If the shared
// quota or disk fails, the sink returns an error and the result explicitly says
// the capture is incomplete; it never claims that a truncated file is complete.
type shellOutputBudget struct {
	mu        sync.Mutex
	left      int64
	failed    bool
	unlimited bool
	cancel    context.CancelFunc
}
type shellOutput struct {
	file   *os.File
	budget *shellOutputBudget
	bytes  int64
	closed bool
}

func (s *shellOutput) Write(data []byte) (int, error) {
	s.budget.mu.Lock()
	defer s.budget.mu.Unlock()
	allowed := min(int64(len(data)), s.budget.left)
	n, err := s.file.Write(data[:allowed])
	s.bytes += int64(n)
	s.budget.left -= int64(n)
	if err == nil && int64(len(data)) > allowed {
		err = errors.New("shell output archive quota exceeded")
	}
	if err != nil {
		s.budget.failed = true
		if s.budget.cancel != nil {
			s.budget.cancel()
		}
	}
	return n, err
}
func (s *shellOutput) close() {
	if s.closed {
		return
	}
	s.closed = true
	err := s.file.Sync()
	closeErr := s.file.Close()
	if err != nil || closeErr != nil {
		s.budget.mu.Lock()
		s.budget.failed = true
		s.budget.mu.Unlock()
	}
}
func (s *shellService) outputFiles(ctx context.Context, e core.Execution, full bool) (*shellOutput, *shellOutput, string, string, error) {
	f := resourceFileTools{adapters: adapters{store: s.store, policy: s.policy, dataDir: s.dataDir, storageConfig: s.config, ownPort: s.ownPort}}
	root, _, err := f.resolve(ctx, e, tools.FileTarget{Path: ".", FullAccess: full}, policy.Write)
	if err != nil {
		return nil, nil, "", "", err
	}
	return createShellTaskOutputs(root, archiveLimit(s.config, full), e)
}
func archiveLimit(c config.Sandbox, full bool) int64 {
	if full {
		return 0
	}
	return c.MaxFileBytes
}
func shellOutputPrefix(e core.Execution) string {
	return "tool-shell-" + digestBytes([]byte(string(e.Principal.OwnerID) + "/" + string(e.OperationID)))[:32]
}
func createShellTaskOutputs(root string, maxBytes int64, e core.Execution) (*shellOutput, *shellOutput, string, string, error) {
	return createShellOutputsNamed(root, maxBytes, shellOutputPrefix(e))
}
func createShellOutputsNamed(root string, maxBytes int64, prefix string) (*shellOutput, *shellOutput, string, string, error) {
	outName, errName := prefix+"-stdout.txt", prefix+"-stderr.txt"
	out, err := createOutputFile(root, outName)
	if err != nil {
		return nil, nil, "", "", err
	}
	stderr, err := createOutputFile(root, errName)
	if err != nil {
		out.Close()
		return nil, nil, "", "", err
	}
	budget := &shellOutputBudget{left: min(64<<20, maxBytes)}
	if maxBytes == 0 {
		budget.left = 1<<63 - 1
		budget.unlimited = true
	}
	return &shellOutput{file: out, budget: budget}, &shellOutput{file: stderr, budget: budget}, "/work/" + outName, "/work/" + errName, nil
}

var _ io.Writer = (*shellOutput)(nil)

func shellOutputResult(stdoutFile, stderrFile *shellOutput, stdoutPath, stderrPath string, exit platform.Exit, stdout, stderr []byte) (json.RawMessage, error) {
	note := "Full stdout/stderr are saved at the workspace paths. OutputComplete=false means a disk error or the 64 MiB/configured file quota was reached; inspect state before retrying effects."
	if stdoutFile.budget.unlimited {
		note = "Full stdout/stderr are saved at the workspace paths without an archive quota. OutputComplete=false indicates a storage error; inspect state before retrying effects."
	}
	return json.Marshal(struct {
		OutputComplete           bool
		StdoutPath, StderrPath   string
		StdoutBytes, StderrBytes int64
		OutputNote               string
		Exit                     platform.Exit
		Stdout, Stderr           string
	}{!stdoutFile.budget.failed, stdoutPath, stderrPath, stdoutFile.bytes, stderrFile.bytes, note, exit, string(stdout), string(stderr)})
}
