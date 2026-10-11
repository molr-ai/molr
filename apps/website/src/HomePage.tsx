import { useState, type CSSProperties, type ReactNode } from 'react';
import { Logo, paletteBrandColors, useTheme } from '@molr/ui';
import { homeMessages, type HomeCopy } from './home-content';
import { localizedDocPath } from './doc-locale';
import { siteLinks } from './site-links';
import { GithubMark } from './GithubMark';
import type { Locale } from './site-content';
import { SiteHeader } from './SiteHeader';
import { InstallSection } from './InstallSection';

// Reuse the UI Design palette values; decorative colors stay local to the homepage.
const brandColors = Object.fromEntries(
  Object.entries(paletteBrandColors).flatMap(([palette, colors]) =>
    Object.entries(colors).map(([role, value]) => [`--home-${palette}-${role}`, value]),
  ),
) as CSSProperties;

function Icon({ name, className = '' }: { name: string; className?: string }) {
  const paths: Record<string, ReactNode> = {
    arrow: <path d="M5 12h14m-5-5 5 5-5 5" />,
    download: <><path d="M12 3v12m-4-4 4 4 4-4M5 16v4h14v-4" /></>,
    book: <><path d="M12 5v16M3 3h5a4 4 0 0 1 4 4 4 4 0 0 1 4-4h5v16h-5a4 4 0 0 0-4 2 4 4 0 0 0-4-2H3V3Z" /></>,
    sun: <><circle cx="12" cy="12" r="4" /><path d="M12 2v2m0 16v2M2 12h2m16 0h2M5 5l1.5 1.5m11 11L19 19M5 19l1.5-1.5m11-11L19 5" /></>,
    moon: <path d="M20 15.5A9 9 0 0 1 8.5 4a9 9 0 1 0 11.5 11.5Z" />,
    language: <><path d="m5 8 6 6" /><path d="m4 14 6-6 2-3" /><path d="M2 5h12" /><path d="M7 2h1" /><path d="m22 22-5-10-5 10" /><path d="M14 18h6" /></>,
    chat: <path d="M20 11a8 8 0 0 1-8 8H4l1-4a8 8 0 1 1 15-4Z" />,
    plan: <><rect x="4" y="5" width="16" height="16" rx="3" /><path d="M8 3v4m8-4v4M4 11h16m-12 4h3m3 0h2" /></>,
    folder: <path d="M3 7V5h6l2 3h10v12H3V7Z" />,
    check: <path d="m5 12 4 4L19 6" />,
    file: <><path d="M14 3H5v18h14V8l-5-5Zm0 0v6h5M8 13h8m-8 4h6" /></>,
    shield: <><path d="m12 3 8 3v6c0 5-8 9-8 9s-8-4-8-9V6l8-3Z" /><path d="m8 12 3 3 5-5" /></>,
    plus: <path d="M12 5v14M5 12h14" />,
    model: <><rect x="6" y="6" width="12" height="12" rx="3" /><path d="M9 2v4m6-4v4M9 18v4m6-4v4M2 9h4m-4 6h4m12-6h4m-4 6h4" /></>,
    server: <><rect x="4" y="3" width="16" height="7" rx="2" /><rect x="4" y="14" width="16" height="7" rx="2" /><path d="M8 6.5h.01M8 17.5h.01M12 6.5h4m-4 11h4M12 10v4" /></>,
    clock: <><circle cx="12" cy="12" r="9" /><path d="M12 7v5l3 2" /></>,
    devices: <><path d="M12 17H3V4h16v4M5 21h6m-4-4v4" /><rect x="14" y="10" width="7" height="11" rx="1.5" /><path d="M17.5 18h.01" /></>,
    settings: <><path d="M4 7h16M4 17h16" /><circle cx="9" cy="7" r="3" /><circle cx="15" cy="17" r="3" /></>,
  };
  return <svg className={`home-icon ${className}`} width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">{paths[name]}</svg>;
}

function Destination({ locale, destination, children, hint, className = '' }: { locale: Locale; destination: keyof typeof siteLinks; children: ReactNode; hint: string; className?: string }) {
  const url = siteLinks[destination];
  return url
    ? <a className={className} href={localizedDocPath(url, locale)}>{children}</a>
    : <a className={`${className} home-placeholder`} role="link" aria-disabled="true" title={hint}>{children}</a>;
}

function PlanPreview({ copy }: { copy: HomeCopy }) {
  return <div className="home-plan-preview">
    <div className="home-preview-heading"><div><strong>{copy.week}</strong><p>{copy.planSub}</p></div><Icon name="plan" /></div>
    <div className="home-preview-segments"><span className="selected">{copy.list}</span><span>{copy.calendar}</span></div>
    <div className="home-plan-day">{copy.today}<span>3</span></div>
    {copy.planItems.map((item, index) => <div className="home-plan-item" key={item}>
      <span className={`home-task-dot ${index === 0 ? 'is-done' : ''}`}>{index === 0 && <Icon name="check" />}</span>
      <div><strong>{item}</strong><small>{copy.times[index]} <span>·</span> {index === 2 ? copy.personal : copy.assistant}</small></div>
      <span className={`home-status ${index === 0 ? 'is-done' : ''}`}>{copy.states[index]}</span>
    </div>)}
  </div>;
}

function StorySection({ id, label, heading, intro, points, flip = false, children }: {
  id: string; label: string; heading: string; intro: string;
  points: ReadonlyArray<{ title: string; body: string }>; flip?: boolean; children: ReactNode;
}) {
  return <section className={`home-story home-container${flip ? ' home-story-flip' : ''}`} aria-labelledby={`${id}-title`}>
    <div className="home-story-copy">
      <p className="home-section-label">{label}</p><h2 id={`${id}-title`}>{heading}</h2><p className="home-story-intro">{intro}</p>
      <div className="home-story-points">{points.map(point => <div key={point.title}><h3>{point.title}</h3><p>{point.body}</p></div>)}</div>
    </div>
    {children}
  </section>;
}

function StoryScene({ icon, title, detail, children }: {
  icon: string; title: string; detail: string; children: ReactNode;
}) {
  return <figure className="home-story-art">
    <div className="home-story-window">
      <div className="home-story-heading"><span className="home-flow-icon"><Icon name={icon} /></span><div><strong>{title}</strong><p>{detail}</p></div></div>
      <div className="home-story-body">{children}</div>
    </div>
  </figure>;
}

function WorkflowScene({ copy }: { copy: HomeCopy }) {
  return <StoryScene icon="server" title={copy.environmentName} detail={copy.environmentLocation}>
    <div className="home-story-resources">{copy.environmentDevices.map(device => <span key={device.icon}><Icon name={device.icon} />{device.name}</span>)}</div>
    <ol className="home-story-flow">{copy.workflowSteps.map(step => <li key={step.icon}>
      <span className="home-flow-icon"><Icon name={step.icon} /></span><div><strong>{step.title}</strong><p>{step.body}</p></div>
    </li>)}</ol>
  </StoryScene>;
}

function ContinuityScene({ copy }: { copy: HomeCopy }) {
  const [asked, working, done] = copy.continuitySteps;
  const step = (item: typeof asked) => <div className="home-device-step" key={item.time}>
    <span className="home-flow-icon"><Icon name={item.icon} /></span>
    <div><div className="home-flow-title"><strong>{item.title}</strong><span>{item.time}</span></div><p>{item.body}</p></div>
  </div>;
  return <StoryScene icon="clock" title={copy.continuityTask} detail={copy.continuityTaskDetail}>
    <div className="home-devices">
      <div className="home-device-desktop">{step(asked)}{step(working)}</div>
      <div className="home-device-phone"><span className="home-device-notch" aria-hidden="true" />{step(done)}</div>
    </div>
  </StoryScene>;
}

function FlowScene({ icon, title, detail, steps }: {
  icon: string; title: string; detail: string;
  steps: ReadonlyArray<{ icon: string; title: string; body: string }>;
}) {
  return <StoryScene icon={icon} title={title} detail={detail}>
    <ol className="home-story-flow">{steps.map(step => <li key={step.title}>
      <span className="home-flow-icon"><Icon name={step.icon} /></span><div><strong>{step.title}</strong><p>{step.body}</p></div>
    </li>)}</ol>
  </StoryScene>;
}

function ControlScene({ copy }: { copy: HomeCopy }) {
  return <StoryScene icon="shield" title={copy.assistant} detail={copy.controlSceneDetail}>
    <div className="home-control-plan"><span className="home-flow-icon"><Icon name="plan" /></span><div><strong>{copy.continuityTask}</strong><p>{copy.times[0]} · {copy.assistant}</p></div><span className="home-status">{copy.states[1]}</span></div>
    <div className="home-resource-row"><span className="home-flow-icon"><Icon name="folder" /></span><div><strong>{copy.folder}</strong><small>{copy.localFolder}</small></div><span className="home-scope">{copy.readOnly}</span></div>
    <div className="home-resource-row"><span className="home-flow-icon"><Icon name="model" /></span><div><strong>{copy.model}</strong><small>{copy.modelScope}</small></div><Icon name="check" /></div>
    <div className="home-budget"><div><strong>{copy.budget}</strong><span>{copy.budgetPeriod}</span></div><div className="home-budget-amount"><span><b>$2.40</b><small>{copy.budgetUsed}</small></span><span><b>$10.00</b><small>{copy.budgetLimit}</small></span></div><div className="home-budget-bar"><span /></div><small>{copy.budgetFoot}</small></div>
  </StoryScene>;
}

function ProductPreview({ copy }: { copy: HomeCopy }) {
  const [scene, setScene] = useState(0);
  const { theme } = useTheme();
  return <div className="home-product-block">
    <figure className="home-product-figure">
      <div className="home-product" role="img" aria-label={`${copy.previewLabel}: ${copy.scenes[scene]}`}>
        <aside className="home-product-sidebar">
          <Logo variant={theme === 'dark' ? 'light' : 'dark'} />
          <div className="home-product-nav">{[copy.assistants, copy.plans, copy.resources].map((label, index) => <div className={scene === index ? 'active' : ''} key={label}><Icon name={['chat', 'plan', 'folder'][index]} />{label}</div>)}</div>
          <div className="home-assistant-list"><small>{copy.assistants}</small><div><span className="home-avatar">M</span>{copy.assistant}</div><div><span className="home-avatar muted">R</span>{copy.otherAssistant}</div><p><Icon name="plus" />{copy.newAssistant}</p></div>
          <div className="home-product-settings"><Icon name="settings" />{copy.settings}</div>
        </aside>
        <div className="home-product-main">
          {scene === 0 ? <><div className="home-chat-header"><span className="home-avatar">M</span><div><strong>{copy.assistant}</strong><small>{copy.assistantDetail}</small></div><span className="home-chat-ellipsis">···</span></div>
            <div className="home-chat-content"><div className="home-chat-date">{copy.today}</div><div className="home-user-message">{copy.userMessage}</div><div className="home-attachment"><Icon name="file" />{copy.attachment}</div><div className="home-assistant-message"><span className="home-avatar">M</span><div><small className="home-tool-result"><Icon name="check" />{copy.fileRead}</small><p>{copy.reply}</p><ol>{copy.tasks.map(task => <li key={task}>{task}</li>)}</ol><p>{copy.replyEnd}</p></div></div></div>
            <div className="home-chat-composer"><span>{copy.composer}</span><div><Icon name="plus" /><span><Icon name="arrow" /></span></div></div></>
            : scene === 1 ? <PlanPreview copy={copy} />
            : <div className="home-resources-preview"><div className="home-preview-heading"><div><strong>{copy.resourceTitle}</strong><p>{copy.resourceSub}</p></div><Icon name="folder" /></div><div className="home-preview-segments"><span className="selected">{copy.modelType}</span><span>{copy.storageType}</span></div><div className="home-model-card"><span className="home-resource-icon"><Icon name="model" /></span><strong>OpenAI-compatible</strong><span className="home-status is-done">{copy.connected}</span></div><div className="home-model-card"><span className="home-resource-icon"><Icon name="folder" /></span><strong>{copy.folder}</strong><small>{copy.localFolder}</small><span className="home-status is-done">{copy.connected}</span></div></div>}
        </div>
      </div>
    </figure>
    <div className="home-scene-picker" role="group" aria-label={copy.scenesLabel}>
      {copy.scenes.map((label, index) => <button type="button" key={label} aria-pressed={scene === index} onClick={() => setScene(index)}><Icon name={['chat', 'plan', 'folder'][index]} />{label}</button>)}
    </div>
  </div>;
}

export function HomePage({ heading, locale, changeLocale }: { heading: string; locale: Locale; changeLocale: (locale: Locale) => void }) {
  const copy = homeMessages[locale];
  const { theme } = useTheme();
  return <div className="home-page" id="top" style={brandColors}>
    <a className="home-skip" href="#main">{copy.skip}</a>
    <SiteHeader locale={locale} changeLocale={changeLocale} active="home" />
    <main id="main" tabIndex={-1}>
      <section className="home-hero home-container" aria-labelledby="home-title">
        <div className="home-hero-art" aria-hidden="true">
          <span className="home-art-orbit" />
          <span className="home-art-tile"><Logo markOnly /></span>
          <span className="home-art-chat"><Icon name="chat" /></span>
          <span className="home-art-spark">✳</span>
          <span className="home-art-plan"><Icon name="check" /></span>
          <span className="home-art-dot" />
        </div>
        <div className="home-eyebrow"><span />{copy.eyebrow}</div>
        <h1 id="home-title"><span>{heading}</span></h1><p className="home-subtitle">{copy.subtitle}</p>
        <div className="home-hero-ctas"><a className="home-button home-button-primary" href="#install"><Icon name="download" />{copy.install}</a><Destination locale={locale} destination="userDocs" hint={copy.userDocsSoon} className="home-button home-button-secondary"><Icon name="book" />{copy.readDocs}</Destination></div>
        <ProductPreview copy={copy} />
      </section>
      <StorySection id="environment" label={copy.sectionLabel} heading={copy.environmentHeading} intro={copy.environmentIntro} points={copy.environmentPoints}>
        <WorkflowScene copy={copy} />
      </StorySection>
      <StorySection id="continuity" flip label={copy.continuityLabel} heading={copy.continuityHeading} intro={copy.continuityIntro} points={copy.continuityPoints}>
        <ContinuityScene copy={copy} />
      </StorySection>
      <StorySection id="learning" label={copy.learningLabel} heading={copy.learningHeading} intro={copy.learningIntro} points={copy.learningPoints}>
        <FlowScene icon="chat" title={copy.learningSceneTitle} detail={copy.learningSceneDetail} steps={copy.learningSteps} />
      </StorySection>
      <StorySection id="models" flip label={copy.modelsLabel} heading={copy.modelsHeading} intro={copy.modelsIntro} points={copy.modelsPoints}>
        <FlowScene icon="model" title={copy.modelsSceneTitle} detail={copy.modelsSceneDetail} steps={copy.modelsSteps} />
      </StorySection>
      <StorySection id="control" label={copy.controlLabel} heading={copy.controlHeading} intro={copy.controlIntro} points={[
        { title: copy.planHeading, body: copy.planIntro },
        { title: copy.permissionHeading, body: copy.permissionIntro },
      ]}>
        <ControlScene copy={copy} />
      </StorySection>
      <InstallSection copy={copy} locale={locale} />
    </main>
    <footer className="home-footer home-container"><div><Logo variant={theme === 'dark' ? 'light' : 'dark'} /><span>{copy.footer}</span></div><small>{copy.footerNote}</small><div className="home-footer-links"><Destination locale={locale} destination="userDocs" hint={copy.userDocsSoon}>{copy.userDocs}</Destination><Destination locale={locale} destination="devDocs" hint={copy.devDocsSoon}>{copy.devDocs}</Destination><Destination locale={locale} destination="github" hint={copy.githubSoon}><GithubMark size={14} />{copy.github}</Destination></div></footer>
  </div>;
}
