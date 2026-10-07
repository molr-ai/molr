import { DismissibleAlert } from '../components/DismissibleAlert';
import type { AgentStatus, Assistant } from '../../../api/types';
import { useCopy } from '../../../state';

export function AssistantWaitingNotice({ assistant, status }: { assistant: Assistant; status: AgentStatus }) {
  const { t } = useCopy();
  const executionKey = [assistant.ID, status.ExecutionID, status.TriggerMessageID];
  if (status.ModelRetry) return <DismissibleAlert noticeKey={JSON.stringify([...executionKey, 'retry', status.ModelRetry.Reason])} tone="warning" title={t.workbench.states.retrying}>{(t.waiting.reasons as Record<string,string>)[status.ModelRetry.Reason]} {t.workbench.retryAttempt} {status.ModelRetry.Attempt}/{status.ModelRetry.MaxAttempts} · {t.workbench.retryNext} {new Date(status.ModelRetry.NextAt).toLocaleTimeString()}</DismissibleAlert>;
  return null;
}
