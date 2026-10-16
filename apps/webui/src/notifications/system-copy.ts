import { feishuCopy } from '../feishu-copy';
import type { RuntimeInfo, Settings, SystemNotice } from '@molr/sdk';
import { telegramCopy } from '../telegram-copy';
import { slackCopy } from '../slack-copy';
import { dingtalkCopy } from '../dingtalk-copy';
import { weixinCopy } from '../weixin-copy';
import { useCopy, useQuery } from '../state';

export const systemCopy = {
 zh: { queue: '助理提醒', open: '查看详情', unknown: '有一条需要关注的通知', failed: '后台执行失败', permission: '有新的授权请求', ready: '计划已可交接', connection: '微信需要重新连接', reconnecting: '微信连接中断', reconnectingHelp: '正在自动重连，微信消息接收暂时不可用。恢复后此提醒会自动解除。', remaining: (n:number) => `还有 ${n} 条提醒` },
 en: { queue: 'Assistant notices', open: 'View details', unknown: 'A notification needs your attention', failed: 'Background execution failed', permission: 'New permission request', ready: 'Plan ready to hand off', connection: 'Reconnect Weixin', reconnecting: 'Weixin connection interrupted', reconnectingHelp: 'Reconnecting automatically. Incoming Weixin messages are temporarily unavailable. This notice clears when the connection recovers.', remaining: (n:number) => `${n} more ${n === 1 ? 'notice' : 'notices'}` },
};
export function useNoticeText() {
 const { t, locale } = useCopy(), copy = systemCopy[locale], weixin = weixinCopy[locale];
 const telegram = telegramCopy[locale];
 const runtime = useQuery<RuntimeInfo>('/api/status'), settings = useQuery<Settings>('/api/settings');
 const workbenchVisible = !!runtime.data?.workbench_available && !!settings.data?.WorkbenchVisible;
 return { copy, workbenchVisible, action: (n: SystemNotice) => {
  const route = noticeRoute(n, workbenchVisible);
  return route ? { route, label: n.Kind === 'execution.failed' ? t.turnMetrics.viewWorkbench : copy.open } : undefined;
 }, text: (n:SystemNotice) => {
  if(n.Kind==='telegram.delivery') { const [state,error]=n.Code.split(':'); return { title:telegram.delivery, description: state==='failed'&&error==='rate_limited' ? telegram.retryExhausted : telegram.errors[error as keyof typeof telegram.errors] ?? telegram.deliveries[state as keyof typeof telegram.deliveries] ?? state, help:state==='unknown'?telegram.unknownHelp:'' }; }
  if(n.Kind==='telegram.connection') return { title:telegram.connection, description:telegram.states[n.Code as keyof typeof telegram.states] ?? n.Code };
  if(n.Kind==='slack.connection') { const c=slackCopy[locale]; return { title: c.name, description: n.Code==='reconnecting'?c.reconnecting:c.login_required }; }
  if(n.Kind==='slack.delivery') { const c=slackCopy[locale], [state,error]=n.Code.split(':'); return {title:c.delivery,description:state==='unknown'?c.unknown:state==='failed'&&error==='rate_limited'?c.exhausted:({rate_limited:c.rate_limited,attachment_failed:c.attachment_failed,missing_scope:c.missing_scope,login_required:c.login_required,rejected:c.rejected} as Record<string,string>)[error]??c.failed}; }
  if(n.Kind==='dingtalk.delivery') { const ding = dingtalkCopy[locale], [,error]=n.Code.split(':'); return {title:ding.delivery,description:ding.errors[error as keyof typeof ding.errors] ?? ding.unknown}; }
  if(n.Kind==='dingtalk.connection') { const ding = dingtalkCopy[locale]; return {title:ding.name,description:ding.states[n.Code as keyof typeof ding.states] ?? ding.states.reconnecting}; }
  if(n.Kind==='feishu.connection') return {title:feishuCopy[locale].name,description:feishuCopy[locale].states[n.Code as keyof typeof feishuCopy.en.states] ?? feishuCopy[locale].states.reconnecting};
  if(n.Kind==='feishu.delivery') return {title:feishuCopy[locale].delivery,description:n.Code.startsWith('pending:')?feishuCopy[locale].pending:n.Code.startsWith('unknown:')?feishuCopy[locale].unknown:feishuCopy[locale].failed};
  if(n.Kind==='weixin.delivery') { const [state,error]=n.Code.split(':');return { title:weixin.delivery, description:state==='failed'&&error==='rate_limited'?weixin.retryExhausted:weixin.deliveryErrors[error as keyof typeof weixin.deliveryErrors]??weixin.deliveries[state as keyof typeof weixin.deliveries]??state, help:state==='unknown'?weixin.deliveryHelp:'' }; }
  if(n.Kind==='weixin.connection')return n.Code==='reconnecting' ? {title:copy.reconnecting,description:copy.reconnectingHelp} : {title:copy.connection,description:weixin.states.login_required};
  if(n.Kind==='assistant.waiting')return {title:t.waiting.title,description:(t.waiting.reasons as Record<string,string>)[n.Code]??t.waiting.unknown};
  if(n.Kind==='execution.failed')return {title:copy.failed,description:n.Code.startsWith('needs_review')?(locale==='zh'?'结果需要核对，请先检查执行情况，避免重复操作。':'The outcome needs review. Check the execution before repeating it.'):(t.waiting.reasons as Record<string,string>)[n.Code.split(':')[1]]??t.waiting.unknown};
  return {title:({'permission.request':copy.permission,'plan.ready':copy.ready,'task.failed':copy.failed,'execution.failed':copy.failed} as Record<string,string>)[n.Kind]??copy.unknown,description:n.Label||''};
 } };
}
export function noticeRoute(n:SystemNotice, workbenchVisible = false) {
 if(n.Kind==='plan.ready')return `#/plans/${n.Source.ID}`;
 // Source.ID is the triggering event ID, not an ExecutionID. The workbench
 // resolves it against retained runs before loading the corresponding steps.
 if(n.Kind==='execution.failed')return workbenchVisible && n.AssistantID && n.Source.ID ? `#/workbench/${n.AssistantID}?event=${n.Source.ID}` : '';
 if(n.Kind==='task.failed'&&n.AssistantID)return `#/assistants/${n.AssistantID}?panel=tasks&task=${n.Source.ID}`;
 if(n.AssistantID){const panel=(n.Kind.startsWith('feishu.') || n.Kind.startsWith('dingtalk.') || n.Kind.startsWith('weixin.') || n.Kind.startsWith('telegram.')||n.Kind.startsWith('slack.'))?'channel':n.Kind==='permission.request'?'permissions':n.Kind==='assistant.waiting'?'info':'tasks';return `#/assistants/${n.AssistantID}?panel=${panel}`;}
 return '#/assistants';
}
