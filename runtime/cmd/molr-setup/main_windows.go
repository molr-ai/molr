package main

import (
	"context"
	"fmt"
	"molr.ai/runtime/internal/installationwindows"
	"os"
)

func main() {
	if err := installationwindows.Run(context.Background(), os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "molr-setup:", err)
		os.Exit(1)
	}
}
