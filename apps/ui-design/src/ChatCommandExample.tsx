import { useEffect, useRef, useState } from 'react';
import { Button, ChatComposer, ChatStatus, SegmentedControl, useUILocale } from '@molr/ui';
import { ComponentExample } from './ComponentExample';
import { productMessages } from './i18n';

export function ChatCommandExample({ active }: { active: boolean }) {
  const { locale } = useUILocale(), t = productMessages[locale].chatShowcase.commandSelector;
  const [value, setValue] = useState(''), [sample, setSample] = useState('all');
  const [phase, setPhase] = useState<'idle' | 'running' | 'done'>('idle');
  const root = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (!active || phase !== 'running') return;
    const timer = setTimeout(() => setPhase('done'), 1500);
    return () => clearTimeout(timer);
  }, [active, phase]);
  function show(next: string) {
    setSample(next); setValue(next === 'filter' ? '/com' : next === 'empty' ? '/unknown' : '/');
    requestAnimationFrame(() => { const input = root.current?.querySelector('textarea'); input?.setSelectionRange(input.value.length, input.value.length); input?.focus({ preventScroll: true }); });
  }
  return <ComponentExample id="command-selector" title={t.title} description={t.description} wide>
    <div className="chat-command-controls"><SegmentedControl label={t.scenarios} value={sample} onValueChange={show} options={[{ value: 'all', label: t.all }, { value: 'filter', label: t.filter }, { value: 'empty', label: t.empty }]} /><Button variant="secondary" size="sm" onClick={() => show(sample)}>{t.open}</Button></div>
    <div className="chat-command-stage" ref={root}>
      <ChatStatus compact status={phase === 'running' ? 'acting' : 'idle'} label={phase === 'running' ? t.running : t.idle} />
      <ChatComposer value={value} onValueChange={setValue} placeholder={t.placeholder} commands={[{ name: 'compress', description: t.command, keywords: ['压缩', '上下文'], disabled: phase === 'running' }]}
        onSubmit={() => { if (value.trim() === '/compress') { setValue(''); setPhase('running'); } }} sendDisabled={value.trim() !== '/compress' || phase === 'running'}
        notice={phase === 'done' ? t.done : undefined} />
    </div>
    <p className="component-copy">{t.hint}</p>
  </ComponentExample>;
}
