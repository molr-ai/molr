import type { ChatChannelStatus } from '@molr/sdk';
import { useQuery } from '../../../state';
import { weixinCopy } from '../../../weixin-copy';
import { telegramCopy } from '../../../telegram-copy';
import { slackCopy } from '../../../slack-copy';
import { dingtalkCopy } from '../../../dingtalk-copy';
import { feishuCopy } from '../../../feishu-copy';
import { qqCopy } from '../../../qq-copy';

export function useChatChannel(assistantID: string, locale: 'zh' | 'en') {
  const query = useQuery<ChatChannelStatus>(`/api/assistants/${assistantID}/chat-channel`);
  const current = query.data?.Channel ?? 'webui';
  const ready = !!query.data;
  const external = current !== 'webui';
  const copies = {
    weixin: weixinCopy[locale],
    telegram: { ...weixinCopy[locale], ...telegramCopy[locale] },
    slack: slackCopy[locale],
    dingtalk: { ...weixinCopy[locale], ...dingtalkCopy[locale] },
    feishu: { ...weixinCopy[locale], ...feishuCopy[locale] },
    qq: { ...weixinCopy[locale], ...qqCopy[locale] },
  };
  return {
    ready, external, readOnly: !ready || external,
    copy: copies[current === 'webui' ? 'weixin' : current],
    error: query.error, reload: query.reload,
  };
}
