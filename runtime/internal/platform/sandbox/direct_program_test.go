//go:build linux || darwin || windows

package sandbox

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"reflect"
	"runtime"
	"slices"
	"testing"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/platform"
)

func TestDirectProgramArgumentChild(t *testing.T) {
	index := slices.Index(os.Args, "--")
	if index < 0 {
		return
	}
	data, _ := json.Marshal(os.Args[index+1:])
	os.Stdout.Write(data)
	os.Exit(0)
}

// Runs natively on each supported platform; Windows TTY uses the CI Runtime helper.
func TestDirectProgramLiteralArguments(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, tty := range []bool{false, true} {
		name := "pipes"
		if tty {
			name = "tty"
		}
		t.Run(name, func(t *testing.T) {
			helper := os.Getenv("MOLR_TEST_BINARY")
			if tty && runtime.GOOS == "windows" && helper == "" {
				t.Skip("Windows TTY requires MOLR_TEST_BINARY")
			}
			r := New(Options{QuotaStore: quotaTestStore(t), PrivateRoot: t.TempDir(), Helper: helper, AllowFullAccess: true, Authorize: func(context.Context, core.Execution, platform.Command) error { return nil }})
			defer r.Close()
			want := []string{"a b", "", `quote"value`, `C:\path with spaces\`, "$(echo injected)", "; & | >"}
			c := platform.Command{FullAccess: true, Executable: executable, Args: append([]string{"-test.run=^TestDirectProgramArgumentChild$", "--"}, want...), TTY: tty, Timeout: 30 * time.Second, MaxOutputBytes: 4096}
			p, err := r.Start(t.Context(), testExecution(), c)
			if err != nil {
				t.Fatal(err)
			}
			exit, err := p.Wait(t.Context())
			if err != nil || exit.Code != 0 {
				t.Fatal(exit, err)
			}
			data, err := io.ReadAll(p.Stdout())
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			if err := json.Unmarshal(data, &got); err != nil || !reflect.DeepEqual(got, want) {
				t.Fatalf("literal arguments: %s (%v)", data, err)
			}
			c.Script = "should never be interpreted"
			if _, err := r.Start(t.Context(), testExecution(), c); err == nil {
				t.Fatal("combined script and executable accepted")
			}
		})
	}
}
