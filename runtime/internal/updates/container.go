package updates

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

var containerRepository = regexp.MustCompile(`^ghcr\.io/[a-z0-9][a-z0-9_.-]*/[a-z0-9][a-z0-9_.-]*$`)

const imageIndexType = "application/vnd.oci.image.index.v1+json"
const imageManifestType = "application/vnd.oci.image.manifest.v1+json"
const dockerManifestType = "application/vnd.docker.distribution.manifest.v2+json"

type containerIndex struct {
	SchemaVersion int               `json:"schemaVersion"`
	MediaType     string            `json:"mediaType"`
	Annotations   map[string]string `json:"annotations"`
	Manifests     []struct {
		MediaType string `json:"mediaType"`
		Digest    string `json:"digest"`
		Size      int    `json:"size"`
		Platform  struct {
			OS           string `json:"os"`
			Architecture string `json:"architecture"`
		} `json:"platform"`
	} `json:"manifests"`
}

// Container checks use anonymous pull credentials and only report published images.
// The fixed GHCR origin is also enforced by the production HTTP transport.
func (s *Service) containerRequest(ctx context.Context, path, token string) ([]byte, http.Header, string) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.source+path, nil)
	if err != nil {
		return nil, nil, "invalid_source"
	}
	req.Header.Set("Accept", imageIndexType+", "+imageManifestType+", "+dockerManifestType)
	req.Header.Set("User-Agent", "molr/"+s.state.Current.Version)
	req.Header.Set("Cache-Control", "no-cache")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := s.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, nil, "timeout"
		}
		return nil, nil, "network_error"
	}
	defer response.Body.Close()
	if response.StatusCode == 404 {
		return nil, nil, "release_not_found"
	}
	if response.StatusCode != 200 {
		return nil, nil, "source_unavailable"
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 1048577))
	if err != nil {
		return nil, nil, "network_error"
	}
	if len(data) > 1048576 {
		return nil, nil, "invalid_manifest"
	}
	return data, response.Header, ""
}

func (s *Service) fetchContainer(ctx context.Context) (*Release, string) {
	image := s.state.Current.ContainerImage
	if !containerRepository.MatchString(image) {
		return nil, "invalid_source"
	}
	repository := strings.TrimPrefix(image, "ghcr.io/")
	data, _, code := s.containerRequest(ctx, "/token?service=ghcr.io&scope="+url.QueryEscape("repository:"+repository+":pull"), "")
	if code != "" {
		if code == "release_not_found" {
			code = "source_unavailable"
		}
		return nil, code
	}
	var auth struct {
		Token string `json:"token"`
	}
	if json.Unmarshal(data, &auth) != nil || auth.Token == "" {
		return nil, "invalid_manifest"
	}
	prefix := "/v2/" + repository
	const tag = "latest"
	data, headers, code := s.containerRequest(ctx, prefix+"/manifests/"+tag, auth.Token)
	if code != "" {
		return nil, code
	}
	digest := fmt.Sprintf("sha256:%x", sha256.Sum256(data))
	if d := headers.Get("Docker-Content-Digest"); d != "" && d != digest {
		return nil, "invalid_manifest"
	}
	var index containerIndex
	if json.Unmarshal(data, &index) != nil || index.SchemaVersion != 2 || index.MediaType != imageIndexType || len(index.Manifests) != 2 {
		return nil, "invalid_manifest"
	}
	a := index.Annotations
	version, revision := a["org.opencontainers.image.version"], a["org.opencontainers.image.revision"]
	timestamp, err := time.Parse(time.RFC3339, a["org.opencontainers.image.created"])
	if err != nil || timestamp.IsZero() || !stableVersion.MatchString(version) || !commit.MatchString(revision) || a["org.opencontainers.image.source"] != "https://github.com/"+repository {
		return nil, "invalid_manifest"
	}
	seen := map[string]bool{}
	for _, descriptor := range index.Manifests {
		arch := descriptor.Platform.Architecture
		if descriptor.Platform.OS != "linux" || arch != "amd64" && arch != "arm64" || seen[arch] || descriptor.Size <= 0 || !strings.HasPrefix(descriptor.Digest, "sha256:") || !hash.MatchString(strings.TrimPrefix(descriptor.Digest, "sha256:")) || descriptor.MediaType != imageManifestType && descriptor.MediaType != dockerManifestType {
			return nil, "invalid_manifest"
		}
		seen[arch] = true
		if arch == s.state.Current.Arch {
			body, _, code := s.containerRequest(ctx, prefix+"/manifests/"+descriptor.Digest, auth.Token)
			if code != "" {
				if code == "release_not_found" {
					code = "invalid_manifest"
				}
				return nil, code
			}
			if len(body) != descriptor.Size || fmt.Sprintf("sha256:%x", sha256.Sum256(body)) != descriptor.Digest {
				return nil, "invalid_manifest"
			}
		}
	}
	if s.state.Current.OS != "linux" || !seen[s.state.Current.Arch] {
		return nil, "invalid_manifest"
	}
	return &Release{Version: version, Commit: revision, PublishedAt: timestamp, NotesURL: "https://github.com/" + repository + "/releases/tag/v" + version, Image: image + ":" + tag, Digest: digest}, ""
}
