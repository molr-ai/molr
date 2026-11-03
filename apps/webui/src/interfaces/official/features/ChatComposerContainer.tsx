import { isDelegationPermission } from '../../../delegation-permissions';
import { validFullAccessScope } from './FullAccessScope';
import { useMutation } from '../../../use-mutation';
import { useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react';
import { Button, ChatAttachmentList, ChatComposer } from '@molr/ui';
import { api, mutationKey } from '@molr/sdk';
import type { DraftTextAttachment } from '@molr/sdk/ui';
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
  onManagePermissions,
  onSent,
}: {
  assistant: Assistant;
  archived: boolean;
  status: AgentStatus | undefined;
  channel: ReturnType<typeof useChatChannel>;
  onStatusChange: () => void;
  onManageChannel: () => void;
  onManagePermissions: () => void;
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
  const [failedTexts, setFailedTexts] = useState<string[]>([]);
  const textUpload = useRef(false);
  const pendingTexts = draft.pendingTexts ?? [];
  const pendingAttachments = useMemo(() => (draft.pendingTexts ?? []).map(item => ({ id: item.id, name: item.name, size: new Blob([item.text]).size, type: 'text/plain' })), [draft.pendingTexts]);
  // Keep original clipboard text and its retry identity in the account/assistant
  // draft until Runtime confirms the upload. Navigation cannot lose the source.
  useEffect(() => {
    const pending = draft.pendingTexts?.find(item => !failedTexts.includes(item.id));
    if (!pending || textUpload.current || uploading || chatReadOnly || archived || action.busy) return;
    textUpload.current = true;
    setUploading(true);
    const file = new File([pending.text], pending.name, { type: 'text/plain;charset=utf-8' });
    void api.upload(file, pending.id).then(info => {
      updateDraft(current => current.pendingTexts?.some(item => item.id === pending.id) ? {
        ...current, files: [...current.files, info], pendingTexts: current.pendingTexts.filter(item => item.id !== pending.id),
      } : current);
    }).catch(() => {
      setFailedTexts(current => [...current, pending.id]);
      setNotice(t.chat.uploadError);
    }).finally(() => { textUpload.current = false; setUploading(false); });
  }, [draft.pendingTexts, failedTexts, uploading, chatReadOnly, archived, action.busy, updateDraft, t.chat.uploadError]);
  function attachText(text: string) {
    if (chatReadOnly || archived) return;
    if (draft.files.length + pendingTexts.length >= 5 || new Blob([text]).size > 20 * 1024 * 1024) {
      setNotice(t.chat.uploadLimit); return;
    }
    const id = mutationKey();
    const pending: DraftTextAttachment = { id, name: `${locale === 'zh' ? '粘贴文本' : 'pasted-text'}-${id.slice(0, 8)}.txt`, text };
    updateDraft(current => ({ ...current, pendingTexts: [...(current.pendingTexts ?? []), pending] }));
    setNotice('');
  }
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
  const sendMessage = useMutation([assistant.ConversationID, draft]);
  const resolvePermission = useMutation([assistant.ID, draft]);
  const compress = useMutation([assistant.ID, draft]);
  async function send() {
    if (
      chatReadOnly ||
      archived ||
      uploading ||
      pendingTexts.length > 0 ||
      draft.text.length > 4000 ||
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
          await compress(`/api/assistants/${assistant.ID}/compress`, {});
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
            items: isDelegationPermission(request.Input) ? [] : [request.Input, ...(request.Input.additional_requests ?? [])],
          }));
          // Validate every proposal before starting a batch; never invent missing limits.
          if (
            decision === 'approve' &&
            proposals.some(({ request, items }) =>
              (isDelegationPermission(request.Input) && (
                (!!request.Input.expires_at && Date.parse(request.Input.expires_at) <= Date.now()) ||
                (request.Input.hiring && !request.Input.hiring.grant_ids?.length) ||
                (request.Input.communication && !request.Input.communication.all_assistants && !request.Input.communication.assistant_ids?.length)
              )) || items.some(
                (item) =>
                  !item.resource_id ||
                  (item.full_access && (!validFullAccessScope(item.full_access_scope) || !item.expires_at)) ||
                  (item.resource_kind === 'model' && !item.model_id) ||
                  (!item.full_access &&
                    ['compute', 'network'].includes(item.resource_kind) &&
                    !item.constraints),
              ),
            )
          ) {
            setNotice(proposals.some(({ request }) => isDelegationPermission(request.Input))
              ? (zh ? '申请需要补全授权范围或调整有效期，请从权限卡片查看并授权。' : 'Complete the authorization scope or validity using the permission card.')
              : (zh ? '申请需要补全资源、授权范围或额度，请从权限卡片查看并授权。' : 'This request needs a resource, access scope or quota. Review it using the permission card.'));

            return;
          }
          let assistantVersion = decision === 'approve' && proposals.some(({ request }) => isDelegationPermission(request.Input))
            ? (await api.get<Assistant>(`/api/assistants/${assistant.ID}`)).Version : 0;
          let completed = 0;
          try {
            for (const { request, items } of proposals) {
              const resolutions = items.map((item) => ({
                ResourceID: item.resource_id,
                GrantID: item.grant_id,
                GrantVersion: item.grant_version,
                Actions: item.actions,
                Scope: {
                  Once: !!item.full_access && !!item.once,
                  FullAccess: item.full_access ?? false,
                  FullAccessScope: item.full_access ? item.full_access_scope : undefined,
                  ModelID: item.model_id ?? '',
                  PathPrefixes: item.path_prefixes ?? [],
                  ReadOnly: item.actions.every((value) => value === 'read'),
                },
                ExpiresAt: item.expires_at,
                Constraints: item.full_access ? undefined : item.constraints,
                Temporary: item.full_access ? undefined : item.temporary,
              }));
              const result = await resolvePermission<PermissionRequest>(
                '/api/permission-requests/resolve',
                {
                  RequestID: request.ID,
                  Decision: decision,
                  ...(decision === 'approve'
                    ? isDelegationPermission(request.Input)
                      ? { Hiring: request.Input.hiring, Communication: request.Input.communication, ExpiresAt: request.Input.expires_at, AssistantVersion: assistantVersion }
                      : { ...resolutions[0], Additional: resolutions.slice(1) }
                    : {}),
                },
                request.Version, request.ID,
              );
              if (result.ApprovedAssistantVersion) assistantVersion = result.ApprovedAssistantVersion;
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
        await sendMessage('/api/conversations/messages', {
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
      draft.files.length + pendingTexts.length + files.length > 5 ||
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
        updateDraft(current => ({ ...current, files: [...current.files, ...added] }));
      setUploading(false);
    }
  }
  return !channel.ready ? (
    <Loading />
  ) : (
    <div className="chat-input">
      {pendingTexts.length > 0 && <div role="group" aria-label={locale === 'zh' ? '待上传文本附件' : 'Pending text attachments'}>
        <ChatAttachmentList files={pendingAttachments}
          disabled={uploading}
          onRemove={id => updateDraft(current => ({ ...current, pendingTexts: current.pendingTexts?.filter(item => item.id !== id) }))} />
        {pendingTexts.some(item => failedTexts.includes(item.id)) && <Button size="sm" variant="ghost" disabled={uploading} onClick={() => { setFailedTexts([]); setNotice(''); }}>
          {locale === 'zh' ? '重试文本附件上传' : 'Retry text uploads'}
        </Button>}
      </div>}
      <ChatComposer
        extension={pendingPermissions.length > 0 && <button type="button" className="composer-permissions" aria-haspopup="dialog" onClick={onManagePermissions}>
          {locale === 'zh' ? `${pendingPermissions.length} 个授权请求待处理` : `${pendingPermissions.length} pending permission request${pendingPermissions.length === 1 ? '' : 's'}`}
        </button>}
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
          ...(pendingPermissions.length ? [{
            name: 'permission', aliases: ['权限审批'],
            description: locale === 'zh' ? '审批授权请求' : 'Review permission requests',
            children: [
              { name: 'approve', aliases: ['同意'], description: locale === 'zh' ? '同意授权请求' : 'Approve permission requests' },
              { name: 'reject', aliases: ['拒绝'], description: locale === 'zh' ? '拒绝授权请求' : 'Reject permission requests' },
            ].map(command => ({ ...command, children: [
              { name: 'all', aliases: ['全部'], description: locale === 'zh' ? `全部 ${pendingPermissions.length} 个待处理请求` : `All ${pendingPermissions.length} pending requests` },
              ...pendingPermissions.map(request => {
                const input = request.Input;
                const label = isDelegationPermission(input)
                  ? input.permission_type === 'temporary_hiring'
                    ? (locale === 'zh' ? '雇佣临时助理' : 'Temporary hiring')
                    : (locale === 'zh' ? '与其它助理对话' : 'Assistant communication')
                  : input.resource_description;
                const summary = input.reason.replace(/\s+/g, ' ').trim();
                return { name: request.ID, label, description: summary, keywords: [summary] };
              }),
            ] })),
          }] : []),
        ]}
        value={draft.text}
        onValueChange={(text) => updateDraft(current => ({ ...current, text }))}
        onSubmit={() => void send()}
        placeholder={t.chat.placeholder}
        disabled={chatReadOnly || archived || uploading || action.busy}
        attachments={draftAttachments}
        onAttachFiles={(files) => void attach(files)}
        onPasteText={attachText}
        sendDisabled={pendingTexts.length > 0 || draft.text.length > 4000}
        onRemoveAttachment={(id) =>
          updateDraft(current => ({
            ...current,
            files: current.files.filter((file) => file.ID !== id),
          }))
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
