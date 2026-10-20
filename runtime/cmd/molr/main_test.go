package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"molr.ai/runtime/internal/app"
	"molr.ai/runtime/internal/auth"
	"molr.ai/runtime/internal/business/todo"
	"molr.ai/runtime/internal/config"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/tests/testkit"
)

func cliConfig(t *testing.T) (config.Config, string) {
	t.Helper()
	root := t.TempDir()
	cfg := config.Defaults(filepath.Join(root, "data"))
	cfg.HTTP.PublicURL = "https://runtime.example"
	cfg.HTTP.TLSMode = config.TrustedProxy
	cfg.HTTP.TrustedProxies = []string{"127.0.0.1/32"}
	data, err := json.Marshal(cfg)
	testkit.RequireNoError(t, err)
	path := filepath.Join(root, "server.json")
	testkit.RequireNoError(t, os.WriteFile(path, data, 0600))
	return cfg, path
}
func invokeCLI(t *testing.T, ctx context.Context, args []string, input string) (string, error) {
	t.Helper()
	var out, diagnostics bytes.Buffer
	err := run(ctx, args, strings.NewReader(input), &out, &diagnostics)
	secret := strings.TrimSpace(input)
	if secret != "" && (strings.Contains(out.String(), secret) || strings.Contains(diagnostics.String(), secret)) {
		t.Fatal("CLI exposed stdin password")
	}
	return out.String(), err
}
func cliLogin(t *testing.T, b *app.Bundle, password string) (core.Principal, core.Secret) {
	t.Helper()
	login, err := b.Services.Auth.Login(testkit.Context(t), auth.LoginRequest{Username: "owner", Password: core.NewSecret(password)})
	testkit.RequireNoError(t, err)
	if login.Session == nil {
		t.Fatal("expected owner session")
	}
	p, err := b.Services.Auth.Authenticate(testkit.Context(t), auth.Authentication{Session: login.Session.Token})
	testkit.RequireNoError(t, err)
	return p, login.Session.Token
}
func TestCLIInitRecoverLifecycleWithoutListener(t *testing.T) {
	cfg, path := cliConfig(t)
	// This is a multi-command lifecycle, not a ten-second latency assertion.
	// Use the test runner's timeout across hashing, database opens and restores.
	ctx := t.Context()
	oldPassword := "cli-original-password-12345!"
	newPassword := "cli-recovered-password-67890!"
	output, err := invokeCLI(t, ctx, []string{"init", "-config", path, "-password-stdin"}, oldPassword+"\n")
	testkit.RequireNoError(t, err)
	var account auth.Account
	testkit.RequireNoError(t, json.Unmarshal([]byte(output), &account))
	if account.ID == "" || account.Username != "owner" {
		t.Fatal("init did not persist owner")
	}
	_, err = invokeCLI(t, ctx, []string{"init", "-config", path, "-password-stdin"}, oldPassword)
	if err == nil {
		t.Fatal("second init overwrote existing owner")
	}
	b, err := app.Build(ctx, cfg)
	testkit.RequireNoError(t, err)
	p, oldSession := cliLogin(t, b, oldPassword)
	proof, err := b.Services.Auth.Reauthenticate(ctx, p, auth.ReauthenticateRequest{Password: core.NewSecret(oldPassword), Purpose: "token.create"})
	testkit.RequireNoError(t, err)
	issued, err := b.Services.Auth.CreateToken(ctx, p, auth.CreateTokenRequest{Name: "before-backup", Scopes: []string{"read"}, ExpiresAt: time.Now().Add(time.Hour), Verification: proof})
	testkit.RequireNoError(t, err)
	testkit.RequireNoError(t, b.Database.Store().Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		return todo.NewRepository(tx.Store()).Insert(ctx, todo.Todo{ID: "before-backup", OwnerID: p.OwnerID, Title: "Keep this", Version: 1})
	}))
	testkit.RequireNoError(t, b.Close())
	_, err = invokeCLI(t, ctx, []string{"recover", "-config", path, "-password-stdin", "-reset-mfa"}, newPassword)
	testkit.RequireNoError(t, err)
	b, err = app.Build(ctx, cfg)
	testkit.RequireNoError(t, err)
	_, err = b.Services.Auth.Login(ctx, auth.LoginRequest{Username: "owner", Password: core.NewSecret(oldPassword)})
	testkit.RequireCode(t, err, core.Unauthenticated)
	_, _ = cliLogin(t, b, newPassword)
	_, err = b.Services.Auth.Authenticate(ctx, auth.Authentication{Session: oldSession})
	testkit.RequireCode(t, err, core.Unauthenticated)
	_, err = b.Services.Auth.Authenticate(ctx, auth.Authentication{Bearer: issued.Value})
	testkit.RequireCode(t, err, core.Unauthenticated)
	testkit.RequireNoError(t, b.Close())

}
func TestCLIRejectsUnsafeInputsAndCanceledCommands(t *testing.T) {
	cfg, path := cliConfig(t)
	ctx := testkit.Context(t)
	_, err := invokeCLI(t, ctx, []string{"init", "-config", path}, "ignored-password")
	testkit.RequireCode(t, err, core.InvalidArgument)
	_, err = invokeCLI(t, ctx, []string{"init", "-config", path, "-password-stdin"}, strings.Repeat("x", 4097))
	testkit.RequireCode(t, err, core.InvalidArgument)
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	_, err = invokeCLI(t, canceled, []string{"init", "-config", path, "-password-stdin"}, "canceled-test-password-123!")
	testkit.RequireCode(t, err, core.Canceled)
	// The canceled setup must release its process/database locks.
	b, err := app.Build(ctx, cfg)
	testkit.RequireNoError(t, err)
	testkit.RequireNoError(t, b.Close())
}

func TestCLIHostToolsDoesNotRequireSeparateAccountService(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(fmt.Sprint("host_tools_", enabled), func(t *testing.T) {
			cfg, path := cliConfig(t)
			cfg.Sandbox.HostTools = enabled
			data, err := json.Marshal(cfg)
			testkit.RequireNoError(t, err)
			testkit.RequireNoError(t, os.WriteFile(path, data, 0600))
			_, err = invokeCLI(t, testkit.Context(t), []string{"init", "-config", path, "-password-stdin"}, "runtime-identity-test-password-123!")
			testkit.RequireNoError(t, err)
			info, err := os.Stat(filepath.Join(cfg.DataDir, "runtime.db"))
			testkit.RequireNoError(t, err)
			if info.Size() == 0 {
				t.Fatal("Runtime was not initialized")
			}
		})
	}
}

func TestBootstrapGeneratesPrivateCredentialsOnce(t *testing.T) {
	cfg, path := cliConfig(t)
	text, err := invokeCLI(t, t.Context(), []string{"bootstrap", "-config", path}, "")
	testkit.RequireNoError(t, err)
	passwordPath := filepath.Join(cfg.DataDir, "initial-password")
	password, err := os.ReadFile(passwordPath)
	testkit.RequireNoError(t, err)
	info, err := os.Stat(passwordPath)
	testkit.RequireNoError(t, err)
	if len(password) < 32 || info.Mode().Perm() != 0600 || strings.Contains(text, string(password)) {
		t.Fatal("unsafe generated credentials")
	}
	_, err = invokeCLI(t, t.Context(), []string{"bootstrap", "-config", path}, "")
	testkit.RequireNoError(t, err)
	again, err := os.ReadFile(passwordPath)
	testkit.RequireNoError(t, err)
	if !bytes.Equal(password, again) {
		t.Fatal("repeat bootstrap replaced credentials")
	}
	b, err := app.Build(t.Context(), cfg)
	testkit.RequireNoError(t, err)
	defer b.Close()
	_, _ = cliLogin(t, b, string(password))
}

func TestCLIModeRejectsAccountCommandsAndInvalidCombinations(t *testing.T) {
	cfg, path := cliConfig(t)
	for _, command := range []string{"bootstrap", "init", "access", "doctor", "recover"} {
		_, err := invokeCLI(t, t.Context(), []string{command, "--mode", "sandbox", "--config", path}, "")
		if err == nil || !strings.Contains(err.Error(), "sandbox mode only supports serve") {
			t.Fatalf("%s: %v", command, err)
		}
	}
	for _, args := range [][]string{{"serve", "--mode", "full"}, {"serve", "--mode", "server", "--token-file", "token"}, {"init", "--token-file", "token"}} {
		if _, err := invokeCLI(t, t.Context(), args, ""); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
	if _, err := os.Stat(cfg.DataDir); !os.IsNotExist(err) {
		t.Fatal("invalid mode created Runtime state")
	}
}

func TestCLIBackupRestoreRemoved(t *testing.T) {
	for _, cmd := range []string{"backup", "restore"} {
		if _, err := invokeCLI(t, t.Context(), []string{cmd}, ""); err == nil || !strings.Contains(err.Error(), "unknown command") {
			t.Fatalf("obsolete command accepted: %s: %v", cmd, err)
		}
	}
}

func TestCLIBootstrapSuppliedAdministratorWithoutPasswordFile(t *testing.T) {
	cfg, path := cliConfig(t)
	password := "chosen-fixture-password-2026"
	args := []string{"bootstrap", "-config", path, "-username", "my-admin", "-password-stdin"}
	_, err := invokeCLI(t, t.Context(), args, password)
	testkit.RequireNoError(t, err)
	if _, err = os.Stat(filepath.Join(cfg.DataDir, "initial-password")); !os.IsNotExist(err) {
		t.Fatal("manual initialization must not save a password file")
	}
	// Retrying installation must not overwrite an initialized account.
	_, err = invokeCLI(t, t.Context(), args, "different-fixture-password")
	testkit.RequireNoError(t, err)
	b, err := app.Build(t.Context(), cfg)
	testkit.RequireNoError(t, err)
	defer b.Close()
	login, err := b.Services.Auth.Login(t.Context(), auth.LoginRequest{Username: "my-admin", Password: core.NewSecret(password)})
	testkit.RequireNoError(t, err)
	p, err := b.Services.Auth.Authenticate(t.Context(), auth.Authentication{Session: login.Session.Token})
	testkit.RequireNoError(t, err)
	proof, err := b.Services.Auth.Reauthenticate(t.Context(), p, auth.ReauthenticateRequest{Password: core.NewSecret(password), Purpose: "login_link.create"})
	testkit.RequireNoError(t, err)
	link, err := b.Services.Auth.CreateLoginLink(t.Context(), p, auth.CreateLoginLinkRequest{Name: "Installer", TargetPath: "/assistants", ExpiresIn: 15 * time.Minute, Verification: proof})
	testkit.RequireNoError(t, err)
	result, err := b.Services.Auth.RedeemLoginLink(t.Context(), auth.RedeemLoginLinkRequest{Token: link.Value})
	testkit.RequireNoError(t, err)
	if result.Session == nil {
		t.Fatal("first login link did not authenticate")
	}
	_, err = b.Services.Auth.RedeemLoginLink(t.Context(), auth.RedeemLoginLinkRequest{Token: link.Value})
	testkit.RequireCode(t, err, core.Unauthenticated)
	var setup app.SetupState
	testkit.RequireNoError(t, b.Database.Store().Read(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		return tx.Get(ctx, "system.setup", string(p.OwnerID), &setup)
	}))
	if !setup.Required {
		t.Fatal("manual initialization skipped model/assistant setup")
	}
}
