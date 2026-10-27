// Package cli assembles terminal clients without opening Runtime configuration or data.
package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"molr.ai/runtime/internal/runtimeclient"
	"molr.ai/runtime/internal/tui"
)

// Run starts the TUI with saved connections, or an explicitly selected offline demo.
func Run(ctx context.Context, args []string, input io.Reader, output, diagnostics io.Writer) error {
	flags := flag.NewFlagSet("molr tui", flag.ContinueOnError)
	flags.SetOutput(diagnostics)
	demo := flags.Bool("demo", false, "offline demo; no Runtime connection")
	server := flags.String("server", os.Getenv("MOLR_SERVER"), "open this Runtime (otherwise restore the last connection)")
	connections := flags.String("connections", "", "connection list file")
	session := flags.String("session", "", "session file for the explicitly selected Runtime")
	ca := flags.String("ca", "", "additional trusted CA certificate (PEM)")
	login := flags.Bool("login", false, "sign in again")
	logout := flags.Bool("logout", false, "revoke the selected session and exit")
	username := flags.String("username", "", "prefill the login username")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected arguments: use molr tui --help")
	}
	if *demo {
		explicitServer := false
		flags.Visit(func(f *flag.Flag) {
			if f.Name == "server" {
				explicitServer = true
			}
		})
		if explicitServer || *connections != "" || *session != "" || *ca != "" || *login || *logout || *username != "" {
			return errors.New("--demo cannot be combined with connection options")
		}
		return tui.Run(ctx, input, output)
	}
	if *connections == "" {
		dir, err := os.UserConfigDir()
		if err != nil {
			return err
		}
		*connections = filepath.Join(dir, "molr", "tui", "connections.json")
	}
	store, err := runtimeclient.OpenConnections(*connections)
	if err != nil {
		return err
	}
	initial := store.Last
	if *server != "" {
		c, err := store.Add("", *server, *ca, *session)
		if err != nil {
			return err
		}
		initial = c.ID
	} else if *ca != "" || *session != "" {
		return errors.New("--ca 和 --session 需要同时指定 --server")
	}
	if *logout {
		for _, c := range store.Items {
			if c.ID == initial {
				client, err := runtimeclient.New(c.Server, c.CAFile)
				if err != nil {
					return err
				}
				restored, err := client.Restore(store.SessionPath(c))
				if err != nil {
					return err
				}
				if restored {
					if err = client.Do(ctx, "POST", "/api/auth/logout", map[string]any{}, nil); err != nil {
						var e *runtimeclient.Error
						if !errors.As(err, &e) || e.Status != 401 {
							return err
						}
					}
				}
				if err = os.Remove(store.SessionPath(c)); err != nil && !errors.Is(err, os.ErrNotExist) {
					return err
				}
				fmt.Fprintln(output, "已退出登录，连接已保留。")
				return nil
			}
		}
		return errors.New("没有可退出的连接")
	}
	return tui.RunManaged(ctx, input, output, tui.ConnectionOptions{Store: store, Initial: initial, ForceLogin: *login, Username: *username})
}
