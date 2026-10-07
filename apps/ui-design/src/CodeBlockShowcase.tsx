import { useState } from 'react';
import { CodeBlock, SegmentedControl, Switch, Textarea, useUILocale } from '@molr/ui';
import { ComponentExample } from './ComponentExample';
import { libraryMessages } from './library-messages';

const sample = JSON.stringify({
  tool: 'conversation_read',
  arguments: { conversation_id: 'conversation-demo', limit: 5 },
  result: { ok: true, latency_ms: 42.5, next_cursor: null, messages: ['你好，Molr', 'A quoted value: "hello"'] },
}, null, 2);

export function CodeBlockShowcase() {
  const { locale } = useUILocale(), t = libraryMessages[locale];
  const [code, setCode] = useState(sample), [language, setLanguage] = useState<'json' | 'text'>('json'), [wrap, setWrap] = useState(true);
  return <ComponentExample id="code-block" title={t.codeBlock} description={t.codeBlockNote} wide>
    <div className="component-row component-row--spread"><SegmentedControl label={t.codeLanguage} hideLabel value={language} onValueChange={value => setLanguage(value as 'json' | 'text')} options={[{ value: 'json', label: 'JSON' }, { value: 'text', label: t.codePlain }]} /><Switch size="sm" label={t.codeWrap} checked={wrap} onChange={event => setWrap(event.target.checked)} /></div>
    <CodeBlock label={t.codeExample} code={code} language={language} wrap={wrap} maxHeight={280} />
    <details><summary>{t.codeEdit}</summary><Textarea label={t.codeInput} value={code} onChange={event => setCode(event.target.value)} rows={8} spellCheck={false} /></details>
  </ComponentExample>;
}
