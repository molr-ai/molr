package core

// DefaultModelContextLimit applies when neither the owner nor provider declares a window.
const DefaultModelContextLimit = 256 * 1024

// CompressionTokens resolves an assistant's token threshold. Zero selects the
// automatic default; positive values follow the current model's valid range.
func CompressionTokens(window, configured int) int {
	upper := max(1, window/5*4+window%5*4/5)
	if configured == 0 {
		return min(128*1024, upper)
	}
	return min(max(configured, min(64*1024, upper)), upper)
}
