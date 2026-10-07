//go:build linux && (amd64 || arm64)

package sandbox

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"
)

func TestLinuxInteractiveShellProfileAndInterrupt(t *testing.T) {
	r := integrationRunner(t, nil)
	c := testCommand()
	c.Script = "echo 'export MOLR_PROFILE_MARK=profile_loaded' > /work/.profile"
	_, exit := executeTest(t, r, c)
	if exit.Code != 0 {
		t.Fatal(exit)
	}
	c.Script = "echo ${MOLR_PROFILE_MARK-unset}"
	out, exit := executeTest(t, r, c)
	if exit.Code != 0 || out != "unset\n" {
		t.Fatalf("noninteractive profile: %q %+v", out, exit)
	}
	c.TTY = true
	c.Managed = true
	c.Timeout = 0
	c.Script = "exec /bin/sh -i"
	e := testExecution()
	p, err := r.Start(t.Context(), e, c)
	if err != nil {
		t.Fatal(err)
	}
	output := make(chan string, 1)
	go func() { b, _ := io.ReadAll(p.Stdout()); output <- string(b) }()
	go io.Copy(io.Discard, p.Stderr())
	if err = r.Terminal(t.Context(), e, "echo $MOLR_PROFILE_MARK; echo kept > /tmp/tty-proof\n", 0, 0); err != nil {
		t.Fatal(err)
	}
	time.Sleep(150 * time.Millisecond)
	if err = r.Terminal(t.Context(), e, "sleep 30\n", 0, 0); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	if err = r.Terminal(t.Context(), e, "\x03", 30, 90); err != nil {
		t.Fatal(err)
	}
	time.Sleep(150 * time.Millisecond)
	if err = r.Terminal(t.Context(), e, "cat /tmp/tty-proof; echo after_interrupt; exit\n", 0, 0); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	exit, err = p.Wait(ctx)
	if err != nil {
		_ = p.Cancel(context.Background())
		t.Fatal(err)
	}
	out = <-output
	if exit.Code != 0 || !strings.Contains(out, "profile_loaded") || !strings.Contains(out, "kept") || !strings.Contains(out, "after_interrupt") || strings.Contains(out, "can't access tty") {
		t.Fatalf("TTY: %+v %q", exit, out)
	}
	c.TTY = false
	c.Timeout = time.Second
	c.Script = "test ! -e /tmp/tty-proof"
	_, exit = executeTest(t, r, c)
	if exit.Code != 0 {
		t.Fatal("temporary filesystem leaked")
	}
}
