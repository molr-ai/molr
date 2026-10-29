import type { ChatChannelSummaries } from '@molr/sdk';

export function channelSummaries(channel: keyof ChatChannelSummaries, status: { Channel: string; State: string }): ChatChannelSummaries {
 return Object.fromEntries(['weixin', 'qq', 'feishu', 'dingtalk', 'telegram', 'slack'].map(name => [name, name === channel ? status : { Channel: 'webui', State: 'disconnected' }])) as ChatChannelSummaries;
}
