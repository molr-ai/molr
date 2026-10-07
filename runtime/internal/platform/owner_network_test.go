package platform_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/platform"
)

func TestOwnerNetworkRequiresApprovedBindingAndLiveIdentity(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(http.StatusNoContent) }))
	defer server.Close()
	p := core.Principal{OwnerID: "owner", SubjectID: "owner", SessionID: "session", Kind: core.ActorOwner}
	allowed := true
	guard := platform.NewNetworkGuard(platform.NetworkOptions{AllowedPrivateAddresses: []netip.Addr{netip.MustParseAddr("127.0.0.1")}, OwnerBinding: func(_ context.Context, actor core.Principal, id core.ID) (platform.NetworkBinding, error) {
		if actor != p || id != "resource" || !allowed {
			return platform.NetworkBinding{}, core.NewError(core.Forbidden, "denied")
		}
		return platform.NetworkBinding{Origin: server.URL}, nil
	}})
	if err := guard.CheckOwner(t.Context(), p, platform.Destination{ResourceID: "resource", URL: server.URL, ResolvedAddresses: []netip.Addr{netip.MustParseAddr("169.254.169.254")}}); err == nil {
		t.Fatal("metadata allowed")
	}
	client := guard.OwnerHTTPClient(p, "resource", server.URL)
	defer client.CloseIdleConnections()
	response, err := client.Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	allowed = false
	if response, err = client.Get(server.URL); err == nil {
		response.Body.Close()
		t.Fatal("revoked owner still connected")
	}
	if calls != 1 {
		t.Fatalf("requests: %d", calls)
	}
}
