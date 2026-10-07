package files

import (
	"context"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
)

// ChannelStore is a trusted composition port, never exposed to HTTP or tools.
// It rechecks the live binding; incoming files are scoped to that binding.
type ChannelStore interface {
	UploadChannel(context.Context, core.ID, core.ID, core.ID, UploadRequest) (File, error)
	ValidateChannelFile(context.Context, core.ID, core.ID, core.ID, core.ID) error
	OpenChannel(context.Context, core.ID, core.ID, core.ID, core.ID) (Download, error)
}

func (s *fileService) channel(ctx context.Context, owner, chat, binding core.ID) error {
	if owner.Validate() != nil || chat.Validate() != nil || binding.Validate() != nil || s.options.AuthorizeChannel == nil {
		return core.NewError(core.Forbidden, "channel file access unavailable")
	}
	return s.options.AuthorizeChannel(ctx, owner, chat, binding)
}
func channelScope(chat, binding core.ID) string {
	return "channel/" + string(chat) + "/" + string(binding) + "/"
}
func (s *fileService) UploadChannel(ctx context.Context, owner, chat, binding core.ID, r UploadRequest) (File, error) {
	if r.TaskID != "" {
		return File{}, core.Invalid("task", "channel uploads cannot target tasks")
	}
	// Only the owner storage partition is passed to the common writer. No session
	// or execution is synthesized, and ordinary authorization is never bypassed by callers.
	return s.upload(ctx, core.Principal{OwnerID: owner}, r, channelScope(chat, binding), func(ctx context.Context) error { return s.channel(ctx, owner, chat, binding) })
}
func (s *fileService) ValidateChannelFile(ctx context.Context, owner, chat, binding, id core.ID) error {
	if err := s.channel(ctx, owner, chat, binding); err != nil {
		return err
	}
	scope, err := persistence.Load[string](ctx, s.store, "files.channel", persistence.Key(owner, id))
	if err != nil {
		return err
	}
	if scope != channelScope(chat, binding) {
		return core.NewError(core.Forbidden, "file belongs to another channel")
	}
	f, err := s.repo.Get(ctx, owner, id)
	if err != nil {
		return err
	}
	if f.State != Ready {
		return core.NewError(core.NotFound, "file not published")
	}
	return nil
}

// OpenChannel is used only for attachment IDs already authorized by Publish and
// captured in the channel outbox; it does not accept an external file selector.
func (s *fileService) OpenChannel(ctx context.Context, owner, chat, binding, id core.ID) (Download, error) {
	if err := s.channel(ctx, owner, chat, binding); err != nil {
		return Download{}, err
	}
	f, err := s.repo.Get(ctx, owner, id)
	if err != nil {
		return Download{}, err
	}
	if f.State != Ready {
		return Download{}, core.NewError(core.NotFound, "file not published")
	}
	body, err := s.objects.Open(ctx, id)
	return Download{File: f, Body: body}, err
}
