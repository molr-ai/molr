// Package updates checks release metadata without downloading or installing programs.
package updates

import (
	"context"
	"encoding/json"
	"io"
	"math/rand/v2"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"molr.ai/runtime/internal/buildinfo"
	"molr.ai/runtime/internal/releaseplatform"
)

type Release struct {
	Version     string    `json:"version"`
	Commit      string    `json:"commit"`
	PublishedAt time.Time `json:"published_at"`
	NotesURL    string    `json:"notes_url"`
	File        string    `json:"file"`
	SHA256      string    `json:"sha256"`
	Image       string    `json:"image,omitempty"`
	Digest      string    `json:"digest,omitempty"`
}
type CheckState struct {
	Checking    bool       `json:"checking"`
	LastAttempt *time.Time `json:"last_attempt_at"`
	LastSuccess *time.Time `json:"last_success_at"`
	NextAllowed *time.Time `json:"next_allowed_at"`
	Error       string     `json:"error_code,omitempty"`
}
type Snapshot struct {
	Current          buildinfo.Info `json:"current"`
	Latest           *Release       `json:"latest"`
	Comparison       string         `json:"comparison"`
	Check            CheckState     `json:"check"`
	AutoCheck        bool           `json:"auto_check"`
	SourceConfigured bool           `json:"source_configured"`
	Supported        bool           `json:"supported"`
}
type Service struct {
	mu      sync.Mutex
	source  string
	client  *http.Client
	state   Snapshot
	pending chan struct{}
	now     func() time.Time
}

func New(source string, auto bool, current buildinfo.Info, client *http.Client) *Service {
	if current.Distribution == "container" {
		source = ""
		if containerRepository.MatchString(current.ContainerImage) {
			source = "https://ghcr.io"
		}
	}
	return &Service{source: strings.TrimRight(source, "/"), client: client, now: time.Now, state: Snapshot{
		Current: current, Comparison: "unknown", AutoCheck: auto && !Development(current.Version), SourceConfigured: source != "",
		Supported: releaseplatform.Supported(current.OS, current.Arch),
	}}
}
func (s *Service) snapshot() Snapshot {
	out := s.state
	if out.Latest != nil {
		v := *out.Latest
		out.Latest = &v
	}
	return out
}
func (s *Service) Snapshot() Snapshot { s.mu.Lock(); defer s.mu.Unlock(); return s.snapshot() }

// Check coalesces concurrent requests and keeps failures separate from the last good release.
func (s *Service) Check(ctx context.Context, automatic bool) Snapshot {
	s.mu.Lock()
	if s.pending != nil {
		done := s.pending
		s.mu.Unlock()
		select {
		case <-ctx.Done():
		case <-done:
		}
		return s.Snapshot()
	}
	now := s.now()
	cooldown := time.Minute
	if automatic {
		cooldown = 5 * time.Minute
	}
	if !s.state.SourceConfigured || !s.state.Supported || (automatic && !s.state.AutoCheck) || (s.state.Check.LastAttempt != nil && now.Sub(*s.state.Check.LastAttempt) < cooldown) {
		out := s.snapshot()
		s.mu.Unlock()
		return out
	}
	s.pending = make(chan struct{})
	s.state.Check.Checking = true
	s.state.Check.LastAttempt = &now
	next := now.Add(time.Minute)
	s.state.Check.NextAllowed = &next
	s.mu.Unlock()
	// One browser disconnect must not cancel a shared check; the bounded request owns its timeout.
	requestCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	release, code := s.fetch(requestCtx)
	cancel()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.Check.Checking = false
	s.state.Check.Error = code
	if code == "" {
		success := s.now()
		s.state.Check.LastSuccess = &success
		s.state.Latest = release
		s.state.Comparison = "uncomparable"
		if !Development(s.state.Current.Version) {
			if n, ok := Compare(s.state.Current.Version, release.Version); ok {
				s.state.Comparison = "current"
				if n < 0 {
					s.state.Comparison = "available"
				} else if n > 0 {
					s.state.Comparison = "ahead"
				}
			}
		}
	}
	close(s.pending)
	s.pending = nil
	return s.snapshot()
}

var hash = regexp.MustCompile(`^[0-9a-f]{64}$`)
var commit = regexp.MustCompile(`^(?:[0-9a-f]{40}|[0-9a-f]{64})$`)

func (s *Service) fetch(ctx context.Context) (*Release, string) {
	if s.state.Current.Distribution == "container" {
		return s.fetchContainer(ctx)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.source+"/latest-"+s.state.Current.OS+"-"+s.state.Current.Arch+".json", nil)
	if err != nil {
		return nil, "invalid_source"
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", buildinfo.UserAgent())
	req.Header.Set("Cache-Control", "no-cache")
	resp, err := s.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, "timeout"
		}
		return nil, "network_error"
	}
	defer resp.Body.Close()
	if resp.StatusCode == 404 {
		return nil, "release_not_found"
	}
	if resp.StatusCode != 200 {
		return nil, "source_unavailable"
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 65537))
	if err != nil {
		return nil, "network_error"
	}
	if len(data) > 65536 {
		return nil, "invalid_manifest"
	}
	var release Release
	if json.Unmarshal(data, &release) != nil || parseVersion(release.Version) == nil || Development(release.Version) || !hash.MatchString(release.SHA256) || !commit.MatchString(release.Commit) || release.PublishedAt.IsZero() || release.File != "molr_"+release.Version+"_"+s.state.Current.OS+"_"+s.state.Current.Arch+".tar.gz" {
		return nil, "invalid_manifest"
	}
	sourceURL, _ := url.Parse(s.source)
	channel := ""
	if sourceURL != nil {
		channel = sourceURL.Path[strings.LastIndex(sourceURL.Path, "/")+1:]
	}
	pre := parseVersion(release.Version)[4]
	if (channel == "stable" && pre != "") || (channel == "preview" && strings.HasPrefix(pre, "0.nightly.")) || (channel == "nightly" && !strings.HasPrefix(pre, "0.nightly.")) {
		return nil, "invalid_manifest"
	}
	notes, err := url.Parse(release.NotesURL)
	if err != nil || notes.Scheme != "https" || notes.Host == "" || notes.User != nil {
		return nil, "invalid_manifest"
	}
	return &release, ""
}
func (s *Service) Run(ctx context.Context) {
	if !s.Snapshot().AutoCheck || !s.Snapshot().SourceConfigured || !s.Snapshot().Supported {
		return
	}
	timer := time.NewTimer(30*time.Second + time.Duration(rand.IntN(30))*time.Second)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			state := s.Check(ctx, true)
			delay := 24*time.Hour + time.Duration(rand.IntN(600))*time.Second
			if state.Check.Error != "" {
				delay = time.Hour
			}
			timer.Reset(delay)
		}
	}
}
