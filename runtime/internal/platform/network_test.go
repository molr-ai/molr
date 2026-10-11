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
	guard := platform.NewNetworkGuard(platform.NetworkOptions{AllowedPrivateAddresses: []netip.Addr{netip.MustParseAddr("127.0.0.1")}, Binding: func(context.Context, core.Execution, core.ID) (platform.NetworkBinding, error) {
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
