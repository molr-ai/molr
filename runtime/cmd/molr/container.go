package main

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"flag"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"molr.ai/runtime/internal/config"
)

// The ordinary image starts itself; explicit serve/bootstrap commands remain available.
func runContainer(ctx context.Context, args []string, output, diagnostics io.Writer) error {
	if runtime.GOOS != "linux" {
		return fmt.Errorf("container startup requires Linux")
	}
	flags := flag.NewFlagSet("container", flag.ContinueOnError)
	flags.SetOutput(diagnostics)
	root := flags.String("data-dir", "/data", "persistent container volume")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected container arguments")
	}
	mode := os.Getenv("MOLR_MODE")
	if mode == "" {
		mode = "server"
	}
	path, err := prepareContainer(*root, mode, os.Getenv("MOLR_PUBLIC_URL"))
	if err != nil {
		return err
	}
	if mode == "server" {
		if err := run(ctx, []string{"bootstrap", "-config", path}, strings.NewReader(""), output, diagnostics); err != nil {
			return err
		}
	}
	return run(ctx, []string{"serve", "--mode", mode, "-config", path}, strings.NewReader(""), output, diagnostics)
}

func containerDirectory(path string) error {
	if err := os.MkdirAll(path, 0700); err != nil {
		return err
	}
	actual, err := filepath.EvalSymlinks(path)
	if err != nil {
		return err
	}
	if actual != filepath.Clean(path) {
		return fmt.Errorf("container data paths must not contain symbolic links")
	}
	return os.Chmod(path, 0700)
}

func containerFile(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > 1<<20 {
		return nil, fmt.Errorf("private regular container file required: %s", path)
	}
	return os.ReadFile(path)
}

func writeContainerFile(path string, data []byte) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".container-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err = file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err = file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}

func prepareContainer(root, mode, publicURL string) (string, error) {
	if mode != "server" && mode != "sandbox" {
		return "", fmt.Errorf("MOLR_MODE must be server or sandbox")
	}
	if !filepath.IsAbs(root) || filepath.Clean(root) == string(filepath.Separator) {
		return "", fmt.Errorf("dedicated absolute container data directory required")
	}
	root = filepath.Clean(root)
	if err := containerDirectory(root); err != nil {
		return "", fmt.Errorf("prepare container volume (must be writable by UID 65532): %w", err)
	}
	path := filepath.Join(root, mode+".json")
	data, err := containerFile(path)
	fresh := os.IsNotExist(err)
	if err != nil && !fresh {
		return "", err
	}
	var document map[string]any
	if fresh {
		address, tlsMode, dataDir := "http://127.0.0.1:8787", "lan", root
		sandbox := map[string]any{"enabled": false, "allow_full_access": false}
		if mode == "sandbox" {
			address, tlsMode, dataDir = "https://127.0.0.1:8443", "direct", filepath.Join(root, "private")
			sandbox = map[string]any{"enabled": true, "allow_full_access": true, "restrict_directories": true, "directories": []map[string]any{{"path": filepath.Join(root, "projects"), "read_only": false}}}
		}
		document = map[string]any{"data_dir": dataDir, "http": map[string]any{"public_url": address, "tls_mode": tlsMode}, "sandbox": sandbox}
		if mode == "sandbox" {
			h := document["http"].(map[string]any)
			h["auto_renew_certificate"] = true
			h["cert_file"], h["key_file"] = filepath.Join(dataDir, "server.crt"), filepath.Join(dataDir, "server.key")
		}
	} else {
		// Validate before applying explicit environment overrides; never repair unknown files.
		if _, err := config.Load(bytes.NewReader(data)); err != nil {
			return "", err
		}
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.UseNumber()
		if err := decoder.Decode(&document); err != nil {
			return "", err
		}
	}
	updateConfig := fresh || publicURL != ""
	if updateConfig {
		h, ok := document["http"].(map[string]any)
		if !ok {
			return "", fmt.Errorf("container configuration requires http")
		}
		previous, _ := h["public_url"].(string)
		if publicURL == "" {
			publicURL = previous
		}
		address, err := url.Parse(publicURL)
		if err != nil || address.Hostname() == "" {
			return "", fmt.Errorf("invalid MOLR_PUBLIC_URL")
		}
		port := address.Port()
		if port == "" {
			if address.Scheme == "https" {
				port = "443"
			} else {
				port = "80"
			}
		}
		h["public_url"], h["listen"] = publicURL, net.JoinHostPort("0.0.0.0", port)
		if mode == "server" {
			origins := []any{publicURL}
			if old, ok := h["allowed_origins"].([]any); ok {
				for _, origin := range old {
					if origin != previous && origin != publicURL {
						origins = append(origins, origin)
					}
				}
			}
			h["allowed_origins"] = origins
		}
		data, err = json.MarshalIndent(document, "", "  ")
		if err != nil {
			return "", err
		}
	}
	cfg, err := config.Load(bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	if mode == "sandbox" {
		if err := prepareContainerSandbox(root, cfg); err != nil {
			return "", err
		}
	}
	if updateConfig {
		if err := writeContainerFile(path, append(data, '\n')); err != nil {
			return "", err
		}
	}
	return path, nil
}

func prepareContainerSandbox(root string, cfg config.Config) error {
	if err := containerDirectory(cfg.DataDir); err != nil {
		return err
	}
	if err := containerDirectory(filepath.Join(root, "projects")); err != nil {
		return err
	}
	keyPath, certPath := cfg.HTTP.KeyFile, cfg.HTTP.CertFile
	// Custom certificate paths are managed by the operator, not regenerated here.
	if keyPath == filepath.Join(root, "private/server.key") && certPath == filepath.Join(root, "private/server.crt") {
		keyBytes, keyErr := containerFile(keyPath)
		_, certErr := containerFile(certPath)
		if keyErr != nil && !os.IsNotExist(keyErr) {
			return keyErr
		}
		if certErr != nil && !os.IsNotExist(certErr) {
			return certErr
		}
		if os.IsNotExist(keyErr) && certErr == nil {
			return fmt.Errorf("sandbox private key is missing; restore the existing key instead of replacing node identity")
		}
		if os.IsNotExist(keyErr) {
			key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
			if err != nil {
				return err
			}
			der, err := x509.MarshalPKCS8PrivateKey(key)
			if err != nil {
				return err
			}
			keyBytes = pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
			if err = writeContainerFile(keyPath, keyBytes); err != nil {
				return err
			}
		}
		if os.IsNotExist(certErr) {
			block, _ := pem.Decode(keyBytes)
			if block == nil {
				return fmt.Errorf("invalid sandbox private key")
			}
			private, err := x509.ParsePKCS8PrivateKey(block.Bytes)
			if err != nil {
				return err
			}
			key, ok := private.(*ecdsa.PrivateKey)
			if !ok {
				return fmt.Errorf("expected generated ECDSA sandbox key")
			}
			serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
			if err != nil {
				return err
			}
			address, _ := url.Parse(cfg.HTTP.PublicURL)
			template := x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: "molr-sandbox"}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().AddDate(1, 0, 0), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
			if ip := net.ParseIP(address.Hostname()); ip != nil {
				template.IPAddresses = []net.IP{ip}
			} else {
				template.DNSNames = []string{address.Hostname()}
			}
			der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
			if err != nil {
				return err
			}
			if err = writeContainerFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})); err != nil {
				return err
			}
		}
	}
	if _, err := tls.LoadX509KeyPair(certPath, keyPath); err != nil {
		return err
	}
	path := filepath.Join(cfg.DataDir, "sandbox-token")
	token, err := containerFile(path)
	if os.IsNotExist(err) {
		secret := make([]byte, 32)
		if _, err := rand.Read(secret); err != nil {
			return err
		}
		return writeContainerFile(path, []byte(hex.EncodeToString(secret)+"\n"))
	}
	if err != nil {
		return err
	}
	if len(strings.TrimSpace(string(token))) < 32 || len(token) > 4096 {
		return fmt.Errorf("invalid existing sandbox token")
	}
	return nil
}
