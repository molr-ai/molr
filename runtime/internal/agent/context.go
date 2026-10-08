package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"

	"molr.ai/runtime/internal/agent/model"
	"molr.ai/runtime/internal/core"
)

// ContextDocument is a complete replacement for one scoped source. Revision
// identifies source changes even when Content only contains a lazy skill entry.
// Project/skill content is workspace guidance, never a new runtime permission.
type ContextDocument struct {
	Kind     string `json:"kind"`
	Source   string `json:"source"`
	Scope    string `json:"scope"`
	Revision string `json:"revision,omitempty"`
	Content  string `json:"content"`
	Loaded   bool   `json:"loaded,omitempty"`
}

// ContextRequest supplies private history and the previous effective sources to
// the trusted composition callback. Implementations must authorize bounded reads
// and return a complete snapshot; a read error must not masquerade as deletion.
type ContextRequest struct {
	SandboxID core.ID // Trusted workspace provenance; empty denotes legacy local.
	Execution core.Execution
	Usage     *contextUsage `json:",omitempty"`
	Inputs    []model.Input
	Previous  []ContextDocument
}

type contextEntry struct {
	ContextDocument
	Version string `json:"version"`
}
type contextReference struct {
	Kind    string `json:"kind"`
	Source  string `json:"source"`
	Scope   string `json:"scope"`
	Version string `json:"version"`
}
type contextChange struct {
	contextReference
	Action string `json:"action"`
}
type automaticContext struct {
	Kind    string             `json:"kind"`
	Version string             `json:"version"`
	Sources []contextEntry     `json:"sources"`
	Removed []contextReference `json:"removed"`
	Changes []contextChange    `json:"changes"`
}
type privateContext struct {
	Current         *executionCursor  `json:",omitempty"`
	PendingEvidence []pendingEvidence `json:",omitempty"`
	Usage           *contextUsage     `json:",omitempty"`
	Inputs          []model.Input
	Automatic       *automaticContext `json:",omitempty"`
}

func contextHash(value any) string {
	data, _ := json.Marshal(value)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
func contextKey(kind, source string) string { return kind + "\x00" + source }
func contextRef(d contextEntry) contextReference {
	return contextReference{d.Kind, d.Source, d.Scope, d.Version}
}
func contextHistory(inputs []model.Input) []model.Input {
	out := make([]model.Input, 0, len(inputs))
	for _, input := range inputs {
		if !input.RuntimeContext {
			out = append(out, input)
		}
	}
	return out
}
func contextInput(snapshot *automaticContext) model.Input {
	return model.Input{Role: model.User, RuntimeContext: true, Text: contextText(snapshot)}
}
func checkContextSize(inputs []model.Input) error {
	encoded, err := json.Marshal(inputs)
	if err != nil {
		return err
	}
	if len(encoded) > 256<<20 {
		return core.WithReason(core.NewError(core.FailedPrecondition, "private context requires compaction"), core.ContextLimit)
	}
	return nil
}

// refreshContext runs only at model request boundaries, after all tool replies.
// The returned Tools and notification are built from exactly the same definitions.
func (e *Engine) refreshContext(ctx context.Context, execution core.Execution, inputs []model.Input, previous *automaticContext) ([]model.Input, []model.ToolDefinition, *automaticContext, error) {
	if err := e.checkEnvironment(ctx); err != nil {
		return nil, nil, nil, err
	}
	definitions, err := e.options.Dependencies.Tools.Definitions(ctx, execution.Principal)
	if err != nil {
		return nil, nil, nil, err
	}
	toolDefinitions := make([]model.ToolDefinition, 0, len(definitions))
	documents := make([]ContextDocument, 0, len(definitions))
	for _, d := range definitions {
		if !json.Valid(d.InputSchema) {
			return nil, nil, nil, core.Invalid("tools", "invalid tool input schema")
		}
		definition := model.ToolDefinition{Name: d.Name, Description: d.Description, InputSchema: d.InputSchema}

		toolDefinitions = append(toolDefinitions, definition)
		documents = append(documents, ContextDocument{Kind: "tool", Source: d.Name, Scope: "assistant", Revision: contextHash(d)})
	}
	sort.Slice(toolDefinitions, func(i, j int) bool { return toolDefinitions[i].Name < toolDefinitions[j].Name })
	if e.options.ContextSources != nil {
		request := ContextRequest{Execution: execution, Inputs: contextHistory(inputs)}
		if previous != nil {
			for _, d := range previous.Sources {
				request.Previous = append(request.Previous, d.ContextDocument)
			}
		}
		sources, err := e.options.ContextSources(ctx, request)
		if err != nil {
			return nil, nil, nil, err
		}
		for _, d := range sources {
			if d.Kind != "workspace" && d.Kind != "chat_channel" && d.Kind != "profile" && d.Kind != "project_instruction" && d.Kind != "skill" && d.Kind != "user_preferences" && d.Kind != "memory" && d.Kind != "permission_requests" && d.Kind != "permissions" {
				return nil, nil, nil, core.Invalid("context", "unsupported automatic context source")
			}
		}
		documents = append(documents, sources...)
	}
	if len(documents) > 256 {
		return nil, nil, nil, core.Invalid("context", "too many automatic context sources")
	}
	sort.Slice(documents, func(i, j int) bool {
		return contextKey(documents[i].Kind, documents[i].Source) < contextKey(documents[j].Kind, documents[j].Source)
	})
	next := &automaticContext{Kind: "context_changed", Sources: []contextEntry{}, Removed: []contextReference{}, Changes: []contextChange{}}
	old := map[string]contextEntry{}
	removed := map[string]contextReference{}
	if previous != nil {
		for _, d := range previous.Sources {
			old[contextKey(d.Kind, d.Source)] = d
		}
		for _, d := range previous.Removed {
			removed[contextKey(d.Kind, d.Source)] = d
		}
	}
	seen := map[string]bool{}
	for _, d := range documents {
		key := contextKey(d.Kind, d.Source)
		if d.Source == "" || d.Scope == "" || seen[key] || len(d.Source) > 4096 || len(d.Scope) > 4096 || len(d.Content) > 256<<10 {
			return nil, nil, nil, core.Invalid("context", "invalid or duplicate automatic context source")
		}
		if err := validateContextPresentation(d); err != nil {
			return nil, nil, nil, err
		}
		seen[key] = true
		entry := contextEntry{d, contextHash(d)}
		next.Sources = append(next.Sources, entry)
		before, exists := old[key]
		if !exists || before.Version != entry.Version {
			action := "replace"
			if !exists {
				action = "add"
			}
			next.Changes = append(next.Changes, contextChange{contextRef(entry), action})
		}
		delete(old, key)
		delete(removed, key)
	}
	for key, d := range old {
		ref := contextRef(d)
		removed[key] = ref
		next.Changes = append(next.Changes, contextChange{ref, "remove"})
	}
	for _, d := range removed {
		next.Removed = append(next.Removed, d)
	}
	sort.Slice(next.Removed, func(i, j int) bool {
		return contextKey(next.Removed[i].Kind, next.Removed[i].Source) < contextKey(next.Removed[j].Kind, next.Removed[j].Source)
	})
	sort.Slice(next.Changes, func(i, j int) bool {
		return contextKey(next.Changes[i].Kind, next.Changes[i].Source) < contextKey(next.Changes[j].Kind, next.Changes[j].Source)
	})
	if len(next.Removed) > 1_000_000 {
		return nil, nil, nil, core.WithReason(core.NewError(core.FailedPrecondition, "automatic context requires compaction"), core.ContextLimit)
	}
	next.Version = contextHash(struct {
		Sources []contextEntry
		Removed []contextReference
	}{next.Sources, next.Removed})
	changed := previous == nil || next.Version != previous.Version
	if !changed {
		next = previous
	}
	found := false
	for _, input := range inputs {
		found = found || input.RuntimeContext
	}
	if changed || !found || !sameContextText(inputs, next) {
		inputs = append(contextHistory(inputs), contextInput(next))
	}
	if err := checkContextSize(inputs); err != nil {
		return nil, nil, nil, err
	}
	if changed {
		e.options.Trace.Record(ctx, "context.changed", "succeeded", "", next)
		e.options.Trace.Record(ctx, "tools.available", "succeeded", "", definitions)
	}
	return inputs, toolDefinitions, next, nil
}
