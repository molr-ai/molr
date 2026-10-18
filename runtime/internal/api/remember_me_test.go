package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLoginRememberMeControlsBothBrowserCookies(t *testing.T) {
	for _, remember := range []bool{false, true} {
		name := "session"
		if remember {
			name = "persistent"
		}
		t.Run(name, func(t *testing.T) {
			f := fixture(t)
			handler := f.Server.Handler()
			pre := httptest.NewRecorder()
			handler.ServeHTTP(pre, httptest.NewRequest("GET", "https://molr.example/api/auth/csrf", nil))
			var proof struct {
				Token string `json:"csrf_token"`
			}
			if err := json.Unmarshal(pre.Body.Bytes(), &proof); err != nil {
				t.Fatal(err)
			}
			body, _ := json.Marshal(map[string]any{"username": "owner", "password": "strong-initial-password-123!", "remember_me": remember})
			request := httptest.NewRequest("POST", "https://molr.example/api/auth/login", strings.NewReader(string(body)))
			request.Header.Set("Origin", "https://molr.example")
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("X-CSRF-Token", proof.Token)
			for _, cookie := range pre.Result().Cookies() {
				request.AddCookie(cookie)
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != 200 {
				t.Fatalf("login status %d: %s", response.Code, response.Body.String())
			}
			found := 0
			for _, cookie := range response.Result().Cookies() {
				if cookie.Name != sessionCookie && cookie.Name != csrfCookie {
					continue
				}
				found++
				if cookie.Expires.IsZero() == remember || cookie.MaxAge != 0 {
					t.Fatalf("unexpected cookie persistence: %s", cookie.Name)
				}
				if cookie.Path != "/" || !cookie.Secure || cookie.SameSite != http.SameSiteStrictMode || cookie.HttpOnly != (cookie.Name == sessionCookie) {
					t.Fatalf("unsafe attributes for %s", cookie.Name)
				}
			}
			if found != 2 {
				t.Fatalf("expected session and proof cookies, got %d", found)
			}
		})
	}
}
