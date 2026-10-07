import { productMessages } from './i18n';
import { useEffect, useId, useState } from 'react';
import { ArrowLeft } from 'lucide-react';
import { useBreakpoint, Button, Drawer } from '@molr/ui';
import type { InterfaceDemo } from './interface-demo';
import { PlanPanel } from './PlanPanel';
import { planMessages } from './plan-messages';
import type { Locale } from './messages';
import { WorkDetail } from './AssistantPanels';

export function TaskDrawer({ demo, locale, mobile, taskId, onClose, onSelect, onDiscuss, openResource, openGrants }: { demo: InterfaceDemo; locale: Locale; mobile: boolean; taskId: string | null; onClose: () => void; onSelect: (id: string) => void; onDiscuss: (id: string) => void; openResource: (id: string) => void; openGrants: () => void }) {
  const t = productMessages[locale];
  const narrow = useBreakpoint('mobile');
  const [amount, setAmount] = useState('2'); const [cancel, setCancel] = useState(false); const id = useId();
  useEffect(() => { setCancel(false); }, [taskId]);
  const [actions,setActions]=useState<HTMLDivElement|null>(null);
  const task = demo.works.find(item => item.id === taskId);
  return <Drawer open={taskId !== null} onOpenChange={open => { if (!open) onClose(); }} side={mobile || narrow ? 'bottom' : 'right'} title={task ? (locale === 'zh' ? task.title : task.titleEn) : planMessages[locale].title} description={demo.activeAssistant.name} closeLabel={t.taskDrawer.closeTasks} className="task-drawer ip-shell">
    {taskId !== null && (task ? <><Button variant="ghost" size="sm" onClick={() => onSelect('list')}><ArrowLeft size={15} />{t.taskDrawer.allTasks}</Button><WorkDetail key={task.id} work={task} demo={demo} locale={locale} onDiscuss={() => onDiscuss(task.id)} openResource={openResource} openGrants={openGrants} />
      {!['complete', 'cancelled'].includes(task.status) && <section className="drawer-task-controls"><h4>{t.taskDrawer.taskControls}</h4><div className="ip-actions"><Button size="sm" disabled={!demo.connected || !demo.grants[task.resource] || task.status === 'failed'} onClick={() => demo.toggleWork(task.id)}>{['running', 'verifying'].includes(task.status) ? t.common.pauseTask : t.common.resumeTask}</Button><Button size="sm" variant="ghost" onClick={() => setCancel(true)}>{t.taskDrawer.cancelTask}</Button>{task.status === 'failed' && <Button size="sm" onClick={() => demo.recoverWork(task.id)}>{t.taskDrawer.recoverCheckpoint}</Button>}</div>
        {task.status === 'budget' && <form className="assistant-inline-form" onSubmit={e => { e.preventDefault(); demo.setBudget(task.id, Number(amount)); }}><label htmlFor={id}>{t.taskDrawer.taskBudgetCny}</label><input autoComplete="off" id={id} type="number" min={Math.max(.2, task.spent)} max="100" step="0.2" required value={amount} onChange={e => setAmount(e.target.value)} /><Button size="sm" type="submit">{t.taskDrawer.updateBudget}</Button></form>}
        {cancel && <div className="ip-confirm"><p>{t.taskDrawer.cancelFutureExecutionAndKeepExistingDeliverables}</p><Button size="sm" onClick={() => { demo.cancelWork(task.id); setCancel(false); }}>{t.taskDrawer.confirmCancel}</Button><Button variant="ghost" size="sm" onClick={() => setCancel(false)}>{t.common.back}</Button></div>}
      </section>}
    </> : <><div ref={setActions} className="ip-actions" /><PlanPanel demo={demo} locale={locale} key={demo.activeAssistantId} assistantId={demo.activeAssistantId} actionTarget={actions} onOpenExecution={onSelect} /></>)}
  </Drawer>;
}
