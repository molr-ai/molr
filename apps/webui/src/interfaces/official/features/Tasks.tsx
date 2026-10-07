import { Drawer, useBreakpoint } from '@molr/ui';
import type { Assistant } from '../../../api/types';
import { useCopy, useQuery } from '../../../state';
import { Plans } from './Plans';
// The conversation panel is a scoped view of the same plans, with execution history nested below each plan.
export function Tasks({ assistantID, taskID, docked, onClose }: { assistantID: string; taskID?: string; docked?: boolean; onClose: () => void }) {
 const {t}=useCopy(), mobile=useBreakpoint('mobile'), assistants=useQuery<Assistant[]>('/api/assistants',true);
 return <Drawer className="assistant-plans" docked={docked} open onOpenChange={open=>{if(!open)onClose();}} side={mobile?'bottom':'right'} title={t.nav.plans}>
  <Plans key={assistantID} embedded assistantID={assistantID} selectedTaskID={taskID} assistants={assistants.data ?? []} />
 </Drawer>;
}
