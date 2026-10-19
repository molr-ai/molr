import weixin from '@molr/ui/brand/chat-channels/weixin.svg';
import qq from '@molr/ui/brand/chat-channels/qq.svg';
import feishu from '@molr/ui/brand/chat-channels/feishu.svg';
import dingtalk from '@molr/ui/brand/chat-channels/dingtalk.svg';
import telegram from '@molr/ui/brand/chat-channels/telegram.svg';
import slack from '@molr/ui/brand/chat-channels/slack.svg';

// Standalone app marks retain their brand colors on a transparent background.
const icons = { weixin, qq, feishu, dingtalk, telegram, slack };
export function ChannelAppIcon({ channel }: { channel: keyof typeof icons }) {
 return <img className="channel-app-icon" src={icons[channel]} width={32} height={32} alt="" aria-hidden="true" />;
}
