package sandboxidentity

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"log/slog"
	"math/big"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const renewalWindow = 30 * 24 * time.Hour

type certificateManager struct {
	mu                sync.Mutex
	certPath, keyPath string
	renew             bool
	current           *tls.Certificate
}

// ServerTLS loads the node identity and renews managed self-signed certificates.
// The caller must cancel ctx when its server stops. Custom certificates are never rewritten.
func ServerTLS(ctx context.Context, certPath, keyPath string, autoRenew bool) (*tls.Config, error) {
	manager := &certificateManager{certPath: certPath, keyPath: keyPath, renew: autoRenew}
	if err := manager.refresh(time.Now()); err != nil {
		return nil, err
	}
	cfg := &tls.Config{MinVersion: tls.VersionTLS13, GetCertificate: func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
		manager.mu.Lock()
		defer manager.mu.Unlock()
		cert := manager.current
		now := time.Now()
		if now.Before(cert.Leaf.NotBefore) || !now.Before(cert.Leaf.NotAfter) {
			return nil, fmt.Errorf("sandbox certificate is not valid now")
		}
		return cert, nil
	}}
	go func() {
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				if err := manager.refresh(now); err != nil {
					slog.Error("sandbox certificate refresh failed", "error", err)
				}
			}
		}
	}()
	return cfg, nil
}

func (m *certificateManager) refresh(now time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	pair, err := tls.LoadX509KeyPair(m.certPath, m.keyPath)
	if err != nil {
		return err
	}
	cert, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return err
	}
	if m.current != nil && PublicKeySHA256(m.current.Leaf) != PublicKeySHA256(cert) {
		return fmt.Errorf("sandbox public key changed; restart after explicitly replacing the node identity")
	}
	if m.renew {
		if err := cert.CheckSignature(cert.SignatureAlgorithm, cert.RawTBSCertificate, cert.Signature); err != nil {
			return fmt.Errorf("automatic renewal requires a self-signed sandbox certificate")
		}
		if cert.NotAfter.Sub(now) <= renewalWindow || now.Before(cert.NotBefore) {
			if err := m.renewCertificate(pair, cert, now); err != nil {
				return err
			}
			pair, err = tls.LoadX509KeyPair(m.certPath, m.keyPath)
			if err != nil {
				return err
			}
			cert, err = x509.ParseCertificate(pair.Certificate[0])
			if err != nil {
				return err
			}
		}
	}
	if now.Before(cert.NotBefore) || !now.Before(cert.NotAfter) {
		return fmt.Errorf("sandbox certificate is not valid now")
	}
	pair.Leaf = cert
	m.current = &pair
	return nil
}

func (m *certificateManager) renewCertificate(pair tls.Certificate, cert *x509.Certificate, now time.Time) error {
	// Reject links before replacement; the service never changes private keys during renewal.
	for _, path := range []string{m.certPath, m.keyPath} {
		actual, err := filepath.EvalSymlinks(path)
		if err != nil || actual != filepath.Clean(path) {
			return fmt.Errorf("sandbox certificate paths must not contain symbolic links")
		}
		st, err := os.Lstat(path)
		if err != nil || !st.Mode().IsRegular() {
			return fmt.Errorf("sandbox certificate requires regular files")
		}
	}
	signer, ok := pair.PrivateKey.(crypto.Signer)
	if !ok {
		return fmt.Errorf("unsupported sandbox private key")
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return err
	}
	template := &x509.Certificate{SerialNumber: serial, Subject: cert.Subject, NotBefore: now.Add(-time.Minute), NotAfter: now.AddDate(1, 0, 0), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, DNSNames: cert.DNSNames, IPAddresses: cert.IPAddresses}
	der, err := x509.CreateCertificate(rand.Reader, template, template, signer.Public(), signer)
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(m.certPath), ".sandbox-cert-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err = file.Write(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})); err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(file.Name(), m.certPath)
}
