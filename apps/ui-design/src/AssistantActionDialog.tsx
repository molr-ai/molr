import { productMessages } from './i18n';
import { Button, Dialog, useToast } from '@molr/ui';
import type { Locale } from './messages';

export type AssistantAction = { kind: 'dismiss' | 'clone' | 'rehire'; name: string };

export function AssistantActionDialog({ action, locale, onClose }: { action: AssistantAction | null; locale: Locale; onClose: () => void }) {
  const t = productMessages[locale];
  const { toast } = useToast();
  const kind = action?.kind ?? 'clone';
  const name = action?.name ?? '';
  const copy = {
    dismiss: {
      title: t.common.dismissAssistant,
      description: t.assistantActionDialog.dismiss(name),
      body: t.assistantActionDialog.yourConversationWithThisAssistantWillBeArchivedAnd,
      detail: t.assistantActionDialog.youCanFindFormerAssistantsViewArchivedConversationsAnd,
      confirm: t.assistantActionDialog.confirmDismissal,
    },
    clone: {
      title: t.common.cloneAssistant,
      description: t.assistantActionDialog.clone(name),
      body: t.assistantActionDialog.theNewAssistantWillInheritTheOriginalAssistantS,
      detail: t.assistantActionDialog.confirmThatThisContextAndAccessShouldBeShared,
      confirm: t.assistantActionDialog.confirmClone,
    },
    rehire: {
      title: t.assistantActionDialog.rehireAssistant,
      description: t.assistantActionDialog.rehire(name),
      body: t.assistantActionDialog.thisAssistantWillReturnToYourAssistantListAnd,
      detail: t.assistantActionDialog.yourPreviousMessagesWillRemainAvailableSoYouCan,
      confirm: t.assistantActionDialog.confirmRehire,
    },
  }[kind];
  return <Dialog open={!!action} onOpenChange={open => { if (!open) onClose(); }} title={copy.title} description={copy.description} size="sm" footer={<>
    <Button variant="secondary" size="sm" onClick={onClose}>{t.common.cancel}</Button>
    <Button variant={kind === 'dismiss' ? 'danger' : 'primary'} size="sm" onClick={() => {
      onClose();
      toast({ title: t.assistantActionDialog.interactionPreview, description: t.assistantActionDialog.thisPreviewsTheInteractionNoAssistantsOrConversationsWere });
    }}>{copy.confirm}</Button>
  </>}>
    <p>{copy.body}</p><p className="ip-muted">{copy.detail}</p>
  </Dialog>;
}
