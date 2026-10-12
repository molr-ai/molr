package memory

import (
	"context"
	"errors"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/textfiles"
)

// HasFiles is a scheduling probe, not a content validation pass. A damaged or
// oversized note still needs maintenance and must not prevent a dream starting.
func (f *Files) HasFiles(ctx context.Context, owner, assistant core.ID) (bool, error) {
	roots, err := f.roots(ctx, owner, RootQuery{Scope: Assistant, AssistantID: assistant})
	if err != nil {
		return false, err
	}
	found := errors.New("memory file present")
	for _, root := range roots {
		if root.Scope != Assistant || root.AssistantID != assistant {
			continue
		}
		file, err := textfiles.Open(root.Directory, "MEMORY.md")
		if err == nil {
			file.Close()
			return true, nil
		}
		if core.CodeOf(err) != core.NotFound {
			return false, err
		}
		budget := 4096
		err = walk(ctx, root.Directory, ".agents/memory", 0, &budget, func(string) error { return found })
		if errors.Is(err, found) {
			return true, nil
		}
		if err != nil {
			return false, err
		}
	}
	return false, nil
}
