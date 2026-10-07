import { attachmentPreview } from './attachment-preview';
import { productMessages } from './i18n';
import { useEffect, useRef, useState } from 'react';
import { ChatComposer } from '@molr/ui';
import type { DemoAssistant, InterfaceDemo } from './interface-demo';
import type { Locale } from './messages';

export function MessageComposer({ assistant, demo, locale, referenced }: { assistant: DemoAssistant; demo: InterfaceDemo; locale: Locale; referenced: boolean }) {
  const t = productMessages[locale];
  const root = useRef<HTMLDivElement>(null);
  const recorder = useRef<MediaRecorder | null>(null); const stream = useRef<MediaStream | null>(null); const generation = useRef(0); const busy = useRef(false); const limitTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const [recording, setRecording] = useState(false); const [notice, setNotice] = useState('');
  const files = assistant.draftAttachments ?? [];
  useEffect(() => () => { generation.current++; if (limitTimer.current) clearTimeout(limitTimer.current); if (recorder.current?.state === 'recording') recorder.current.stop(); stream.current?.getTracks().forEach(track => track.stop()); }, []);
  useEffect(() => {
    const panel = root.current?.closest('[role="tabpanel"]');
    if (!panel) return;
    const observer = new MutationObserver(() => { if (!root.current?.getClientRects().length || root.current?.closest('[inert], [aria-hidden="true"]')) { if (recorder.current?.state === 'recording') recorder.current.stop(); else if (busy.current) { generation.current++; busy.current = false; } } });
    observer.observe(panel, { subtree: true, attributes: true, attributeFilter: ['hidden', 'inert', 'aria-hidden'] });
    return () => observer.disconnect();
  }, []);
  async function attach(selected: File[]) {
    const accepted = selected.filter(file => file.size <= 20 * 1024 * 1024);
    const added = await Promise.all(accepted.slice(0, 5 - files.length).map(async (file, index) => ({ id: `${Date.now()}-${files.length + index}`, name: file.name, size: file.size, type: file.type, previewUrl: await attachmentPreview(file) })));
    const next = [...files, ...added];
    demo.patchAssistant({ draftAttachments: next });
    setNotice(accepted.length !== selected.length || files.length + accepted.length > 5 ? t.common.upTo5Attachments20MbEach : t.messageComposer.fileMetadataAttachedThisPrototypeDoesNotUploadContents);
  }
  async function voice() {
    if (recording) { recorder.current?.stop(); return; }
    if (busy.current) return;
    if (!navigator.mediaDevices?.getUserMedia || typeof MediaRecorder === 'undefined') { setNotice(t.messageComposer.recordingIsUnavailableInThisBrowserOrHttpContext); return; }
    busy.current = true; const current = generation.current;
    try {
      const media = await navigator.mediaDevices.getUserMedia({ audio: true });
      if (current !== generation.current) { media.getTracks().forEach(track => track.stop()); return; }
      stream.current = media; const rec = new MediaRecorder(media); recorder.current = rec; const chunks: Blob[] = [];
      rec.ondataavailable = event => { if (event.data.size) chunks.push(event.data); };
      rec.onstop = () => { if (limitTimer.current) clearTimeout(limitTimer.current); media.getTracks().forEach(track => track.stop()); if (current !== generation.current) return; setRecording(false); busy.current = false; const blob = new Blob(chunks, { type: rec.mimeType }); attach([new File([blob], `voice-${Date.now()}.${rec.mimeType.includes('mp4') ? 'm4a' : 'webm'}`, { type: rec.mimeType })]); };
      rec.onerror = () => { media.getTracks().forEach(track => track.stop()); busy.current = false; setRecording(false); setNotice(t.messageComposer.recordingFailedRetryOrAttachAnAudioFile); };
      rec.start(); limitTimer.current = setTimeout(() => { if (rec.state === 'recording') rec.stop(); }, 60000); setRecording(true); setNotice(t.messageComposer.recordingPressStopToFinishUpTo60Seconds);
    } catch { stream.current?.getTracks().forEach(track => track.stop()); setNotice(t.messageComposer.cannotAccessMicrophoneCheckBrowserPermissions); busy.current = false; }
  }
  function submit() { if (!demo.connected || recording || (!assistant.draft.trim() && !files.length)) return; if (assistant.draft.trim() === '/compress') { demo.patchAssistant({ draft: '' }); setNotice(t.chatShowcase.commandSelector.done); return; } demo.sendMessage(assistant.draft, assistant.reference ?? undefined, locale); setNotice(''); }
  return <div ref={root}><ChatComposer commands={[{ name: 'compress', description: t.chatShowcase.commandSelector.command, keywords: ['压缩', '上下文'] }]} value={assistant.draft} onValueChange={draft => demo.patchAssistant({ draft })} onSubmit={submit} maxLength={1000}
    attachments={files} onAttachFiles={attach} onRemoveAttachment={id => demo.patchAssistant({ draftAttachments: files.filter(file => file.id !== id) })}
    model={assistant.draftModel ?? 'standard'} onModelChange={draftModel => demo.patchAssistant({ draftModel })} models={[{ value: 'standard', label: t.common.standardDemo }, { value: 'fast', label: t.common.fastDemo }]}
    onVoiceInput={voice} recording={recording} sendDisabled={!demo.connected} notice={notice}
    placeholder={referenced ? t.messageComposer.addARequestOrAskAboutProgress : undefined}
    hint={demo.interfacePreferences.showHints ? t.messageComposer.enterToSendShiftEnterForANewLine : undefined} /></div>;
}
