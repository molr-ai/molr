import { GPUAccessSummary } from './GPUAccess';
import { gpuName } from '../../../gpu';
import { Checkbox, DateTimePicker, Select, TextField } from '@molr/ui';
import type { ResourceConstraints, TemporaryConstraints } from '../../../api/types';
import { useCopy, useQuery } from '../../../state';
import type { GPUDevice } from '../../../sandbox-types';
export function suggestedConstraints(kind: string): ResourceConstraints | undefined {
 if(kind==='compute')return {compute:{cpu_percent:100,memory_bytes:512*1048576,processes:64,max_duration_seconds:1800,unlimited_duration:false,temp_bytes:64*1048576}};
 if(kind==='network')return {network:{bytes:1073741824,connections:32,period:'day',timezone:Intl.DateTimeFormat().resolvedOptions().timeZone||'UTC'}};
 if(kind==='storage')return {storage:{capacity_bytes:10*1073741824,max_file_bytes:1073741824,max_files:100000}};
}
export function ConstraintsEditor({kind,value,onChange,lockNetworkPeriod=false,compact=false,gpus=[]}: {gpus?:GPUDevice[]|null;kind:string;value?:ResourceConstraints;onChange:(v:ResourceConstraints|undefined)=>void;lockNetworkPeriod?:boolean;compact?:boolean}){
 const {locale}=useCopy(),zh=locale==='zh';
 if(!['compute','network','storage'].includes(kind))return null;
 if(!value)return null;
 const field=(label:string,n:number,change:(n:number)=>void,min=1,step: number | 'any'=1,description?:string)=><TextField description={description} type="number" label={label} value={String(n)} min={min} step={step} required onChange={e=>change(Number(e.target.value))}/>;
 const c=value.compute,n=value.network,s=value.storage;
 gpus=gpus??[];
 return <div className={`resource-constraints-editor${compact ? " resource-constraints-editor--compact" : ""}`}>
  {!compact&&<p className="muted">{zh?'额度由该助理在此资源上的所有任务共享。示例值可按需修改。':'All tasks of this assistant share these resource limits. Adjust the suggested values as needed.'}</p>}
  {c&&<>
   {!compact&&<p className="muted">{zh?'修改计算授权会停止受影响的运行任务；新任务使用批准后的额度。':'Changing a compute grant stops affected running tasks. New tasks use the approved limits.'}</p>}
   {!!gpus.length && <>
    <Select multiple label={zh?'GPU 设备':'GPU devices'} value={c.gpu?.device_ids??[]} options={[...gpus.map(g=>({value:g.ID,description:g.ID,label:`${gpuName(g.ID,gpus,locale)}${g.Available?'':zh?' · 不可用':' · unavailable'}`})),...(c.gpu?.device_ids??[]).filter(id=>!gpus.some(g=>g.ID===id)).map(id=>({value:id,description:id,label:`${gpuName(id,gpus,locale)} · ${zh?'超出当前范围':'outside current scope'}`}))]} onValueChange={device_ids=>onChange({compute:{...c,gpu:device_ids.length?{device_ids}:undefined}})} />
    <p className="muted">{zh?'留空表示不允许使用 GPU。所选设备会自动提供给该计算权限下的所有命令；共享访问，不预留显存或保证算力。':'An empty selection grants no GPU access. Selected devices are available to every command using this compute grant. Access is shared; memory and compute capacity are not reserved.'}</p>
   </>}
   {field(zh?'CPU 算力（核）':'CPU capacity (cores)',c.cpu_percent/100,v=>onChange({compute:{...c,cpu_percent:Math.round(v*100)}}),0.01,0.01)}
   {field(zh?'内存上限（MiB）':'Memory limit (MiB)',c.memory_bytes/1048576,v=>onChange({compute:{...c,memory_bytes:Math.round(v*1048576)}}),32)}
   {field(zh?'进程／线程总数':'Total processes / threads',c.processes,v=>onChange({compute:{...c,processes:v}}),8)}
   {field(zh?'内存临时盘 /tmp（MiB）':'RAM disk (/tmp) (MiB)',c.temp_bytes/1048576,v=>onChange({compute:{...c,temp_bytes:Math.round(v*1048576)}}),1,1,zh?'每次执行独立，写入占用计算内存，结束后清空。':'Private to each execution; writes count toward compute memory and are cleared when execution ends.')}
   <Checkbox label={zh?'不限制单次运行时长':'No per-execution duration limit'} checked={c.unlimited_duration} onChange={e=>onChange({compute:{...c,unlimited_duration:e.target.checked,max_duration_seconds:e.target.checked?0:1800}})}/>
   {!c.unlimited_duration&&field(zh?'单次最长运行（分钟）':'Maximum execution duration (minutes)',c.max_duration_seconds/60,v=>onChange({compute:{...c,max_duration_seconds:Math.round(v*60)}}),1/60,1/60)}
  </>}
  {n&&<>
   {field(zh?'出站网络双向流量（GiB）':'Outbound network traffic, both directions (GiB)',n.bytes/1073741824,v=>onChange({network:{...n,bytes:Math.round(v*1073741824)}}),1/1073741824,'any')}
   <Select label={zh?'额度周期':'Quota period'} value={n.period} onValueChange={v=>onChange({network:{...n,period:v}})} options={[{value:'lifetime',label:zh?'累计额度':'Lifetime'},{value:'day',label:zh?'每天':'Daily'},{value:'week',label:zh?'每周':'Weekly'},{value:'month',label:zh?'每月':'Monthly'}]} disabled={lockNetworkPeriod}/>
   <TextField label={zh?'周期时区':'Period timezone'} value={n.timezone} required disabled={lockNetworkPeriod} onChange={e=>onChange({network:{...n,timezone:e.target.value}})}/>
   {field(zh?'并发连接总数':'Total concurrent connections',n.connections,v=>onChange({network:{...n,connections:v}}))}
  </>}
  {s&&<>
   {field(zh?'工作区容量（GiB）':'Workspace capacity (GiB)',s.capacity_bytes/1073741824,v=>onChange({storage:{...s,capacity_bytes:Math.round(v*1073741824)}}),0.001,0.001)}
   {field(zh?'单文件上限（GiB）':'Maximum file size (GiB)',s.max_file_bytes/1073741824,v=>onChange({storage:{...s,max_file_bytes:Math.round(v*1073741824)}}),0.001,0.001)}
   {field(zh?'文件和目录数量上限':'Maximum files and directories',s.max_files,v=>onChange({storage:{...s,max_files:v}}))}
  </>}
 </div>;
}
export function ConstraintsSummary({value,temporary,gpus}:{value?:ResourceConstraints;temporary?:TemporaryConstraints;gpus?:GPUDevice[]|null}){
 const {locale}=useCopy(),zh=locale==='zh';
 if(!value)return null;
 const c=value.compute,n=value.network,s=value.storage;
 const period={lifetime:zh?'累计':'lifetime',day:zh?'天':'day',week:zh?'周':'week',month:zh?'月':'month'};
 return <div className="resource-constraints-summary">
  {c&&<p>{zh?'共享计算额度':'Shared compute quota'} · {c.cpu_percent/100} {zh?'核':'cores'} · {c.memory_bytes/1048576} MiB · {c.processes} {zh?'进程／线程':'processes / threads'}<br/>{zh?'单次最长运行':'Maximum duration'} · {c.unlimited_duration?(zh?'不限时长':'Unlimited'):`${c.max_duration_seconds/60} ${zh?'分钟':'minutes'}`} · {zh?'内存临时盘':'RAM disk'} /tmp {c.temp_bytes/1048576} MiB</p>}
  {c&&<GPUAccessSummary ids={c.gpu?.device_ids} devices={gpus}/> }
  {n&&<p>{n.bytes/1073741824} GiB / {period[n.period]} · {n.connections} {zh?'并发连接':'concurrent connections'}<br/>{n.timezone}</p>}
  {s&&<p>{zh?'工作区容量':'Workspace capacity'} · {s.capacity_bytes/1073741824} GiB · {zh?'单文件':'Per file'} {s.max_file_bytes/1073741824} GiB · {s.max_files} {zh?'个文件':'files'}</p>}
  {temporary&&<><p>{zh?'临时额度有效至':'Temporary quota until'} {new Date(temporary.until).toLocaleString(locale)}</p><ConstraintsSummary gpus={gpus} value={temporary.limits}/></>}
 </div>;
}

export function ResourceQuotaUsage({assistantID,resourceID}:{assistantID:string;resourceID:string}){
 const {locale}=useCopy(),zh=locale==='zh';
 const query=useQuery<{Network?:{UsedBytes:number;ReservedBytes:number;LimitBytes:number;Connections:number;PeriodEnd:string};Compute?:{MemoryBytes:number;Processes:number};Storage?:{Supported:boolean;Reason:string;UsedBytes:number;CapacityBytes:number;Files:number;MaxFiles:number}}>(`/api/resource-usage?assistant_id=${encodeURIComponent(assistantID)}&resource_id=${encodeURIComponent(resourceID)}`);
 if(query.error)return <p className="muted">{zh?'用量暂时不可用':'Usage temporarily unavailable'}</p>;
 const n=query.data?.Network,c=query.data?.Compute,s=query.data?.Storage;
 return <div className="muted">

 {n&&<p>{zh?'已用':'Used'} {(n.UsedBytes/1048576).toFixed(1)} MiB · {zh?'预留':'Reserved'} {(n.ReservedBytes/1048576).toFixed(1)} MiB · {zh?'剩余':'Remaining'} {(Math.max(0,n.LimitBytes-n.UsedBytes-n.ReservedBytes)/1048576).toFixed(1)} MiB<br/>{zh?'当前连接':'Active connections'} {n.Connections}{!n.PeriodEnd.startsWith('0001')&&<><br/>{zh?'下次重置':'Next reset'} {new Date(n.PeriodEnd).toLocaleString(locale)}</>}</p>}
 {c&&<p>{zh?'当前占用':'Current usage'} {(c.MemoryBytes/1048576).toFixed(1)} MiB · {c.Processes} {zh?'进程／线程':'processes / threads'}</p>}
 {s&&<p>{s.Supported?`${(s.UsedBytes/1073741824).toFixed(2)} / ${(s.CapacityBytes/1073741824).toFixed(2)} GiB · ${s.Files} / ${s.MaxFiles}`:zh?'此工作区尚未配置文件系统硬配额。':'Filesystem hard quotas are not configured for this workspace.'}</p>}
 </div>;
}

export function TemporaryConstraintsEditor({kind,base,value,onChange,compact=false,gpus=[]}:{gpus?:GPUDevice[]|null;compact?:boolean;kind:string;base?:ResourceConstraints;value?:TemporaryConstraints;onChange:(v:TemporaryConstraints|undefined)=>void}){
 const {locale}=useCopy(),zh=locale==='zh';if(!base)return null;
 return <div className="resource-quota-group"><Checkbox label={zh?'设置临时额度，到期恢复基础额度':'Use temporary quotas, then restore base limits'} checked={!!value} onChange={e=>onChange(e.target.checked?{limits:structuredClone(base),until:new Date(Date.now()+86400000).toISOString()}:undefined)}/>
 {value&&<><ConstraintsEditor gpus={gpus} compact={compact} kind={kind} value={value.limits} lockNetworkPeriod={!!base.network} onChange={limits=>{if(limits)onChange({...value,limits});}}/><DateTimePicker label={zh?'临时额度到期时间':'Temporary quota expiry'} value={value.until} onValueChange={until=>{if(until)onChange({...value,until});}} min={new Date().toISOString()}/></>}
 </div>;
}
