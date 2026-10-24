package sandboxidentity

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"molr.ai/runtime/internal/core"
)

func TestConnectionCodeVersionValidationAndRedaction(t *testing.T) {
	pin, token := strings.Repeat("ab", 32), strings.Repeat("synthetic-", 8)
	code, err := Encode(pin, core.NewSecret(token))
	if err != nil {
		t.Fatal(err)
	}
	gotPin, gotToken, err := Decode("\n" + code.Reveal() + "\n")
	if err != nil || gotPin != pin || gotToken.Reveal() != token {
		t.Fatal("code round trip failed", err)
	}
	if strings.Contains(fmt.Sprintf("%v %+v %#v", code, code, code), token) || strings.Contains(fmt.Sprint(code), code.Reveal()) {
		t.Fatal("secret formatting leaked code")
	}
	invalid := []string{"", "https://example.invalid", prefix + "bad!", strings.Replace(code.Reveal(), "v1.", "v2.", 1), strings.Repeat("x", 8193)}
	for _, v := range []connectionCode{{pin, "short"}, {"not-a-pin", token}, {pin, token + "\n"}} {
		data, _ := json.Marshal(v)
		invalid = append(invalid, prefix+base64.RawURLEncoding.EncodeToString(data))
	}
	data, _ := json.Marshal(map[string]string{"public_key_sha256": pin, "token": token, "endpoint": "https://example.invalid"})
	invalid = append(invalid, prefix+base64.RawURLEncoding.EncodeToString(data))
	for _, value := range invalid {
		_, _, err := Decode(value)
		if err == nil || strings.Contains(err.Error(), token) || strings.Contains(err.Error(), value) && value != "" {
			t.Fatal("invalid code accepted or disclosed")
		}
	}
	_, _, err = Decode(strings.Replace(code.Reveal(), "v1.", "v2.", 1))
	if core.ReasonOf(err) != "sandbox_connection_code_unsupported" {
		t.Fatal(err)
	}
	if _, err = Encode(pin, core.NewSecret("short")); err == nil {
		t.Fatal("invalid token exported")
	}
}

func writeTestIdentity(t *testing.T, dir string, expiry time.Time) (string, string, *x509.Certificate) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	cert := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "synthetic-sandbox"}, NotBefore: time.Now().Add(-48 * time.Hour), NotAfter: expiry, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, DNSNames: []string{"synthetic.invalid"}}
	der, err := x509.CreateCertificate(rand.Reader, cert, cert, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	private, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	certPath, keyPath := filepath.Join(dir, "server.crt"), filepath.Join(dir, "server.key")
	if err = os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: private}), 0600); err != nil {
		t.Fatal(err)
	}
	parsed, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return certPath, keyPath, parsed
}

func TestRenewalPreservesIdentityAndRecoversExpiredStartup(t *testing.T) {
	for _, remaining := range []time.Duration{7 * 24 * time.Hour, -time.Hour} {
		t.Run(remaining.String(), func(t *testing.T) {
			certPath, keyPath, before := writeTestIdentity(t, t.TempDir(), time.Now().Add(remaining))
			keyBefore, _ := os.ReadFile(keyPath)
			cfg, err := ServerTLS(t.Context(), certPath, keyPath, true)
			if err != nil {
				t.Fatal(err)
			}
			pair, err := cfg.GetCertificate(nil)
			if err != nil {
				t.Fatal(err)
			}
			after := pair.Leaf
			if bytes.Equal(before.Raw, after.Raw) || PublicKeySHA256(before) != PublicKeySHA256(after) || time.Until(after.NotAfter) < 364*24*time.Hour {
				t.Fatal("renewal changed identity or failed to extend validity")
			}
			keyAfter, _ := os.ReadFile(keyPath)
			if !bytes.Equal(keyBefore, keyAfter) {
				t.Fatal("renewal changed private key")
			}
			if st, _ := os.Stat(certPath); st.Mode().Perm() != 0600 {
				t.Fatal("certificate lost private permissions")
			}
			beforeCode, _ := Encode(PublicKeySHA256(before), core.NewSecret(strings.Repeat("synthetic-", 8)))
			afterCode, _ := Encode(PublicKeySHA256(after), core.NewSecret(strings.Repeat("synthetic-", 8)))
			if beforeCode.Reveal() != afterCode.Reveal() {
				t.Fatal("renewal changed connection code")
			}
		})
	}
}

func TestCertificateHotReloadAndCustomIdentityBoundaries(t *testing.T) {
	certPath, keyPath, before := writeTestIdentity(t, t.TempDir(), time.Now().Add(7*24*time.Hour))
	manager := &certificateManager{certPath: certPath, keyPath: keyPath}
	if err := manager.refresh(time.Now()); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(manager.current.Certificate[0], before.Raw) {
		t.Fatal("custom certificate rewritten")
	}
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	server.TLS = &tls.Config{MinVersion: tls.VersionTLS13, GetCertificate: func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
		manager.mu.Lock()
		defer manager.mu.Unlock()
		return manager.current, nil
	}}
	server.StartTLS()
	defer server.Close()
	// httptest installs a fallback certificate; a ServerName exercises the dynamic certificate callback.
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13, ServerName: "synthetic.invalid", InsecureSkipVerify: true, VerifyConnection: func(state tls.ConnectionState) error {
		if PublicKeySHA256(state.PeerCertificates[0]) != PublicKeySHA256(before) {
			return fmt.Errorf("wrong node")
		}
		return nil
	}}}}
	defer client.CloseIdleConnections()
	get := func() {
		t.Helper()
		resp, err := client.Get(server.URL)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 204 {
			t.Fatal(resp.Status)
		}
	}
	get()
	manager.renew = true
	if err := manager.refresh(time.Now()); err != nil {
		t.Fatal(err)
	}
	client.CloseIdleConnections()
	get()
	if bytes.Equal(manager.current.Certificate[0], before.Raw) {
		t.Fatal("running certificate was not replaced")
	}
	// An administrative key replacement cannot silently change a running identity.
	writeTestIdentity(t, filepath.Dir(certPath), time.Now().Add(365*24*time.Hour))
	if err := manager.refresh(time.Now()); err == nil {
		t.Fatal("changed public key accepted")
	}
	if PublicKeySHA256(manager.current.Leaf) != PublicKeySHA256(before) {
		t.Fatal("failed refresh replaced active identity")
	}
	expiredCert, expiredKey, _ := writeTestIdentity(t, t.TempDir(), time.Now().Add(-time.Hour))
	if _, err := ServerTLS(context.Background(), expiredCert, expiredKey, false); err == nil {
		t.Fatal("expired unmanaged certificate accepted")
	}
}
