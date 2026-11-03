import { useState } from 'react';
import { Button, ChatCard, ModalPresence } from '@molr/ui';
import { MessagesSquare, UsersRound } from 'lucide-react';
import type { Assistant, DelegationPermissionRequestInput, Model, PermissionRequest } from '@molr/sdk';
import { useCopy, useQuery } from '../../../state';
import { useErrorToast } from '../../../error-toasts';
import { useMutation } from '../../../use-mutation';
import { Confirm, Loading } from '../components/common';
import { HiringEditor } from './AssistantDelegationPermissions';
import { PeerCommunicationEditor } from './AssistantPeerCommunication';
import { DelegationPermissionSummary } from './DelegationPermissionSummary';

export function DelegationPermissionRequestCard({ request, assistant }: { request: PermissionRequest<DelegationPermissionRequestInput>; assistant: Assistant }) {
  const { locale } = useCopy(), zh = locale === 'zh';
  const [review, setReview] = useState(false), [reject, setReject] = useState(false);
  const current = useQuery<Assistant>(review ? `/api/assistants/${assistant.ID}` : null, false, 0);
  const models = useQuery<Model[]>(review && request.Input.hiring ? '/api/models' : null, false, 0);
  const peers = useQuery<Assistant[]>(review && request.Input.communication ? '/api/assistants' : null, true, 0);
  const decline = useMutation([request.ID, request.Version, reject]);
  useErrorToast(current.error || models.error || peers.error, () => { current.reload(); models.reload(); peers.reload(); });
  const hiring = request.Input.permission_type === 'temporary_hiring', pending = request.State === 'pending';
  const title = hiring ? (zh ? '申请雇佣临时助理' : 'Request temporary hiring') : (zh ? '申请与其它助理对话' : 'Request assistant communication');
  const actions = pending && <><Button size="sm" variant="secondary" disabled={assistant.State !== 'active'} onClick={() => setReview(true)}>{zh ? '查看并授权' : 'Review permission'}</Button><Button size="sm" variant="ghost" disabled={assistant.State !== 'active'} onClick={() => setReject(true)}>{zh ? '拒绝' : 'Reject'}</Button></>;
  return <>
    <ChatCard className="permission-request-card" eyebrow={zh ? '权限申请' : 'Permission request'} title={title} icon={hiring ? <UsersRound /> : <MessagesSquare />} description={request.Input.reason}
      status={{ label: pending ? (zh ? '等待授权' : 'Awaiting permission') : request.State === 'approved' ? (zh ? '已批准' : 'Approved') : (zh ? '已拒绝' : 'Rejected'), tone: pending ? 'warning' : request.State === 'approved' ? 'positive' : 'neutral' }}
      actions={actions}>
      <DelegationPermissionSummary input={request.ApprovedDelegation ?? request.Input} assistantID={assistant.ID} />
      {review && !current.data && <Loading />}
    </ChatCard>
    <ModalPresence>{review && current.data && (!hiring || models.data) && (hiring || peers.data) && (hiring
      ? <HiringEditor assistant={current.data} models={models.data ?? []} request={request} onClose={() => setReview(false)} />
      : <PeerCommunicationEditor assistant={current.data} peers={peers.data ?? []} request={request} onClose={() => setReview(false)} />)}</ModalPresence>
    <ModalPresence>{reject && <Confirm title={zh ? '拒绝授权请求' : 'Reject permission request'} description={request.Input.reason} onClose={() => setReject(false)} onConfirm={() => decline('/api/permission-requests/resolve', { RequestID: request.ID, Decision: 'reject' }, request.Version)} />}</ModalPresence>
  </>;
}
