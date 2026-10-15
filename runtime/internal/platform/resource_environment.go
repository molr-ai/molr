package platform

import (
	"crypto/sha256"
	"fmt"
)

// ResourceEnvironment names a native remote scope without publishing host paths.
// The destination is constructed from the resource ID and normalized scope path.
func ResourceEnvironment(destination string) string {
	sum := sha256.Sum256([]byte(destination))
	return fmt.Sprintf("MOLR_RESOURCE_%x", sum[:16])
}
