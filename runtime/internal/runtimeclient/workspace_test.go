package runtimeclient

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"molr.ai/runtime/internal/workspace"
)

func TestWorkspaceTransferUsesVersionsAndDetectsChangedDownload(t *testing.T) {
	version := "v1"
	uploads := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/assistants/a/workspace" {
			fmt.Fprintf(w, `{"Version":%q}`, version)
			return
		}
		if r.URL.Path != "/api/assistants/a/workspace/content" || r.URL.Query().Get("path") != "reports/中文.txt" {
			t.Error("incorrect workspace target")
		}
		if r.URL.Query().Get("version") != "v1" {
			t.Error("missing version")
		}
		if r.Method == "PUT" {
			uploads++
			body, _ := io.ReadAll(r.Body)
			if string(body) != "report" {
				t.Error("upload bytes changed")
			}
			fmt.Fprint(w, `{"Path":"reports/中文.txt","Size":6,"Version":"v2"}`)
			return
		}
		fmt.Fprint(w, "report")
	}))
	defer server.Close()
	c, _ := New(server.URL, "")
	entry, err := c.WorkspaceUpload(t.Context(), "a", "reports/中文.txt", "v1", []byte("report"))
	if err != nil || entry.Version != "v2" || uploads != 1 {
		t.Fatalf("upload %+v %v", entry, err)
	}
	file := workspace.Entry{Path: "reports/中文.txt", Size: 6, Version: "v1"}
	var out bytes.Buffer
	if err := c.WorkspaceDownload(t.Context(), "a", file, &out); err != nil || out.String() != "report" {
		t.Fatalf("download %v", err)
	}
	version = "v2"
	out.Reset()
	if err := c.WorkspaceDownload(t.Context(), "a", file, &out); err == nil {
		t.Fatal("changed file published")
	}
	file.Size = 5
	out.Reset()
	if err := c.WorkspaceDownload(t.Context(), "a", file, &out); err == nil {
		t.Fatal("size mismatch accepted")
	}
}
func TestConnectionEditingPreservesOriginAndIsolation(t *testing.T) {
	store, err := OpenConnections(t.TempDir() + "/connections.json")
	if err != nil {
		t.Fatal(err)
	}
	a, err := store.Add("A", "https://a.example.com", "ca.pem", "")
	if err != nil {
		t.Fatal(err)
	}
	if store.StateDirectory() == "" {
		t.Fatal("missing state directory")
	}
	edited, err := store.Update(a.ID, "Renamed", a.Server, "")
	if err != nil || edited.ID != a.ID || edited.CAFile != "" || edited.Name != "Renamed" {
		t.Fatalf("metadata edit %+v %v", edited, err)
	}
	b, err := store.Update(a.ID, "B", "https://b.example.com", "")
	if err != nil || b.ID == a.ID || store.SessionPath(b) == store.SessionPath(a) || len(store.Items) != 2 {
		t.Fatal("origin change reused credentials")
	}
	store.Select(a.ID)
	if err := store.Remove(a.ID); err != nil {
		t.Fatal(err)
	}
	restored, err := OpenConnections(store.path)
	if err != nil || len(restored.Items) != 1 || restored.Items[0].ID != b.ID || restored.Last != "" {
		t.Fatal("remove did not persist")
	}
	if err := store.Remove("missing"); err == nil {
		t.Fatal("missing entry accepted")
	}
}
func TestErrorsKeepOnlySafeProtocolDetails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "2")
		w.WriteHeader(429)
		fmt.Fprint(w, `{"code":"rate_limited","reason":"model_rate_limited","request_id":"request-id","message":"secret raw error"}`)
	}))
	defer server.Close()
	c, _ := New(server.URL, "")
	err := c.Do(t.Context(), "GET", "/api/test", nil, nil)
	e, ok := err.(*Error)
	if !ok || e.Reason != "model_rate_limited" || e.RequestID != "request-id" || e.RetryAfter.Seconds() != 2 {
		t.Fatalf("missing safe error detail: %v", err)
	}
	for _, unsafe := range []string{"bad\x1b[31m", "secret with whitespace", string(bytes.Repeat([]byte("a"), 129))} {
		if safeProtocolLabel(unsafe) != "" {
			t.Fatal("unsafe diagnostic allowed")
		}
	}
}
