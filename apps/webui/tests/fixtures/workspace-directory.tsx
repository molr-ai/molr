import { useState } from 'react';
import { createRoot } from 'react-dom/client';
import { useWorkspaceDirectory } from '../../src/interfaces/official/features/workspace-files/useWorkspaceDirectory';

function Directory() {
  const { listing, loading, error, loadList } = useWorkspaceDirectory('reader', '.', false);
  return <><button onClick={() => void loadList()}>Reload</button><output>{loading ? 'Loading' : error ? 'Error' : listing?.Entries.map(entry => entry.Name).join(',')}</output></>;
}
function Fixture() {
  const [mounted, setMounted] = useState(true);
  return <><button onClick={() => setMounted(false)}>Unmount</button>{mounted && <Directory />}</>;
}
createRoot(document.getElementById('root')!).render(<Fixture />);
