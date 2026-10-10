// molr-server runs the self-host Runtime with an embedded local sandbox.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	_ "time/tzdata"

	"molr.ai/runtime/internal/app"
	"molr.ai/runtime/internal/auth"
	"molr.ai/runtime/internal/buildinfo"
	"molr.ai/runtime/internal/config"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/platform/sandbox"
	"molr.ai/runtime/internal/storage/sqlite"
)

func main() {
	if len(os.Args) == 2 && os.Args[1] == "sandbox-supervisor" {
		os.Exit(sandbox.SupervisorMain())
	}
	if len(os.Args) > 1 && os.Args[1] == "sandbox-helper" {
		os.Exit(sandbox.HelperMain(os.Args[2:]))
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "molr-server:", err)
		os.Exit(1)
	}
}
func password(r io.Reader) (core.Secret, error) {
	data, err := io.ReadAll(io.LimitReader(r, 4097))
	if err != nil {
		return core.Secret{}, err
	}
	defer clear(data)
	if len(data) > 4096 {
		return core.Secret{}, core.Invalid("password", "password input too large")
	}
	value := strings.TrimRight(string(data), "\r\n")
	if value == "" {
		return core.Secret{}, core.Invalid("password", "password required on stdin")
	}
	return core.NewSecret(value), nil
}
func run(ctx context.Context, args []string, input io.Reader, output, diagnostics io.Writer) error {
	if len(args) == 1 && args[0] == "version" {
		fmt.Fprintf(output, "Molr %s (%s, %s)\n", buildinfo.Version, commit, builtAt)
		return nil
	}
	if len(args) == 0 {
		fmt.Fprintln(output, "Usage: molr-server <bootstrap|init|serve|access|doctor|recover|backup|restore|version> -config /path/server.json")
		return nil
	}
	command := args[0]
	switch command {
	case "bootstrap", "access", "doctor", "init", "serve", "recover", "backup", "restore":
	default:
		return fmt.Errorf("unknown command %q", command)
	}
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(diagnostics)
	configPath := flags.String("config", "configs/server.example.json", "strict server configuration JSON")
	username := flags.String("username", "owner", "initial owner username")
	passwordStdin := flags.Bool("password-stdin", false, "read password from standard input (init/recover only)")
	mfaCode := flags.String("mfa-code", "", "MFA code for online access/doctor")
	resetMFA := flags.Bool("reset-mfa", false, "disable MFA during local recovery")
	manifest := flags.String("backup-manifest", "", "backup descriptor JSON to restore, while server is stopped")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments")
	}
	file, err := os.Open(*configPath)
	if err != nil {
		return fmt.Errorf("open config: %w", err)
	}
	cfg, err := config.Load(file)
	_ = file.Close()
	if err != nil {
		return err
	}
	if command == "restore" {
		if *manifest == "" {
			return core.Invalid("backup_manifest", "backup descriptor required")
		}
		f, err := os.Open(*manifest)
		if err != nil {
			return err
		}
		defer f.Close()
		var backup sqlite.Backup
		decoder := json.NewDecoder(io.LimitReader(f, 65536))
		decoder.DisallowUnknownFields()
		if err = decoder.Decode(&backup); err != nil {
			return err
		}
		var extra any
		if decoder.Decode(&extra) != io.EOF {
			return core.Invalid("backup_manifest", "trailing data")
		}
		restorer, err := sqlite.NewRestorer(filepath.Join(cfg.DataDir, "runtime.db"))
		if err != nil {
			return err
		}
		if err = restorer.Restore(ctx, backup); err != nil {
			return err
		}
		fmt.Fprintln(output, "Database restored; previous sessions and API tokens are invalidated.")
		return nil
	}
	var secret core.Secret
	if command == "init" || command == "recover" {
		if !*passwordStdin {
			return core.Invalid("password_stdin", "use -password-stdin; passwords are never command-line arguments")
		}
		secret, err = password(input)
		if err != nil {
			return err
		}
	}
	if command == "access" || command == "doctor" {
		if *passwordStdin {
			secret, err = password(input)
		} else {
			var data []byte
			data, err = os.ReadFile(filepath.Join(cfg.DataDir, "initial-password"))
			if err == nil {
				secret = core.NewSecret(strings.TrimSpace(string(data)))
				clear(data)
			}
		}
		if err != nil {
			return fmt.Errorf("read login password (or use -password-stdin): %w", err)
		}
		return online(ctx, cfg, command, *username, *mfaCode, secret, output)
	}
	bundle, err := app.Build(ctx, cfg)
	if err != nil {
		return err
	}
	defer bundle.Close()
	switch command {
	case "bootstrap":
		return bootstrap(ctx, bundle, cfg, *username, output)
	case "init":
		account, err := bundle.Bootstrap.Initialize(ctx, auth.InitializeRequest{Username: *username, Password: secret})
		if err != nil {
			return err
		}
		return json.NewEncoder(output).Encode(account)
	case "recover":
		if err = bundle.Bootstrap.Recover(ctx, auth.RecoveryRequest{Password: secret, ResetMFA: *resetMFA}); err != nil {
			return err
		}
		fmt.Fprintln(output, "Owner recovered; previous sessions and API tokens are invalidated.")
		return nil
	case "backup":
		backup, err := bundle.Database.Backup(ctx)
		if err != nil {
			return err
		}
		return json.NewEncoder(output).Encode(backup)
	case "serve":
		fmt.Fprintf(diagnostics, "Starting Molr on %s (%s)\n", cfg.HTTP.Listen, cfg.HTTP.PublicURL)
		return bundle.Application.Run(ctx)
	}
	return nil
}
