import type { Grant } from '@molr/sdk';
import { ChatCopyButton } from '@molr/ui';
import { useCopy, useQuery } from '../../../state';

export function GrantUseSummary({ once, grant, timeZone }: { once?: boolean; grant?: Grant; timeZone?: string }) {
 const { locale } = useCopy(), zh=locale==='zh';
 // Scope always comes from the approved snapshot; only usage is read live.
 const live=useQuery<Grant[]>(grant?.Scope.Once && !grant.Used ? `/api/grants?assistant_id=${grant.AssistantID}` : null,true,30000);
 const used=grant?.Used ?? live.data?.find(g=>g.ID===grant?.ID)?.Used;
 const single=grant ? grant.Scope.Once : once;
 return <div className="permission-use">
  <div className="permission-use__heading"><span>{zh?'使用次数':'Usage'}</span>{single ? <span>{used ? (zh?'仅执行一次（已使用）':'Use once (used)') : (zh?'仅执行一次':'Use once')}</span> : <span>{zh?'有效期内可重复':'Repeatable within expiry'}</span>}</div>
  {single && used && <details className="permission-use__record"><summary>{zh?'使用记录':'Use record'}</summary><time dateTime={used.UsedAt}>{new Intl.DateTimeFormat(locale,{timeZone,year:'numeric',month:'short',day:'numeric',hour:'2-digit',minute:'2-digit'}).format(new Date(used.UsedAt))}</time><div><span>{zh?'操作 ID':'Operation ID'}</span><code>{used.OperationID}</code><ChatCopyButton text={used.OperationID} label={zh?'复制操作 ID':'Copy operation ID'} /></div></details>}
 </div>;
}
