package app

import (
	"context"
	"molr.ai/runtime/internal/auth"
	"molr.ai/runtime/internal/config"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/im/conversation"
	"molr.ai/runtime/internal/im/dingtalk"
	"molr.ai/runtime/internal/im/feishu"
	"molr.ai/runtime/internal/im/qq"
	"molr.ai/runtime/internal/im/realtime"
	"molr.ai/runtime/internal/im/slack"
	"molr.ai/runtime/internal/im/telegram"
	"molr.ai/runtime/internal/im/weixin"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/policy"
	"molr.ai/runtime/internal/storage/files"
	"path/filepath"
)

type chatServices struct {
	files  files.Service
	chat   conversation.Service
	router *chatChannels
}

func (b *Bundle) buildConversations(ctx context.Context, cfg config.Config, key []byte, permissions policy.Authorizer) (chatServices, error) {
	store := b.store
	var err error
	b.objects, err = files.NewObjects(filepath.Join(cfg.DataDir, "files"), store)
	if err != nil {
		return chatServices{}, err
	}
	if err = b.objects.Recover(ctx); err != nil {
		return chatServices{}, err
	}
	channelRouter := &chatChannels{}
	fileService := files.NewService(store, b.objects, files.Options{Authorizer: permissions, AuthorizeChannel: func(ctx context.Context, owner, chat, binding core.ID) error {
		if len(channelRouter.items) == 0 {
			return core.NewError(core.Forbidden, "channel unavailable")
		}
		return channelRouter.AuthorizeChannel(ctx, owner, chat, binding)
	}, MaxUploadBytes: cfg.Execution.MaxUploadBytes, DiskQuota: func() int64 { return cfg.Execution.MaxDiskBytes }})
	visible := realtime.NewStore(store)
	if err = b.buildChannels(cfg, key, permissions, fileService.(files.ChannelStore), channelRouter); err != nil {
		return chatServices{}, err
	}

	chat := conversation.NewService(store, conversation.Options{ClientOrigin: func(ctx context.Context, p core.Principal) (conversation.Origin, error) {
		if p.SessionID == "" {
			return conversation.Origin{ClientKind: "api"}, nil
		}
		session, err := persistence.Load[auth.StoredSession](ctx, store, "auth.sessions", persistence.Key(p.OwnerID, p.SessionID))
		kind := session.Session.ClientType
		if kind == "" || kind == "browser" || kind == "web" {
			kind = "webui"
		}
		return conversation.Origin{ClientKind: kind}, err
	}, CheckDirect: func(ctx context.Context, p core.Principal, target conversation.Endpoint) error {
		return checkDirectMessage(ctx, store, p, target)
	}, CheckAssistantInput: func(ctx context.Context, p core.Principal, m conversation.Message) error {
		return checkAssistantInput(ctx, store, p, m)
	}, Authorizer: permissions, Visible: visible, CheckWebInput: channelRouter.CheckWebInput, AuthorizeChannel: channelRouter.AuthorizeChannel, ValidateChannelFile: fileService.(files.ChannelStore).ValidateChannelFile, Published: channelRouter.Published})
	b.weixin.SetReceiver(chat.(weixin.Receiver))
	b.telegram.SetReceiver(chat.(telegram.Receiver))
	b.slack.SetReceiver(chat.(slack.Receiver))
	b.dingtalk.SetReceiver(chat.(dingtalk.Receiver))
	b.feishu.SetReceiver(chat.(feishu.Receiver))
	b.qq.SetReceiver(chat.(qq.Receiver))
	return chatServices{fileService, chat, channelRouter}, nil
}
