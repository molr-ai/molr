import { useEffect, useState } from 'react';
import { ShieldCheck } from 'lucide-react';
import { Button, ChatCard, Drawer, Select, TextField, useBreakpoint, useUILocale } from '@molr/ui';
import { productMessages } from './i18n';

const initial = { resource: 'outing', actions: 'read', path: '/trips/weekend', duration: 'hour' };

export function ChatPermissionExample({ active }: { active: boolean }) {
  const { locale } = useUILocale(), t = productMessages[locale].chatShowcase.permissionCard;
  const narrow = useBreakpoint('mobile');
  const [state, setState] = useState<'pending' | 'approved' | 'rejected'>('pending');
  const [open, setOpen] = useState(false), [values, setValues] = useState(initial), [draft, setDraft] = useState(initial);
  useEffect(() => { if (!active) setOpen(false); }, [active]);
  function review() { setDraft(values); setOpen(true); }
  return <>
    <ChatCard title={t.title} eyebrow={t.type} icon={<ShieldCheck />} description={t.reason}
      status={{ label: t[state], tone: state === 'pending' ? 'warning' : state === 'approved' ? 'positive' : 'neutral' }}
      details={[
        { label: t.resource, value: t.resourceName },
        { label: t.actions, value: values.actions === 'read' ? t.read : t.readWrite },
        { label: t.scope, value: <code>{values.path}</code> },
        { label: t.duration, value: values.duration === 'hour' ? t.oneHour : t.oneDay },
      ]}
      metadata={state === 'approved' ? t.applied : state === 'rejected' ? t.declined : undefined}
      actions={state === 'pending'
        ? <><Button size="sm" variant="secondary" onClick={review}>{t.review}</Button><Button size="sm" variant="ghost" onClick={() => setState('rejected')}>{t.reject}</Button></>
        : <Button size="sm" variant="secondary" onClick={() => { setState('pending'); setValues(initial); }}>{t.reset}</Button>} />
    <Drawer open={open} onOpenChange={setOpen} title={t.reviewTitle} side={narrow ? 'bottom' : 'right'} footer={<><Button size="sm" variant="secondary" onClick={() => setOpen(false)}>{t.cancel}</Button><Button size="sm" disabled={!draft.path.trim()} onClick={() => { setValues(draft); setState('approved'); setOpen(false); }}>{t.approve}</Button></>}>
      <div className="chat-permission-form">
        <p className="component-copy">{t.hint}</p>
        <Select label={t.resource} value={draft.resource} onValueChange={resource => setDraft({ ...draft, resource })} options={[{ value: 'outing', label: t.resourceName }]} />
        <Select label={t.actions} value={draft.actions} onValueChange={actions => setDraft({ ...draft, actions })} options={[{ value: 'read', label: t.read }, { value: 'read-write', label: t.readWrite }]} />
        <TextField label={t.scope} value={draft.path} onChange={event => setDraft({ ...draft, path: event.target.value })} />
        <Select label={t.duration} value={draft.duration} onValueChange={duration => setDraft({ ...draft, duration })} options={[{ value: 'hour', label: t.oneHour }, { value: 'day', label: t.oneDay }]} />
      </div>
    </Drawer>
  </>;
}
