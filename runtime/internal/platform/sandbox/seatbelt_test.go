package sandbox

import (
	"fmt"
	"molr.ai/runtime/internal/browser"
	"molr.ai/runtime/internal/platform"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestSeatbeltPlanRejectsUnsupportedControls(t *testing.T) {
	base := platform.Command{Script: "true", Directory: "/work", Timeout: time.Second, MaxOutputBytes: 4096}
	if err := validateSeatbelt(base); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*platform.Command){
		func(c *platform.Command) { c.Limits.CPUPercent = 100 },
		func(c *platform.Command) {
			c.NetworkTargets = []platform.NetworkTarget{{Protocols: []string{"tcp"}, Addresses: []string{"0.0.0.0/0"}, Ports: []string{"443"}}}
		},
		func(c *platform.Command) { c.ForwardPorts = []int{8080} },
		func(c *platform.Command) { c.NetworkAccess = true; c.ForwardPorts = []int{browserControlFirst} },
		func(c *platform.Command) { c.BrowserAssets = true },
		func(c *platform.Command) { c.Environment = map[string]string{"DYLD_INSERT_LIBRARIES": "evil"} },
	} {
		c := base
		change(&c)
		if validateSeatbelt(c) == nil {
			t.Fatalf("accepted %+v", c)
		}
	}
	for _, path := range []string{"/", "relative", "/a/../b", "/a\n(allow default)", "/a\x00b"} {
		if _, err := seatbeltProfile([]string{path}, nil, "", false); err == nil {
			t.Fatalf("accepted %q", path)
		}
	}
	profile, err := seatbeltProfile([]string{`/中文/quote"\(allow default)`}, []string{"/work"}, "/dev/ttys009", true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(profile, `(deny process-info*)`) || strings.Contains(profile, `(allow network*)`) || strings.Contains(profile, `(subpath "/dev")`) || !strings.Contains(profile, `(literal "/dev/ttys009")`) {
		t.Fatal(profile)
	}
}

func TestSeatbeltBrowserControlPorts(t *testing.T) {
	for _, network := range []bool{false, true} {
		ordinary, err := seatbeltProfile(nil, []string{"/workspace"}, "", network)
		if err != nil {
			t.Fatal(err)
		}
		browser, err := seatbeltBrowserProfile([]string{"/programs"}, []string{"/workspace", "/private/session"}, "/private/session", network, []int{browserControlFirst, browserControlFirst + 1})
		if err != nil {
			t.Fatal(err)
		}
		for port := browserControlFirst; port < browserControlLast; port++ {
			denied := fmt.Sprintf(`(deny network-outbound (remote tcp "*:%d"))`, port)
			bind := fmt.Sprintf(`(deny network-bind network-inbound (local tcp "*:%d"))`, port)
			if !strings.Contains(ordinary, denied) || !strings.Contains(ordinary, bind) {
				t.Fatalf("ordinary control port exposed: %d", port)
			}
			if strings.Contains(browser, denied) != (port > browserControlFirst+1) {
				t.Fatalf("browser control scope: %d", port)
			}
		}
		if strings.Contains(browser, `(allow default)`) || strings.Contains(browser, `(subpath "/private/tmp")`) {
			t.Fatal("broad browser policy")
		}
	}
}

func TestFullAccessBrowserPlanRequiresTrustedAssets(t *testing.T) {
	r := New(Options{AllowFullAccess: true})
	base := platform.Command{FullAccess: true, Managed: true, BrowserAssets: true, BrowserProxy: &browser.Proxy{ProxySummary: browser.ProxySummary{Mode: "direct"}}, Script: "exec /molr/helper sandbox-helper browser", Directory: "/work", MaxOutputBytes: 4096, ForwardPorts: []int{browser.CommandPort, browser.StreamPort}, Mounts: []platform.Mount{{Source: "/programs/agent", Destination: "/resources/browser-agent", ReadOnly: true}, {Source: "/programs/chrome", Destination: "/resources/browser-chromium", ReadOnly: true}}}
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		if r.validateMode(base) == nil {
			t.Fatal("unsupported browser accepted")
		}
		return
	}
	if err := r.validateMode(base); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*platform.Command){
		func(c *platform.Command) { c.BrowserAssets = false }, func(c *platform.Command) { c.BrowserProxy = nil }, func(c *platform.Command) { c.Managed = false }, func(c *platform.Command) { c.TTY = true }, func(c *platform.Command) { c.Script = "exec arbitrary" }, func(c *platform.Command) { c.Mounts = nil }, func(c *platform.Command) { c.Mounts[0].ReadOnly = false }, func(c *platform.Command) { c.Mounts[1].Destination = c.Mounts[0].Destination }, func(c *platform.Command) { c.ForwardPorts = []int{1234} },
	} {
		c := base
		c.Mounts = append([]platform.Mount(nil), base.Mounts...)
		change(&c)
		if r.validateMode(c) == nil {
			t.Fatalf("unsafe browser plan accepted: %+v", c)
		}
	}
}
