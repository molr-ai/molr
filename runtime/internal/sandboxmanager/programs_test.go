package sandboxmanager

import (
	"context"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/platform/hostprograms"
	"reflect"
	"strings"
	"testing"
)

type programsTestBackend struct {
	testBackend
	request hostprograms.Request
	calls   int
	err     error
}

func (b *programsTestBackend) Programs(_ context.Context, q hostprograms.Request) (hostprograms.Snapshot, error) {
	b.calls++
	b.request = q
	return hostprograms.Snapshot{Programs: []hostprograms.Program{{Name: "Remote Agent", Executable: "/remote/bin/agent", Available: true}}}, b.err
}
func TestProgramsClientAuthenticatedRemoteDiscovery(t *testing.T) {
	b := &programsTestBackend{}
	server, pin, token := directoryTestServer(t, b)
	client, err := NewClient(server.URL, pin, token, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.HTTP.CloseIdleConnections)
	q := hostprograms.Request{Command: "agent", Refresh: true}
	out, err := client.Programs(t.Context(), q)
	if err != nil || len(out.Programs) != 1 || out.Programs[0].Executable != "/remote/bin/agent" || b.request != q || b.calls != 1 {
		t.Fatal(out, err, b)
	}
	b.err = core.NewError(core.Forbidden, "private diagnostic")
	if _, err := client.Programs(t.Context(), q); core.CodeOf(err) != core.Forbidden || strings.Contains(err.Error(), "private diagnostic") {
		t.Fatal(err)
	}
	client.Token = core.NewSecret(strings.Repeat("b", 48))
	out, err = client.Programs(t.Context(), q)
	if core.CodeOf(err) != core.Unauthenticated || b.calls != 2 || !reflect.DeepEqual(out, hostprograms.Snapshot{}) {
		t.Fatal(out, err, b.calls)
	}
}
