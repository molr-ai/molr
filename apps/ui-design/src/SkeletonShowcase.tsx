import { useState } from 'react';
import { FileText, UserRound } from 'lucide-react';
import { Skeleton, Switch } from '@molr/ui';
import { ComponentExample as Example } from './ComponentExample';
import { libraryMessages } from './library-messages';
import type { Locale } from './messages';
import './skeleton-showcase.css';

export function SkeletonShowcase({ locale }: { locale: Locale }) {
  const [loading, setLoading] = useState(true);
  const t = libraryMessages[locale];
  const copy = t.skeletonDemo;
  return <div className="skeleton-showcase">
    <div className="skeleton-showcase__controls">
      <Switch label={t.skeletonToggle} checked={loading} onChange={event => setLoading(event.target.checked)} />
      <p className="component-copy">{copy.policy}</p>
    </div>
    <div className="component-library__grid">
      <Example title={copy.shapes} description={copy.shapesNote} wide>
        <div className="skeleton-showcase__shapes">
          <div><p className="control-label">{copy.line}</p><div className="skeleton-showcase__lines"><Skeleton width="75%" height={18} /><Skeleton /><Skeleton width="55%" /></div></div>
          <div><p className="control-label">{copy.circle}</p><Skeleton shape="circle" width={44} /></div>
          <div><p className="control-label">{copy.block}</p><Skeleton shape="block" /></div>
        </div>
      </Example>
      <Example title={copy.list} description={copy.listNote}>
        <div className="skeleton-showcase__list" role="status" aria-busy={loading} aria-label={`${copy.list}: ${loading ? t.skeleton : t.ready}`}>
          {copy.people.map((name, index) => <div className="skeleton-showcase__row" key={index}>
            {loading ? <Skeleton shape="circle" width={44} /> : <span className="skeleton-showcase__avatar"><UserRound size={20} aria-hidden="true" /></span>}
            <div className="skeleton-showcase__text">{loading ? <><Skeleton width={`${65 - index * 10}%`} height={16} /><Skeleton width="85%" /></> : <><strong>{name}</strong><p>{copy.detail}</p></>}</div>
          </div>)}
        </div>
      </Example>
      <Example title={copy.card} description={copy.cardNote}>
        <div className="skeleton-showcase__card" role="status" aria-busy={loading} aria-label={`${copy.card}: ${loading ? t.skeleton : t.ready}`}>
          {loading ? <Skeleton shape="block" height={144} /> : <div className="skeleton-showcase__cover"><FileText size={36} aria-hidden="true" /></div>}
          <div className="skeleton-showcase__text">{loading ? <><Skeleton width="60%" height={16} /><Skeleton width="90%" /></> : <><strong>{copy.resource}</strong><p>{copy.resourceDetail}</p></>}</div>
        </div>
      </Example>
    </div>
  </div>;
}
