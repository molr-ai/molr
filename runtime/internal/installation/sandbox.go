//go:build linux || darwin

package installation

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/installrecord"
	sm "molr.ai/runtime/internal/sandboxmanager"
)

func (e *Engine) prepareSandboxSecrets(r *installrecord.Record, cfg map[string]any) error {
	h, ok := cfg["http"].(map[string]any)
	if !ok || h["tls_mode"] != "direct" {
		return fmt.Errorf("standalone sandbox requires direct TLS")
	}
	certPath, _ := h["cert_file"].(string)
	keyPath, _ := h["key_file"].(string)
	for _, p := range []string{certPath, keyPath, filepath.Join(r.DataDir, "sandbox-token")} {
		if !filepath.IsAbs(p) {
			return fmt.Errorf("absolute certificate and key paths required")
		}
		if err := checkPath(p); err != nil {
			return err
		}
	}
	// Generate only the managed pair. Custom certificates are supplied by the deployer.
	if certPath == filepath.Join(r.DataDir, "sandbox.crt") && keyPath == filepath.Join(r.DataDir, "sandbox.key") {
		_, ce := os.Stat(certPath)
		_, ke := os.Stat(keyPath)
		if os.IsNotExist(ce) && os.IsNotExist(ke) {
			u, err := url.Parse(h["public_url"].(string))
			if err != nil {
				return err
			}
			key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
			if err != nil {
				return err
			}
			serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
			if err != nil {
				return err
			}
			template := x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: r.Name}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().AddDate(1, 0, 0), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
			if ip := net.ParseIP(u.Hostname()); ip != nil {
				template.IPAddresses = []net.IP{ip}
			} else {
				template.DNSNames = []string{u.Hostname()}
			}
			der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
			if err != nil {
				return err
			}
			private, err := x509.MarshalPKCS8PrivateKey(key)
			if err != nil {
				return err
			}
			if err = atomicWrite(keyPath, bytes.NewReader(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: private})), 0600, r.UID, r.GID); err != nil {
				return err
			}
			if err = atomicWrite(certPath, bytes.NewReader(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})), 0600, r.UID, r.GID); err != nil {
				return err
			}
		}
	}
	if _, err := tls.LoadX509KeyPair(certPath, keyPath); err != nil {
		return err
	}
	tokenPath := filepath.Join(r.DataDir, "sandbox-token")
	if _, err := os.Stat(tokenPath); os.IsNotExist(err) {
		secret := make([]byte, 32)
		if _, err = rand.Read(secret); err != nil {
			return err
		}
		defer clear(secret)
		return atomicWrite(tokenPath, strings.NewReader(hex.EncodeToString(secret)+"\n"), 0600, r.UID, r.GID)
	} else if err != nil {
		return err
	}
	st, err := os.Stat(tokenPath)
	if err != nil {
		return err
	}
	if !st.Mode().IsRegular() || st.Mode().Perm()&0077 != 0 {
		return fmt.Errorf("sandbox token requires a private regular file")
	}
	return nil
}

func sandboxFingerprint(cfg map[string]any) (string, error) {
	h, ok := cfg["http"].(map[string]any)
	if !ok {
		return "", fmt.Errorf("http configuration required")
	}
	certPath, _ := h["cert_file"].(string)
	data, err := os.ReadFile(certPath)
	if err != nil {
		return "", err
	}
	block, _ := pem.Decode(data)
	if block == nil || block.Type != "CERTIFICATE" {
		return "", fmt.Errorf("invalid sandbox certificate")
	}
	sum := sha256.Sum256(block.Bytes)
	return hex.EncodeToString(sum[:]), nil
}
func (e *Engine) sandboxInstructions(r *installrecord.Record, cfg map[string]any) error {
	fingerprint, err := sandboxFingerprint(cfg)
	if err != nil {
		return err
	}
	h := cfg["http"].(map[string]any)
	e.UI.print("Connect this Sandbox in a Runtime:\nHTTPS address: %s\nCertificate SHA-256: %s\nService token file: %s\nRead the token locally and paste it into the Sandbox connection form.", h["public_url"], fingerprint, filepath.Join(r.DataDir, "sandbox-token"))
	return nil
}
func (e *Engine) verifySandbox(r *installrecord.Record, cfg map[string]any) error {
	fingerprint, err := sandboxFingerprint(cfg)
	if err != nil {
		return err
	}
	token, err := os.ReadFile(filepath.Join(r.DataDir, "sandbox-token"))
	if err != nil {
		return err
	}
	defer clear(token)
	h := cfg["http"].(map[string]any)
	origin, _ := h["public_url"].(string)
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	client, err := sm.NewClient(origin, fingerprint, core.NewSecret(strings.TrimSpace(string(token))), &http.Client{Transport: transport})
	if err != nil {
		return err
	}
	defer client.HTTP.CloseIdleConnections()
	// An unauthenticated request is used only to wait for TLS/HTTP readiness.
	ready := false
	deadline := time.Now().Add(90 * time.Second)
	client.HTTP.Timeout = 2 * time.Second
	for time.Now().Before(deadline) {
		req, err := http.NewRequestWithContext(e.Ctx, "GET", origin+"/", nil)
		if err != nil {
			return err
		}
		response, err := client.HTTP.Do(req)
		if err == nil {
			response.Body.Close()
			if response.StatusCode == http.StatusUnauthorized {
				ready = true
				break
			}
		}
		select {
		case <-e.Ctx.Done():
			return e.Ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
	if !ready {
		return fmt.Errorf("sandbox HTTPS readiness deadline exceeded")
	}
	client.HTTP.Timeout = 30 * time.Second
	info, err := client.Info(e.Ctx)
	if err != nil {
		return err
	}
	if info.Build.Version != r.Build.Version || info.Build.Commit != r.Build.Commit || info.Build.OS != r.Build.OS || info.Build.Arch != r.Build.Arch {
		return fmt.Errorf("running sandbox differs from installed package")
	}
	s, _ := cfg["sandbox"].(map[string]any)
	wanted, _ := s["enabled"].(bool)
	if info.Enabled != wanted || (wanted && (!info.Storage || !info.Capabilities.ControlledReady())) || (!wanted && info.Storage) {
		return fmt.Errorf("sandbox capability verification failed: %s", info.Capabilities.Reason)
	}
	r.Health = "verified"
	return nil
}
