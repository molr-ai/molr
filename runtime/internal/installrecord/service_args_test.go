package installrecord

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestServiceArgsKeepRuntimeAndSandboxBoundaries(t *testing.T) {
	root := t.TempDir()
	program := filepath.Join(root, "program with spaces")
	config := filepath.Join(root, "config with spaces")
	data := filepath.Join(root, "data with spaces")
	runtimeArgs := []string{filepath.Join(program, "bin", "molr"), "serve", "-config", filepath.Join(config, "server.json")}
	for _, tc := range []struct {
		name, target, binary string
		args                 []string
	}{
		{"default", "", "molr", runtimeArgs},
		{"runtime", "runtime", "molr", runtimeArgs},
		{"sandbox", "sandbox", "molr-sandbox", []string{filepath.Join(program, "bin", "molr-sandbox"), "-config", filepath.Join(config, "server.json"), "-token-file", filepath.Join(data, "sandbox-token")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			record := Record{Target: tc.target, Paths: Paths{Program: program, Config: config}, DataDir: data}
			if got := record.Binary(); got != tc.binary {
				t.Fatalf("binary = %q, want %q", got, tc.binary)
			}
			if got := record.ServiceArgs(); !reflect.DeepEqual(got, tc.args) {
				t.Fatalf("service arguments = %q, want %q", got, tc.args)
			}
		})
	}
}
