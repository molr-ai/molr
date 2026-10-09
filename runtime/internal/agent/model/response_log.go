package model

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"molr.ai/runtime/internal/core"
)

// RawResponse is optional file-only diagnostic material, never replay evidence.
// No request headers, credentials or endpoint query strings are included.
type RawResponse struct {
	Status      int
	ContentType string
	Body        string
	Incomplete  bool
}

type ResponseLogger struct {
	mu        sync.Mutex
	directory string
	maxBytes  int64
	files     int
}

// NewResponseLogger has no filesystem effects until the first record. Three
// rotating files cap retained diagnostics at 192 MiB, including JSON escaping
// of responses bounded by the client's 8 MiB transport limit.
func NewResponseLogger(directory string) *ResponseLogger {
	return &ResponseLogger{directory: directory, maxBytes: 64 << 20, files: 3}
}

func (l *ResponseLogger) Record(req Request, response RawResponse) error {
	record := struct {
		At                                         time.Time
		OwnerID, AssistantID, OperationID, ModelID core.ID
		RawResponse
	}{time.Now().UTC(), req.Execution.Principal.OwnerID, req.Execution.AssistantID, req.Execution.OperationID, req.ModelID, response}
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if int64(len(data)) > l.maxBytes {
		return fmt.Errorf("model diagnostic record exceeds file limit")
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := os.MkdirAll(l.directory, 0700); err != nil {
		return err
	}
	name := filepath.Join(l.directory, "responses.jsonl")
	info, err := os.Lstat(name)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err == nil {
		if !info.Mode().IsRegular() {
			return fmt.Errorf("model diagnostic log must be a regular file")
		}
		if info.Size()+int64(len(data)) > l.maxBytes {
			oldest := fmt.Sprintf("%s.%d", name, l.files-1)
			if err := os.Remove(oldest); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
			for i := l.files - 2; i >= 0; i-- {
				source := name
				if i > 0 {
					source = fmt.Sprintf("%s.%d", name, i)
				}
				if err := os.Rename(source, fmt.Sprintf("%s.%d", name, i+1)); err != nil && !errors.Is(err, os.ErrNotExist) {
					return err
				}
			}
		}
	}
	file, err := os.OpenFile(name, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	if err := file.Chmod(0600); err != nil {
		file.Close()
		return err
	}
	_, err = file.Write(data)
	return errors.Join(err, file.Close())
}
