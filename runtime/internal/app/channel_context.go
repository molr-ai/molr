package app

import (
	"context"
	"molr.ai/runtime/internal/agent"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
)

// Channel context shares one live execution check and assistant record per call.
// Nothing is retained across context refreshes or model invocation boundaries.
func (b *Bundle) channelContext(ctx context.Context, store persistence.Store, execution core.Execution) ([]agent.ContextDocument, error) {
	a, err := (access{store}).executionAssistant(ctx, execution)
	if err != nil {
		return nil, err
	}
	channelDoc, err := weixinContext(ctx, b.weixin, a)
	if err == nil && channelDoc == nil {
		channelDoc, err = qqContext(ctx, b.qq, a)
	}
	if err == nil && channelDoc == nil {
		channelDoc, err = feishuContext(ctx, b.feishu, a)
	}
	if err == nil && channelDoc == nil {
		channelDoc, err = telegramContext(ctx, b.telegram, a)
	}
	if err == nil && channelDoc == nil {
		channelDoc, err = dingtalkContext(ctx, b.dingtalk, a)
	}
	if err != nil {
		return nil, err
	}
	var docs []agent.ContextDocument
	if channelDoc != nil {
		docs = append(docs, *channelDoc)
	}
	slackDoc, err := slackContext(ctx, b.slack, a)
	if err != nil {
		return nil, err
	}
	if slackDoc != nil {
		docs = append(docs, *slackDoc)
	}
	return docs, nil
}
