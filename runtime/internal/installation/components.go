package installation

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"golang.org/x/sys/unix"
	"molr.ai/runtime/internal/installrecord"
)

//go:embed licenses/agent-browser-LICENSE
var agentBrowserLicense []byte

type ComponentError struct {
	Code   string
	Detail string
}

func (e *ComponentError) Error() string        { return e.Code + ": " + e.Detail }
func componentError(code, detail string) error { return &ComponentError{code, detail} }

type componentAsset struct {
	URL, SHA256, Format, Prefix, Directory, Executable string
	Size, Expanded                                     int64
}
type componentRelease struct {
	Version string
	Assets  []componentAsset
}

// Release identities are compiled into the installer. Neither HTTP nor agent
// requests can choose a URL, digest, archive layout, or installation destination.
func browserRelease() (componentRelease, error) {
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		return componentRelease{}, componentError("component_platform_unsupported", "no tested browser package for this platform")
	}
	return componentRelease{Version: "0.39.0-chromium-149.0.7827.55", Assets: []componentAsset{
		{URL: "https://api.github.com/repos/vercel-labs/agent-browser/releases/assets/625880353", SHA256: "d258593db4899577748a4b2a671a315f76f07a4553ba7fbbeaa95f0d16a4aaa8", Format: "raw", Directory: "agent", Executable: "agent-browser", Size: 18302648, Expanded: 18302648},
		{URL: "https://storage.googleapis.com/chrome-for-testing-public/149.0.7827.55/linux64/chrome-linux64.zip", SHA256: "13113b963ac22fffdad898a677591028e4397c46c1daa9e61811258eed6e35b5", Format: "zip", Prefix: "chrome-linux64/", Directory: "chromium", Executable: "chrome", Size: 185646494, Expanded: 1 << 30},
	}}, nil
}

type ComponentReceipt struct {
	Version     string    `json:"version"`
	Directory   string    `json:"directory"`
	InstalledAt time.Time `json:"installed_at"`
}
type ComponentStatus struct {
	ID            string `json:"id"`
	State         string `json:"state"`
	Version       string `json:"version,omitempty"`
	TargetVersion string `json:"target_version,omitempty"`
	DownloadBytes int64  `json:"download_bytes"`
	DiskBytes     int64  `json:"disk_bytes"`
	CanInstall    bool   `json:"can_install"`
	Reason        string `json:"reason,omitempty"`
	Task          *Task  `json:"task,omitempty"`
	AutoInstall   bool   `json:"auto_install"`
}

func componentRoot(r *installrecord.Record) string { return filepath.Join(r.Paths.Cache, "components") }
func componentRecord(r *installrecord.Record) string {
	return filepath.Join(componentRoot(r), "browser.json")
}
func componentReceipt(r *installrecord.Record) (ComponentReceipt, error) {
	var v ComponentReceipt
	p := componentRecord(r)
	if err := checkPath(p); err != nil {
		return v, err
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return v, err
	}
	if len(b) > 8192 {
		return v, fmt.Errorf("oversized component receipt")
	}
	err = json.Unmarshal(b, &v)
	if err == nil && (!taskIDPattern.MatchString(v.Directory) || v.Version == "") {
		err = fmt.Errorf("invalid component receipt")
	}
	return v, err
}
func componentPrograms(r *installrecord.Record) (string, string, error) {
	receipt, err := componentReceipt(r)
	if err != nil {
		return "", "", err
	}
	release, err := browserRelease()
	if err != nil {
		return "", "", err
	}
	if receipt.Version != release.Version {
		return "", "", componentError("component_update_required", "prepare the compatible component version")
	}
	root := filepath.Join(componentRoot(r), "browser", receipt.Directory)
	a, c := filepath.Join(root, "agent"), filepath.Join(root, "chromium")
	for _, p := range []string{filepath.Join(a, "agent-browser"), filepath.Join(c, "chrome")} {
		if err = checkManaged(p, r.UID, nil); err != nil {
			return "", "", err
		}
		info, err := os.Lstat(p)
		if err != nil {
			return "", "", err
		}
		if !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
			return "", "", fmt.Errorf("invalid component executable")
		}
	}
	return a, c, nil
}
func latestTask(r *installrecord.Record) *Task {
	var latest *Task
	entries, _ := os.ReadDir(taskRoot(r))
	for _, entry := range entries {
		n := entry.Name()
		if len(n) != 37 || n[32:] != ".json" {
			continue
		}
		v, err := readTask(r, n[:32])
		if err == nil && (latest == nil || v.StartedAt.After(latest.StartedAt)) {
			latest = &v
		}
	}
	return latest
}
func componentSnapshot(r *installrecord.Record) ComponentStatus {
	s := ComponentStatus{ID: "browser", State: "missing"}
	release, err := browserRelease()
	if err != nil {
		s.State = "unsupported"
		s.Reason = "component_platform_unsupported"
		return s
	}
	s.TargetVersion = release.Version
	for _, a := range release.Assets {
		s.DownloadBytes += a.Size
		s.DiskBytes += a.Expanded + a.Size
	}
	if receipt, err := componentReceipt(r); err == nil {
		s.Version = receipt.Version
		if _, _, err = componentPrograms(r); err == nil {
			s.State = "installed"
		} else {
			s.State = "broken"
			s.Reason = "component_files_invalid"
			if receipt.Version != release.Version {
				s.State = "installed"
				s.Reason = "component_update_required"
			}
		}
	}
	s.Task = latestTask(r)
	if s.Task != nil && s.Task.Component != "browser" {
		s.Task = nil
	}
	_, err = os.Stat(filepath.Join(componentRoot(r), "browser.enabled"))
	s.AutoInstall = err == nil
	return s
}
func (e *Engine) componentCommand() error {
	record := e.Options.Record
	if record == "" {
		record = filepath.Join(e.Root, "installation.json")
	}
	r, err := readRecord(record, false)
	if err != nil {
		return err
	}
	uid := os.Geteuid()
	if uid != r.OwnerUID && uid != r.UID {
		return componentError("maintenance_permission_denied", "installation owner or service account required")
	}
	if err = checkManaged(r.Paths.Cache, r.UID, nil); err != nil {
		return err
	}
	out := e.UI.Out
	if e.Options.JSON {
		out = e.machineOut
	}
	if e.Options.Command == "tasks" {
		t, err := readTask(r, e.Options.TaskID)
		if err != nil {
			return err
		}
		if e.Options.TaskAction == "cancel" {
			t, err = requestTaskCancel(r, e.Options.TaskID)
			if err != nil {
				return err
			}
		} else if e.Options.TaskAction != "status" {
			return fmt.Errorf("unknown task operation")
		}
		return json.NewEncoder(out).Encode(t)
	}
	if e.Options.Component != "" && e.Options.Component != "browser" {
		return fmt.Errorf("unknown component")
	}
	if e.Options.TaskAction == "status" {
		s := componentSnapshot(r)
		return json.NewEncoder(out).Encode(s)
	}
	if e.Options.Component != "browser" {
		return fmt.Errorf("component required")
	}
	switch e.Options.TaskAction {
	case "install", "update", "repair":
	default:
		return fmt.Errorf("unknown component operation")
	}
	release, err := browserRelease()
	if err != nil {
		return err
	}
	if e.Options.DryRun {
		return json.NewEncoder(out).Encode(componentSnapshot(r))
	}
	if err = e.beginTask(r, "component_"+e.Options.TaskAction, "browser", release.Version); err != nil {
		return err
	}
	// Explicit setup configuration is authoritative even for a stale caller.
	cfg, err := os.ReadFile(filepath.Join(r.Paths.Config, "server.json"))
	if err != nil {
		return err
	}
	var options struct {
		Browser struct {
			Agent    string `json:"agent_directory"`
			Chromium string `json:"chromium_directory"`
		} `json:"browser"`
	}
	if err = json.Unmarshal(cfg, &options); err != nil {
		return err
	}
	if options.Browser.Agent != "" || options.Browser.Chromium != "" {
		return componentError("component_externally_managed", "remove explicit browser paths before using managed installation")
	}
	if e.Options.InstallDeps {
		if err = e.progress("installing", 0, 0); err != nil {
			return err
		}
		if err = e.prepareBrowserDependencies(r); err != nil {
			return err
		}
	}
	if err = mkdirOwned(componentRoot(r), 0700, r.UID, r.GID); err != nil {
		return err
	}
	if err = atomicWrite(filepath.Join(componentRoot(r), "browser.enabled"), strings.NewReader("enabled\n"), 0600, r.UID, r.GID); err != nil {
		return err
	}
	if old, err := componentReceipt(r); err == nil && old.Version == release.Version && e.Options.TaskAction != "repair" {
		if _, _, err = componentPrograms(r); err == nil {
			return nil
		}
	}
	return e.installComponent(r, release)
}
func (e *Engine) installComponent(r *installrecord.Record, release componentRelease) error {
	root := componentRoot(r)
	var disk unix.Statfs_t
	if err := unix.Statfs(root, &disk); err != nil {
		return err
	}
	var required int64
	for _, a := range release.Assets {
		required += a.Size + a.Expanded
	}
	if uint64(disk.Bavail)*uint64(disk.Bsize) < uint64(required) {
		return componentError("component_disk_full", "insufficient free disk space")
	}
	staging, err := os.MkdirTemp(root, ".staging-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(staging)
	for _, a := range release.Assets {
		e.downloadTotal += a.Size
	}
	for _, a := range release.Assets {
		if err = e.progress("downloading", e.downloadBase, e.downloadTotal); err != nil {
			return err
		}
		archive := filepath.Join(staging, "download")
		if err = e.fetchComponent(a, archive); err != nil {
			return err
		}
		if err = e.progress("verifying", e.downloadBase+a.Size, e.downloadTotal); err != nil {
			return err
		}
		destination := filepath.Join(staging, a.Directory)
		if err = os.Mkdir(destination, 0755); err != nil {
			return err
		}
		if a.Format == "raw" {
			err = copyFile(filepath.Join(destination, a.Executable), archive, 0755, r.UID, r.GID)
		} else {
			err = extractComponent(archive, destination, a)
		}
		if err != nil {
			return err
		}
		e.downloadBase += a.Size
		if err = os.Remove(archive); err != nil {
			return err
		}
	}
	if err = atomicWrite(filepath.Join(staging, "agent", "LICENSE"), bytes.NewReader(agentBrowserLicense), 0644, r.UID, r.GID); err != nil {
		return err
	}
	if err = filepath.WalkDir(staging, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("component symlink rejected")
		}
		if os.Geteuid() == 0 {
			return os.Chown(p, r.UID, r.GID)
		}
		return nil
	}); err != nil {
		return err
	}
	if err = e.progress("installing", 0, 0); err != nil {
		return err
	}
	parent := filepath.Join(root, "browser")
	if err = mkdirOwned(parent, 0700, r.UID, r.GID); err != nil {
		return err
	}
	id := newTaskID()
	final := filepath.Join(parent, id)
	if err = os.Rename(staging, final); err != nil {
		return err
	}
	receipt := ComponentReceipt{Version: release.Version, Directory: id, InstalledAt: time.Now().UTC()}
	return atomicWrite(componentRecord(r), bytes.NewReader(jsonBytes(receipt)), 0600, r.UID, r.GID)
}
func componentDownloadURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Fragment != "" || u.Port() != "" {
		return fmt.Errorf("invalid component source")
	}
	switch u.Hostname() {
	case "api.github.com", "github.com", "release-assets.githubusercontent.com", "objects.githubusercontent.com", "storage.googleapis.com":
		return nil
	}
	return fmt.Errorf("unapproved component host")
}
func (e *Engine) fetchComponent(a componentAsset, destination string) error {
	if err := componentDownloadURL(a.URL); err != nil {
		return err
	}
	ctx, cancel := e.downloadContext()
	defer cancel()
	transport := e.componentTransport
	if transport == nil {
		t := http.DefaultTransport.(*http.Transport).Clone()
		t.ResponseHeaderTimeout = 30 * time.Second
		transport = t
		defer t.CloseIdleConnections()
	}
	client := &http.Client{Transport: transport, Timeout: 15 * time.Minute, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) > 5 {
			return fmt.Errorf("too many redirects")
		}
		return componentDownloadURL(req.URL.String())
	}}
	req, err := http.NewRequestWithContext(ctx, "GET", a.URL, nil)
	if err != nil {
		return err
	}
	if req.URL.Hostname() == "api.github.com" {
		req.Header.Set("Accept", "application/octet-stream")
	}
	res, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return componentError("component_download_failed", err.Error())
	}
	defer res.Body.Close()
	if res.StatusCode != 200 || res.ContentLength > a.Size {
		return componentError("component_download_failed", "unexpected response or size")
	}
	f, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	hash := sha256.New()
	reader := io.LimitReader(res.Body, a.Size+1)
	buffer := make([]byte, 128<<10)
	var total int64
	last := time.Time{}
	for {
		n, readErr := reader.Read(buffer)
		if n > 0 {
			total += int64(n)
			if total > a.Size {
				return componentError("component_download_failed", "size limit exceeded")
			}
			if _, err = io.MultiWriter(f, hash).Write(buffer[:n]); err != nil {
				return err
			}
			if time.Since(last) > 250*time.Millisecond {
				if err = e.progress("downloading", e.downloadBase+total, max(e.downloadTotal, a.Size)); err != nil {
					return err
				}
				last = time.Now()
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return componentError("component_download_failed", readErr.Error())
		}
	}
	if total != a.Size || hex.EncodeToString(hash.Sum(nil)) != a.SHA256 {
		return componentError("component_checksum_mismatch", "download identity mismatch")
	}
	return f.Sync()
}
func extractComponent(archive, destination string, a componentAsset) error {
	z, err := zip.OpenReader(archive)
	if err != nil {
		return err
	}
	defer z.Close()
	if len(z.File) > 10000 {
		return fmt.Errorf("archive file count exceeded")
	}
	seen := map[string]bool{}
	var total uint64
	for _, entry := range z.File {
		name := entry.Name
		if strings.ContainsAny(name, "\\\x00") || !strings.HasPrefix(name, a.Prefix) {
			return fmt.Errorf("invalid archive path")
		}
		name = strings.TrimPrefix(name, a.Prefix)
		if name == "" && entry.FileInfo().IsDir() {
			continue
		}
		clean := strings.TrimSuffix(name, "/")
		if clean == "" || path.Clean(clean) != clean || path.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, "../") || seen[clean] {
			return fmt.Errorf("invalid archive path")
		}
		seen[clean] = true
		mode := entry.Mode()
		if !mode.IsRegular() && !mode.IsDir() {
			return fmt.Errorf("archive links and special files forbidden")
		}
		target := filepath.Join(destination, filepath.FromSlash(clean))
		if mode.IsDir() {
			d, err := openDir(target, true)
			if err != nil {
				return err
			}
			d.Close()
			continue
		}
		if entry.UncompressedSize64 > uint64(a.Expanded)-total {
			return fmt.Errorf("archive expanded size exceeded")
		}
		total += entry.UncompressedSize64
		d, err := openDir(filepath.Dir(target), true)
		if err != nil {
			return err
		}
		d.Close()
		perm := os.FileMode(0644)
		if mode&0111 != 0 {
			perm = 0755
		}
		src, err := entry.Open()
		if err != nil {
			return err
		}
		dst, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
		if err != nil {
			src.Close()
			return err
		}
		if err = dst.Chmod(perm); err != nil {
			src.Close()
			dst.Close()
			return err
		}
		n, copyErr := io.Copy(dst, io.LimitReader(src, int64(entry.UncompressedSize64)+1))
		src.Close()
		syncErr := dst.Sync()
		closeErr := dst.Close()
		if copyErr != nil {
			return copyErr
		}
		if n != int64(entry.UncompressedSize64) {
			return fmt.Errorf("archive size mismatch")
		}
		if syncErr != nil {
			return syncErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	p := filepath.Join(destination, a.Executable)
	info, err := os.Stat(p)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
		return fmt.Errorf("archive executable missing")
	}
	return nil
}
