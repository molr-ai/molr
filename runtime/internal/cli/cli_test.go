package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestRunConnectionOptionsAndTerminal(t *testing.T) {
	t.Setenv("MOLR_SERVER", "")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	for _, tc := range []struct {
		args []string
		want string
	}{
		{nil, "interactive terminal"},
		{[]string{"--demo", "extra"}, "unexpected arguments"},
		{[]string{"--server", "https://example.com"}, "interactive terminal"},
		{[]string{"--demo"}, "interactive terminal"},
	} {
		var output, diagnostics bytes.Buffer
		err := Run(context.Background(), tc.args, strings.NewReader(""), &output, &diagnostics)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%v: %v", tc.args, err)
		}
		if output.Len() != 0 {
			t.Fatal("failed command wrote terminal output")
		}
	}
	var help bytes.Buffer
	if err := Run(context.Background(), []string{"--help"}, nil, &help, &help); err != nil || !strings.Contains(help.String(), "-demo") {
		t.Fatalf("help: %s, %v", help.String(), err)
	}
}
