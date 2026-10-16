import { useErrorToast } from '../error-toasts';
import { useEffect, useRef, useState } from 'react';
import { Alert, Button } from '@molr/ui';
import type { AgentStatus, Assistant, Model, SystemNotice } from '@molr/sdk';
import { api } from '../api/client';
import { useCopy, useData, useQuery } from '../state';
import { useAction } from '../interfaces/official/components/common';
import { AssistantWaitingNotice } from '../interfaces/official/features/AssistantWaitingNotice';
import { useNoticeText } from './system-copy';
import './assistant-notifications.css';

export function AssistantNotifications({ assistant, status, onAction, onNavigate }: {
 assistant: Assistant; status?: AgentStatus;
 onAction: (kind: 'info' | 'permissions' | 'channel') => void;
 onNavigate: (kind: string, id: string) => void;
}) {
 const { copy, text, action: noticeAction } = useNoticeText(), { t } = useCopy(), { invalidateTopics } = useData(), action = useAction();
 const models = useQuery<Model[]>('/api/models');
 const query = useQuery<SystemNotice[]>(`/api/notifications?filter=active&assistant_id=${assistant.ID}&limit=100`, true);
 const [currentID, setCurrentID] = useState('');
 const region = useRef<HTMLElement>(null), focusNext = useRef(false);
 // Keep the visible notice stable when a newer one arrives. The oldest pending
 // notice is next; closing acknowledges exactly one occurrence, never a group.
 const rows = query.data ?? [];
 const current = rows.find(n => n.ID === currentID) ?? rows.at(-1);
 useEffect(() => { setCurrentID(current?.ID ?? ''); }, [current?.ID]);
 useEffect(() => {
  if (focusNext.current) {
   region.current?.querySelector<HTMLButtonElement>('.assistant-notice > .ui-icon-button')?.focus();
   focusNext.current = false;
  }
 }, [current?.ID]);
 useErrorToast(action.error || query.error, query.reload);
 if (!current) return <>{status?.ModelRetry && <AssistantWaitingNotice assistant={assistant} status={status} />}</>;
 const n = current, content = text(n);
 const target = noticeAction(n);
 let label = target?.label ?? copy.open, open = () => { if (target) location.hash = target.route; };
 if (n.Kind.startsWith('weixin.') || n.Kind.startsWith('telegram.')) open = () => onAction('channel');
 else if (n.Kind === 'permission.request' || n.Code === 'model_not_authorized') { label = t.waiting.permissions; open = () => onAction('permissions'); }
 else if (n.Kind === 'assistant.waiting') {
  open = () => onAction('info');
  if (n.Code === 'budget_exceeded' || n.Code === 'assistant_budget_missing') label = t.waiting.budget;
  else if (['model_pricing_missing', 'model_authentication_failed', 'model_service_unavailable'].includes(n.Code)) {
   label = n.Code === 'model_pricing_missing' ? t.waiting.pricing : t.waiting.provider;
   open = () => onNavigate('resource', models.data?.find(m => m.ID === assistant.ModelID)?.ResourceID ?? '');
  } else if (['model_not_configured', 'model_unavailable'].includes(n.Code)) label = t.waiting.selectModel;
 }
 async function update(kind: 'read' | 'dismiss') {
  await action.run(async () => {
   if (kind === 'dismiss') focusNext.current = true;
   try { await api.post('/api/notifications/update', { IDs: [n.ID], Action: kind }); }
   catch (error) { focusNext.current = false; throw error; }
   invalidateTopics(['notifications']);
  }, undefined, false);
 }
 return <section ref={region} className="assistant-notifications" aria-label={copy.queue} data-notification-id={n.ID}>
  <Alert key={n.ID} className="assistant-notice" title={content.title} tone={n.Severity} onDismiss={() => { if (!action.busy) void update('dismiss'); }} action={(target || rows.length > 1) && <div className="form-actions">{target && <Button size="sm" variant="secondary" onClick={() => { void update('read'); open(); }}>{label}</Button>}{rows.length > 1 && <small>{copy.remaining(rows.length - 1)}</small>}</div>}>
   {content.description && <p>{content.description}</p>}{content.help && <p className="assistant-notice__help">{content.help}</p>}
  </Alert>
 </section>;
}
