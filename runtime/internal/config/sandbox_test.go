package config

import (
	"fmt"
	"molr.ai/runtime/internal/platform"
	"net/netip"
	"strings"
	"testing"
	"time"
)

func TestSandboxDefaultsEnabledWithoutBlockingUnconfiguredRuntime(t *testing.T) {
	const input = `{"data_dir":"/var/lib/molr","http":{"public_url":"https://molr.example.com","tls_mode":"proxy","trusted_proxies":["192.0.2.10/32"]}}`
	c, err := Load(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	if !c.Sandbox.Enabled || c.Sandbox.Timeout <= 0 || (!platform.NativeSandbox() && c.Sandbox.MinFreeDiskBytes <= 0) {
		t.Fatal("sandbox must default on with bounded limits")
	}
	for _, change := range []func(*Sandbox){
		func(s *Sandbox) { s.RootFS = "relative/rootfs" },
		func(s *Sandbox) { s.WorkRoot = "/var/lib/molr/work" },
		func(s *Sandbox) {
			if platform.NativeSandbox() {
				s.MemoryBytes = 1
			} else {
				s.MemoryBytes = 0
			}
		},
		func(s *Sandbox) { s.HostToolsUser = "bad/account" },
		func(s *Sandbox) { s.HostToolsUser = "molr-sandbox-review" },
		func(s *Sandbox) { s.HostToolsUser = "molr-sandbox"; s.RootFS = "/opt/custom-rootfs" },
		func(s *Sandbox) {
			if platform.NativeSandbox() {
				s.MinFreeDiskBytes = 1
			} else {
				s.MinFreeDiskBytes = 0
			}
		},
	} {
		invalid := c
		change(&invalid.Sandbox)
		if invalid.Validate() == nil {
			t.Fatal("invalid explicit sandbox configuration accepted")
		}
	}
	disabled := strings.TrimSuffix(input, "}") + `,"sandbox":{"enabled":false}}`
	c, err = Load(strings.NewReader(disabled))
	if err != nil || c.Sandbox.Enabled {
		t.Fatalf("explicit disable not respected: enabled=%v err=%v", c.Sandbox.Enabled, err)
	}
}

func TestSandboxNetworkConfiguration(t *testing.T) {
	if platform.NativeSandbox() {
		t.Skip("Linux hard quotas and network rules; macOS uses duration grants and a network toggle")
	}
	const base = `{"data_dir":"/var/lib/molr","http":{"public_url":"https://molr.example.com","tls_mode":"proxy","trusted_proxies":["192.0.2.10/32"]},"sandbox":{"networks":[%s]}}`
	for _, target := range []string{
		`{"name":"lan","protocols":["tcp"],"addresses":["192.168.1.0/24","fd12::/32"],"ports":["80","443","8000-8099","9000-9099"],"exclude_addresses":["192.168.1.10"],"exclude_ports":["8080-8089"]}`,
		`{"name":"internet","protocols":["tcp"],"addresses":["0.0.0.0/0","::/0"],"ports":["80","443"]}`,
	} {
		if _, err := Load(strings.NewReader(fmt.Sprintf(base, target))); err != nil {
			t.Fatal(err)
		}
	}
	for _, target := range []string{
		`{"name":"old","host":"example.com","port":"80"}`,
		`{"name":"old","protocols":["tcp"],"addresses":["example.com"],"ports":["80"],"allow_private":true}`,
		`{"name":"bad","protocols":["tcp"],"addresses":["example.com"],"ports":["80"],"exclude_ports":["9000-8000"]}`,
		`{"name":"bad","protocols":["tcp"],"addresses":["192.168.1.1/24"],"ports":["80"]}`,
		`{"name":"bad","protocols":["tcp"],"addresses":["example.com"],"ports":["9000-8000"]}`,
		`{"name":"empty","protocols":["tcp"],"addresses":[],"ports":["80"]}`,
	} {
		if _, err := Load(strings.NewReader(fmt.Sprintf(base, target))); err == nil {
			t.Fatalf("accepted %s", target)
		}
	}
}

func TestSandboxTimeoutDefaultAndOverride(t *testing.T) {
	base := `{"data_dir":"/var/lib/molr","http":{"public_url":"https://molr.example.com","tls_mode":"proxy","trusted_proxies":["192.0.2.10/32"]}`
	for _, tc := range []struct {
		suffix string
		want   time.Duration
	}{{"}", 10 * time.Minute}, {`,"sandbox":{"timeout":"3m"}}`, 3 * time.Minute}} {
		c, err := Load(strings.NewReader(base + tc.suffix))
		if err != nil || c.Sandbox.Timeout.Value() != tc.want {
			t.Fatalf("timeout=%v err=%v", c.Sandbox.Timeout, err)
		}
	}
}

func TestSandboxNetworkRequiresExplicitProtocolsAndPresets(t *testing.T) {
	if platform.NativeSandbox() {
		t.Skip("Linux hard quotas and network rules; macOS uses duration grants and a network toggle")
	}
	base := `{"data_dir":"/var/lib/molr","http":{"public_url":"https://molr.example.com","tls_mode":"proxy","trusted_proxies":["127.0.0.1/32"]},"sandbox":%s}`
	for _, network := range []string{
		`{"networks":[{"name":"legacy","addresses":["example.com"],"ports":["443"]}]}`,
		`{"networks":[{"name":"bad","protocols":["icmp"],"addresses":["example.com"],"ports":["443"]}]}`,
		`{"networks":[{"name":"bad","protocols":["tcp","tcp"],"addresses":["example.com"],"ports":["443"]}]}`,
		`{"network_presets":[],"networks":[{"name":"preset:web","protocols":["tcp"],"addresses":["0.0.0.0/0"],"ports":["1-65535"]}]}`,
		`{"network_presets":["unknown"]}`, `{"network_presets":["internet","internet"]}`, `{"network_presets":["web"]}`, `{"network_presets":["molr"]}`, `{"network_connections":0}`, `{"network_bytes":0}`,
	} {
		if _, err := Load(strings.NewReader(fmt.Sprintf(base, network))); err == nil {
			t.Fatalf("accepted %s", network)
		}
	}
	c, err := Load(strings.NewReader(fmt.Sprintf(base, `{"network_presets":[],"networks":[]}`)))
	if err != nil || len(c.Sandbox.NetworkTargets(c.OwnListenPort())) != 0 {
		t.Fatal("explicitly disabled presets exposed networks", err)
	}
	c, err = Load(strings.NewReader(fmt.Sprintf(base, `{}`)))
	if err != nil || len(c.Sandbox.NetworkTargets(c.OwnListenPort())) != 3 {
		t.Fatal("default optional presets missing", err)
	}
	for _, network := range c.Sandbox.NetworkTargets(c.OwnListenPort()) {
		if _, err := platform.CompileNetworkTarget(network.Target()); err != nil {
			t.Fatal(err)
		}
	}
}

// Preset contents are a security boundary; lock them against accidental edits.
func TestNetworkPresetBoundaries(t *testing.T) {
	if platform.NativeSandbox() {
		t.Skip("Linux hard quotas and network rules; macOS uses duration grants and a network toggle")
	}
	const base = `{"data_dir":"/var/lib/molr","http":{"public_url":"https://molr.example.com","tls_mode":"proxy","trusted_proxies":["127.0.0.1/32"],"listen":"127.0.0.1:8080"},"sandbox":{"network_presets":[%s]}}`
	load := func(t *testing.T, presets string) map[string]platform.NetworkRule {
		t.Helper()
		c, err := Load(strings.NewReader(fmt.Sprintf(base, presets)))
		if err != nil {
			t.Fatal(err)
		}
		rules := map[string]platform.NetworkRule{}
		for _, n := range c.Sandbox.NetworkTargets(c.OwnListenPort()) {
			rule, err := platform.CompileNetworkTarget(n.Target())
			if err != nil {
				t.Fatal(err)
			}
			rules[n.Name] = rule
		}
		return rules
	}
	type check struct {
		protocol, port, ip string
	}
	allowed := func(rule platform.NetworkRule, c check) bool {
		return rule.AllowsTransport(c.protocol, "", c.port, netip.MustParseAddr(c.ip))
	}

	host := load(t, `"host"`)["preset:host"]
	for _, c := range []check{{"tcp", "8080", "127.0.0.1"}, {"udp", "8080", "::1"}, {"tcp", "1083", "127.3.2.1"}, {"udp", "65535", "127.255.255.254"}} {
		if !allowed(host, c) {
			t.Fatalf("host must allow all loopback ports: %+v", c)
		}
	}
	for _, c := range []check{{"tcp", "8080", "192.168.1.8"}, {"udp", "53", "8.8.8.8"}} {
		if allowed(host, c) {
			t.Fatalf("host must exclude non-loopback: %+v", c)
		}
	}
	lan := load(t, `"lan"`)["preset:lan"]
	for _, c := range []check{{"tcp", "1083", "192.168.1.8"}, {"udp", "1", "10.0.0.1"}, {"tcp", "65535", "172.31.255.254"}, {"udp", "1083", "fd12::1"}} {
		if !allowed(lan, c) {
			t.Fatalf("LAN must allow all private ports: %+v", c)
		}
	}
	for _, c := range []check{{"tcp", "1083", "127.0.0.1"}, {"udp", "53", "::1"}, {"tcp", "443", "8.8.8.8"}, {"tcp", "443", "172.32.0.1"}} {
		if allowed(lan, c) {
			t.Fatalf("LAN must exclude loopback/public addresses: %+v", c)
		}
	}

	internet := load(t, `"internet"`)["preset:internet"]
	for _, c := range []check{
		{"tcp", "1083", "8.8.8.8"}, {"udp", "65535", "8.8.8.8"}, {"tcp", "1", "2001:4860::8888"}, {"tcp", "443", "100.64.0.1"},
	} {
		if !allowed(internet, c) {
			t.Fatalf("internet preset must allow public traffic: %+v", c)
		}
	}
	for _, c := range []check{
		{"tcp", "443", "127.0.0.1"}, {"tcp", "443", "::1"},
		{"tcp", "443", "10.0.0.1"}, {"tcp", "443", "172.16.0.1"}, {"tcp", "443", "192.168.1.1"},
		{"tcp", "443", "fd00::1"},
		{"tcp", "443", "169.254.169.254"}, {"tcp", "443", "100.100.100.200"}, {"tcp", "443", "fd00:ec2::254"},
		{"tcp", "443", "198.18.0.1"}, {"tcp", "443", "198.19.255.254"}, {"tcp", "443", "fe80::1"},
		{"tcp", "443", "0.0.0.0"}, {"tcp", "443", "224.0.0.1"},
	} {
		if allowed(internet, c) {
			t.Fatalf("internet preset must refuse non-public traffic: %+v", c)
		}
	}
}

func TestHostToolsIdentityModes(t *testing.T) {
	for _, name := range []string{"", "current", "molr-sandbox"} {
		s := sandboxDefaults()
		s.HostToolsUser = name
		if err := s.validate("/var/lib/molr", 8080); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if name != "" {
			s.RootFS = "/opt/rootfs"
			if s.validate("/var/lib/molr", 8080) == nil {
				t.Fatal("mixed host and custom rootfs accepted")
			}
		}
	}
}

func TestHostToolsInheritsDeploymentAccount(t *testing.T) {
	for _, setting := range []string{`"host_tools":true`, `"host_tools_user":"current"`, `"host_tools_user":"molr-sandbox"`} {
		raw := `{"data_dir":"/var/lib/molr","http":{"public_url":"https://molr.example.com","tls_mode":"proxy","trusted_proxies":["127.0.0.1/32"]},"sandbox":{` + setting + `}}`
		c, err := Load(strings.NewReader(raw))
		if err != nil || !c.Sandbox.UsesHostTools() {
			t.Fatalf("%s: %v", setting, err)
		}
		c.Sandbox.RootFS = "/opt/rootfs"
		if c.Validate() == nil {
			t.Fatal("host tools and custom rootfs accepted")
		}
	}
	s := sandboxDefaults()
	if s.UsesHostTools() {
		t.Fatal("unconfigured existing deployments changed")
	}
	s.HostTools = true
	if err := s.validate("/var/lib/molr", 8080); err != nil {
		t.Fatal(err)
	}
}

func TestFullAccessIsPermissionOptInNotMode(t *testing.T) {
	const base = `{"data_dir":"/var/lib/molr","http":{"public_url":"https://molr.example.com","tls_mode":"proxy","trusted_proxies":["192.0.2.10/32"]}`
	c, err := Load(strings.NewReader(base + `}`))
	if err != nil {
		t.Fatal(err)
	}
	if c.Sandbox.AllowFullAccess {
		t.Fatal("full access enabled implicitly")
	}
	c, err = Load(strings.NewReader(base + `,"sandbox":{"allow_full_access":true}}`))
	if err != nil || !c.Sandbox.AllowFullAccess || !c.Sandbox.Enabled {
		t.Fatal(c.Sandbox, err)
	}
	if _, err := Load(strings.NewReader(base + `,"sandbox":{"mode":"direct"}}`)); err == nil {
		t.Fatal("removed sandbox mode still accepted")
	}
}

// Disabling commands must not bypass validation for the still-active file layer.
func TestWorkspaceConfigurationValidatedWithCommandsDisabled(t *testing.T) {
	for _, change := range []func(*Sandbox){
		func(s *Sandbox) { s.WorkRoot = "/var/lib/molr/private" },
		func(s *Sandbox) { s.WorkRoot = "relative" },
		func(s *Sandbox) {
			if platform.NativeSandbox() {
				s.MaxFileBytes = 1
			} else {
				s.MaxFileBytes = 0
			}
		},
		func(s *Sandbox) { s.Directories = []SandboxDirectory{{Path: "/var/lib/molr"}} },
	} {
		cfg := Defaults("/var/lib/molr")
		cfg.HTTP.PublicURL = "https://runtime.example"
		cfg.HTTP.TLSMode = TrustedProxy
		cfg.HTTP.TrustedProxies = []string{"127.0.0.1/32"}
		cfg.Sandbox.Enabled = false
		change(&cfg.Sandbox)
		if cfg.Validate() == nil {
			t.Fatal("invalid storage accepted with commands disabled")
		}
	}
}
