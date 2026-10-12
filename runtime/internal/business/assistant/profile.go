package assistant

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	avatarutil "molr.ai/runtime/internal/avatar"
	"strings"
	"unicode"
	"unicode/utf8"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/im/realtime"
)

// Profile contains no image payload, model settings or other private configuration.
type Profile struct {
	Name    string
	Status  string
	Avatar  ProfileAvatar
	Version core.Version
}
type ProfileAvatar struct {
	Type   string
	ID     string `json:",omitempty"`
	Digest string `json:",omitempty"`
}

func (a Assistant) Profile() Profile {
	v := Profile{Name: a.Name, Status: a.Status, Version: a.Version, Avatar: ProfileAvatar{Type: "initial"}}
	if strings.HasPrefix(a.Avatar, "preset:") {
		v.Avatar = ProfileAvatar{Type: "preset", ID: strings.TrimPrefix(a.Avatar, "preset:")}
	} else if a.Avatar != "" {
		digest := sha256.Sum256([]byte(a.Avatar))
		v.Avatar = ProfileAvatar{Type: "image", Digest: hex.EncodeToString(digest[:])}
	}
	return v
}

type ProfileConflict struct {
	Cause   *core.Error
	Current Profile
}

func (e *ProfileConflict) Error() string { return e.Cause.Error() }
func (e *ProfileConflict) Unwrap() error { return e.Cause }

type SelfProfileRequest struct {
	Mutation core.Mutation
	Name     *string
	Status   *string
	Avatar   *string
}
type ProfileResult struct {
	Changed bool
	Profile Profile
}

func NormalizeName(value string) (string, error) {
	if !utf8.ValidString(value) || strings.ContainsFunc(value, unicode.IsControl) {
		return "", core.Invalid("name", "name must not contain control characters")
	}
	value = strings.TrimSpace(value)
	if value == "" || utf8.RuneCountInString(value) > 256 {
		return "", core.Invalid("name", "name must contain 1 to 256 characters")
	}
	return value, nil
}

func normalizeStatus(value string) (string, error) {
	if !utf8.ValidString(value) || strings.ContainsFunc(value, unicode.IsControl) {
		return "", core.Invalid("status", "status must not contain control characters")
	}
	value = strings.TrimSpace(value)
	if utf8.RuneCountInString(value) > 256 {
		return "", core.Invalid("status", "status must contain at most 256 characters")
	}
	return value, nil
}

// UpdateSelfProfile never accepts a target identity or broader assistant settings.
// The authorizer checks the live execution again inside the mutation transaction.
func (s *service) UpdateSelfProfile(ctx context.Context, e core.Execution, req SelfProfileRequest) (ProfileResult, error) {
	if err := e.Validate(); err != nil {
		return ProfileResult{}, err
	}
	return mutate(ctx, s, e.Principal, "profile", e.AssistantID, req.Mutation, req, true, false, func(ctx context.Context) (ProfileResult, error) {
		v, err := s.repo.Get(ctx, e.Principal.OwnerID, e.AssistantID)
		if err != nil {
			return ProfileResult{}, err
		}
		if v.OwnerID != e.Principal.OwnerID || v.ID != e.AssistantID || v.State != Active {
			return ProfileResult{}, forbidden()
		}
		if v.Version != req.Mutation.ExpectedVersion {
			return ProfileResult{}, &ProfileConflict{Cause: core.NewError(core.Conflict, "profile changed; inspect current_profile before deciding whether to update again"), Current: v.Profile()}
		}
		if req.Name == nil && req.Avatar == nil && req.Status == nil {
			return ProfileResult{}, core.Invalid("profile", "name, status or avatar required")
		}
		before := v
		if req.Name != nil {
			v.Name, err = NormalizeName(*req.Name)
			if err != nil {
				return ProfileResult{}, err
			}
		}
		if req.Status != nil {
			v.Status, err = normalizeStatus(*req.Status)
			if err != nil {
				return ProfileResult{}, err
			}
		}
		if req.Avatar != nil {
			v.Avatar, err = avatarutil.Normalize(*req.Avatar)
			if err != nil {
				return ProfileResult{}, err
			}
		}
		changed := v.Name != before.Name || v.Avatar != before.Avatar || v.Status != before.Status
		if changed {
			v.Version++
			v.UpdatedAt = s.now()
			if err = s.repo.Save(ctx, v, before.Version); err != nil {
				return ProfileResult{}, err
			}
			if err = s.profileChanged(ctx, v); err != nil {
				return ProfileResult{}, err
			}
		}
		return ProfileResult{Changed: changed, Profile: v.Profile()}, nil
	})
}

func (s *service) profileChanged(ctx context.Context, v Assistant) error {
	return realtime.NewStore(s.store).Append(ctx, realtime.Visible{ID: s.id(), OwnerID: v.OwnerID, Topic: "assistant.profile", Object: core.Object{Kind: "assistant", ID: v.ID}, Version: v.Version, CreatedAt: s.now()})
}
