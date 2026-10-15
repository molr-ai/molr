package maintenance

import "context"

// Unavailable reports unsupported maintenance independently of Runtime capability.
type Unavailable struct{ ExternalBrowser bool }

func (Unavailable) ProtectedRoots() []string          { return nil }
func (Unavailable) Programs() (string, string, error) { return "", "", unsupported() }
func (Unavailable) Enabled() bool                     { return false }
func (u Unavailable) Snapshot(context.Context) []ComponentStatus {
	s := ComponentStatus{ID: "browser", State: "unsupported", Reason: "component_platform_unsupported"}
	if u.ExternalBrowser {
		s.State, s.Reason = "external", "component_externally_managed"
	}
	return []ComponentStatus{s}
}
func (Unavailable) CanUpdate(context.Context) bool             { return false }
func (Unavailable) Task(context.Context, string) (Task, error) { return Task{}, unsupported() }
func (Unavailable) Start(context.Context, string, string, string, string) (Task, error) {
	return Task{}, unsupported()
}
func (Unavailable) Cancel(context.Context, string) (Task, error) { return Task{}, unsupported() }
func (Unavailable) Latest(context.Context) *Task                 { return nil }
func unsupported() error {
	return &ComponentError{Code: "maintenance_platform_unsupported", Detail: "host maintenance is unavailable on this platform"}
}
