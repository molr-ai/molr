//go:build linux && (amd64 || arm64)

package sandbox

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestLinuxHostToolsTTYInitialForeground(t *testing.T) {
	runHostToolsTTY(t, "probe", "probe", func(terminal *ttyTestTerminal) {
		terminal.wait("tty-initial-group-ok")
	})
}

type ttyTestTerminal struct {
	t       *testing.T
	file    *os.File
	ctx     context.Context
	mu      sync.Mutex
	text    strings.Builder
	changed chan struct{}
	done    chan struct{}
}

func newTTYTestTerminal(t *testing.T, file *os.File, ctx context.Context) *ttyTestTerminal {
	p := &ttyTestTerminal{t: t, file: file, ctx: ctx, changed: make(chan struct{}, 1), done: make(chan struct{})}
	go func() {
		defer close(p.done)
		b := make([]byte, 4096)
		for {
			n, err := file.Read(b)
			if n > 0 {
				p.mu.Lock()
				p.text.Write(b[:n])
				p.mu.Unlock()
				select {
				case p.changed <- struct{}{}:
				default:
				}
			}
			if err != nil {
				return
			}
		}
	}()
	return p
}
func (p *ttyTestTerminal) output() string   { p.mu.Lock(); defer p.mu.Unlock(); return p.text.String() }
func (p *ttyTestTerminal) wait(text string) { p.t.Helper(); p.waitFrom(text, 0) }
func (p *ttyTestTerminal) waitFrom(text string, offset int) {
	p.t.Helper()
	for !strings.Contains(p.output()[offset:], text) {
		select {
		case <-p.changed:
		case <-p.done:
			if strings.Contains(p.output()[offset:], text) {
				return
			}
			p.t.Fatalf("TTY exited waiting for %q: %s", text, p.output())
		case <-p.ctx.Done():
			p.t.Fatalf("TTY timed out waiting for %q: %s", text, p.output())
		}
	}
}
func (p *ttyTestTerminal) promptAfter(marker string) {
	p.t.Helper()
	p.wait(marker)
	offset := strings.Index(p.output(), marker) + len(marker)
	p.waitFrom("TTY_PROMPT> ", offset)
}
func (p *ttyTestTerminal) write(text string) {
	p.t.Helper()
	p.file.SetWriteDeadline(time.Now().Add(time.Second))
	if _, err := p.file.WriteString(text); err != nil {
		p.t.Fatal(err)
	}
}

func TestLinuxHostToolsTTYJobControl(t *testing.T) {
	for _, shell := range []string{"busybox", "dash", "bash"} {
		t.Run(shell, func(t *testing.T) {
			runHostToolsTTY(t, shell, "exec /bin/sh -i", func(terminal *ttyTestTerminal) {
				// Disable terminal echo before sending markers, so expectations
				// prove execution rather than merely observing echoed input.
				terminal.write("PS1='TTY_PROMPT> '; stty -echo; printf 'ready-%s\\n' tty\n")
				terminal.promptAfter("ready-tty")
				terminal.write("printf 'profile=%s\\n' \"$MOLR_PROFILE_MARK\"; echo saved > /tmp/proof\n")
				terminal.promptAfter("profile=profile_loaded")
				terminal.write("less /work/input\n")
				terminal.wait("tty-host-tools-ok")
				offset := len(terminal.output())
				terminal.write("q")
				terminal.waitFrom("TTY_PROMPT> ", offset)
				terminal.write("printf 'after-%s\\n' pager\n")
				terminal.promptAfter("after-pager")
				// The readiness marker is printed by the foreground job itself.
				terminal.write("/bin/sh -c 'echo foreground-ready; exec sleep 30'\n")
				terminal.wait("foreground-ready")
				offset = len(terminal.output())
				terminal.write("\x03")
				terminal.waitFrom("TTY_PROMPT> ", offset)
				terminal.write("printf 'after-%s\\n' interrupt; cat /tmp/proof\n")
				terminal.wait("after-interrupt")
				terminal.promptAfter("saved")
				terminal.write("/bin/sh -c 'echo stopping; kill -STOP $$; echo resumed'\n")
				terminal.promptAfter("stopping")
				terminal.write("printf 'prompt-%s\\n' returned; jobs\n")
				terminal.promptAfter("prompt-returned")
				terminal.write("fg\n")
				terminal.promptAfter("resumed")
				terminal.write("/bin/sh -c 'echo background-ready; exec sleep 30' & bg=$!\n")
				terminal.wait("background-ready")
				terminal.write("kill $bg; wait $bg; printf 'background-%s\\n' done; exit 0\n")
				terminal.wait("background-done")
			})
		})
	}
}
