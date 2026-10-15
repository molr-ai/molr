//go:build !linux && !darwin

package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "molr-setup requires Linux or macOS; run molr directly on this platform")
	os.Exit(1)
}
