import { useState } from 'react';
import { Badge, ChatMessage, SegmentedControl, useUILocale } from '@molr/ui';
import type { ChatAttachment } from '@molr/ui';
import { productMessages } from './i18n';
import { ComponentExample } from './ComponentExample';
import route from './assets/chat-route.svg';
import lake from './assets/chat-attachments/lake.svg';
import cafe from './assets/chat-attachments/cafe.svg';
import panorama from './assets/chat-attachments/panorama.svg';
import leaf from './assets/chat-attachments/leaf.svg';

export function ChatAttachmentExamples() {
  const { locale } = useUILocale();
  const t = productMessages[locale].chatShowcase.attachmentCases;
  const [role, setRole] = useState<'user' | 'assistant'>('user');
  const images: ChatAttachment[] = [route, lake, cafe, panorama, leaf].map((previewUrl, index) => ({
    id: `image-${index}`, name: t.imageNames[index], size: [8400, 3200, 2800, 1900, 1200][index], type: 'image/svg+xml', previewUrl,
  }));
  const files: ChatAttachment[] = [
    { id: 'pdf', name: t.pdfName, type: 'application/pdf', size: 248 * 1024 },
    { id: 'markdown', name: t.notesName, type: 'text/markdown', size: 2048 },
    { id: 'csv', name: t.csvName, type: 'text/csv', size: 780 },
    { id: 'audio', name: t.audioName, type: 'audio/mpeg', size: 3.6 * 1024 * 1024 },
    { id: 'archive', name: t.archiveName, type: 'application/zip', size: 12.8 * 1024 * 1024 },
  ];
  const mixed = [images[0], files[0], images[2], files[1], images[4]];
  const cases = [
    { id: 'one-short', title: t.oneShort, text: t.shortText, attachments: images.slice(0, 1) },
    { id: 'five-short', title: t.fiveShort, text: t.shortText, attachments: images },
    { id: 'one-long', title: t.oneLong, text: t.longText, attachments: images.slice(1, 2) },
    { id: 'five-long', title: t.fiveLong, text: t.longText, attachments: images },
    { id: 'one-only', title: t.oneOnly, text: '', attachments: images.slice(2, 3) },
    { id: 'five-only', title: t.fiveOnly, text: '', attachments: images },
    { id: 'mixed-short', title: t.mixedShort, text: t.shortText, attachments: mixed },
    { id: 'mixed-long', title: t.mixedLong, text: t.longText, attachments: mixed },
    { id: 'files-short', title: t.filesShort, text: t.filesText, attachments: files },
    { id: 'files-only', title: t.filesOnly, text: '', attachments: files },
  ];
  return <ComponentExample id="chat-attachment-cases" title={t.title} description={t.description} wide>
    <div className="chat-attachment-cases__controls">
      <SegmentedControl label={t.sender} value={role} onValueChange={setRole} options={[{ value: 'user', label: t.user }, { value: 'assistant', label: t.assistant }]} />
      <p>{t.hint}</p>
    </div>
    <div className="chat-attachment-cases">
      {cases.map((sample, index) => <section className="chat-attachment-case" aria-labelledby={`attachment-case-${sample.id}`} key={sample.id}>
        <header><h4 id={`attachment-case-${sample.id}`}><span aria-hidden="true">{String(index + 1).padStart(2, '0')}</span>{sample.title}</h4><Badge>{t.count(sample.attachments.filter(file => !!file.previewUrl).length, sample.attachments.filter(file => !file.previewUrl).length)}</Badge></header>
        <div className="chat-attachment-case__message"><ChatMessage role={role} sender={role === 'user' ? t.user : 'Molr'} readStatus={role === 'user' ? 'read' : undefined} text={sample.text} attachments={sample.attachments} /></div>
      </section>)}
    </div>
  </ComponentExample>;
}
