import { useLayoutEffect, useRef, useState } from 'react';
import { Button, ChatComposer } from '@molr/ui';
import { api, mutationKey } from '@molr/sdk';
import type { AgentStatus, Assistant, FileInfo, PermissionRequest } from '@molr/sdk';
import { useDraft } from '../../../drafts';
import { useErrorToast } from '../../../error-toasts';
import { useCopy, useData, useQuery } from '../../../state';
import { Loading, useAction } from '../components/common';
import type { useChatChannel } from './use-chat-channel';
import { useFilePreviews } from './use-file-previews';

export function ChatComposerContainer({
  assistant,
  archived,
  status,
  channel,
  onStatusChange,
  onManageChannel,
  onSent,
}: {
  assistant: Assistant;
  archived: boolean;
  status: AgentStatus | undefined;
  channel: ReturnType<typeof useChatChannel>;
  onStatusChange: () => void;
  onManageChannel: () => void;
  onSent: () => void;
}) {
  const [draft, updateDraft] = useDraft(assistant.ID);
  const { t, locale } = useCopy();
  const { invalidateTopics } = useData();
  const action = useAction();
  const permissions = useQuery<PermissionRequest[]>(
    `/api/permission-requests?assistant_id=${assistant.ID}`,
  );
  const pendingPermissions =
    permissions.data?.filter((request) => request.State === 'pending') ?? [];
  const {
    readOnly: chatReadOnly,
    external: externalChannel,
    copy: channelCopy,
  } = channel;
  const [uploading, setUploading] = useState(false),
    [notice, setNotice] = useState('');
  const draftAttachments = useFilePreviews(draft.files);
  const working = status?.State === 'working',
    dreaming = status?.State === 'dreaming',
    compressing = status?.Compression?.State === 'running';
  useErrorToast(
    action.error || permissions.error,
    action.error ? send : permissions.reload,
  );
  const navigation = useRef(0);
  useLayoutEffect(() => {
    const changed = () => {
      navigation.current++;
    };
    window.addEventListener('hashchange', changed);
    return () => {
      changed();
      window.removeEventListener('hashchange', changed);
    };
  }, []);
  async function send() {
    if (
      chatReadOnly ||
      archived ||
      uploading ||
      action.busy ||
      (!draft.text.trim() && !draft.files.length)
    )
      return;
    const sent = draft,
      startedNavigation = navigation.current,
      startedHash = location.hash;
    const [command, ...args] = sent.text.trim().split(/\s+/);
    if (command === '/compress' || command === '/压缩') {
      if (args.length) {
        setNotice(t.chat.compressArguments);
        return;
      }
      await action.run(
        async () => {
          await api.mutate(`/api/assistants/${assistant.ID}/compress`, {});
          updateDraft((current) =>
            current === sent ? { ...sent, text: '' } : current,
          );
          setNotice('');
          onStatusChange();
        },
        undefined,
        false,
      );
      return;
    }
    if (command === '/permission' || command === '/权限审批') {
      const zh = locale === 'zh';
      const decision = ['approve', '同意'].includes(args[0])
        ? 'approve'
        : ['reject', '拒绝'].includes(args[0])
          ? 'reject'
          : undefined;
      if (args.length < 1 || args.length > 2 || !decision) {
        setNotice(
          zh
            ? '用法：/权限审批 同意|拒绝 [申请ID|全部]。'
            : 'Usage: /permission approve|reject [id|all].',
        );
        return;
      }
      const target = args[1],
        all = target === 'all' || target === '全部';
      // The API returns pending requests first, newest first (including sub-ms ordering).
      // Freeze this command's targets so new requests are never added during a batch.
      const selected = all
        ? pendingPermissions
        : target
          ? pendingPermissions.filter((request) => request.ID === target)
          : pendingPermissions.slice(0, 1);
      if (!selected.length) {
        setNotice(
          target && !all
            ? zh
              ? '当前助理没有此 ID 的待审批申请。'
              : 'No pending request with this ID belongs to the current assistant.'
            : zh
              ? '当前没有待审批的权限申请。'
              : 'There is no pending permission request.',
        );
        return;
      }
      await action.run(
        async () => {
          const current = (
            await api.get<PermissionRequest[]>(
              `/api/permission-requests?assistant_id=${assistant.ID}`,
            )
          ).filter((request) => request.State === 'pending');
          if (
            (!target && current[0]?.ID !== selected[0].ID) ||
            selected.some(
              (request) =>
                !current.some(
                  (row) =>
                    row.ID === request.ID && row.Version === request.Version,
                ),
            )
          ) {
            permissions.reload();
            setNotice(
              zh
                ? '待审批申请已变化，请查看最新权限卡片后重试。'
                : 'Pending requests changed. Review the latest permission card and try again.',
            );
            return;
          }
          const proposals = selected.map((request) => ({
            request,
            items: [
              request.Input,
              ...(request.Input.additional_requests ?? []),
            ],
          }));
          // Validate every proposal before starting a batch; never invent missing limits.
          if (
            decision === 'approve' &&
            proposals.some(({ items }) =>
              items.some(
                (item) =>
                  !item.resource_id ||
                  (item.resource_kind === 'model' && !item.model_id) ||
                  (!item.full_access &&
                    ['compute', 'network'].includes(item.resource_kind) &&
                    !item.constraints),
              ),
            )
          ) {
            setNotice(
              zh
                ? '申请需要补全资源或额度，请从权限卡片查看并授权。'
                : 'This request needs a resource or quota. Review it using the permission card.',
            );
            return;
          }
          let completed = 0;
          try {
            for (const { request, items } of proposals) {
              const resolutions = items.map((item) => ({
                ResourceID: item.resource_id,
                GrantID: item.grant_id,
                GrantVersion: item.grant_version,
                Actions: item.actions,
                Scope: {
                  FullAccess: item.full_access ?? false,
                  ModelID: item.model_id ?? '',
                  PathPrefixes: item.path_prefixes ?? [],
                  ReadOnly: item.actions.every((value) => value === 'read'),
                },
                ExpiresAt: item.expires_at,
                Constraints: item.full_access ? undefined : item.constraints,
                Temporary: item.full_access ? undefined : item.temporary,
              }));
              await api.mutate(
                '/api/permission-requests/resolve',
                {
                  RequestID: request.ID,
                  Decision: decision,
                  ...(decision === 'approve'
                    ? { ...resolutions[0], Additional: resolutions.slice(1) }
                    : {}),
                },
                request.Version,
              );
              completed++;
            }
            updateDraft((current) =>
              current === sent ? { ...sent, text: '' } : current,
            );
            setNotice(
              zh
                ? `已${decision === 'approve' ? '同意' : '拒绝'} ${completed} 条权限申请。`
                : `${completed} permission request(s) ${decision === 'approve' ? 'approved' : 'rejected'}.`,
            );
          } catch (error) {
            setNotice(
              zh
                ? `已处理 ${completed}/${selected.length} 条申请，处理已停止；请检查剩余申请后重试。`
                : `Processed ${completed}/${selected.length} requests, then stopped. Review the remaining requests before retrying.`,
            );
            throw error;
          } finally {
            permissions.reload();
            onStatusChange();
          }
        },
        undefined,
        false,
      );
      return;
    }
    await action.run(
      async () => {
        await api.mutate('/api/conversations/messages', {
          ConversationID: assistant.ConversationID,
          Content: {
            Kind: 'text',
            Text: sent.text,
            AttachmentIDs: sent.files.map((file) => file.ID),
          },
        });
        invalidateTopics([`messages:${assistant.ConversationID}`]);
        updateDraft((current) =>
          current === sent ? { text: '', files: [] } : current,
        );
        // A mobile exit animation may keep this component mounted after navigation.
        if (
          navigation.current === startedNavigation &&
          location.hash === startedHash
        ) {
          onSent();
        }
      },
      undefined,
      false,
    );
  }
  async function attach(files: File[]) {
    if (chatReadOnly || uploading || action.busy) return;
    if (
      draft.files.length + files.length > 5 ||
      files.some((file) => file.size > 20 * 1024 * 1024)
    ) {
      setNotice(t.chat.uploadLimit);
      return;
    }
    setNotice('');
    setUploading(true);
    const added: FileInfo[] = [];
    try {
      for (const file of files)
        added.push(await api.upload(file, mutationKey()));
    } catch {
      setNotice(t.chat.uploadError);
    } finally {
      if (added.length)
        updateDraft({ ...draft, files: [...draft.files, ...added] });
      setUploading(false);
    }
  }
  return !channel.ready ? (
    <Loading />
  ) : (
    <div className="chat-input">
      <ChatComposer
        lock={
          externalChannel
            ? {
                title: channelCopy.readonly,
                description: channelCopy.readonlyHelp,
                action: (
                  <Button
                    variant="secondary"
                    size="sm"
                    onClick={onManageChannel}
                  >
                    {channelCopy.manage}
                  </Button>
                ),
              }
            : undefined
        }
        commands={[
          {
            name: 'compress',
            aliases: ['压缩'],
            description: `${t.chat.compress} · /压缩`,
            keywords: ['上下文'],
            disabled: compressing,
          },
          ...(pendingPermissions.length
            ? [
                {
                  name: 'permission approve',
                  aliases: ['权限审批 同意'],
                  description:
                    locale === 'zh'
                      ? '同意最新申请；可加 ID 或 all（全部）'
                      : 'Approve latest; optionally add an ID or all',
                },
                {
                  name: 'permission reject',
                  aliases: ['权限审批 拒绝'],
                  description:
                    locale === 'zh'
                      ? '拒绝最新申请；可加 ID 或 all（全部）'
                      : 'Reject latest; optionally add an ID or all',
                },
              ]
            : []),
        ]}
        value={draft.text}
        onValueChange={(text) => updateDraft({ ...draft, text })}
        onSubmit={() => void send()}
        placeholder={t.chat.placeholder}
        disabled={chatReadOnly || archived || uploading || action.busy}
        attachments={draftAttachments}
        onAttachFiles={(files) => void attach(files)}
        onRemoveAttachment={(id) =>
          updateDraft({
            ...draft,
            files: draft.files.filter((file) => file.ID !== id),
          })
        }
        notice={
          uploading
            ? t.chat.uploading
            : dreaming
              ? `${t.chat.dreaming}${status?.DreamDeadline ? ` · ${new Date(status.DreamDeadline).toLocaleTimeString()} ${locale.startsWith('zh') ? '前结束，消息排队等待' : 'deadline; messages queued'}` : ''}`
              : notice ||
                (status?.Compression?.State === 'failed'
                  ? t.chat.compressFailed
                  : '')
        }
        processingLabel={
          compressing
            ? t.chat.compressing
            : dreaming
              ? t.chat.dreaming
              : undefined
        }
        sending={working || dreaming}
        allowSendWhileSending
        onStop={
          working || dreaming
            ? () =>
                void action.run(
                  () => api.post(`/api/assistants/${assistant.ID}/stop`),
                  undefined,
                  false,
                )
            : undefined
        }
      />
    </div>
  );
}
