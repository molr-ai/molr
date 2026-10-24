package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"molr.ai/runtime/internal/app"
	"molr.ai/runtime/internal/config"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/platform"
	"molr.ai/runtime/internal/sandboxidentity"
)

// serveSandbox only initializes the resource service; no Runtime accounts or database.
func serveSandbox(ctx context.Context, cfg config.Config, tokenPath string, diagnostics io.Writer) error {
	if tokenPath == "" {
		tokenPath = filepath.Join(cfg.DataDir, "sandbox-token")
	}
	for _, secretPath := range []string{tokenPath, cfg.HTTP.KeyFile} {
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
	st, err := os.Lstat(tokenPath)
	if err != nil {
		return err
	}
	if !platform.PrivateFile(tokenPath, st) || st.Size() > 4096 {
		return core.Invalid("token_file", "private regular file required")
	}
	token, err := os.ReadFile(tokenPath)
	if err != nil {
		return err
	}
	defer clear(token)
	cert, err := tls.LoadX509KeyPair(cfg.HTTP.CertFile, cfg.HTTP.KeyFile)
	if err != nil {
		return err
	}
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		return err
	}
	fmt.Fprintf(diagnostics, "Sandbox public key SHA-256: %s\nListening on %s\n", sandboxidentity.PublicKeySHA256(leaf), cfg.HTTP.Listen)
	return app.ServeSandbox(ctx, cfg, core.NewSecret(strings.TrimSpace(string(token))))
}

// exportSandboxConnectionCode is an explicit local credential read, never a startup log.
func exportSandboxConnectionCode(cfg config.Config, tokenPath string, output io.Writer) error {
	if tokenPath == "" {
		tokenPath = filepath.Join(cfg.DataDir, "sandbox-token")
	}
	rel, err := filepath.Rel(cfg.DataDir, tokenPath)
	if err != nil || !filepath.IsLocal(rel) {
		return core.Invalid("token_file", "token must be inside sandbox data_dir")
	}
	actual, err := filepath.EvalSymlinks(tokenPath)
	if err != nil {
		return err
	}
	if actual != filepath.Clean(tokenPath) {
		return core.Invalid("token_file", "symbolic links are not allowed")
	}
	st, err := os.Lstat(tokenPath)
	if err != nil {
		return err
	}
	if !platform.PrivateFile(tokenPath, st) || st.Size() > 4096 {
		return core.Invalid("token_file", "private regular file required")
	}
	token, err := os.ReadFile(tokenPath)
	if err != nil {
		return err
	}
	defer clear(token)
	pair, err := tls.LoadX509KeyPair(cfg.HTTP.CertFile, cfg.HTTP.KeyFile)
	if err != nil {
		return err
	}
	cert, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return err
	}
	code, err := sandboxidentity.Encode(sandboxidentity.PublicKeySHA256(cert), core.NewSecret(strings.TrimSpace(string(token))))
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(output, code.Reveal())
	return err
}
