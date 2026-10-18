package api

import (
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"molr.ai/runtime/internal/config"
)

func TestAuthenticationCookieScopesIncludeNonstandardPorts(t *testing.T) {
	for _, test := range []struct {
		origin string
		mode   config.TLSMode
		want   string
	}{
		{"https://molr.example", config.TLSMode("manual"), "__Host-molr_session"},
		{"https://molr.example:9443", config.TLSMode("manual"), "__Host-molr_session_p9443"},
		{"https://molr.example:9444", config.TLSMode("manual"), "__Host-molr_session_p9444"},
		{"http://127.0.0.1:8080", config.LocalHTTP, "molr_dev_session_p8080"},
		{"http://127.0.0.1:8081", config.LocalHTTP, "molr_dev_session_p8081"},
		{"http://192.168.1.10:8080", config.LANHTTP, "molr_lan_session_p8080"},
	} {
		t.Run(test.origin, func(t *testing.T) {
			origin, err := url.Parse(test.origin)
			if err != nil {
				t.Fatal(err)
			}
			s := fixture(t).Server.(*HTTPServer)
			s.origin = origin
			s.config.HTTP.TLSMode = test.mode
			// Discovery is available even before authentication or on a rejected request.
			w := httptest.NewRecorder()
			s.Handler().ServeHTTP(w, httptest.NewRequest("GET", test.origin+"/api/auth/me", nil))
			if got := w.Header().Get("X-Molr-CSRF-Cookie"); got != strings.Replace(test.want, "session", "csrf", 1) {
				t.Fatalf("wrong CSRF cookie discovery: %q", got)
			}
			if got := s.cookieName(sessionCookie); got != test.want {
				t.Fatalf("%s != %s", got, test.want)
			}
		})
	}
}
