package platform

import (
	"net/netip"
	"strings"
	"testing"
)

func TestNetworkRuleAddressAndPortSets(t *testing.T) {
	rule, err := CompileNetworkTarget(NetworkTarget{Protocols: []string{"tcp"}, Addresses: []string{"192.168.1.0/24", "10.20.0.0/16", "fd12::/32", "::1", "EXAMPLE.com"}, Ports: []string{"80", "443", "8000-8099", "9000-9099"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		host, ip, port string
		want           bool
	}{
		{"192.168.1.20", "192.168.1.20", "8000", true},
		{"lan.example", "10.20.1.9", "8099", true},
		{"lan.example", "10.20.1.9", "9000", true},
		{"lan.example", "10.20.1.9", "9099", true},
		{"lan.example", "10.20.1.9", "8100", false},
		{"lan.example", "10.21.1.9", "80", false},
		{"lan.example", "192.168.2.20", "443", false},
		{"lan.example", "fd12::123", "443", true},
		{"::1", "::1", "80", true},
		{"mapped", "::ffff:192.168.1.20", "443", true},
		{"example.COM", "93.184.216.34", "443", true},
		{"other.example", "93.184.216.34", "443", false},
		{"example.com", "127.0.0.1", "443", true},
		{"example.com", "93.184.216.34", "0443", false},
	} {
		if got := rule.Allows(tc.host, tc.port, netip.MustParseAddr(tc.ip)); got != tc.want {
			t.Errorf("%+v: got %v", tc, got)
		}
	}
}

func TestNetworkRuleWidePrefixesRetainAddressGuards(t *testing.T) {
	for _, private := range []bool{false, true} {
		var exclusions []string
		if !private {
			exclusions = []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "fc00::/7", "127.0.0.0/8", "::1/128", "100.64.0.0/10"}
		}
		rule, err := CompileNetworkTarget(NetworkTarget{Protocols: []string{"tcp"}, Addresses: []string{"0.0.0.0/0", "::/0"}, Ports: []string{"1-65535"}, ExcludeAddresses: exclusions})
		if err != nil {
			t.Fatal(err)
		}
		for _, ip := range []string{"0.0.0.0", "::", "169.254.169.254", "100.100.100.200", "168.63.129.16", "fd00:ec2::254", "fe80::1", "fe80::1%eth0", "224.0.0.1", "ff02::1", "::ffff:169.254.169.254"} {
			if rule.Allows(ip, "80", netip.MustParseAddr(ip)) {
				t.Fatalf("private=%v: prohibited address %s accepted", private, ip)
			}
		}
		for _, ip := range []string{"127.0.0.1", "::1", "10.0.0.1", "192.168.1.1", "100.64.0.1", "fd12::1", "::ffff:127.0.0.1"} {
			if rule.Allows(ip, "80", netip.MustParseAddr(ip)) != private {
				t.Fatalf("private=%v: incorrect policy for %s", private, ip)
			}
		}
		for _, port := range []string{"1", "65535"} {
			if !rule.Allows("public.example", port, netip.MustParseAddr("2606:4700:4700::1111")) {
				t.Fatal("public IPv6 rejected")
			}
		}
	}
}

func TestNetworkTargetRejectsInvalidConfiguration(t *testing.T) {
	for _, address := range []string{"", "*", "https://example.com", "user@example.com", "example.com.", "-bad.test", "bad_.test", "192.168.1.1/24", "10.0.0.0/33", "::ffff:192.168.1.0/120", "fd12::1/32", "fe80::1%eth0", "example.com:80", strings.Repeat("a", 64) + ".test"} {
		if _, err := CompileNetworkTarget(NetworkTarget{Protocols: []string{"tcp"}, Addresses: []string{address}, Ports: []string{"80"}}); err == nil {
			t.Errorf("accepted address %q", address)
		}
	}
	for _, port := range []string{"", "0", "65536", "0443", " 80", "+80", "80-", "-80", "90-80", "0-100", "80-65536", "80-90-100", "*", "80,443"} {
		if _, err := CompileNetworkTarget(NetworkTarget{Protocols: []string{"tcp"}, Addresses: []string{"example.com"}, Ports: []string{port}}); err == nil {
			t.Errorf("accepted port %q", port)
		}
	}
	for _, target := range []NetworkTarget{
		{}, {Protocols: []string{"tcp"}, Addresses: []string{"example.com"}}, {Ports: []string{"80"}},
		{Addresses: make([]string, 33), Ports: []string{"80"}},
		{Protocols: []string{"tcp"}, Addresses: []string{"example.com"}, Ports: make([]string, 33)},
	} {
		if _, err := CompileNetworkTarget(target); err == nil {
			t.Errorf("accepted unbounded/empty target %+v", target)
		}
	}
}

func TestNetworkRuleExclusions(t *testing.T) {
	rule, err := CompileNetworkTarget(NetworkTarget{Protocols: []string{"tcp"}, Addresses: []string{"0.0.0.0/0", "::/0"}, Ports: []string{"1-65535"}, ExcludeAddresses: []string{"10.0.0.0/8", "fd12::/32", "192.168.1.10", "blocked.example"}, ExcludePorts: []string{"22", "8000-8099", "9000-9099"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		host, ip, port string
		want           bool
	}{
		{"service.example", "192.168.1.20", "7999", true},
		{"service.example", "192.168.1.20", "8000", false},
		{"service.example", "192.168.1.20", "8099", false},
		{"service.example", "192.168.1.20", "8100", true},
		{"service.example", "192.168.1.20", "22", false},
		{"service.example", "192.168.1.20", "9099", false},
		{"service.example", "10.1.2.3", "443", false},
		{"service.example", "::ffff:10.1.2.3", "443", false},
		{"service.example", "fd12::1", "443", false},
		{"service.example", "192.168.1.10", "443", false},
		{"BLOCKED.example", "93.184.216.34", "443", false},
		{"other.example", "93.184.216.34", "443", true},
	} {
		if got := rule.Allows(tc.host, tc.port, netip.MustParseAddr(tc.ip)); got != tc.want {
			t.Errorf("%+v: got %v", tc, got)
		}
	}
	// A hostname allow does not override a resolved-address exclusion.
	rule, err = CompileNetworkTarget(NetworkTarget{Protocols: []string{"tcp"}, Addresses: []string{"service.example"}, Ports: []string{"443"}, ExcludeAddresses: []string{"127.0.0.0/8"}})
	if err != nil {
		t.Fatal(err)
	}
	if rule.Allows("service.example", "443", netip.MustParseAddr("127.0.0.1")) {
		t.Fatal("hostname bypassed excluded range")
	}
	// Overlap is allowed; exclusion takes precedence, including a fully excluded rule.
	rule, err = CompileNetworkTarget(NetworkTarget{Protocols: []string{"tcp"}, Addresses: []string{"example.com"}, Ports: []string{"443"}, ExcludeAddresses: []string{"example.com"}})
	if err != nil || rule.Allows("example.com", "443", netip.MustParseAddr("93.184.216.34")) {
		t.Fatal("fully excluded rule did not deny")
	}
}

func TestNetworkRuleValidatesExclusionSets(t *testing.T) {
	for _, target := range []NetworkTarget{
		{Protocols: []string{"tcp"}, Addresses: []string{"example.com"}, Ports: []string{"80"}, ExcludeAddresses: []string{"10.0.0.1/8"}},
		{Protocols: []string{"tcp"}, Addresses: []string{"example.com"}, Ports: []string{"80"}, ExcludeAddresses: []string{"*"}},
		{Protocols: []string{"tcp"}, Addresses: []string{"example.com"}, Ports: []string{"80"}, ExcludeAddresses: make([]string, 33)},
		{Protocols: []string{"tcp"}, Addresses: []string{"example.com"}, Ports: []string{"80"}, ExcludePorts: []string{"90-80"}},
		{Protocols: []string{"tcp"}, Addresses: []string{"example.com"}, Ports: []string{"80"}, ExcludePorts: make([]string, 33)},
	} {
		if _, err := CompileNetworkTarget(target); err == nil {
			t.Errorf("accepted invalid exclusions %+v", target)
		}
	}
}

func TestNetworkTransportAndDNSAuthorization(t *testing.T) {
	rule, err := CompileNetworkTarget(NetworkTarget{Protocols: []string{"tcp"}, Addresses: []string{"allowed.test"}, Ports: []string{"443"}, ExcludeAddresses: []string{"10.0.0.0/8"}})
	if err != nil {
		t.Fatal(err)
	}
	public := netip.MustParseAddr("192.0.2.1")
	private := netip.MustParseAddr("10.0.0.1")
	if !rule.MayResolve("allowed.test") || rule.MayResolve("other.test") {
		t.Fatal("domain resolution scope changed")
	}
	if !rule.AllowsAddress("allowed.test", public) || rule.AllowsAddress("allowed.test", private) {
		t.Fatal("DNS address exclusion ignored")
	}
	if !rule.AllowsTransport("tcp", "allowed.test", "443", public) || rule.AllowsTransport("udp", "allowed.test", "443", public) || rule.AllowsTransport("tcp", "allowed.test", "80", public) {
		t.Fatal("protocol and port authorization mixed")
	}
}

func TestFullyExcludedPortsDoNotAuthorizeDNS(t *testing.T) {
	rule, err := CompileNetworkTarget(NetworkTarget{Protocols: []string{"tcp"}, Addresses: []string{"0.0.0.0/0"}, Ports: []string{"1-65535"}, ExcludePorts: []string{"1-100", "101-65535"}})
	if err != nil {
		t.Fatal(err)
	}
	if rule.MayResolve("data.example") || rule.AllowsAddress("data.example", netip.MustParseAddr("192.0.2.1")) {
		t.Fatal("empty network scope authorized DNS")
	}
}
