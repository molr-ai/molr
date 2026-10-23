import { StrictMode, useRef, useState } from 'react';
import { createRoot } from 'react-dom/client';
import { DataProvider, useConnection, useData, useQuery, useTopicVersion } from '../../src/state';

function Topic({ topic }: { topic: string }) {
  const revision = useTopicVersion(topic), renders = useRef(0);
  return <output data-testid={topic} data-renders={++renders.current}>{revision}</output>;
}
function Query({ id }: { id: string }) {
  const { data, loading } = useQuery<{ revision: number }>(`/api/assistants/${id}/status`, false, 0), renders = useRef(0);
  return <output data-testid={`query:${id}`} data-renders={++renders.current} data-loading={loading}>{data?.revision}</output>;
}
function Controls() {
  const { invalidate, invalidateTopics } = useData(), renders = useRef(0);
  return <div data-testid="controls" data-renders={++renders.current}>
    <button onClick={() => invalidateTopics(['messages:a'])}>Conversation A</button>
    <button onClick={() => invalidateTopics(['messages'])}>All conversations</button>
    <button onClick={() => invalidateTopics(['status:a'])}>Assistant A</button>
    <button onClick={invalidate}>All data</button>
  </div>;
}
function Connection() {
  const { connection } = useConnection(), renders = useRef(0);
  return <output data-testid="connection" data-renders={++renders.current}>{connection}</output>;
}
function Fixture() {
  const [account, setAccount] = useState(1);
  return <><button onClick={() => setAccount(value => value + 1)}>Switch account</button><DataProvider key={account}>
    <Controls /><Connection />
    {['messages:a', 'messages:b', 'messages', 'plans', 'debug'].map(topic => <Topic key={topic} topic={topic} />)}
    <Query id="a" /><Query id="b" />
  </DataProvider></>;
}
createRoot(document.getElementById('root')!).render(<StrictMode><Fixture /></StrictMode>);
