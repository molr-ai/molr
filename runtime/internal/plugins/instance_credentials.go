package plugins

import (
	"context"
	"slices"
	"strings"

	"molr.ai/runtime/internal/auth"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/textfiles"
)

type InstanceCredentialRequest struct {
	InstanceRequest
	Field        string
	Value        core.Secret
	Verification auth.Verification
}

// CLI credentials are intentionally scoped workspace files, like CLI-generated
// login state. The configuration API never returns them or injects them into
// model calls; the assistant can still read its own workspace credential files.
func (s *Service) SaveInstanceCredential(ctx context.Context, p core.Principal, q InstanceCredentialRequest) error {
	if err := s.authorize(ctx, p, false); err != nil {
		return err
	}
	if s.options.AuthorizeSettings == nil {
		return core.NewError(core.Forbidden, "settings write unavailable")
	}
	if err := s.options.AuthorizeSettings(ctx, p); err != nil {
		return err
	}

	if q.Value.Empty() || len(q.Value.Reveal()) > 8192 || strings.ContainsRune(q.Value.Reveal(), 0) {
		return invalid("bounded credential required")
	}
	if s.options.CheckVerification == nil || s.options.WorkspaceRoot == nil {
		return core.NewError(core.Unavailable, "credential configuration unavailable")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	v, err := s.checkInstance(ctx, p, q.ID, q.AssistantID, true)
	if err != nil {
		return err
	}
	instance, err := s.instance(ctx, p.OwnerID, q.AssistantID, q.ID)
	if err != nil {
		return err
	}
	if !instance.Enabled || instance.Version != q.ExpectedVersion {
		return core.NewError(core.Conflict, "plugin instance changed")
	}
	v, _, err = s.release(ctx, v, instance.Release)
	if err != nil {
		return err
	}
	if !slices.ContainsFunc(v.Manifest.Credentials, func(f SecretField) bool { return f.ID == q.Field }) {
		return invalid("undeclared credential")
	}
	if err = s.options.CheckVerification(ctx, p, q.Verification, "credential.put"); err != nil {
		return err
	}
	root, err := s.options.WorkspaceRoot(ctx, p, q.AssistantID, true)
	if err != nil {
		return err
	}
	if s.options.CheckWorkspaceFile != nil {
		if err := s.options.CheckWorkspaceFile(ctx, p, q.AssistantID, int64(len(q.Value.Reveal()))); err != nil {
			return err
		}
	}
	parent := ".local/plugins/" + q.ID + "/config/secrets"
	if err = textfiles.MkdirAll(root, parent); err != nil {
		return err
	}
	_, err = textfiles.ReplacePrivate(ctx, root, parent+"/"+q.Field, q.Value.Reveal(), true, "", func() error { _, err := s.checkInstance(ctx, p, q.ID, q.AssistantID, true); return err })
	if err != nil {
		return err
	}
	return s.audit(ctx, p, "instance.credential.save", q.ID)
}

// Cleanup is explicit and independent of catalog removal. It is available
// even after revocation so users can remove their own retained login files.
func (s *Service) CleanInstance(ctx context.Context, p core.Principal, q InstanceRequest) error {
	if err := s.authorize(ctx, p, false); err != nil {
		return err
	}
	if s.options.AuthorizeSettings == nil {
		return core.NewError(core.Forbidden, "settings write unavailable")
	}
	if err := s.options.AuthorizeSettings(ctx, p); err != nil {
		return err
	}

	if !pluginID.MatchString(q.ID) || s.options.CheckAssistant == nil || s.options.WorkspaceRoot == nil {
		return invalid("invalid cleanup request")
	}
	if err := s.options.CheckAssistant(ctx, p, q.AssistantID); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	v, err := s.instance(ctx, p.OwnerID, q.AssistantID, q.ID)
	if err != nil {
		return err
	}
	if v.Version == 0 || v.Version != q.ExpectedVersion || v.Enabled {
		return core.NewError(core.Conflict, "disable instance and reload before cleaning")
	}
	root, err := s.options.WorkspaceRoot(ctx, p, q.AssistantID, true)
	if err != nil {
		return err
	}
	if err = textfiles.RemoveTree(ctx, root, ".local/plugins/"+q.ID, func() error { return s.options.CheckAssistant(ctx, p, q.AssistantID) }); err != nil && core.CodeOf(err) != core.NotFound {
		return err
	}
	v.Release, v.Digest = "", ""
	v.Version++
	return s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		if err := tx.Put(ctx, "plugins.instances", instanceKey(p.OwnerID, q.AssistantID, q.ID), v); err != nil {
			return err
		}
		return s.audit(ctx, p, "instance.clean", q.ID)
	})
}
