import { useErrorToast, useErrorToasts } from '../../../error-toasts';
import { useEffect, useRef, useState } from 'react';
import type { FormEvent, ReactNode } from 'react';
import { Button, Dialog, EmptyState, Skeleton, useToast } from '@molr/ui';
import { APIError } from '@molr/sdk';
import { useCopy } from '../../../state';

export function Loading({ delay = 300 }: { delay?: number }) {
  const { t } = useCopy(), [visible, setVisible] = useState(false);
  useEffect(() => { const timer = setTimeout(() => setVisible(true), delay); return () => clearTimeout(timer); }, [delay]);
  return visible ? <div role="status" aria-label={t.loading} className="loading"><Skeleton height={24} /><Skeleton height={80} /><Skeleton height={80} /></div> : null;
}
export function Empty({ title, description, action }: { title?: string; description?: string; action?: ReactNode }) { const { t } = useCopy(); return <EmptyState title={title ?? t.empty} description={description ?? t.emptyHelp} action={action} />; }
export class ActionCancelled extends Error {}
export function useAction() {
  const notices = useErrorToasts();
  const running = useRef(false);
  const [busy, setBusy] = useState(false), [error, setError] = useState<unknown>(null);
  const { toast } = useToast(); const { t } = useCopy();
  async function run(action: () => Promise<unknown>, done?: () => void, notify = true) {
    if (running.current) return;
    running.current = true;
    notices?.rearm();
    setBusy(true); setError(null);
    try { await action(); done?.(); if (notify) toast({ title: t.saved }); }
    catch (error) { if (!(error instanceof ActionCancelled)) setError(error); }
    finally { running.current = false; setBusy(false); }
  }
  return { busy, error, run, reset: () => setError(null) };
}
type EditFormProps = { id?: string; externalActions?: boolean; children: ReactNode; onClose: () => void; onSubmit: () => Promise<unknown>; submitLabel?: string; submitDisabled?: boolean; notifyOnSave?: boolean; action: ReturnType<typeof useAction> };
export function EditForm({ id, externalActions = false, children, onClose, onSubmit, submitLabel, submitDisabled, notifyOnSave = true, action }: EditFormProps) {
  const { t } = useCopy();
  useErrorToast(action.error);
  return <form id={id} className="editor" onSubmit={(event: FormEvent) => { event.preventDefault(); if (!submitDisabled) void action.run(onSubmit, onClose, notifyOnSave); }}>
    {children}
    {!externalActions && <div className="form-actions"><Button variant="secondary" onClick={onClose} disabled={action.busy}>{t.cancel}</Button><Button type="submit" disabled={submitDisabled} loading={action.busy}>{submitLabel ?? t.save}</Button></div>}
  </form>;
}
export function EditDialog({ title, description, children, onClose, onSubmit, submitLabel, submitDisabled, notifyOnSave }: { title: string; description?: string; children: ReactNode; onClose: () => void; onSubmit: () => Promise<unknown>; submitLabel?: string; submitDisabled?: boolean; notifyOnSave?: boolean }) {
  const action = useAction();
  return <Dialog open title={title} description={description} onOpenChange={open => { if (!open && !action.busy) onClose(); }} dismissible={!action.busy}>
    <EditForm action={action} onClose={onClose} onSubmit={onSubmit} submitLabel={submitLabel} submitDisabled={submitDisabled} notifyOnSave={notifyOnSave}>{children}</EditForm>
  </Dialog>;
}
export function Confirm({ title, description, onConfirm, onClose, danger = false, notify = true }: { title: string; description: string; onConfirm: () => Promise<unknown>; onClose: () => void; danger?: boolean; notify?: boolean }) {
  const { t } = useCopy(); const action = useAction();
  useErrorToast(action.error);
  return <Dialog open title={title} description={description} onOpenChange={open => { if (!open && !action.busy) onClose(); }} dismissible={!action.busy} footer={<><Button variant="secondary" disabled={action.busy} onClick={onClose}>{t.cancel}</Button><Button variant={danger ? 'danger' : 'primary'} loading={action.busy} onClick={() => void action.run(onConfirm, onClose, notify)}>{t.confirm}</Button></>} />;
}
export { PageHeader } from './PageHeader';
export function localDateTime(value?: string | null) { if (!value) return ''; const date = new Date(value); return new Date(date.getTime() - date.getTimezoneOffset() * 60_000).toISOString().slice(0, 16); }
export const lines = (text: string) => text.split('\n').map(value => value.trim()).filter(Boolean);
export function micros(value: string) { const amount = Number(value); if (!Number.isFinite(amount) || amount < 0 || !Number.isSafeInteger(Math.round(amount * 1_000_000))) throw new APIError('invalid_argument'); return Math.round(amount * 1_000_000); }

export function zonedInput(value: string, timezone: string) {
  const parts = new Intl.DateTimeFormat('en-CA', { timeZone: timezone, year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', hourCycle: 'h23' }).formatToParts(new Date(value));
  const get = (type: string) => parts.find(part => part.type === type)?.value;
  return `${get('year')}-${get('month')}-${get('day')}T${get('hour')}:${get('minute')}`;
}
export function zonedInstant(value: string, timezone: string) {
  try {
    const wall = Date.parse(`${value}:00Z`); let instant = wall;
    if (!Number.isFinite(wall)) throw new Error();
    for (let index = 0; index < 4; index++) { const actual = Date.parse(`${zonedInput(new Date(instant).toISOString(), timezone)}:00Z`); if (actual === wall) return new Date(instant).toISOString(); instant += wall - actual; }
  } catch { /* invalid timezone or wall time */ }
  throw new APIError('invalid_argument'); // Includes nonexistent DST wall times.
}
