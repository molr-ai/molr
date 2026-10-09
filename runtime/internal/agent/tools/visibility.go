package tools

import "context"

type visibilityKey struct{}

// WithVisibleTools narrows a turn's capabilities without introducing another
// catalog or executor. Both discovery and dispatch apply the same allowlist.
func WithVisibleTools(ctx context.Context, names []string) context.Context {
	allowed := map[string]bool{}
	for _, name := range names {
		if visibleTool(ctx, name) {
			allowed[name] = true
		}
	}
	return context.WithValue(ctx, visibilityKey{}, allowed)
}
func visibleTool(ctx context.Context, name string) bool {
	allowed, limited := ctx.Value(visibilityKey{}).(map[string]bool)
	return !limited || allowed[name]
}
