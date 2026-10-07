import { productMessages } from './i18n';
import { Activity, memo, useId, useState } from 'react';
import { Globe, Monitor, RotateCcw, Smartphone, Terminal } from 'lucide-react';
import { Button, useChoiceScroll } from '@molr/ui';
import type { Locale } from './messages';
import { useInterfaceDemo } from './interface-demo';
import { InterfaceWorkspace } from './InterfaceWorkspace';
import { InterfaceTerminal } from './InterfaceTerminal';
import { interfacePlatforms } from './interface-platform';
import './interface-showcase.css';

type Version = 'web' | 'desktop' | 'mobile' | 'cli';
// Activity preserves state and suspends effects. Ignore parent data updates
// while hidden too; activation always renders the latest props.
const WorkspacePreview = memo(InterfaceWorkspace, (previous, next) => previous.active === false && next.active === false);
const TerminalPreview = memo(function TerminalPreview({ locale, demo }: { active: boolean; locale: Locale; demo: ReturnType<typeof useInterfaceDemo> }) {
  return <InterfaceTerminal locale={locale} demo={demo} />;
}, (previous, next) => !previous.active && !next.active);
const versions = [
  { id: 'web', label: 'Web', icon: Globe },
  { id: 'desktop', label: 'Desktop', icon: Monitor },
  { id: 'mobile', label: 'Mobile', icon: Smartphone },
  { id: 'cli', label: 'CLI', icon: Terminal },
] as const;
export function InterfaceShowcase({ locale }: { locale: Locale }) {
  const [version, setVersion] = useState<Version>('web');
  const tabsRef = useChoiceScroll(version);
  const [graphicalPlatform, setGraphicalPlatform] = useState<'web' | 'desktop'>('web');
  const [visited, setVisited] = useState({ mobile: false, cli: false });
  const id = useId();
  const demo = useInterfaceDemo();
  function select(next: Version) {
    setVersion(next);
    if (next === 'web' || next === 'desktop') setGraphicalPlatform(next);
    else setVisited(previous => previous[next] ? previous : { ...previous, [next]: true });
  }
  return <section className="foundation-section interface-showcase" id="interfaces" aria-labelledby="interfaces-title">
    <div className="section-heading"><div><span className="section-number">05 / USER INTERFACE</span><h2 id="interfaces-title">{productMessages[locale].interfaceShowcase.oneMolrFourWaysIn}</h2></div><p>{productMessages[locale].interfaceShowcase.aPersonalAiAssistantYouCanTrustVisibleProgress}</p></div>
    <div className="ui-tabs ui-tabs--line ui-tabs--horizontal">
      <div ref={tabsRef} className="ui-tabs__list" role="tablist" aria-label={productMessages[locale].interfaceShowcase.interfaceVersions}>
        {versions.map(({ id: value, label, icon: Icon }, index) => <button key={value} type="button" role="tab" id={`${id}-${value}`} aria-controls={`${id}-panel`} aria-selected={version === value} tabIndex={version === value ? 0 : -1} onClick={() => select(value)} onKeyDown={event => {
          if (!['ArrowLeft', 'ArrowRight', 'Home', 'End'].includes(event.key)) return;
          event.preventDefault();
          const next = event.key === 'Home' ? 0 : event.key === 'End' ? versions.length - 1 : (index + (event.key === 'ArrowRight' ? 1 : -1) + versions.length) % versions.length;
          select(versions[next].id);
          document.getElementById(`${id}-${versions[next].id}`)?.focus({ preventScroll: true });
        }}><span aria-hidden="true"><Icon size={16} /></span>{label}</button>)}
      </div>
      <div id={`${id}-panel`} className="ui-tabs__panel" role="tabpanel" aria-labelledby={`${id}-${version}`} tabIndex={0}>
        <div hidden={version !== 'web' && version !== 'desktop'}><Activity mode={version === 'web' || version === 'desktop' ? 'visible' : 'hidden'}><WorkspacePreview active={version === 'web' || version === 'desktop'} platform={interfacePlatforms[graphicalPlatform]} locale={locale} demo={demo} /></Activity></div>
        <div hidden={version !== 'mobile'} className="ip-mobile-stage">{visited.mobile && <Activity mode={version === 'mobile' ? 'visible' : 'hidden'}><WorkspacePreview active={version === 'mobile'} layout="mobile" platform={interfacePlatforms.mobile} locale={locale} demo={demo} /></Activity>}</div>
        <div hidden={version !== 'cli'}>{visited.cli && <Activity mode={version === 'cli' ? 'visible' : 'hidden'}><TerminalPreview active={version === 'cli'} locale={locale} demo={demo} /></Activity>}</div>
      </div>
    </div>
    <div className="interface-showcase__caption"><p>{productMessages[locale].interfaceShowcase.tryDelegateScopeExecuteVerifyExploreBudgetLimitsRevoked}</p><div className="interface-showcase__demo-actions"><Button size="sm" variant="secondary" onClick={demo.resetUnread} title={productMessages[locale].interfaceShowcase.markExistingAssistantMessagesAsUnreadAgain}><RotateCcw size={14} />{productMessages[locale].interfaceShowcase.resetUnreadBadges}</Button><span>{productMessages[locale].interfaceShowcase.interactivePrototypeSampleDataSavedInThisBrowser}</span></div></div>
  </section>;
}
