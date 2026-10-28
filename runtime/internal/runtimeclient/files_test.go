package runtimeclient

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"molr.ai/runtime/internal/storage/files"
)

func TestFileTransferAuthenticationIntegrityAndRetry(t *testing.T) {
	data := bytes.Repeat([]byte("中文\x00\xff\n"), 10000)
	sum := sha256.Sum256(data)
	file := files.File{ID: "file", Name: "中文 报表.bin", Size: int64(len(data)), SHA256: hex.EncodeToString(sum[:]), State: files.Ready}
	attempts := 0
	corrupt := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Origin") != "http://"+r.Host {
			t.Error("missing origin")
		}
		cookie, err := r.Cookie("session")
		if err != nil || cookie.Value != "synthetic" {
			t.Error("missing session")
		}
		if r.Method == "POST" {
			attempts++
			if r.Header.Get("X-CSRF-Token") != "proof" || r.Header.Get("Idempotency-Key") != "same-key" {
				t.Error("missing mutation identity")
			}
			name, _ := url.PathUnescape(r.Header.Get("X-File-Name"))
			if name != file.Name || r.Header.Get("X-File-Name-Encoding") != "percent" {
				t.Error("filename encoding")
			}
			body, _ := io.ReadAll(r.Body)
			if !bytes.Equal(body, data) || r.ContentLength != file.Size {
				t.Error("bytes changed")
			}
			if attempts == 1 {
				w.WriteHeader(503)
				return
			}
			json.NewEncoder(w).Encode(file)
			return
		}
		if corrupt {
			w.Write(data[:len(data)-1])
		} else {
			w.Write(data)
		}
	}))
	defer server.Close()
	c, _ := New(server.URL, "")
	c.csrf = "proof"
	c.http.Jar.SetCookies(c.base, []*http.Cookie{{Name: "session", Value: "synthetic", Path: "/"}})
	progress := int64(0)
	if _, err := c.Upload(t.Context(), file.Name, data, "same-key", func(n int64) { progress = n }); err == nil {
		t.Fatal("failure hidden")
	}
	got, err := c.Upload(t.Context(), file.Name, data, "same-key", nil)
	if err != nil || got.ID != file.ID || progress != file.Size {
		t.Fatal(got, err, progress)
	}
	var dst bytes.Buffer
	if err = c.Download(t.Context(), file, &dst, func(n int64) { progress = n }); err != nil || !bytes.Equal(dst.Bytes(), data) || progress != file.Size {
		t.Fatal("download", err)
	}
	corrupt = true
	dst.Reset()
	if err = c.Download(t.Context(), file, &dst, nil); err == nil {
		t.Fatal("corruption accepted")
	}
	file.SHA256 = "invalid"
	if c.Download(t.Context(), file, &dst, nil) == nil {
		t.Fatal("invalid metadata accepted")
	}
}

func TestFileTransferRejectsRedirectsAndCancelledRequests(t *testing.T) {
	hits := 0
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits++ }))
	defer destination.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, destination.URL, 307) }))
	defer server.Close()
	c, _ := New(server.URL, "")
	if _, err := c.Upload(t.Context(), "file", []byte("data"), "key", nil); err == nil || hits != 0 {
		t.Fatal("redirect followed")
	}
	sum := sha256.Sum256([]byte("data"))
	file := files.File{ID: "file", Size: 4, SHA256: hex.EncodeToString(sum[:])}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := c.Upload(ctx, "file", []byte("data"), "cancel", nil); err == nil {
		t.Fatal("cancelled upload accepted")
	}
	if c.Download(ctx, file, io.Discard, nil) == nil {
		t.Fatal("cancelled download accepted")
	}
	if c.Download(t.Context(), file, io.Discard, nil) == nil || hits != 0 {
		t.Fatal("download redirect followed")
	}
}
