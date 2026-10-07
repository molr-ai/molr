import { ChevronRight } from 'lucide-react';
import type { DreamRun } from '../../../api/types';
import { useCopy, useQuery } from '../../../state';
import { ErrorNotice, Loading } from '../components/common';

export function DreamInspection({ id }: { id: string }) {
  const { t, locale } = useCopy(), c = t.workbench.dream;
  const query = useQuery<DreamRun>(`/api/dreams/runs/${id}`), run = query.data;
  const state = (value: string) => (c.states as Record<string, string>)[value] ?? value;
  const decisions = run?.Decisions ?? [];
  return <section className="workbench-content-block dream-inspection">
    <div className="workbench-section-heading"><h3>{c.title}</h3></div>
    <ErrorNotice error={query.error} retry={query.reload} />
    {query.loading && !run && <Loading />}
    {run && <div className="workbench-response-content">
      {run.Summary && <div className="workbench-prose">{run.Summary}</div>}
      {decisions.some(decision => decision.Replaces && decision.Action === 'pending') && <p className="workbench-section-note">{c.pendingHelp}</p>}
      {decisions.map((decision, index) => <details className="workbench-disclosure" key={index} open>
        <summary><ChevronRight size={14} aria-hidden="true" /><h3>{decision.Action === 'discarded' ? c.discarded : decision.Replaces ? c.replacement : decision.Action === 'active' ? c.added : c.pending}</h3></summary>
        <div className="workbench-disclosure-body">
          {decision.Replaces && <><span className="muted">{c.before}</span><div className="workbench-prose">{decision.PreviousText || decision.Replaces.Path}</div><span className="muted">{c.after}</span></>}
          <div className="workbench-prose">{decision.Text}</div>
          <small className="muted">{(c.reasons as Record<string, string>)[decision.Reason] ?? decision.Reason}</small>
        </div>
      </details>)}
      {!decisions.length && <p className="workbench-empty">{['completed', 'skipped', 'up_to_date', 'undone'].includes(run.State) ? c.empty : c.unfinished}</p>}
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
