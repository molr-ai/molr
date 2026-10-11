import { useErrorToast } from '../../../error-toasts';
import { ChevronRight } from 'lucide-react';
import type { DreamRun } from '../../../api/types';
import { useCopy, useQuery } from '../../../state';
import { Loading } from '../components/common';

export function DreamInspection({ id }: { id: string }) {
  const { t, locale } = useCopy(), c = t.workbench.dream;
  const query = useQuery<DreamRun>(`/api/dreams/runs/${id}`), run = query.data;
  const state = (value: string) => (c.states as Record<string, string>)[value] ?? value;
  const changes = run?.FileChanges ?? [];
  const zh = locale.startsWith('zh');
  const labels: Record<string, string> = { file_write: zh ? '写入文件' : 'Write file', file_edit: zh ? '编辑文件' : 'Edit file', file_move: zh ? '移动文件' : 'Move file', file_delete: zh ? '删除文件' : 'Delete file' };
  useErrorToast(query.error, query.reload);
  return <section className="workbench-content-block dream-inspection">
    <div className="workbench-section-heading"><h3>{c.title}</h3></div>
    {query.loading && !run && <Loading />}
    {run && <div className="workbench-response-content">
      {run.Summary && <div className="workbench-prose">{run.Summary}</div>}
      {changes.map(change => <div className="workbench-disclosure-body" key={change.OperationID}>
        <strong>{labels[change.Tool] ?? change.Tool}</strong>
        <p className="memory-path"><code>{change.Path}</code>{change.ToPath && <> → <code>{change.ToPath}</code></>}</p>
      </div>)}
      {!changes.length && <p className="workbench-empty">{['completed', 'skipped', 'up_to_date', 'undone'].includes(run.State) ? (zh ? '本批没有文件变更。' : 'No file changes in this batch.') : c.unfinished}</p>}
      <details className="workbench-disclosure">
        <summary><ChevronRight size={14} aria-hidden="true" /><h3>{c.batch}</h3></summary>
        <div className="workbench-disclosure-body">
          <p className="workbench-prose">{run.Day} · {c.batch} {run.Batch || 1} · {state(run.State)}{run.SessionState ? ` / ${state(run.SessionState)}` : ''}</p>
          {run.Deadline && <p className="workbench-prose">{c.deadline}: {new Date(run.Deadline).toLocaleString(locale)}</p>}
          <p className="workbench-prose">{run.Sources.length} {c.sources} · {run.Timezone}{run.Backfill ? ` · ${c.backfill}` : ''}</p>
          {run.FailureCode && <p className="workbench-prose">{c.endReason}: {run.FailureCode}</p>}
        </div>
      </details>
      <a href="#/account/memory">{c.view}</a>
    </div>}
  </section>;
}
