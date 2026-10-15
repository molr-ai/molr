//go:build linux || darwin

// molr-setup installs and maintains a versioned Molr instance without an interpreter.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"molr.ai/runtime/internal/installation"
)

func main() {
	syscall.Umask(0077)
	os.Setenv("PATH", "/usr/sbin:/usr/bin:/sbin:/bin")
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := installation.Run(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "molr-setup:", err)
		var pending *installation.VerificationPending
		if errors.As(err, &pending) {
			os.Exit(3)
		}
		if errors.Is(err, context.Canceled) {
			os.Exit(130)
		}
		os.Exit(1)
	}
}
