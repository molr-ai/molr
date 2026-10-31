package platform_test

import (
	"context"
	"io"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/platform"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"sync/atomic"
	"testing"
)

func TestNetworkHTTPClientChecksActualDialAndRevocation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, "http://169.254.169.254/latest/meta-data/", 302)
			return
		}
		_, _ = io.WriteString(w, "ok")
	}))
	defer server.Close()
	e := core.Execution{Principal: core.Principal{OwnerID: "owner", Kind: core.ActorAssistant, SubjectID: "assistant", ExecutionID: "run"}, AssistantID: "assistant", OperationID: "operation"}
	var revoked atomic.Bool
	guard := platform.NewNetworkGuard(platform.NetworkOptions{AllowPrivate: true, Binding: func(context.Context, core.Execution, core.ID) (platform.NetworkBinding, error) {
		if revoked.Load() {
			return platform.NetworkBinding{}, core.NewError(core.Forbidden, "revoked")
		}
		return platform.NetworkBinding{Origin: server.URL}, nil
	}})
	client := guard.HTTPClient(e, "resource", server.URL)
	response, err := client.Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response, err = client.Get(server.URL + "/redirect"); err == nil {
		_ = response.Body.Close()
		t.Fatal("redirect accepted")
	}
	revoked.Store(true)
	if response, err = client.Get(server.URL); err == nil {
		_ = response.Body.Close()
		t.Fatal("revoked binding accepted on cached connection")
	}
}
func TestNetworkMetadataDeniedEvenWhenExplicit(t *testing.T) {
	e := core.Execution{Principal: core.Principal{OwnerID: "owner", Kind: core.ActorAssistant, SubjectID: "assistant", ExecutionID: "run"}, AssistantID: "assistant", OperationID: "op"}
	for _, ip := range []string{"169.254.169.254", "100.100.100.200", "168.63.129.16", "fd00:ec2::254"} {
		addr := netip.MustParseAddr(ip)
		guard := platform.NewNetworkGuard(platform.NetworkOptions{AllowedPrivateAddresses: []netip.Addr{addr}, Binding: func(context.Context, core.Execution, core.ID) (platform.NetworkBinding, error) {
			return platform.NetworkBinding{Origin: "https://bound.test"}, nil
		}})
		err := guard.Check(t.Context(), e, platform.Destination{ResourceID: "resource", URL: "https://bound.test", ResolvedAddresses: []netip.Addr{addr}})
		if core.CodeOf(err) != core.Forbidden || core.ReasonOf(err) != core.NetworkDestinationProhibited {
			t.Fatalf("metadata accepted %s: %v", ip, err)
		}
	}
}

func TestNetworkDeploymentPolicyCannotBeExpandedByResourceBinding(t *testing.T) {
	e := core.Execution{Principal: core.Principal{OwnerID: "owner", Kind: core.ActorAssistant, SubjectID: "assistant", ExecutionID: "run"}, AssistantID: "assistant", OperationID: "operation"}
	destination := platform.Destination{ResourceID: "resource", URL: "http://model.test", ResolvedAddresses: []netip.Addr{netip.MustParseAddr("127.0.0.1")}}
	options := platform.NetworkOptions{Binding: func(context.Context, core.Execution, core.ID) (platform.NetworkBinding, error) {
		return platform.NetworkBinding{Origin: destination.URL}, nil
	}}
	if err := platform.NewNetworkGuard(options).Check(t.Context(), e, destination); core.ReasonOf(err) != core.NetworkPrivateAddressDenied {
		t.Fatal("resource origin bypassed deployment policy")
	}
	options.AllowedPrivateAddresses = []netip.Addr{netip.MustParseAddr("127.0.0.1")}
	guard := platform.NewNetworkGuard(options)
	if err := guard.Check(t.Context(), e, destination); err != nil {
		t.Fatal(err)
	}
	options.AllowedPrivateAddresses[0] = netip.MustParseAddr("10.0.0.1")
	destination.ResolvedAddresses = []netip.Addr{netip.MustParseAddr("10.0.0.1")}
	if err := guard.Check(t.Context(), e, destination); err == nil {
		t.Fatal("deployment policy changed after guard construction")
	}
	destination.ResolvedAddresses = []netip.Addr{netip.MustParseAddr("93.184.216.34")}
	if err := guard.Check(t.Context(), e, destination); core.ReasonOf(err) != core.NetworkHTTPNotAllowed {
		t.Fatal("public HTTP allowed by private allowlist")
	}
}

func TestServicePrivateAccessPreservesNetworkBoundaries(t *testing.T) {
	e := core.Execution{Principal: core.Principal{OwnerID: "owner", Kind: core.ActorAssistant, SubjectID: "assistant", ExecutionID: "run"}, AssistantID: "assistant", OperationID: "operation"}
	binding := func(context.Context, core.Execution, core.ID) (platform.NetworkBinding, error) {
		return platform.NetworkBinding{Origin: "http://service.test:8000"}, nil
	}
	for _, allow := range []bool{false, true} {
		guard := platform.NewNetworkGuard(platform.NetworkOptions{AllowPrivate: allow, Binding: binding})
		for _, ip := range []string{"127.0.0.1", "127.0.0.2", "::1", "10.0.0.2", "172.16.0.2", "192.168.1.2", "fd00::2", "100.64.1.2", "::ffff:192.168.1.2"} {
			d := platform.Destination{URL: "http://service.test:8000", ResolvedAddresses: []netip.Addr{netip.MustParseAddr(ip)}}
			err := guard.Check(t.Context(), e, d)
			if allow && err != nil || !allow && core.ReasonOf(err) != core.NetworkPrivateAddressDenied {
				t.Fatalf("allow=%t ip=%s: %v", allow, ip, err)
			}
		}
		for _, ip := range []string{"169.254.169.254", "100.100.100.200", "168.63.129.16", "fd00:ec2::254", "fe80::1", "0.0.0.0", "::", "224.0.0.1", "ff02::1"} {
			d := platform.Destination{URL: "http://service.test:8000", ResolvedAddresses: []netip.Addr{netip.MustParseAddr("127.0.0.1"), netip.MustParseAddr(ip)}}
			// Whitelist the first address so both policies reach the prohibited DNS answer.
			g := platform.NewNetworkGuard(platform.NetworkOptions{AllowPrivate: allow, AllowedPrivateAddresses: []netip.Addr{netip.MustParseAddr("127.0.0.1")}, Binding: binding})
			if err := g.Check(t.Context(), e, d); core.ReasonOf(err) != core.NetworkDestinationProhibited {
				t.Fatalf("allow=%t prohibited=%s: %v", allow, ip, err)
			}
		}
		for _, target := range []string{"http://other.test:8000", "http://service.test:8001", "https://service.test:8000"} {
			if err := guard.Check(t.Context(), e, platform.Destination{URL: target, ResolvedAddresses: []netip.Addr{netip.MustParseAddr("127.0.0.1")}}); err == nil {
				t.Fatalf("unbound origin allowed: %s", target)
			}
		}
		if err := guard.Check(t.Context(), e, platform.Destination{URL: "http://service.test:8000", ResolvedAddresses: []netip.Addr{netip.MustParseAddr("93.184.216.34")}}); core.ReasonOf(err) != core.NetworkHTTPNotAllowed {
			t.Fatalf("public HTTP accepted: %v", err)
		}
	}
}
