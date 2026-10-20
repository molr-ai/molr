//go:build linux || darwin

package installation

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAdministratorDefaultsAndUnattendedOptIn(t *testing.T) {
	for _, args := range [][]string{nil, {"--non-interactive"}, {"--auto-init-admin"}, {"--init-owner", "my-admin"}} {
		o, err := parseOptions(args, io.Discard)
		if err != nil {
			t.Fatal(err)
		}
		if o.AutoInitAdmin != (len(args) > 0 && args[0] == "--auto-init-admin") {
			t.Fatal("automatic initialization must be explicit")
		}
	}
	if _, err := parseOptions([]string{"--auto-init-admin", "--init-owner", "admin"}, io.Discard); err == nil {
		t.Fatal("conflicting initialization accepted")
	}
	e, r := fixture(t)
	p := &Plan{Record: r}
	if err := e.prepareAdministrator(p); err == nil {
		t.Fatal("noninteractive default silently generated a password")
	}
	e.Options.AutoInitAdmin = true
	if err := e.prepareAdministrator(p); err != nil {
		t.Fatal(err)
	}
	e.Options.AutoInitAdmin = false
	e.Options.InitOwner = "my-admin"
	e.Input = strings.NewReader("fixture-password-2026\n")
	if err := e.prepareAdministrator(p); err != nil {
		t.Fatal(err)
	}
	if string(e.initPassword) != "fixture-password-2026" {
		t.Fatal("stdin password not captured")
	}
	if _, err := os.Stat(filepath.Join(r.DataDir, "initial-password")); !os.IsNotExist(err) {
		t.Fatal("password persisted")
	}
	clear(e.initPassword)
	e.initPassword = nil
	// An uninstalled instance with retained data uses its existing account.
	r.State = "uninstalled"
	if err := os.WriteFile(filepath.Join(r.DataDir, "runtime.db"), []byte("retained"), 0600); err != nil {
		t.Fatal(err)
	}
	e.Input = strings.NewReader("must-not-be-used")
	if err := e.prepareAdministrator(&Plan{Record: r, Previous: r}); err != nil {
		t.Fatal(err)
	}
	if len(e.initPassword) != 0 {
		t.Fatal("reinstallation read new credentials")
	}
}
