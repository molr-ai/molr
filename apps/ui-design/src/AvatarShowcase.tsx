import { useState } from 'react';
import { Avatar, AvatarNotice, Select, Switch, assistantAvatarPresets, useUILocale } from '@molr/ui';
import { ComponentExample } from './ComponentExample';
import { libraryMessages } from './library-messages';
import './avatar-showcase.css';

export function AvatarShowcase() {
 const { locale } = useUILocale(), copy = libraryMessages[locale].avatars;
 const [selected, setSelected] = useState(false), [tone, setTone] = useState('warning'), [size, setSize] = useState('44');
 const [upload, setUpload] = useState<string>(), [error, setError] = useState(false);
 const notice = tone === 'none' ? undefined : <AvatarNotice tone={tone as 'warning' | 'error'} label={tone === 'warning' ? copy.warning : copy.error} />;
 function avatar(value?: string) { return <Avatar name={copy.name} avatar={value} size={Number(size)} selected={selected} notice={notice} />; }
 return <ComponentExample id="avatars" title={copy.title} description={copy.description} wide>
  <div className="component-row">
   <Switch label={copy.selected} checked={selected} onChange={event => setSelected(event.target.checked)} />
   <Select label={copy.notice} value={tone} onValueChange={setTone} options={[{value:'none',label:copy.none},{value:'warning',label:copy.warning},{value:'error',label:copy.error}]} />
   <Select label={copy.size} value={size} onValueChange={setSize} options={['32','36','44','64'].map(value => ({value,label:`${value} px`}))} />
  </div>
  <div className="avatar-showcase__samples">
   {[{label:copy.initials,value:undefined},{label:copy.preset,value:'preset:cat'},{label:copy.image,value:upload ?? 'preset:star'}].map(item => <div className="avatar-showcase__sample" key={item.label}>
    <button type="button" className="avatar-showcase__button" aria-label={item.label} aria-pressed={selected} onClick={() => setSelected(value => !value)}>{avatar(item.value)}</button><span>{item.label}</span>
   </div>)}
  </div>
  <label className="avatar-showcase__upload">{copy.upload}<input type="file" accept="image/png,image/jpeg" onChange={event => {
   const file = event.target.files?.[0]; if (!file) return;
   if (!['image/png','image/jpeg'].includes(file.type) || file.size > 2 * 1024 * 1024) { setError(true); return; }
   const reader = new FileReader(); reader.onload = () => { setUpload(String(reader.result)); setError(false); }; reader.onerror = () => setError(true); reader.readAsDataURL(file);
  }} /></label>
  {error && <p className="avatar-showcase__error" role="alert">{copy.uploadError}</p>}
  <p className="component-copy">{copy.hint}</p>
  <div className="avatar-showcase__presets" aria-label={copy.presets}>{assistantAvatarPresets.map(value => <Avatar key={value} name={value} avatar={`preset:${value}`} size={36} />)}</div>
 </ComponentExample>;
}
