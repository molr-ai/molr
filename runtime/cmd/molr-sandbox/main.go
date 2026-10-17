// molr-sandbox exposes the shared Linux and macOS sandbox backends as a standalone service.
package main

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	"molr.ai/runtime/internal/buildinfo"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"molr.ai/runtime/internal/app"
	"molr.ai/runtime/internal/config"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/platform/sandbox"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "version" {
		if len(os.Args) == 3 && os.Args[2] == "--json" {
			_ = json.NewEncoder(os.Stdout).Encode(buildinfo.Current())
		} else {
			fmt.Println("molr-sandbox", buildinfo.Version)
		}
		return
	}
	if len(os.Args) == 2 && os.Args[1] == "sandbox-supervisor" {
		os.Exit(sandbox.SupervisorMain())
	}
	if len(os.Args) > 1 && os.Args[1] == "sandbox-helper" {
		os.Exit(sandbox.HelperMain(os.Args[2:]))
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "molr-sandbox:", err)
		os.Exit(1)
	}
}
func run(ctx context.Context) error {
	flags := flag.NewFlagSet("molr-sandbox", flag.ContinueOnError)
	path := flags.String("config", "", "sandbox configuration JSON")
	tokenPath := flags.String("token-file", "", "private file containing service token (32+ characters)")
	if err := flags.Parse(os.Args[1:]); err != nil {
		return err
	}
	if *path == "" || *tokenPath == "" || flags.NArg() != 0 {
		return core.Invalid("arguments", "use -config and -token-file")
	}
	f, err := os.Open(*path)
	if err != nil {
		return err
	}
	cfg, err := config.Load(f)
	f.Close()
	if err != nil {
		return err
	}
	cfg.ConfigPath = *path
	for _, secretPath := range []string{*tokenPath, cfg.HTTP.KeyFile} {
		rel, err := filepath.Rel(cfg.DataDir, secretPath)
		if err != nil || !filepath.IsLocal(rel) {
			return core.Invalid("secrets", "token and TLS private key must be inside sandbox data_dir")
		}
		actual, err := filepath.EvalSymlinks(secretPath)
		if err != nil {
			return err
		}
		if actual != filepath.Clean(secretPath) {
			return core.Invalid("secrets", "symbolic links are not allowed")
		}
	}
	st, err := os.Lstat(*tokenPath)
	if err != nil {
		return err
	}
	if !st.Mode().IsRegular() || st.Mode().Perm()&0077 != 0 || st.Size() > 4096 {
		return core.Invalid("token_file", "private regular file required")
	}
	token, err := os.ReadFile(*tokenPath)
	if err != nil {
		return err
	}
	defer clear(token)
	cert, err := tls.LoadX509KeyPair(cfg.HTTP.CertFile, cfg.HTTP.KeyFile)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(cert.Certificate[0])
	fmt.Fprintf(os.Stderr, "Sandbox TLS SHA-256: %x\nListening on %s\n", sum, cfg.HTTP.Listen)
	return app.ServeSandbox(ctx, cfg, core.NewSecret(strings.TrimSpace(string(token))))
}
