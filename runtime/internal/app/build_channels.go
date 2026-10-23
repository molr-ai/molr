package app

import (
	"context"
	"molr.ai/runtime/internal/auth"
	"molr.ai/runtime/internal/business/assistant"
	"molr.ai/runtime/internal/config"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/im/dingtalk"
	"molr.ai/runtime/internal/im/feishu"
	"molr.ai/runtime/internal/im/qq"
	"molr.ai/runtime/internal/im/slack"
	"molr.ai/runtime/internal/im/telegram"
	"molr.ai/runtime/internal/im/weixin"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/policy"
	"molr.ai/runtime/internal/storage/files"
)

// Construction remains ordered; callbacks run only after all channels are wired.
func (b *Bundle) buildChannels(cfg config.Config, key []byte, permissions policy.Authorizer, media files.ChannelStore, router *chatChannels) error {
	access := channelAccess{store: b.store, permissions: permissions}
	var err error
	b.weixin, err = weixin.New(b.store, weixin.Options{
		MasterKey: key, PublicURL: cfg.HTTP.PublicURL,
		Live: access.live, LiveSignals: channelLiveSignals,
		CheckBinding: router.bindingCheck("qq", "feishu", "dingtalk", "telegram", "slack"),
		Authorize: func(ctx context.Context, p core.Principal, id core.ID, write bool) (weixin.Target, error) {
			target, err := access.target(ctx, p, id, write)
			return weixin.Target(target), err
		},
		Files: media, MaxMediaBytes: cfg.Execution.MaxUploadBytes,
	})
	if err != nil {
		return err
	}
	client, err := telegram.NewHTTPClient(cfg.Telegram.ProxyURL)
	if err != nil {
		return err
	}
	b.telegram, err = telegram.New(b.store, telegram.Options{
		MasterKey: key, PublicURL: cfg.HTTP.PublicURL,
		Live: access.live, LiveSignals: channelLiveSignals,
		CheckBinding: router.bindingCheck("qq", "feishu", "dingtalk", "weixin", "slack"),
		Authorize: func(ctx context.Context, p core.Principal, id core.ID, write bool) (telegram.Target, error) {
			target, err := access.target(ctx, p, id, write)
			return telegram.Target(target), err
		},
		Files: media, MaxMediaBytes: cfg.Execution.MaxUploadBytes,
		Client: client,
	})
	if err != nil {
		return err
	}
	b.slack, err = slack.New(b.store, slack.Options{
		MasterKey: key, PublicURL: cfg.HTTP.PublicURL,
		Live: access.live, LiveSignals: channelLiveSignals,
		CheckConnect: router.bindingCheck("qq", "feishu", "dingtalk", "weixin", "telegram"),
		Authorize: func(ctx context.Context, p core.Principal, id core.ID, write bool) (slack.Target, error) {
			target, err := access.target(ctx, p, id, write)
			return slack.Target(target), err
		},
		Files: media, MaxMediaBytes: cfg.Execution.MaxUploadBytes,
	})
	if err != nil {
		return err
	}
	b.dingtalk, err = dingtalk.New(b.store, dingtalk.Options{
		MasterKey: key, PublicURL: cfg.HTTP.PublicURL,
		Live: access.live, LiveSignals: channelLiveSignals,
		CheckAvailable: router.bindingCheck("qq", "feishu", "weixin", "telegram", "slack"),
		Authorize: func(ctx context.Context, p core.Principal, id core.ID, write bool) (dingtalk.Target, error) {
			target, err := access.target(ctx, p, id, write)
			return dingtalk.Target(target), err
		},
		Files: media, MaxMediaBytes: cfg.Execution.MaxUploadBytes,
	})
	if err != nil {
		return err
	}
	b.feishu, err = feishu.New(b.store, feishu.Options{
		MasterKey: key, PublicURL: cfg.HTTP.PublicURL,
		Live: access.live, LiveSignals: channelLiveSignals,
		CheckBinding: router.bindingCheck("qq", "weixin", "telegram", "slack", "dingtalk"),
		Authorize: func(ctx context.Context, p core.Principal, id core.ID, write bool) (feishu.Target, error) {
			target, err := access.target(ctx, p, id, write)
			return feishu.Target(target), err
		},
	})
	if err != nil {
		return err
	}
	b.qq, err = qq.New(b.store, qq.Options{
		MasterKey: key, PublicURL: cfg.HTTP.PublicURL,
		Live: access.live, LiveSignals: channelLiveSignals,
		CheckBinding: router.bindingCheck("weixin", "telegram", "slack", "feishu", "dingtalk"),
		Authorize: func(ctx context.Context, p core.Principal, id core.ID, write bool) (qq.Target, error) {
			target, err := access.target(ctx, p, id, write)
			return qq.Target(target), err
		},
	})
	if err != nil {
		return err
	}

	router.items = []chatChannel{b.weixin, b.telegram, b.slack, b.dingtalk, b.feishu, b.qq}
	// Reservations in Slack, DingTalk and QQ block binding even before confirmation.
	// Keep each caller's existing check order so the first error remains unchanged.
	router.bindingChecks = map[string]func(context.Context, core.ID, core.ID) error{
		"weixin": b.weixin.CheckWebInput, "telegram": b.telegram.CheckWebInput,
		"slack": b.slack.CheckAvailable, "dingtalk": b.dingtalk.CheckAvailable,
		"feishu": b.feishu.CheckWebInput, "qq": b.qq.CheckAvailable,
	}
	return nil
}

type channelTarget struct {
	ConversationID core.ID
	Active         bool
}
type channelAccess struct {
	store       persistence.Store
	permissions policy.Authorizer
}

func (a channelAccess) target(ctx context.Context, p core.Principal, id core.ID, write bool) (channelTarget, error) {
	if err := p.RequireOwner(); err != nil {
		return channelTarget{}, err
	}
	action := policy.Read
	if write {
		action = policy.Write
	}
	if err := a.permissions.Require(ctx, p, policy.CheckRequest{Target: core.Object{Kind: "assistant", ID: id}, Action: action}); err != nil {
		return channelTarget{}, err
	}
	assistantValue, err := assistant.NewRepository(a.store).Get(ctx, p.OwnerID, id)
	return channelTarget{ConversationID: assistantValue.ConversationID, Active: assistantValue.State == assistant.Active}, err
}
func (a channelAccess) live(ctx context.Context, owner, id core.ID) error {
	account, err := auth.NewRepository(a.store).AccountByID(ctx, owner)
	if err != nil {
		return err
	}
	if account.Account.Disabled {
		return core.NewError(core.Forbidden, "account disabled")
	}
	assistantValue, err := assistant.NewRepository(a.store).Get(ctx, owner, id)
	if err != nil {
		return err
	}
	if assistantValue.State != assistant.Active {
		return core.NewError(core.Forbidden, "assistant inactive")
	}
	return nil
}
func channelLiveSignals(owner, id core.ID) []persistence.SignalKey {
	return []persistence.SignalKey{auth.AccountStateSignal(owner), assistant.StateSignal(owner, id)}
}
