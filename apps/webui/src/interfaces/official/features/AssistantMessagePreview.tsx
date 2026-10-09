import type { AssistantActivity } from '../../../api/types';
import { useCopy } from '../../../state';

export function AssistantMessagePreview({ activity }: { activity?: AssistantActivity }) {
  const { t } = useCopy();
  if (!activity) return null;
  if (!activity.Preview) return t.assistants.start;
  return activity.Preview.Text || (activity.Preview.Kind === 'card' ? t.assistants.cardPreview : t.assistants.attachmentPreview);
}
