import type { SystemNotice } from '@molr/sdk';
import { weixinCopy } from '../weixin-copy';
import { useCopy } from '../state';

export const systemCopy = {
 zh: { queue: '助理提醒', open: '查看详情', unknown: '有一条需要关注的通知', failed: '后台执行失败', permission: '有新的授权请求', ready: '计划已可交接', connection: '微信需要重新连接', remaining: (n:number) => `还有 ${n} 条提醒` },
 en: { queue: 'Assistant notices', open: 'View details', unknown: 'A notification needs your attention', failed: 'Background execution failed', permission: 'New permission request', ready: 'Plan ready to hand off', connection: 'Reconnect Weixin', remaining: (n:number) => `${n} more ${n === 1 ? 'notice' : 'notices'}` },
};
export function useNoticeText() {
 const { t, locale } = useCopy(), copy = systemCopy[locale], weixin = weixinCopy[locale];
 return { copy, text: (n:SystemNotice) => {
  if(n.Kind==='weixin.delivery') { const [state,error]=n.Code.split(':');return { title:weixin.delivery, description:state==='failed'&&error==='rate_limited'?weixin.retryExhausted:weixin.deliveryErrors[error as keyof typeof weixin.deliveryErrors]??weixin.deliveries[state as keyof typeof weixin.deliveries]??state, help:state==='unknown'?weixin.deliveryHelp:'' }; }
  if(n.Kind==='weixin.connection')return {title:copy.connection,description:weixin.states.login_required};
  if(n.Kind==='assistant.waiting')return {title:t.waiting.title,description:(t.waiting.reasons as Record<string,string>)[n.Code]??t.waiting.unknown};
  if(n.Kind==='execution.failed')return {title:copy.failed,description:n.Code.startsWith('needs_review')?(locale==='zh'?'结果需要核对，请先检查执行情况，避免重复操作。':'The outcome needs review. Check the execution before repeating it.'):(t.waiting.reasons as Record<string,string>)[n.Code.split(':')[1]]??t.waiting.unknown};
  return {title:({'permission.request':copy.permission,'plan.ready':copy.ready,'task.failed':copy.failed,'execution.failed':copy.failed} as Record<string,string>)[n.Kind]??copy.unknown,description:n.Label||''};
 } };
}
export function noticeRoute(n:SystemNotice) {
 if(n.Kind==='plan.ready')return `#/plans/${n.Source.ID}`;
 if(n.Kind==='execution.failed'&&n.AssistantID)return `#/assistants/${n.AssistantID}`;
 if(n.Kind==='task.failed'&&n.AssistantID)return `#/assistants/${n.AssistantID}?panel=tasks&task=${n.Source.ID}`;
 if(n.AssistantID){const panel=n.Kind.startsWith('weixin.')?'channel':n.Kind==='permission.request'?'permissions':n.Kind==='assistant.waiting'?'info':'tasks';return `#/assistants/${n.AssistantID}?panel=${panel}`;}
 return '#/assistants';
}
