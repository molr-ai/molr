package pricing

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"time"

	"molr.ai/runtime/internal/buildinfo"
	"molr.ai/runtime/internal/core"
)

const refreshInterval = 24 * time.Hour

type UpdateOptions struct {
	File, URL, CacheDir string
	Client              *http.Client
}

type RefreshResult struct {
	Source  string // builtin, file or url; never exposes deployment paths or URLs.
	Version string
	Updated bool
}

type refreshAttempt struct {
	done   chan struct{}
	result RefreshResult
	err    error
}

// Updater serializes automatic and manual refreshes of one configured source.
type Updater struct {
	catalog            *Catalog
	options            UpdateOptions
	client             *http.Client
	cachePath          string
	last               []byte
	etag, lastModified string
	mu                 sync.Mutex
	pending            *refreshAttempt
}

// NewUpdater restores the source-specific cache, then checks the source once.
// Source failures retain that cache, or the caller's built-in catalog.
func NewUpdater(ctx context.Context, catalog *Catalog, options UpdateOptions) (*Updater, error) {
	if catalog == nil || catalog.current.Load() == nil || options.File != "" && options.URL != "" {
		return nil, core.Invalid("pricing", "one source and an initialized catalog required")
	}
	if options.File != "" && !filepath.IsAbs(options.File) {
		return nil, core.Invalid("pricing.rules_file", "absolute path required")
	}
	if options.URL != "" {
		u, err := url.Parse(options.URL)
		if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Fragment != "" {
			return nil, core.Invalid("pricing.rules_url", "HTTPS URL without credentials or fragment required")
		}
	}
	u := &Updater{catalog: catalog, options: options}
	if options.File == "" && options.URL == "" {
		return u, nil
	}
	if !filepath.IsAbs(options.CacheDir) {
		return nil, core.Invalid("pricing", "absolute cache directory required")
	}
	client := options.Client
	if client == nil {
		client = &http.Client{}
	}
	copy := *client
	copy.Timeout = 10 * time.Second
	copy.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	u.client = &copy
	source := "file:" + options.File
	if options.URL != "" {
		source = "url:" + options.URL
	}
	u.cachePath = filepath.Join(options.CacheDir, fmt.Sprintf("%x.json", sha256.Sum256([]byte(source))))
	if data, err := readRulesFile(u.cachePath); err == nil {
		if err = catalog.Reload(bytes.NewReader(data)); err == nil {
			u.last = data
		} else {
			slog.Warn("pricing cache invalid; retaining built-in rules")
		}
	} else if !os.IsNotExist(err) {
		slog.Warn("pricing cache unavailable; retaining built-in rules")
	}
	if _, err := u.Refresh(ctx); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		slog.Warn("pricing source refresh failed; retaining last valid rules")
	}
	return u, nil
}

// Refresh coalesces overlapping checks. A failed check never changes the active
// catalog or the last valid cache; a successful replacement is persisted first.
func (u *Updater) Refresh(ctx context.Context) (RefreshResult, error) {
	if err := ctx.Err(); err != nil {
		return RefreshResult{}, err
	}
	u.mu.Lock()
	if pending := u.pending; pending != nil {
		u.mu.Unlock()
		select {
		case <-ctx.Done():
			return RefreshResult{}, ctx.Err()
		case <-pending.done:
			return pending.result, pending.err
		}
	}
	attempt := &refreshAttempt{done: make(chan struct{})}
	u.pending = attempt
	u.mu.Unlock()
	bounded, cancel := context.WithTimeout(ctx, 10*time.Second)
	attempt.result, attempt.err = u.refresh(bounded)
	cancel()
	u.mu.Lock()
	u.pending = nil
	close(attempt.done)
	u.mu.Unlock()
	return attempt.result, attempt.err
}

// Run checks configured sources every 24 hours until Runtime shutdown.
func (u *Updater) Run(ctx context.Context) { u.run(ctx, refreshInterval) }

func (u *Updater) run(ctx context.Context, interval time.Duration) {
	if u.cachePath == "" {
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	defer u.client.CloseIdleConnections()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := u.Refresh(ctx); err != nil && ctx.Err() == nil {
				slog.Warn("pricing source refresh failed; retaining last valid rules")
			}
		}
	}
}

func (u *Updater) refresh(ctx context.Context) (RefreshResult, error) {
	result := RefreshResult{Source: "builtin", Version: u.catalog.current.Load().version}
	if u.cachePath == "" {
		return result, nil
	}
	result.Source = "file"
	var data []byte
	var err error
	var etag, modified string
	if u.options.URL == "" {
		data, err = readRulesFile(u.options.File)
	} else {
		result.Source = "url"
		data, etag, modified, err = u.fetch(ctx)
	}
	if err != nil {
		return result, core.WithReason(core.NewError(core.Unavailable, "pricing source refresh failed; previous rules retained"), "pricing_refresh_failed")
	}
	if data == nil { // HTTP 304 with a valid conditional request.
		return result, nil
	}
	if !bytes.Equal(data, u.last) {
		candidate, err := New(bytes.NewReader(data))
		if err != nil {
			return result, core.WithReason(core.NewError(core.FailedPrecondition, "invalid pricing rules; previous rules retained"), "pricing_rules_invalid")
		}
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if err := u.save(data); err != nil {
			return result, core.WithReason(core.NewError(core.Unavailable, "pricing cache write failed; previous rules retained"), "pricing_refresh_failed")
		}
		u.catalog.current.Store(candidate.current.Load())
		u.last = data
		result.Updated, result.Version = true, candidate.current.Load().version
	}
	// Validators belong only to successfully accepted content.
	u.etag, u.lastModified = etag, modified
	return result, nil
}

func (u *Updater) fetch(ctx context.Context) ([]byte, string, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.options.URL, nil)
	if err != nil {
		return nil, "", "", err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", buildinfo.UserAgent())
	req.Header.Set("Cache-Control", "no-cache")
	if u.etag != "" {
		req.Header.Set("If-None-Match", u.etag)
	}
	if u.lastModified != "" {
		req.Header.Set("If-Modified-Since", u.lastModified)
	}
	response, err := u.client.Do(req)
	if err != nil {
		return nil, "", "", err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotModified && len(u.last) > 0 && (u.etag != "" || u.lastModified != "") {
		return nil, "", "", nil
	}
	if response.StatusCode != http.StatusOK {
		return nil, "", "", fmt.Errorf("unexpected pricing source status: %d", response.StatusCode)
	}
	data, err := readRules(response.Body)
	return data, response.Header.Get("ETag"), response.Header.Get("Last-Modified"), err
}

func readRulesFile(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > 1<<20 {
		return nil, fmt.Errorf("bounded regular pricing file required")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return readRules(file)
}

func readRules(reader io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(reader, (1<<20)+1))
	if err == nil && len(data) > 1<<20 {
		err = fmt.Errorf("pricing rules exceed 1 MiB")
	}
	return data, err
}

func (u *Updater) save(data []byte) error {
	if err := os.MkdirAll(u.options.CacheDir, 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(u.options.CacheDir, ".pricing-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err = file.Write(data); err == nil {
		err = file.Sync()
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(file.Name(), u.cachePath)
}
