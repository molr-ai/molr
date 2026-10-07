//go:build linux && (amd64 || arm64)

package sandbox

import (
	"context"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"

	"golang.org/x/net/dns/dnsmessage"
	"molr.ai/runtime/internal/platform"
)

func policyGateway(t *testing.T, targets ...platform.NetworkTarget) *networkGateway {
	t.Helper()
	g, err := newNetworkGateway(t.Context(), networkConfig{Targets: targets, Bytes: 64 << 20, Connections: 16}, func() bool { return true }, func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(g.close)
	return g
}
func dnsQuery(host string, kind dnsmessage.Type) []byte {
	name, _ := dnsmessage.NewName(host + ".")
	packet, _ := (&dnsmessage.Message{Header: dnsmessage.Header{ID: 123, RecursionDesired: true}, Questions: []dnsmessage.Question{{Name: name, Type: kind, Class: dnsmessage.ClassINET}}}).Pack()
	return packet
}
func dnsAnswer(t *testing.T, g *networkGateway, host string) dnsmessage.Message {
	t.Helper()
	var answer dnsmessage.Message
	if err := answer.Unpack(g.dnsReply(dnsQuery(host, dnsmessage.TypeA))); err != nil {
		t.Fatal(err)
	}
	return answer
}

func TestTransparentDomainIdentityAndRebinding(t *testing.T) {
	g := policyGateway(t, platform.NetworkTarget{Protocols: []string{"tcp"}, Addresses: []string{"allowed.test", "second.test"}, Ports: []string{"443"}, ExcludeAddresses: []string{"10.0.0.0/8"}})
	g.lookup = func(context.Context, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("192.0.2.1")}, nil
	}
	first, second := dnsAnswer(t, g, "allowed.test"), dnsAnswer(t, g, "second.test")
	if len(first.Answers) != 1 || len(second.Answers) != 1 {
		t.Fatal("domain DNS unavailable")
	}
	ip := netip.AddrFrom4(first.Answers[0].Body.(*dnsmessage.AResource).A)
	ip2 := netip.AddrFrom4(second.Answers[0].Body.(*dnsmessage.AResource).A)
	if !virtualPrefix.Contains(ip) || ip == ip2 {
		t.Fatal("domains on a shared IP lost identity")
	}
	if _, err := g.dial("tcp", netip.MustParseAddr("192.0.2.1"), 443); err == nil {
		t.Fatal("domain grant authorized raw shared IP")
	}
	if _, err := g.dial("udp", ip, 443); err == nil {
		t.Fatal("TCP grant authorized UDP")
	}
	if _, err := g.dial("tcp", ip, 80); err == nil {
		t.Fatal("port scope bypassed")
	}
	g.lookup = func(context.Context, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("192.0.2.1"), netip.MustParseAddr("10.0.0.1")}, nil
	}
	if _, err := g.dial("tcp", ip, 443); err == nil {
		t.Fatal("mixed/rebound DNS escaped exclusion")
	}
	if dnsAnswer(t, g, "allowed.test").RCode != dnsmessage.RCodeRefused {
		t.Fatal("mixed DNS answer returned")
	}
	var queries atomic.Int64
	g.lookup = func(context.Context, string) ([]netip.Addr, error) { queries.Add(1); return nil, nil }
	if dnsAnswer(t, g, "ungranted.test").RCode != dnsmessage.RCodeRefused || queries.Load() != 0 {
		t.Fatal("ungranted domain leaked a DNS query")
	}
}

func TestTransparentDNSBoundsAndRevocation(t *testing.T) {
	g := policyGateway(t, platform.NetworkTarget{Protocols: []string{"tcp", "udp"}, Addresses: []string{"0.0.0.0/0", "::/0"}, Ports: []string{"1-65535"}})
	g.lookup = func(context.Context, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("192.0.2.1")}, nil
	}
	for _, packet := range [][]byte{nil, []byte("not dns"), make([]byte, 12), []byte(strings.Repeat("x", 4096))} {
		g.dnsReply(packet)
	}
	var answer dnsmessage.Message
	if err := answer.Unpack(g.dnsReply(dnsQuery("example.test", dnsmessage.TypeTXT))); err != nil || answer.RCode != dnsmessage.RCodeRefused {
		t.Fatal("arbitrary DNS record type allowed")
	}
	g.authorize = func() bool { return false }
	if dnsAnswer(t, g, "example.test").RCode != dnsmessage.RCodeRefused {
		t.Fatal("revoked DNS allowed")
	}
	if _, err := g.dial("tcp", netip.MustParseAddr("192.0.2.1"), 443); err == nil {
		t.Fatal("revoked connection allowed")
	}
	g.authorize = func() bool { return true }
	for i := 0; i < 1024; i++ {
		g.names[strings.Repeat("x", i%40)+string(rune(i))] = "used"
	}
	if dnsAnswer(t, g, "new.test").RCode != dnsmessage.RCodeServerFailure {
		t.Fatal("DNS mapping capacity exceeded")
	}
}

func TestTransparentTrafficAccountingDoesNotOverdraw(t *testing.T) {
	var left atomic.Int64
	left.Store(100)
	if networkTake(&left, 60) != 60 || networkTake(&left, 80) != 40 || networkTake(&left, 1) != 0 || left.Load() != 0 {
		t.Fatal("shared quota overdrawn")
	}
}

func FuzzTransparentDNS(f *testing.F) {
	f.Add(dnsQuery("ungranted.test", dnsmessage.TypeA))
	f.Add([]byte{0, 1, 2})
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 4096 {
			return
		}
		g := policyGateway(t)
		g.dnsReply(data)
	})
}
