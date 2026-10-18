import type { CSSProperties, ReactNode } from 'react';
import { Logo, paletteBrandColors, useTheme } from '@molr/ui';
import { Icon } from './HomeIcon';
import { ProductPreview } from './ProductPreview';
import { homeMessages, type HomeCopy } from './home-content';
import { localizedDocPath } from './doc-locale';
import { siteLinks } from './site-links';
import { GithubMark } from './GithubMark';
import type { Locale } from './site-content';
import { SiteHeader } from './SiteHeader';
import { InstallSection } from './InstallSection';
import { HeroDownload } from './HeroDownload';

// Reuse the UI Design palette values; decorative colors stay local to the homepage.
const brandColors = Object.fromEntries(
  Object.entries(paletteBrandColors).flatMap(([palette, colors]) =>
    Object.entries(colors).map(([role, value]) => [`--home-${palette}-${role}`, value]),
  ),
) as CSSProperties;

function Destination({ locale, destination, children, hint, className = '' }: { locale: Locale; destination: keyof typeof siteLinks; children: ReactNode; hint: string; className?: string }) {
  const url = siteLinks[destination];
  return url
    ? <a className={className} href={localizedDocPath(url, locale)}>{children}</a>
    : <a className={`${className} home-placeholder`} role="link" aria-disabled="true" title={hint}>{children}</a>;
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
        <HeroDownload locale={locale} />
        <ProductPreview copy={copy} locale={locale} />
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
    <footer className="home-footer home-container"><div><Logo variant={theme === 'dark' ? 'light' : 'dark'} /><span>{copy.footer}</span></div><small>{copy.footerNote}</small><div className="home-footer-links"><Destination locale={locale} destination="userDocs" hint={copy.userDocsSoon}>{copy.userDocs}</Destination><Destination locale={locale} destination="adminDocs" hint={copy.adminDocs}>{copy.adminDocs}</Destination><Destination locale={locale} destination="devDocs" hint={copy.devDocsSoon}>{copy.devDocs}</Destination><Destination locale={locale} destination="github" hint={copy.githubSoon}><GithubMark size={14} />{copy.github}</Destination></div></footer>
  </div>;
}
