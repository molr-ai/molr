package plugins

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/platform"
)

const MaxPackageBytes = 8 << 20

// ReadArchive validates the entire archive before storing anything. Package
// paths are never extracted into the host filesystem by the import operation.
func ReadArchive(data []byte) (Package, error) {
	p := Package{Files: map[string]string{}}
	if len(data) > MaxPackageBytes {
		return p, invalid("archive exceeds 8 MiB")
	}
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil || len(z.File) > 512 {
		return p, invalid("invalid plugin ZIP")
	}
	seen := map[string]bool{}
	total := int64(0)
	manifest := false
	for _, f := range z.File {
		name := strings.TrimSuffix(f.Name, "/")
		if name == "" || path.Clean(name) != name || strings.HasPrefix(name, "/") || strings.Contains(name, "..") || strings.ContainsAny(name, "\\\x00") || seen[name] {
			return p, invalid("invalid or duplicate archive path")
		}
		seen[name] = true
		if f.FileInfo().IsDir() {
			continue
		}
		if !f.Mode().IsRegular() || f.UncompressedSize64 > 1<<20 {
			return p, invalid("ordinary bounded package files required")
		}
		total += int64(f.UncompressedSize64)
		if total > MaxPackageBytes {
			return p, invalid("expanded package exceeds 8 MiB")
		}
		r, err := f.Open()
		if err != nil {
			return p, invalid("cannot read archive")
		}
		body, err := io.ReadAll(io.LimitReader(r, (1<<20)+1))
		r.Close()
		if err != nil || len(body) > 1<<20 {
			return p, invalid("cannot read bounded archive file")
		}
		if name == "plugin.json" {
			dec := json.NewDecoder(bytes.NewReader(body))
			dec.DisallowUnknownFields()
			if err := dec.Decode(&p.Manifest); err != nil {
				return p, invalid("invalid plugin.json")
			}
			var extra any
			if dec.Decode(&extra) != io.EOF {
				return p, invalid("single plugin manifest required")
			}
			manifest = true
		} else {
			p.Files[name] = string(body)
		}
	}
	if !manifest {
		return p, invalid("ZIP root must contain plugin.json")
	}
	return p, p.Validate()
}

func (s *Service) ImportArchive(ctx context.Context, p core.Principal, data []byte) (Installation, error) {
	if err := s.authorize(ctx, p, true); err != nil {
		return Installation{}, err
	}
	pkg, err := ReadArchive(data)
	if err != nil {
		return Installation{}, err
	}
	return s.Install(ctx, p, pkg)
}

// Remote imports use the same public-address, pinned-dial network guard as
// server integrations. No cookies, credentials, proxies or redirects are used.
func (s *Service) ImportURL(ctx context.Context, p core.Principal, address string) (Installation, error) {
	if err := s.authorize(ctx, p, true); err != nil {
		return Installation{}, err
	}
	u, err := url.Parse(address)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Fragment != "" || len(address) > 4096 {
		return Installation{}, invalid("public HTTPS package URL required")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	client := platform.PublicHTTPClient("https://" + u.Host)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return Installation{}, invalid("invalid package URL")
	}
	res, err := client.Do(req)
	if err != nil {
		return Installation{}, core.NewError(core.Unavailable, "package download failed")
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return Installation{}, core.NewError(core.Unavailable, "package download failed")
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, MaxPackageBytes+1))
	if err != nil {
		return Installation{}, core.NewError(core.Unavailable, "package download failed")
	}
	return s.ImportArchive(ctx, p, data)
}
