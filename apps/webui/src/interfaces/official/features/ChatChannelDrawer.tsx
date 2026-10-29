import { useEffect, useRef, useState } from 'react';
import { Alert, Badge, Drawer, IconButton, useBreakpoint } from '@molr/ui';
import { ChevronLeft, ChevronRight } from 'lucide-react';
import type { Assistant, ChatChannelSummaries, WeixinStatus, QQStatus, DingtalkStatus, FeishuStatus } from '@molr/sdk';
import { useCopy, useData, useQuery } from '../../../state';
import { useErrorToast } from '../../../error-toasts';
import { weixinCopy } from '../../../weixin-copy';
import { Loading } from '../components/common';
import { WeixinChannel } from './WeixinChannel';
import { TelegramChannel } from './TelegramChannel';
import { SlackChannel } from './SlackChannel';
import { DingtalkChannel } from './DingtalkChannel';
import { FeishuChannel } from './FeishuChannel';
import { QQChannel } from './QQChannel';
import { ChannelAppIcon } from './ChannelAppIcon';
import './weixin.css';

type Channel = keyof ChatChannelSummaries;
const channels: Channel[] = ['weixin', 'qq', 'feishu', 'dingtalk', 'telegram', 'slack'];
const names = { zh: { weixin: '微信', qq: 'QQ', telegram: 'Telegram', slack: 'Slack', dingtalk: '钉钉', feishu: '飞书' }, en: { weixin: 'Weixin', qq: 'QQ', telegram: 'Telegram', slack: 'Slack', dingtalk: 'DingTalk', feishu: 'Feishu' } };
export function ChatChannelDrawer({ assistant, onClose }: { assistant: Assistant; onClose: () => void }) {
 const { locale } = useCopy(), t = weixinCopy[locale], mobile = useBreakpoint('mobile'), { invalidateTopics } = useData();
 const summary = useQuery<ChatChannelSummaries>(`/api/assistants/${assistant.ID}/chat-channels`);
 const ready = !!summary.data;
 const bound = channels.find(key => summary.data?.[key].Channel === key);
 // A management read may observe binding changes before a missed SSE event.
 useEffect(() => { if (ready) invalidateTopics(['chat-channel']); }, [ready, bound, invalidateTopics]);
 const reserved = channels.find(key => ['qq', 'slack', 'dingtalk'].includes(key) && summary.data?.[key] && !['disconnected', 'expired'].includes(summary.data[key].State));
 const [selected, setSelected] = useState<Channel | null>(null);
 const lastBound = useRef<Channel | undefined>(undefined), lastSelected = useRef<Channel | null>(null);
 const back = useRef<HTMLButtonElement>(null), triggers = useRef<Partial<Record<Channel, HTMLButtonElement>>>({});
 const current = bound ?? selected;
 useEffect(() => {
  if (bound) setSelected(bound);
  else if (lastBound.current) { const previous = lastBound.current; setSelected(null); requestAnimationFrame(() => triggers.current[previous]?.focus()); }
  lastBound.current = bound;
 }, [bound]);
 useEffect(() => { if (current && !bound) back.current?.focus(); }, [current, bound]);
 useErrorToast(summary.error, summary.reload);
 const onStatusChange = (value: { Channel: string; State: string }, channel: Channel) => {
  const previous = summary.data?.[channel];
  if (previous && (value.Channel !== previous.Channel || value.State !== previous.State)) invalidateTopics(['chat-channels', 'chat-channel']);
 };
 const returnToList = () => { if (selected) { const previous = selected; setSelected(null); requestAnimationFrame(() => triggers.current[previous]?.focus()); } };
 if (current) lastSelected.current = current;
 const shown = current ?? lastSelected.current;
 const disabled = !ready || !!reserved && reserved !== shown;
 return <Drawer open title={t.title} description={assistant.Name} className="chat-channel-drawer" side={mobile ? 'bottom' : 'right'} onOpenChange={open => { if (!open) onClose(); }}
  headerContent={current ? <div className="channel-heading">{!bound && <IconButton ref={back} label={locale === 'zh' ? '返回入口列表' : 'Back to channels'} onClick={returnToList}><ChevronLeft size={18} /></IconButton>}<span>{names[locale][current]}</span>{bound && <Badge tone="positive">{locale === 'zh' ? '已绑定' : 'Bound'}</Badge>}</div> : undefined}>
  {!ready ? <Loading /> : <div className="channel-viewport"><div className="channel-pages" data-detail={!!current}>
   <div className="channel-page channel-list" inert={!!current} aria-hidden={!!current}>
    <p className="muted">{locale === 'zh' ? '选择一个聊天入口，与这位助理继续聊天。已有记录和上下文会保留。' : 'Choose where to chat with this assistant. Your history and context stay with you.'}</p>
    {channels.map(key => <button type="button" key={key} ref={node => { triggers.current[key] = node!; }} className="channel-choice" onClick={() => setSelected(key)}><ChannelAppIcon channel={key} /><span>{names[locale][key]}</span>{reserved === key && <Badge>{locale === 'zh' ? '待配对' : 'Pairing'}</Badge>}<ChevronRight size={18} aria-hidden="true" /></button>)}
   </div>
   <div className="channel-page channel-detail" inert={!current} aria-hidden={!current}>
    {current && <div key={shown}>
     {disabled && <Alert title={locale === 'zh' ? '请先取消其他入口正在进行的配对。' : 'Cancel the pending pairing in the other channel first.'} />}
     <ChannelDetails assistant={assistant} channel={shown!} disabled={disabled} onClose={returnToList} onStatusChange={onStatusChange} />
    </div>}
   </div>
  </div></div>}
 </Drawer>;
}

function ChannelDetails({ assistant, channel, disabled, onClose, onStatusChange }: {
 assistant: Assistant; channel: Channel; disabled: boolean; onClose: () => void;
 onStatusChange: (value: { Channel: string; State: string }, channel: Channel) => void;
}) {
 const path = (name: Channel) => channel === name ? `/api/assistants/${assistant.ID}/${name}` : null;
 const weixin = useQuery<WeixinStatus>(path('weixin'));
 const qq = useQuery<QQStatus>(path('qq'));
 const dingtalk = useQuery<DingtalkStatus>(path('dingtalk'));
 const feishu = useQuery<FeishuStatus>(path('feishu'));
 const query = { weixin, qq, dingtalk, feishu };
 const status = channel in query ? query[channel as keyof typeof query] : undefined;
 useErrorToast(status?.error, status?.reload);
 useEffect(() => { if (status?.data) onStatusChange(status.data, channel); }, [status?.data?.Channel, status?.data?.State]);
 if (channel === 'telegram') return <TelegramChannel assistant={assistant} onClose={onClose} disabled={disabled} onStatusChange={value => onStatusChange(value, channel)} />;
 if (channel === 'slack') return <SlackChannel assistant={assistant} disabled={disabled} onStatusChange={value => onStatusChange(value, channel)} />;
 if (!status?.data) return <Loading />;
 if (channel === 'weixin') return <WeixinChannel assistant={assistant} status={weixin.data!} disabled={disabled} />;
 if (channel === 'qq') return <QQChannel assistant={assistant} status={qq.data!} disabled={disabled} />;
 if (channel === 'dingtalk') return <DingtalkChannel assistant={assistant} status={dingtalk.data} reload={dingtalk.reload} disabled={disabled} />;
 return <FeishuChannel assistant={assistant} status={feishu.data} disabled={disabled} />;
}
