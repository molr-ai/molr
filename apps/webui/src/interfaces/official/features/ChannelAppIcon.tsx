import { Bell, BriefcaseBusiness, Hash, MessageCircle, MessagesSquare, Radio } from 'lucide-react';

// Generic communication symbols; channel names provide service identification.
const icons = { weixin: MessageCircle, qq: MessagesSquare, feishu: BriefcaseBusiness, dingtalk: Bell, telegram: Radio, slack: Hash };
export function ChannelAppIcon({ channel }: { channel: keyof typeof icons }) {
 const Icon = icons[channel];
 return <Icon className="channel-app-icon" size={32} strokeWidth={1.7} aria-hidden="true" />;
}
