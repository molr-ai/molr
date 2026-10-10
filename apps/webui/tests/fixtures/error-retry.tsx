import { Profiler, StrictMode, useState } from 'react';
import { createRoot } from 'react-dom/client';
import { Button } from '@molr/ui';
import '@molr/ui/styles.css';
import { LocaleProvider } from '../../src/state';
import { ErrorNoticeProvider } from '../../src/error-notices';
import { ErrorNotice } from '../../src/interfaces/official/components/common';
import { APIError } from '../../src/api/client';

declare global { interface Window { retryNoticeCommits: number; retryNoticeWakeups: number } }
window.retryNoticeCommits = 0;
function Example() {
 const [error,setError]=useState<APIError|null>(null),[retry,setRetry]=useState(true),[mounted,setMounted]=useState(true),[attempts,setAttempts]=useState(0);
 const limit=(delay:number)=>{setMounted(true);setError(new APIError('rate_limited','',429,Date.now()+delay));};
 return <main style={{maxWidth:640,margin:'32px auto',padding:16}}>
  <h1>Error retry countdown</h1>
  <p>This fixture renders the actual error notice with a simulated rate limit. It does not send requests.</p>
  <div style={{display:'flex',gap:8,flexWrap:'wrap'}}>
   <Button onClick={()=>limit(2500)}>Wait 2.5 seconds</Button>
   <Button onClick={()=>limit(5000)}>Wait 5 seconds</Button>
   <Button onClick={()=>limit(-1000)}>Expired deadline</Button>
   <Button onClick={()=>setError(null)}>Clear error</Button>
   <Button onClick={()=>setRetry(value=>!value)}>Toggle retry action</Button>
   <Button onClick={()=>setMounted(value=>!value)}>Toggle notice</Button>
  </div>
  {mounted&&<Profiler id="notice" onRender={()=>{window.retryNoticeCommits++;}}><ErrorNoticeProvider><ErrorNotice error={error} retry={retry?()=>setAttempts(value=>value+1):undefined}/></ErrorNoticeProvider></Profiler>}
  <p>Retry attempts: <output aria-label="Retry attempts">{attempts}</output></p>
 </main>;
}
createRoot(document.getElementById('root')!).render(<StrictMode><LocaleProvider><Example/></LocaleProvider></StrictMode>);
