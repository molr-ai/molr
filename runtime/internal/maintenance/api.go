// Package maintenance defines the Runtime boundary to an installed maintenance worker.
package maintenance

import (
	"context"
	"time"
)

// Service is optional host integration. Core Runtime startup does not require it.
type Service interface {
	ProtectedRoots() []string
	Programs() (string, string, error)
	Enabled() bool
	Snapshot(context.Context) []ComponentStatus
	CanUpdate(context.Context) bool
	Task(context.Context, string) (Task, error)
	Start(context.Context, string, string, string, string) (Task, error)
	Cancel(context.Context, string) (Task, error)
	Latest(context.Context) *Task
}

type Task struct {
	ID         string     `json:"id"`
	Operation  string     `json:"operation"`
	Component  string     `json:"component,omitempty"`
	Target     string     `json:"target,omitempty"`
	State      string     `json:"state"`
	Stage      string     `json:"stage"`
	Downloaded int64      `json:"downloaded,omitempty"`
	Total      int64      `json:"total,omitempty"`
	ErrorCode  string     `json:"error_code,omitempty"`
	StartedAt  time.Time  `json:"started_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
}

func (t Task) Active() bool { return t.State == "running" }

type ComponentStatus struct {
	ID            string `json:"id"`
	State         string `json:"state"`
	Version       string `json:"version,omitempty"`
	TargetVersion string `json:"target_version,omitempty"`
	DownloadBytes int64  `json:"download_bytes"`
	DiskBytes     int64  `json:"disk_bytes"`
	CanInstall    bool   `json:"can_install"`
	Reason        string `json:"reason,omitempty"`
	Task          *Task  `json:"task,omitempty"`
	AutoInstall   bool   `json:"auto_install"`
}

type ComponentError struct {
	Code   string
	Detail string
}

func (e *ComponentError) Error() string { return e.Code + ": " + e.Detail }
