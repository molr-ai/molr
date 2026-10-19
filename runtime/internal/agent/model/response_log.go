package model

import (
	"encoding/json"
	"path/filepath"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/logging"
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
	file *logging.File
}

// NewResponseLogger is lazy; shared logging policy owns file retention.
func NewResponseLogger(directory string, options logging.Options) (*ResponseLogger, error) {
	file, err := logging.NewFile(filepath.Join(directory, "responses.jsonl"), options)
	if err != nil {
		return nil, err
	}
	return &ResponseLogger{file: file}, nil
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
	_, err = l.file.Write(data)
	return err
}
