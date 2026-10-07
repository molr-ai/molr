// Package settings owns user preferences and sensitive data-management requests.
package settings

import (
	"context"
	"time"

	"molr.ai/runtime/internal/auth"
	"molr.ai/runtime/internal/business/memory"
	"molr.ai/runtime/internal/core"
)

type Theme string

const (
	System Theme = "system"
	Light  Theme = "light"
	Dark   Theme = "dark"
)

type Palette string

const (
	Forest     Palette = "forest"
	Ocean      Palette = "ocean"
	Iris       Palette = "iris"
	Sand       Palette = "sand"
	Rose       Palette = "rose"
	Monochrome Palette = "monochrome"
)

// UITheme identifies the UI package and version; Theme controls light/dark display.
const DefaultUITheme = "molr.official@1.0.0"

type Settings struct {
	OwnerID        core.ID
	Language       string
	Timezone       string
	Currency       string
	Theme          Theme
	UITheme        string
	Palette        Palette
	DefaultModelID core.ID

	AnswerPreferences string
	ReducedMotion     bool
	WorkbenchEnabled  bool
	Version           core.Version
}
type UpdateRequest struct {
	Mutation core.Mutation
	Settings Settings
}
type ExportRequest struct {
	Mutation         core.Mutation
	Verification     auth.Verification
	IncludeMessages  bool
	IncludeArtifacts bool
}
type Export struct {
	ID        core.ID
	State     string
	FileID    core.ID
	CreatedAt time.Time
}
type CleanupRequest struct {
	MemoryFiles  []memory.Ref
	Mutation     core.Mutation
	Verification auth.Verification
	ObjectIDs    []core.Object
}

// Settings never expose server secrets. Export/cleanup require recent verification, not a confirmation flag.
type Service interface {
	Get(context.Context, core.Principal) (Settings, error)
	Update(context.Context, core.Principal, UpdateRequest) (Settings, error)
	Export(context.Context, core.Principal, ExportRequest) (Export, error)
	ExportStatus(context.Context, core.Principal, core.ID) (Export, error)
	Cleanup(context.Context, core.Principal, CleanupRequest) error
}
type Repository interface {
	Get(context.Context, core.ID) (Settings, error)
	Save(context.Context, Settings, core.Version) error
	PutExport(context.Context, core.ID, Export) error
	Export(context.Context, core.ID, core.ID) (Export, error)
}
