import { useMutation } from '../../../use-mutation';
import { useErrorToast } from '../../../error-toasts';
import { useState } from 'react';
import { PlanDescription } from './PlanDescription';
import { ModalPresence, Badge, Button, Card, Textarea } from '@molr/ui';
import type { Task, Revision, Usage, PlanExecution } from '@molr/sdk';
import { useCopy, useQuery } from '../../../state';
import { Confirm, EditDialog, lines, useAction } from '../components/common';
import { MoneyText } from '../components/Money';
import { Attachment } from './Chat';
import { DreamConflictNotice } from './DreamConflictNotice';
export function ExecutionDetails({ task, execution }: { task: Task; execution?: PlanExecution }) {
 const { t } = useCopy(), action = useAction(); const [revise,setRevise]=useState(false),[cancel,setCancel]=useState(false);
 useErrorToast(action.error);
  const cancelTask = useMutation([task.ID, cancel]);
  const changeTask = useMutation([task.ID, task.State]);
  return <section className="plan-execution-details">{task.Plan?.Kind==='schedule' && task.OccurrenceAt && !['executed','completed','failed','canceled','interrupted'].includes(execution?.State || task.State) && <DreamConflictNotice assistantID={task.AssistantID} startAt={task.OccurrenceAt} />}{!execution && <Badge>{t.tasks[task.State as 'pending'] ?? task.State}</Badge>}{(execution?.WaitingReason || task.WaitingReason) && <p>{execution?.WaitingReason || task.WaitingReason}</p>}
 <div className="form-actions">{!['executed','completed','failed','canceled','interrupted'].includes(execution?.State || task.State) && <><Button size="sm" disabled={action.busy} onClick={() => void action.run(() => changeTask(`/api/tasks/${task.State === 'paused' ? 'resume' : 'pause'}`, { ID: task.ID }, task.Version))}>{task.State === 'paused' ? t.tasks.resume : t.tasks.pause}</Button><Button size="sm" variant="ghost" onClick={() => setCancel(true)}>{t.tasks.cancel}</Button></>}<Button size="sm" variant="secondary" disabled={(execution?.State || task.State) === 'running'} onClick={() => setRevise(true)}>{t.tasks.revise}</Button></div>
 {task.ArtifactIDs?.map(id => <Attachment key={id} id={id} />)}<Revisions taskID={task.ID} />
 <ModalPresence>{revise && <RevisionEditor task={task} onClose={() => setRevise(false)} />}</ModalPresence><ModalPresence>{cancel && <Confirm title={t.tasks.cancel} description={t.tasks.cancelHelp} danger onClose={() => setCancel(false)} onConfirm={() => cancelTask('/api/tasks/cancel', { ID: task.ID }, task.Version)} />}</ModalPresence>
 </section>;
}
function Revisions({ taskID }: { taskID: string }) {
  const { t } = useCopy(), query = useQuery<Revision[]>(`/api/tasks/${taskID}/revisions`, true);
  useErrorToast(query.error, query.reload);
  const revisions=query.data?.filter(row=>row.Number>1 || row.AcceptanceCriteria?.length || row.ArtifactIDs?.length) ?? [];
  if(!revisions.length)return null;
  return <details className="plans-run-disclosure"><summary>{t.tasks.revisions}</summary>{revisions.map(row => <Card key={row.Number}><h4>v{row.Number}</h4>{row.Number>1 && <PlanDescription text={row.Instructions}/>}<ul>{row.AcceptanceCriteria?.map((text, index) => <li key={index}>{text}</li>)}</ul>{row.ArtifactIDs?.map(id => <Attachment id={id} key={id} />)}</Card>)}</details>;
}
function RevisionEditor({ task, onClose }: { task: Task; onClose: () => void }) {
  const { t } = useCopy(), [instructions, setInstructions] = useState(''), [criteria, setCriteria] = useState('');
  const reviseTask = useMutation([task.ID, instructions, criteria]);
  return <EditDialog title={t.tasks.revise} onClose={onClose} onSubmit={() => reviseTask('/api/tasks/revise', { ID: task.ID, Instructions: instructions.trim(), AcceptanceCriteria: lines(criteria) }, task.Version)}><Textarea label={t.tasks.instructions} value={instructions} onChange={event => setInstructions(event.target.value)} required /><Textarea label={t.tasks.criteria} value={criteria} onChange={event => setCriteria(event.target.value)} required /></EditDialog>;
}

export function ExecutionSpend({ task }: { task: Task }) {
  const { t } = useCopy();
  const usage = useQuery<Usage>(`/api/usage?assistant_id=${task.AssistantID}&task_id=${task.ID}`);
  useErrorToast(usage.error, usage.reload);
  return usage.data ? <div><dt>{t.settings.spent}</dt><dd><MoneyText value={usage.data.Spent} originals={usage.data.OriginalSpent} /></dd></div> : null;
}
