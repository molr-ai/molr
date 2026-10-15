package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"molr.ai/runtime/internal/app"
	"molr.ai/runtime/internal/auth"
	"molr.ai/runtime/internal/config"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/platform"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func bootstrap(ctx context.Context, b *app.Bundle, cfg config.Config, username string, out io.Writer) error {
	initialized, err := b.Bootstrap.Initialized(ctx)
	if err != nil {
		return err
	}
	if initialized {
		fmt.Fprintln(out, "Already initialized; account and setup state preserved.")
		return nil
	}
	name := filepath.Join(cfg.DataDir, "initial-password")
	var passwordBytes []byte
	file, err := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if os.IsExist(err) {
		info, e := os.Lstat(name)
		if e != nil {
			return e
		}
		if !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
			return fmt.Errorf("initial-password must be a private regular file")
		}
		passwordBytes, err = os.ReadFile(name)
	} else if err == nil {
		raw := make([]byte, 32)
		if _, err = rand.Read(raw); err == nil {
			passwordBytes = []byte(base64.RawURLEncoding.EncodeToString(raw))
			_, err = file.Write(passwordBytes)
		}
		if closeErr := file.Close(); err == nil {
			err = closeErr
		}
	}
	if err != nil {
		return err
	}
	defer clear(passwordBytes)
	_, err = b.InitializeSetup(ctx, auth.InitializeRequest{Username: username, Password: core.NewSecret(string(passwordBytes))})
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "Owner: %s\nInitial password file: %s\n", username, name)
	return nil
}

// Online maintenance never opens the live database. It uses normal sessions,
// MFA and verification, and does not reset credentials on an existing instance.
func online(ctx context.Context, cfg config.Config, command, username, mfa string, secret core.Secret, out io.Writer) error {
	jar, _ := cookiejar.New(nil)
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	if cfg.HTTP.TLSMode == config.DirectTLS {
		pem, err := os.ReadFile(cfg.HTTP.CertFile)
		if err != nil {
			return err
		}
		roots, _ := x509.SystemCertPool()
		if roots == nil {
			roots = x509.NewCertPool()
		}
		if !roots.AppendCertsFromPEM(pem) {
			return fmt.Errorf("invalid server certificate")
		}
		transport.TLSClientConfig = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
	}
	client := &http.Client{Jar: jar, Transport: transport, Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	defer transport.CloseIdleConnections()
	origin := strings.TrimSuffix(cfg.HTTP.PublicURL, "/")
	csrf := ""
	call := func(method, path string, input, output any) error {
		var data []byte
		var err error
		if input != nil {
			data, err = json.Marshal(input)
			if err != nil {
				return err
			}
		}
		req, err := http.NewRequestWithContext(ctx, method, origin+path, bytes.NewReader(data))
		if err != nil {
			return err
		}
		req.Header.Set("Origin", origin)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-CSRF-Token", csrf)
		response, err := client.Do(req)
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
	if err := call("GET", "/api/auth/csrf", nil, &login); err != nil {
		return err
	}
	csrf = login.CSRF
	if err := call("POST", "/api/auth/login", map[string]string{"username": username, "password": secret.Reveal(), "device_name": "Local CLI", "client_type": "cli"}, &login); err != nil {
		return err
	}
	if login.Challenge != "" {
		if mfa == "" {
			return fmt.Errorf("MFA required; provide -mfa-code")
		}
		if err := call("POST", "/api/auth/mfa/complete", map[string]string{"challenge": login.Challenge, "code": mfa}, &login); err != nil {
			return err
		}
	}
	csrf = login.CSRF
	defer call("POST", "/api/auth/logout", map[string]any{}, nil)
	if command == "doctor" {
		var status any
		if err := call("GET", "/api/status", nil, &status); err != nil {
			return err
		}
		var sandbox struct {
			Enabled      bool
			Storage      bool
			Capabilities platform.Capabilities
		}
		if err := call("GET", "/api/sandbox", nil, &sandbox); err != nil {
			return err
		}
		if err := json.NewEncoder(out).Encode(map[string]any{"Runtime": status, "Sandbox": sandbox}); err != nil {
			return err
		}
		c := sandbox.Capabilities
		if sandbox.Enabled && !commandEnvironmentReady(cfg.Sandbox.AllowFullAccess, c) {
			return fmt.Errorf("chat service is running; configured command execution unavailable: %s", c.Reason)
		}
		return nil
	}
	var proof struct {
		Token string `json:"verification"`
	}
	if err := call("POST", "/api/auth/reauthenticate", map[string]string{"password": secret.Reveal(), "mfa_code": mfa, "purpose": "login_link.create"}, &proof); err != nil {
		return err
	}
	var issued struct {
		Value string `json:"value"`
	}
	if err := call("POST", "/api/auth/login-links", map[string]any{"name": "Local CLI", "target_path": "/assistants", "expires_in": 900, "verification": proof.Token}, &issued); err != nil {
		return err
	}
	fmt.Fprintf(out, "%s/#/login?token=%s\n", origin, url.QueryEscape(issued.Value))
	return nil
}

// Diagnostics accept direct execution only when the deployment explicitly allows
// it. The returned capability document still reports each isolation flag.
func commandEnvironmentReady(allowFullAccess bool, c platform.Capabilities) bool {
	isolated := c.Reason == "available" && c.Shell && c.ProcessIsolation && c.FileIsolation && c.NetworkIsolation && c.ResourceLimits
	return isolated || (allowFullAccess && c.FullAccess)
}
