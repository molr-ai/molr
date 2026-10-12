package installation

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"molr.ai/runtime/internal/installrecord"
	"molr.ai/runtime/internal/updates"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"strings"
	"time"
)

var releaseVersion = regexp.MustCompile(`^v?[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?$`)
var digestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

func channelFor(version string) string {
	if strings.Contains(version, "nightly") {
		return "nightly"
	}
	if strings.Contains(version, "-") {
		return "preview"
	}
	return "stable"
}
func validateURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("invalid release URL")
	}
	if u.Scheme == "https" {
		return nil
	}
	ip := net.ParseIP(u.Hostname())
	if u.Scheme == "http" && ip != nil && (ip.IsLoopback() || ip.IsPrivate()) {
		return nil
	}
	return fmt.Errorf("release URL requires HTTPS (private numeric HTTP is allowed for local mirrors)")
}
func validateSubscription(s installrecord.Updates) error {
	if s.Channel != "stable" && s.Channel != "preview" && s.Channel != "nightly" {
		return fmt.Errorf("invalid update channel")
	}
	return validateURL(s.Source)
}
func downloadClient() *http.Client {
	return &http.Client{Timeout: 30 * time.Minute, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return fmt.Errorf("too many redirects")
		}
		if via[0].URL.Scheme == "https" && req.URL.Scheme != "https" {
			return fmt.Errorf("refusing HTTPS downgrade")
		}
		return validateURL(req.URL.String())
	}}
}
func (e *Engine) fetch(raw string, destination io.Writer, limit int64, progress bool) error {
	if err := validateURL(raw); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(e.Ctx, http.MethodGet, raw, nil)
	if err != nil {
		return err
	}
	res, err := downloadClient().Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return fmt.Errorf("download returned HTTP %d", res.StatusCode)
	}
	if res.ContentLength > limit {
		return fmt.Errorf("download exceeds size limit")
	}
	reader := io.Reader(io.LimitReader(res.Body, limit+1))
	if progress {
		reader = &progressReader{Reader: reader, ui: e.UI, total: res.ContentLength}
	}
	n, err := io.Copy(destination, reader)
	if err != nil {
		return err
	}
	if n > limit {
		return fmt.Errorf("download exceeds size limit")
	}
	if progress {
		e.UI.print("Downloaded %d bytes", n)
	}
	return nil
}

type progressReader struct {
	io.Reader
	ui          *UI
	total, read int64
	last        time.Time
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.Reader.Read(b)
	p.read += int64(n)
	if time.Since(p.last) >= time.Second {
		if p.total > 0 {
			p.ui.print("Download: %.1f%% (%d / %d bytes)", 100*float64(p.read)/float64(p.total), p.read, p.total)
		} else {
			p.ui.print("Download: %d bytes", p.read)
		}
		p.last = time.Now()
	}
	return n, err
}

type releaseManifest struct {
	Schema    int            `json:"schema"`
	Version   string         `json:"version"`
	Commit    string         `json:"commit"`
	Channel   string         `json:"channel"`
	Platforms []releaseAsset `json:"platforms"`
}
type releaseAsset struct {
	OS string `json:"os"
 "os/exec"`
	Arch   string `json:"arch"`
	File   string `json:"file"`
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
}

func (e *Engine) update(r *installrecord.Record) error {
	e.UI.step(1, "Check release source")
	subscription := r.Updates
	if e.Options.Source != "" {
		subscription.Source = strings.TrimRight(e.Options.Source, "/")
	}
	if e.Options.Channel != "" {
		subscription.Channel = e.Options.Channel
	}
	if err := validateSubscription(subscription); err != nil {
		return err
	}
	endpoint := subscription.Source + "/" + subscription.Channel + "/manifest.json"
	if e.Options.Version != "" {
		if !releaseVersion.MatchString(e.Options.Version) {
			return fmt.Errorf("invalid release version")
		}
		endpoint = subscription.Source + "/versions/" + e.Options.Version + ".json"
	}
	var data strings.Builder
	if err := e.fetch(endpoint, &data, 2<<20, false); err != nil {
		return err
	}
	var manifest releaseManifest
	if err := json.Unmarshal([]byte(data.String()), &manifest); err != nil {
		return err
	}
	if manifest.Schema != 1 || !releaseVersion.MatchString(manifest.Version) || manifest.Commit == "" {
		return fmt.Errorf("invalid release manifest")
	}
	if e.Options.Version != "" && manifest.Version != e.Options.Version {
		return fmt.Errorf("release version mismatch")
	}
	if e.Options.Version == "" && manifest.Channel != subscription.Channel && !(subscription.Channel == "preview" && manifest.Channel == "stable") {
		return fmt.Errorf("release channel mismatch")
	}
	n, ok := updates.Compare(manifest.Version, r.Build.Version)
	if !ok {
		return fmt.Errorf("cannot compare release versions")
	}
	if n < 0 && !e.Options.AllowDowngrade {
		return fmt.Errorf("selected release is older; an intended downgrade requires --allow-downgrade")
	}
	if n == 0 && manifest.Commit == r.Build.Commit && !e.Options.Repair {
		e.UI.print("Already current: %s", r.Build.Version)
		if subscription == r.Updates {
			return nil
		}
		e.UI.print("Update subscription: %s/%s", subscription.Source, subscription.Channel)
		if e.Options.DryRun {
			return nil
		}
		ok, err := e.UI.confirm("Save this update subscription?")
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("subscription change cancelled")
		}
		lock, err := lockInstance(r.Paths.Program)
		if err != nil {
			return err
		}
		defer lock.Close()
		current, err := readRecord(r.File(), true)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(current, r) {
			return fmt.Errorf("installation changed since planning")
		}
		next := *r
		next.Updates = subscription
		return writeRecord(&next)
	}
	var asset *releaseAsset
	for i := range manifest.Platforms {
		a := &manifest.Platforms[i]
		if a.OS == runtime.GOOS && a.Arch == runtime.GOARCH {
			if asset != nil {
				return fmt.Errorf("duplicate platform")
			}
			asset = a
		}
	}
	if asset == nil {
		return fmt.Errorf("release has no %s/%s package", runtime.GOOS, runtime.GOARCH)
	}
	if !digestPattern.MatchString(asset.SHA256) || filepath.Base(asset.File) != asset.File {
		return fmt.Errorf("invalid release asset")
	}
	if err := validateURL(asset.URL); err != nil {
		return err
	}
	e.UI.print("Installed: %s\nSelected: %s\nPlatform: %s/%s\nPackage: %s\nURL: %s\nSHA-256: %s", r.Build.Version, manifest.Version, runtime.GOOS, runtime.GOARCH, asset.File, asset.URL, asset.SHA256)
	if e.Options.DryRun {
		e.UI.print("Dry run complete: no package downloaded or installation changes.")
		return nil
	}
	ok, err := e.UI.confirm("Download and apply this update?")
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("update cancelled")
	}
	temp, err := os.MkdirTemp("", "molr-update-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(temp)
	if err = os.Chmod(temp, 0755); err != nil {
		return err
	}
	archive, err := os.CreateTemp(temp, "archive-")
	if err != nil {
		return err
	}
	hash := sha256.New()
	e.UI.step(2, "Download and verify package")
	err = e.fetch(asset.URL, io.MultiWriter(archive, hash), 512<<20, true)
	closeErr := archive.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if hex.EncodeToString(hash.Sum(nil)) != asset.SHA256 {
		return fmt.Errorf("package checksum mismatch")
	}
	root, err := extractPackage(archive.Name(), temp)
	if err != nil {
		return err
	}
	metadata, err := os.ReadFile(filepath.Join(root, "release.json"))
	if err != nil {
		return err
	}
	var build installrecord.Build
	if err = json.Unmarshal(metadata, &build); err != nil {
		return err
	}
	if build != (installrecord.Build{Version: manifest.Version, Commit: manifest.Commit, OS: runtime.GOOS, Arch: runtime.GOARCH}) {
		return fmt.Errorf("downloaded package identity mismatch")
	}
	args := []string{filepath.Join(root, "bin/molr-setup"), "--record", r.File(), "--source", subscription.Source, "--channel", subscription.Channel, "--directory-permissions", e.Options.Permissions}
	for flag, on := range map[string]bool{"--interactive": e.Options.Interactive, "--non-interactive": e.Options.NonInteractive, "--yes": e.Options.Yes, "--install-deps": e.Options.InstallDeps, "--skip-dependencies": e.Options.SkipDependencies, "--no-start": e.Options.NoStart, "--no-browser": e.Options.NoBrowser, "--repair": e.Options.Repair, "--allow-downgrade": e.Options.AllowDowngrade} {
		if on {
			args = append(args, flag)
		}
	}
	e.UI.print("Starting verified package installer...")
	_, err = e.Command(e.Ctx, args, e.Input, nil, e.UI.Out, e.UI.Err)
	var exited *exec.ExitError
	if errors.As(err, &exited) && exited.ExitCode() == 3 {
		return &VerificationPending{}
	}
	return err
}
func extractPackage(archive, destination string) (string, error) {
	file, err := os.Open(archive)
	if err != nil {
		return "", err
	}
	defer file.Close()
	gz, err := gzip.NewReader(file)
	if err != nil {
		return "", err
	}
	defer gz.Close()
	reader := tar.NewReader(gz)
	allowed := map[string]os.FileMode{"molr/LICENSE": 0644, "molr/THIRD_PARTY_NOTICES.md": 0644, "molr/release.json": 0644, "molr/bin/molr": 0755, "molr/bin/molr-setup": 0755}
	seen := map[string]bool{}
	var size int64
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
		name := strings.TrimSuffix(header.Name, "/")
		if header.Typeflag == tar.TypeDir && (name == "molr" || name == "molr/bin") {
			continue
		}
		mode, ok := allowed[name]
		if !ok || seen[name] || header.Typeflag != tar.TypeReg || header.Size < 0 {
			return "", fmt.Errorf("unexpected archive member: %s", header.Name)
		}
		seen[name] = true
		if header.Size > (1<<30)-size {
			return "", fmt.Errorf("expanded package exceeds size limit")
		}
		size += header.Size
		path := filepath.Join(destination, filepath.FromSlash(name))
		parent, err := openDir(filepath.Dir(path), true)
		if err != nil {
			return "", err
		}
		parent.Close()
		out, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
		if err != nil {
			return "", err
		}
		_, err = io.Copy(out, reader)
		chmodErr := out.Chmod(mode)
		closeErr := out.Close()
		if err != nil {
			return "", err
		}
		if chmodErr != nil {
			return "", chmodErr
		}
		if closeErr != nil {
			return "", closeErr
		}
	}
	if len(seen) != len(allowed) {
		return "", fmt.Errorf("incomplete release package")
	}
	root := filepath.Join(destination, "molr")
	for _, name := range []string{"molr", "molr-setup"} {
		if err = staticELF(filepath.Join(root, "bin", name), runtime.GOARCH); err != nil {
			return "", err
		}
	}
	return root, nil
}
