import type { Assistant } from '../../../api/types';
import { useQuery } from '../../../state';
import { Plans } from './Plans';

export function Tasks({ assistantID, taskID, docked, onClose }: { assistantID: string; taskID?: string; docked?: boolean; onClose: () => void }) {
 const assistants = useQuery<Assistant[]>('/api/assistants', true);
 return <Plans key={assistantID} embedded={{ docked, onClose }} assistantID={assistantID} selectedTaskID={taskID} assistants={assistants.data ?? []} />;
}
