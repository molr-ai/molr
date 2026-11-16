import { useId, useLayoutEffect, useRef, useState } from 'react';
import { useCopy } from '../../../state';
import { planMessages } from '../../../plan-messages';

export function PlanDescription({text}:{text:string}) {
  const {locale}=useCopy(),t=planMessages[locale];
  const id=useId(),content=useRef<HTMLDivElement>(null);
  const [expanded,setExpanded]=useState(false),[overflow,setOverflow]=useState(false);
  useLayoutEffect(()=>{
    const node=content.current;
    if(!node)return;
    let active=true;
    const measure=()=>{
      if(!active)return;
      const lineHeight=Number.parseFloat(getComputedStyle(node).lineHeight);
      setOverflow(node.scrollHeight>Math.ceil(lineHeight*6)+1);
    };
    setExpanded(false);
    measure();
    const observer=new ResizeObserver(measure);
    observer.observe(node);
    document.fonts.addEventListener('loadingdone',measure);
    void document.fonts.ready.then(measure);
    return()=>{active=false;observer.disconnect();document.fonts.removeEventListener('loadingdone',measure);};
  },[text]);
  return <>
    <div ref={content} id={id} className="plans-description-text" data-expanded={expanded}>{text}</div>
    {overflow && <button type="button" className="plans-description-toggle" aria-expanded={expanded} aria-controls={id} onClick={()=>setExpanded(value=>!value)}>{expanded?t.collapseDescription:t.expandDescription}</button>}
  </>;
}
