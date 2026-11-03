import type { Assistant, DelegationPermissionRequestInput, Grant, Model, PermissionRequest, Resource } from '@molr/sdk';
import { useCopy, useQuery } from '../../../state';
import { useErrorToast } from '../../../error-toasts';
import { currencyText } from '../../../currency';
import { grantOptionLabel } from '../../../resource-permissions';
import { delegationConfiguration } from '../../../delegation-permissions';
import { PermissionExpiry } from './PermissionScope';

export function DelegationPermissionSummary({ input, assistantID }: { input: DelegationPermissionRequestInput; assistantID: string }) {
  const { t, locale } = useCopy(), zh = locale === 'zh';
  const grants = useQuery<Grant[]>(input.hiring ? `/api/grants?assistant_id=${assistantID}` : null, true, 0);
  const resources = useQuery<Resource[]>(input.hiring ? '/api/resources' : null, true, 0);
  const models = useQuery<Model[]>(input.hiring ? '/api/models' : null, false, 0);
  const peers = useQuery<Assistant[]>(input.communication ? '/api/assistants' : null, true, 0);
  useErrorToast(grants.error || resources.error || models.error || peers.error, () => { grants.reload(); resources.reload(); models.reload(); peers.reload(); });
  return <dl className="assistant-info-facts">
    {input.hiring && <>
      <div><dt>{zh ? '同时在职' : 'Active limit'}</dt><dd>{input.hiring.max_assistants}</dd></div>
      <div><dt>{zh ? '共享总预算' : 'Shared total budget'}</dt><dd>{currencyText(input.hiring.budget, locale)}</dd></div>
      <div><dt>{zh ? '可授予的权限' : 'Delegable permissions'}</dt><dd>{input.hiring.grant_ids?.length ? input.hiring.grant_ids.map(id => {
        const grant = grants.data?.find(g => g.ID === id);
        return <p key={id}>{grant ? grantOptionLabel(t, grant, resources.data?.find(r => r.ID === grant.ResourceID), models.data ?? []) : id}</p>;
      }) : (zh ? '审批时选择' : 'Choose during review')}</dd></div>
    </>}
    {input.communication && <div><dt>{zh ? '助理范围' : 'Assistants'}</dt><dd>{input.communication.all_assistants ? (zh ? '全部助理' : 'All assistants') : input.communication.assistant_ids?.length ? input.communication.assistant_ids.map(id => peers.data?.find(a => a.ID === id)?.Name ?? id).join(zh ? '、' : ', ') : (zh ? '审批时选择' : 'Choose during review')}</dd></div>}
    <div><dt>{t.resources.expires}</dt><dd><PermissionExpiry value={input.expires_at} /></dd></div>
  </dl>;
}

export function DelegationReviewContext({ assistant, request }: { assistant: Assistant; request: PermissionRequest<DelegationPermissionRequestInput> }) {
  const { locale } = useCopy(), zh = locale === 'zh';
  const current = delegationConfiguration(assistant, request.Input.permission_type);
  return <>
    <p>{request.Input.reason}</p>
    <details className="delegation-review-current"><summary>{zh ? '当前授权' : 'Current authorization'}</summary>{current ? <DelegationPermissionSummary input={current} assistantID={assistant.ID} /> : <p className="muted">{zh ? '未允许' : 'Not allowed'}</p>}</details>
    <p className="assistant-info-help">{zh ? '以下为申请配置，可调整后批准。' : 'Review and adjust the requested settings below before approving.'}</p>
  </>;
}
