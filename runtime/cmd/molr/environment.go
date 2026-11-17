package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"molr.ai/runtime/internal/config"
	"molr.ai/runtime/internal/execenv"
)

func refreshEnvironment(ctx context.Context, args []string, output, diagnostics io.Writer) error {
	flags := flag.NewFlagSet("environment", flag.ContinueOnError)
	flags.SetOutput(diagnostics)
	configPath := flags.String("config", "", "server or standalone sandbox configuration JSON")
	names := flags.String("env", "", "comma-separated exported variables to import; default PATH")
	dryRun := flags.Bool("dry-run", false, "validate the selection without writing")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if *configPath == "" || flags.NArg() != 0 {
		return fmt.Errorf("use molr environment --config /path/server.json [--env PATH,JAVA_HOME] [--dry-run]")
	}
	f, err := os.Open(*configPath)
	if err != nil {
		return err
	}
	cfg, err := config.Load(f)
	f.Close()
	if err != nil {
		return err
	}
	selected, err := execenv.Capture(*names, os.Environ())
	if err != nil {
		return err
	}
	keys := make([]string, 0, len(selected))
	for name := range selected {
		keys = append(keys, name)
	}
	sort.Strings(keys)
	if *dryRun {
		if _, err := execenv.EncodeUpdate(cfg.DataDir, selected); err != nil {
			return err
		}
		fmt.Fprintf(output, "Would import %s into the execution environment for %s. No changes made.\n", strings.Join(keys, ", "), cfg.DataDir)
		return nil
	}
	if err := execenv.Save(ctx, cfg.DataDir, selected); err != nil {
		return err
	}
	fmt.Fprintf(output, "Imported %s into the execution environment for %s. New Full Access commands use it without restarting; running tasks keep their environment.\n", strings.Join(keys, ", "), cfg.DataDir)
	return nil
}
