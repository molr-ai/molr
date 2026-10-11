package api

import (
	"net/url"
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
			s := &HTTPServer{origin: origin}
			s.config.HTTP.TLSMode = test.mode
			if got := s.cookieName(sessionCookie); got != test.want {
				t.Fatalf("%s != %s", got, test.want)
			}
		})
	}
}
