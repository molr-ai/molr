package debugtrace

import (
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
)

func visibleQuery(owner core.ID) persistence.Query {
	return persistence.Query{Where: []persistence.Filter{persistence.Eq("OwnerID", owner)}}
}
