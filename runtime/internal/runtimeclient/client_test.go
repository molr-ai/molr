package runtimeclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestSessionLoginMFAPersistenceAndOriginIsolation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Origin") != "http://"+r.Host {
			t.Error("wrong origin")
		}
		switch r.URL.Path {
		case "/api/auth/csrf":
			http.SetCookie(w, &http.Cookie{Name: "login", Value: "challenge", Path: "/"})
			fmt.Fprint(w, `{"csrf_token":"pre"}`)
		case "/api/auth/login":
			if r.Header.Get("X-CSRF-Token") != "pre" {
				t.Error("missing pre-login proof")
			}
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			if body["username"] != "tester" || body["password"] != "synthetic-password" || body["client_type"] != "cli" || len(body["device_id"].(string)) < 32 {
				t.Error("login body")
			}
			fmt.Fprint(w, `{"challenge":"mfa"}`)
		case "/api/auth/mfa/complete":
			http.SetCookie(w, &http.Cookie{Name: "session", Value: "synthetic-session", Path: "/"})
			http.SetCookie(w, &http.Cookie{Name: "csrf", Value: "proof", Path: "/"})
			w.Header().Set("X-Molr-CSRF-Cookie", "csrf")
			fmt.Fprint(w, `{"csrf_token":"proof"}`)
		case "/api/write":
			cookie, err := r.Cookie("session")
			if err != nil || cookie.Value != "synthetic-session" || r.Header.Get("X-CSRF-Token") != "proof" {
				t.Error("session not restored")
			}
			fmt.Fprint(w, `{}`)
		default:
			w.WriteHeader(401)
			fmt.Fprint(w, `{"code":"unauthenticated"}`)
		}
	}))
	defer server.Close()
	c, err := New(server.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	challenge, err := c.Login(t.Context(), "tester", "synthetic-password")
	if err != nil || challenge != "mfa" {
		t.Fatal(challenge, err)
	}
	if err = c.CompleteMFA(t.Context(), challenge, "123456"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "config", "session.json")
	if err = c.Save(path); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Fatal("session permissions")
	}
	restored, _ := New(server.URL, "")
	ok, err := restored.Restore(path)
	if !ok || err != nil {
		t.Fatal(ok, err)
	}
	if err = restored.Do(t.Context(), "POST", "/api/write", map[string]string{}, nil); err != nil {
		t.Fatal(err)
	}
	other, _ := New("https://example.invalid", "")
	if ok, err = other.Restore(path); ok || err != nil {
		t.Fatal("credentials crossed origins")
	}
	if err = restored.Do(t.Context(), "GET", "/api/private", nil, nil); err == nil {
		t.Fatal("401 hidden")
	} else {
		var e *Error
		if !errors.As(err, &e) || e.Status != 401 || e.Error() == "" {
			t.Fatal(err)
		}
	}
	os.Chmod(path, 0644)
	if _, err = restored.Restore(path); err == nil {
		t.Fatal("insecure session accepted")
	}
}

func TestTransportValidationRedirectAndTLS(t *testing.T) {
	for _, address := range []string{"", "ftp://example.com", "https://user:password@example.com", "https://example.com/api", "https://example.com?token=x"} {
		if _, err := New(address, ""); err == nil {
			t.Fatal(address)
		}
	}
	hits := atomic.Int32{}
	dest := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1) }))
	defer dest.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, dest.URL+"/api/private", 302) }))
	defer source.Close()
	c, _ := New(source.URL, "")
	if c.Do(t.Context(), "GET", "/api/test", nil, nil) == nil || hits.Load() != 0 {
		t.Fatal("redirect followed")
	}
	tlsServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{}`) }))
	defer tlsServer.Close()
	secure, _ := New(tlsServer.URL, "")
	if secure.Do(t.Context(), "GET", "/api/test", nil, nil) == nil {
		t.Fatal("untrusted certificate accepted")
	}
	if a, b := NewKey(), NewKey(); a == b || len(a) < 32 {
		t.Fatal("mutation keys")
	}
}

func TestWatchReadyResumeAndCancellation(t *testing.T) {
	attempts := atomic.Int32{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := attempts.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		if n == 1 {
			fmt.Fprint(w, "id: cursor-1\nevent: ready\ndata: {}\n\nid: cursor-2\nevent: update\ndata: {}\n\n")
			return
		}
		if r.Header.Get("Last-Event-ID") != "cursor-2" {
			t.Error("resume cursor missing")
		}
		fmt.Fprint(w, "id: cursor-2\nevent: ready\ndata: {}\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer server.Close()
	c, _ := New(server.URL, "")
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	changes := 0
	c.Watch(ctx, func(event Event) {
		if event.Connected {
			changes++
			if changes == 3 {
				cancel()
			}
		}
	})
	if changes != 3 || attempts.Load() != 2 {
		t.Fatalf("changes=%d attempts=%d", changes, attempts.Load())
	}
}
