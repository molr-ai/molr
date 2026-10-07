import { useId, useRef, useState } from 'react';
import { AssistantAvatar, assistantAvatarPresets, Button } from '@molr/ui';
import { useCopy } from '../../../state';
import './assistant-avatar.css';

// Only the thumbnail is sent to Runtime. Original images never leave this browser.
async function thumbnail(file: File): Promise<string> {
 if (!['image/png', 'image/jpeg', 'image/webp'].includes(file.type) || file.size > 10 * 1024 * 1024) throw new Error('invalid image');
 const bitmap = await createImageBitmap(file);
 try {
  if (bitmap.width * bitmap.height > 40_000_000) throw new Error('image too large');
  const canvas = document.createElement('canvas'); canvas.width = canvas.height = 256;
  const context = canvas.getContext('2d'); if (!context) throw new Error('canvas unavailable');
  const side = Math.min(bitmap.width, bitmap.height);
  // Fixed artwork exception: flatten transparency onto white for the exported JPEG, independent of the UI theme.
  context.fillStyle = '#ffffff'; context.fillRect(0, 0, 256, 256);
  context.drawImage(bitmap, (bitmap.width - side) / 2, (bitmap.height - side) / 2, side, side, 0, 0, 256, 256);
  return canvas.toDataURL('image/jpeg', 0.85);
 } finally { bitmap.close(); }
}
export function AssistantAvatarField({ name, value, onChange, onBusyChange }: { name: string; value: string; onChange: (value: string) => void; onBusyChange: (busy: boolean) => void }) {
 const { t } = useCopy(), copy = t.assistantAvatar;
 const input = useRef<HTMLInputElement>(null), id = useId();
 const [busy, setBusy] = useState(false), [error, setError] = useState(false);
 return <fieldset className="assistant-avatar-field" disabled={busy} aria-busy={busy}>
  <legend>{copy.label}</legend>
  <div className="assistant-avatar-current"><span className="assistant-avatar-preview" role="img" aria-label={copy.preview}><AssistantAvatar name={name || '?'} avatar={value} /></span><div className="assistant-avatar-actions">
   <Button type="button" size="sm" variant="secondary" onClick={() => input.current?.click()}>{busy ? copy.processing : copy.upload}</Button>
   {value && <Button type="button" size="sm" variant="ghost" onClick={() => { onChange(''); setError(false); }}>{copy.clear}</Button>}
  </div></div>
  <input ref={input} className="assistant-avatar-input" type="file" accept="image/png,image/jpeg,image/webp" aria-label={copy.upload} aria-describedby={`${id}-help`} onChange={async event => {
   const file = event.target.files?.[0]; event.target.value = ''; if (!file) return;
   setBusy(true); onBusyChange(true); setError(false);
   try { onChange(await thumbnail(file)); } catch { setError(true); } finally { setBusy(false); onBusyChange(false); }
  }} />
  <p id={`${id}-help`} className="muted">{copy.help}</p>
  {error && <p role="alert">{copy.error}</p>}
  <div className="assistant-avatar-presets" role="group" aria-label={copy.presets}>{assistantAvatarPresets.map(preset => <button key={preset} type="button" aria-label={copy.names[preset]} aria-pressed={value === `preset:${preset}`} title={copy.names[preset]} onClick={() => { onChange(`preset:${preset}`); setError(false); }}><AssistantAvatar name="" avatar={`preset:${preset}`} /></button>)}</div>
 </fieldset>;
}
