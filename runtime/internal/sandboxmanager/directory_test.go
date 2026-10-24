package sandboxmanager

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence/sqlitestore"
	"molr.ai/runtime/internal/platform"
)

type directoryTestBackend struct {
	testBackend
	requests chan DirectoryRequest
	result   Directory
	err      error
}

func (b *directoryTestBackend) Directory(_ context.Context, q DirectoryRequest) (Directory, error) {
	b.requests <- q
	return b.result, b.err
}

func directoryTestServer(t *testing.T, backend Backend) (*httptest.Server, string, core.Secret) {
	t.Helper()
	store, err := sqlitestore.Open(filepath.Join(t.TempDir(), "ledger.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	manager, err := New(t.Context(), store, backend)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { manager.Close() })
	token := core.NewSecret(strings.Repeat("a", 48))
	handler, err := Handler(manager, token)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewTLSServer(handler)
	t.Cleanup(server.Close)
	sum := sha256.Sum256(server.Certificate().RawSubjectPublicKeyInfo)
	return server, hex.EncodeToString(sum[:]), token
}

func TestDirectoryClientRoundTripAndBackendErrors(t *testing.T) {
	want := Directory{
		ID: "selected", Name: "shared files", Path: "/selected path", ReadOnly: true,
		Capacity: platform.StorageCapacity{StorageTotalBytes: 1024, StorageUsedBytes: 256, StorageBytes: 768, StorageAvailableBytes: 512},
	}
	request := DirectoryRequest{SourceID: "source", Path: "/requested path"}
	for _, tc := range []struct {
		name   string
		code   core.Code
		reason core.Reason
	}{
		{"success", "", ""},
		{"denied", core.Forbidden, "directory_denied"},
		{"missing", core.NotFound, "directory_missing"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			backend := &directoryTestBackend{requests: make(chan DirectoryRequest, 1), result: want}
			if tc.code != "" {
				backend.err = core.WithReason(core.NewError(tc.code, "private backend diagnostic"), tc.reason)
			}
			server, pin, token := directoryTestServer(t, backend)
			client, err := NewClient(server.URL, pin, token, server.Client())
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(client.HTTP.CloseIdleConnections)
			got, err := client.Directory(t.Context(), request)
			select {
			case received := <-backend.requests:
				if received != request {
					t.Fatalf("request = %+v, want %+v", received, request)
				}
			default:
				t.Fatal("directory backend was not called")
			}
			if tc.code == "" {
				if err != nil || !reflect.DeepEqual(got, want) {
					t.Fatalf("directory = %+v, error = %v; want %+v", got, err, want)
				}
				return
			}
			if core.CodeOf(err) != tc.code || core.ReasonOf(err) != tc.reason {
				t.Fatalf("error = %v, want code %s and reason %s", err, tc.code, tc.reason)
			}
			if !reflect.DeepEqual(got, Directory{}) || strings.Contains(err.Error(), "private backend diagnostic") {
				t.Fatalf("backend error exposed result or diagnostic: %+v, %v", got, err)
			}
		})
	}
}

func TestDirectoryClientRequiresServiceAuthentication(t *testing.T) {
	backend := &directoryTestBackend{requests: make(chan DirectoryRequest, 1)}
	server, pin, _ := directoryTestServer(t, backend)
	client, err := NewClient(server.URL, pin, core.NewSecret(strings.Repeat("b", 48)), server.Client())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.HTTP.CloseIdleConnections)
	got, err := client.Directory(t.Context(), DirectoryRequest{SourceID: "source"})
	if core.CodeOf(err) != core.Unauthenticated || core.ReasonOf(err) != "sandbox_auth_failed" {
		t.Fatalf("wrong service token accepted: %v", err)
	}
	if len(backend.requests) != 0 || !reflect.DeepEqual(got, Directory{}) {
		t.Fatal("unauthenticated request reached backend or returned a directory")
	}
}

func TestDirectoryClientRejectsUnsupportedBackend(t *testing.T) {
	server, pin, token := directoryTestServer(t, &testBackend{})
	client, err := NewClient(server.URL, pin, token, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.HTTP.CloseIdleConnections)
	got, err := client.Directory(t.Context(), DirectoryRequest{SourceID: "source"})
	if core.CodeOf(err) != core.Unavailable || !reflect.DeepEqual(got, Directory{}) {
		t.Fatalf("unsupported backend returned %+v, %v", got, err)
	}
}
