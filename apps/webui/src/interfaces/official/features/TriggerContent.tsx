import type { DebugSource } from '@molr/sdk';
import { useCopy } from '../../../state';

export function TriggerContent({ source }: { source?: DebugSource }) {
  const { t } = useCopy();
  if (!source || !['captured', 'redacted'].includes(source.Status)) return <p className="muted">{source?.Status === 'version_mismatch' ? t.workbench.sourceVersionMismatch : source?.Status === 'too_large' ? t.workbench.payloadLimited : t.workbench.sourceUnavailable}</p>;
  return <><div className="trajectory-source-meta"><span>{source.Kind === 'user_message' ? t.workbench.userInput : t.workbench.triggerText}</span><small>{source.Origin === 'reference' ? t.workbench.linkedSource : t.workbench.capturedSource}</small></div>
    {source.Title && <strong className="trajectory-source-title">{source.Title}</strong>}<pre className="workbench-prose">{source.Text}</pre>
    {!!source.AttachmentIDs?.length && <small>{t.workbench.attachments} · {source.AttachmentIDs.length}</small>}
    {source.Status === 'redacted' && <small>{t.workbench.redacted}</small>}
  </>;
}
