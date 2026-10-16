//go:build linux || darwin

package installation

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"molr.ai/runtime/internal/platform"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"molr.ai/runtime/internal/installrecord"
)

func (e *Engine) verify(r *installrecord.Record, cfg map[string]any) (string, error) {
	h, ok := cfg["http"].(map[string]any)
	if !ok {
		return "", fmt.Errorf("http configuration required")
	}
	origin, _ := h["public_url"].(string)
	origin = strings.TrimRight(origin, "/")
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	if cert, ok := h["cert_file"].(string); ok && cert != "" {
		data, err := os.ReadFile(cert)
		if err != nil {
			return "", err
		}
		roots, _ := x509.SystemCertPool()
		if roots == nil {
			roots = x509.NewCertPool()
		}
		if !roots.AppendCertsFromPEM(data) {
			return "", fmt.Errorf("invalid TLS certificate")
		}
		transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots}
	}
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Transport: transport, Jar: jar, Timeout: 2 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	defer transport.CloseIdleConnections()
	deadline := time.Now().Add(90 * time.Second)
	ready := false
	for time.Now().Before(deadline) {
		req, _ := http.NewRequestWithContext(e.Ctx, "GET", origin+"/", nil)
		if req == nil {
			return "", fmt.Errorf("invalid public URL")
		}
		response, err := client.Do(req)
		if err == nil {
			response.Body.Close()
			if response.StatusCode == 200 {
				ready = true
				break
			}
		}
		select {
		case <-e.Ctx.Done():
			return "", e.Ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
	if !ready {
		return "", fmt.Errorf("HTTP readiness deadline exceeded; inspect service logs")
	}
	client.Timeout = 20 * time.Second
	passwordPath := filepath.Join(r.DataDir, "initial-password")
	if err := checkPath(passwordPath); err != nil {
		return "", err
	}
	secret, err := os.ReadFile(passwordPath)
	if os.IsNotExist(err) {
		r.Health = "verification_pending"
		return "", nil
	}
	if err != nil {
		return "", err
	}
	defer clear(secret)
	csrf := ""
	call := func(method, path string, input, output any) error {
		var reader io.Reader
		if input != nil {
			data, _ := json.Marshal(input)
			reader = bytes.NewReader(data)
		}
		request, err := http.NewRequestWithContext(e.Ctx, method, origin+path, reader)
		if err != nil {
			return err
		}
		request.Header.Set("Origin", origin)
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("X-CSRF-Token", csrf)
		response, err := client.Do(request)
		if err != nil {
			return err
		}
		defer response.Body.Close()
		if response.StatusCode != 200 {
			return fmt.Errorf("%s: HTTP %d", path, response.StatusCode)
		}
		if output != nil {
			return json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(output)
		}
		return nil
	}
	var login struct {
		CSRF      string `json:"csrf_token"`
		Challenge string `json:"challenge"`
	}
	if err = call("GET", "/api/auth/csrf", nil, &login); err != nil {
		return "", err
	}
	csrf = login.CSRF
	if err = call("POST", "/api/auth/login", map[string]string{"username": "owner", "password": strings.TrimSpace(string(secret)), "device_name": "Molr setup", "client_type": "cli"}, &login); err != nil {
		if strings.Contains(err.Error(), "HTTP 401") || strings.Contains(err.Error(), "HTTP 403") || strings.Contains(err.Error(), "HTTP 429") {
			r.Health = "verification_pending"
			return "", nil
		}
		return "", err
	}
	if login.Challenge != "" {
		r.Health = "verification_pending"
		return "", nil
	}
	csrf = login.CSRF
	defer call("POST", "/api/auth/logout", map[string]any{}, nil)
	var status struct {
		Build installrecord.Build `json:"build"`
	}
	if err = call("GET", "/api/status", nil, &status); err != nil {
		return "", err
	}
	if status.Build != r.Build {
		return "", fmt.Errorf("running build differs from installed package")
	}
	var sandbox struct {
		Enabled      bool
		Storage      bool
		Capabilities platform.Capabilities
	}
	if err = call("GET", "/api/sandbox", nil, &sandbox); err != nil {
		return "", err
	}
	wanted := true
	if s, ok := cfg["sandbox"].(map[string]any); ok {
		if b, ok := s["enabled"].(bool); ok {
			wanted = b
		}
	}
	c := sandbox.Capabilities
	if runtime.GOOS == "darwin" {
		if !sandbox.Storage || sandbox.Enabled != wanted || (wanted && !c.ControlledReady()) {
			return "", fmt.Errorf("macOS storage/execution verification failed: %s", c.Reason)
		}
	} else if wanted && (!sandbox.Enabled || c.Reason != "available" || !c.Shell || !c.ProcessIsolation || !c.FileIsolation || !c.NetworkIsolation || !c.ResourceLimits) {
		return "", fmt.Errorf("sandbox verification failed: %s", c.Reason)
	}
	r.Health = "verified"
	if e.Options.JSON && e.Options.Command != "desktop" {
		return "", nil
	}
	var proof struct {
		Value string `json:"verification"`
	}
	if err = call("POST", "/api/auth/reauthenticate", map[string]string{"password": strings.TrimSpace(string(secret)), "purpose": "login_link.create"}, &proof); err != nil {
		e.UI.print("Automatic login link unavailable; use molr access with your credentials.")
		return "", nil
	}
	var link struct {
		Value string `json:"value"`
	}
	if err = call("POST", "/api/auth/login-links", map[string]any{"name": "Molr setup", "target_path": "/assistants", "expires_in": 900, "verification": proof.Value}, &link); err != nil {
		return "", nil
	}
	return fmt.Sprintf("%s/#/login?token=%s", origin, url.QueryEscape(link.Value)), nil
}
func (e *Engine) openBrowser(link string, system bool) {
	if !e.UI.interactive() || e.Options.NoBrowser || system || os.Getenv("SSH_CONNECTION") != "" || (runtime.GOOS != "darwin" && os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "") {
		return
	}
	opener := "xdg-open"
	if runtime.GOOS == "darwin" {
		opener = "/usr/bin/open"
	}
	if _, err := exec.LookPath(opener); err != nil {
		return
	}
	cmd := exec.Command(opener, link)
	if cmd.Start() == nil {
		go cmd.Wait()
	}
}
