import { useMemo, useRef, useState } from 'react';
import { createRoot } from 'react-dom/client';
import { DataProvider, useData } from '../../src/state';
import { useTrajectory } from '../../src/interfaces/official/features/use-trajectory';

function Probe({ assistant }: { assistant: string }) {
  const trajectory = useTrajectory(assistant), projections = useRef(0), { invalidateTopics } = useData();
  const projection = useMemo(() => ++projections.current, [trajectory.runs, trajectory.entries]);
  return <>
    <button onClick={trajectory.reload}>Refresh</button>
    <button onClick={trajectory.loadOlder}>Older</button>
    <button onClick={() => invalidateTopics(['debug'])}>Reconnect</button>
    <output data-testid="snapshot">{JSON.stringify({ runs: trajectory.runs.map(run => [run.ID, run.State]), entries: trajectory.entries.map(entry => `${entry.RunID}:${entry.Step.Sequence}`), loading: trajectory.loading, older: trajectory.older, error: !!trajectory.error, projection })}</output>
  </>;
}
function Fixture() {
  const [assistant, setAssistant] = useState('assistant');
  return <DataProvider><button onClick={() => setAssistant('other')}>Other assistant</button><Probe assistant={assistant} /></DataProvider>;
}
createRoot(document.getElementById('root')!).render(<Fixture />);
