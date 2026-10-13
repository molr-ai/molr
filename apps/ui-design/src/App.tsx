import { productMessages } from './i18n';
import { useEffect, useRef, useState } from 'react';
import { ArrowDown, ArrowDownToLine, Languages, RotateCcw } from 'lucide-react';
import { Button, Logo, Select, Switch, ToastProvider, UIProvider, logoChoreography, useDailyLogoPreset, useTheme, useToast, useUILocale, useChoiceScroll } from '@molr/ui';
import { animationIds, messages } from './messages';
import type { AnimationId, Locale } from './messages';
import { useLocale } from './use-locale';
import { ComponentsShowcase } from './ComponentsShowcase';
import { InterfaceShowcase } from './InterfaceShowcase';
import { AppIconShowcase } from './AppIconShowcase';
import { LogoConnectionShowcase } from './LogoConnectionShowcase';

import { DesignAppearanceSelect, PaletteSelect, ThemeStudio } from './ThemeStudio';
import { useDesignPalette } from './design-themes';
import { designBrandAssetBase } from './design-brand';

const languageOptions = [
  { value: 'zh' as const, label: '简体中文', icon: <Languages /> },
  { value: 'en' as const, label: 'English', icon: <Languages /> },
];

type Feedback = { kind: 'copied' | 'copyManually' | 'validated'; value: string }
  | { kind: 'primary' | 'secondary' | 'ghost' | 'accent' };

export default function App() {
  const language = useLocale();
  const { theme } = useTheme();
  const paletteState = useDesignPalette(theme);
  return <UIProvider locale={language.locale} brandAssetBase={designBrandAssetBase(paletteState.palette)}><ToastProvider><BrandApp {...language} {...paletteState} /></ToastProvider></UIProvider>;
}

function FeedbackMessage({ feedback }: { feedback: Feedback }) {
  const { locale } = useUILocale();
  const copy = messages[locale];
  return 'value' in feedback ? copy.feedback[feedback.kind](feedback.value) : copy.feedback[feedback.kind];
}

function BrandApp({ locale, copy, changeLocale, palette, changePalette }: ReturnType<typeof useLocale> & ReturnType<typeof useDesignPalette>) {
  const { toast } = useToast();
  const { preference: themePreference, theme, changePreference } = useTheme();
  const brandAssetBase = designBrandAssetBase(palette);
  const logoVariant = theme === 'dark' ? 'light' : 'dark';
  const [logoAnimation, setLogoAnimation] = useState<AnimationId>('daily');
  const animationChoicesRef = useChoiceScroll(logoAnimation);
  const [logoReplayKey, setLogoReplayKey] = useState(0);
  const [logoMarkOnly, setLogoMarkOnly] = useState(false);
  const dailyPreset = useDailyLogoPreset(logoAnimation === 'daily');
  const selectedAnimation = logoAnimation === 'daily' ? dailyPreset.animation : logoAnimation;
  const logoSequence = selectedAnimation === 'alive' || selectedAnimation === 'playful' ? logoChoreography.sequences[selectedAnimation] : null;
  const sequenceDuration = logoSequence ? Number(logoSequence.steps.reduce((total, step) => total + step.duration * (step.cycles ?? 1) + step.pause, 0).toFixed(1)) : 0;
  const downloadLabel = copy.animations[selectedAnimation].label;
  const [copied, setCopied] = useState<string | null>(null);
  const timeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const logoName = logoMarkOnly ? 'molr-mark' : 'molr-wordmark';
  const logoSuffix = selectedAnimation === 'static' ? '' : `-${selectedAnimation}`;

  useEffect(() => {
    const target = document.getElementById(window.location.hash.slice(1));
    target?.scrollIntoView({ behavior: 'instant', block: 'start' });
  }, []);

  useEffect(() => () => {
    if (timeoutRef.current) clearTimeout(timeoutRef.current);
  }, []);

  function announce(message: Feedback) {
    toast({ title: <FeedbackMessage feedback={message} />, tone: message.kind === 'copyManually' ? 'neutral' : 'positive', duration: 2800 });
    if (timeoutRef.current) clearTimeout(timeoutRef.current);
    timeoutRef.current = setTimeout(() => setCopied(null), 2800);
  }

  async function copyColor(hex: string) {
    try {
      await navigator.clipboard.writeText(hex);
      setCopied(hex);
      announce({ kind: 'copied', value: hex });
    } catch {
      announce({ kind: 'copyManually', value: hex });
    }
  }

  return (
    <div className="app-shell">
      <a className="skip-link" href="#main">{copy.navigation.skip}</a>
      <header className="site-header">
        <Logo variant={logoVariant} animation="daily" interactive interactionLabel={copy.identity.logoAction} />
        <nav aria-label={copy.navigation.label}>
          <a href="#colors">{copy.navigation.colors}</a>
          <a href="#identity">{copy.navigation.identity}</a>
          <a href="#app-icon">{copy.appIcon.title}</a>
          <a href="#components">{copy.navigation.components}</a>
          <a href="#interfaces">{productMessages[locale].app.interfaces}</a>
        </nav>
        <div className="header-tools">
          <PaletteSelect locale={locale} palette={palette} onChange={changePalette} />
          <Select<Locale> label={copy.navigation.language} hideLabel size="sm" align="end" className="language-select" options={languageOptions} value={locale} onValueChange={changeLocale} />
          <DesignAppearanceSelect locale={locale} value={themePreference} onValueChange={changePreference} />
        </div>
      </header>

      <main id="main">
        <section className="introduction" aria-labelledby="page-title">
          <div className="introduction__text">
            <p className="eyebrow"><span className="little-plus">+</span> BRAND & INTERFACE</p>
            <h1 id="page-title">{copy.introduction.title}<br /><span>{copy.introduction.titleAccent}</span></h1>
            <p className="introduction__description">{copy.introduction.description}<br className="desktop-break" />{copy.introduction.descriptionEnd}</p>
            <a className="text-link" href="#colors">{copy.introduction.explore} <ArrowDown size={15} /></a>
          </div>
          <div className="identity-tile" aria-label={copy.introduction.artwork}>
            <span className="tile-caption">A CONTINUOUS EVOLUTION</span>
            <div className="identity-tile__art"><Logo animation="daily" interactive interactionLabel={copy.identity.logoAction} /></div>
            <div className="tile-bottom"><span>molr.ai</span><span>↗</span></div>
            <span className="registration-mark registration-mark--tl" />
            <span className="registration-mark registration-mark--br" />
          </div>
        </section>

        <section className="foundation-section" id="colors" aria-labelledby="colors-title">
          <div className="section-heading">
            <div><span className="section-number">01 / COLOR</span><h2 id="colors-title">{copy.colors.title}</h2></div>
            <p>{copy.colors.description}<br />{copy.colors.instruction}</p>
          </div>
          <ThemeStudio locale={locale} palette={palette} onChange={changePalette} theme={theme} copied={copied} onCopy={copyColor} />
          <div className="color-ratio"><span className="color-ratio__label">{copy.colors.ratio}</span><div role="img" aria-label={copy.colors.ratioDescription}><span /><span /><span /></div><span className="mono">80 / 15 / 5</span></div>
        </section>

        <section className="foundation-section" id="identity" aria-labelledby="identity-title">
          <div className="section-heading">
            <div><span className="section-number">02 / IDENTITY</span><h2 id="identity-title">{copy.identity.title}</h2></div>
            <p>{copy.identity.description}<br />{copy.identity.descriptionEnd}</p>
          </div>
          <div className="logo-controls">
            <div ref={animationChoicesRef} className="ui-choice-list ui-choice-list--segmented" role="group" aria-label={copy.identity.animationGroup}>
              {animationIds.map((animation) => (
                <button key={animation} type="button" aria-pressed={logoAnimation === animation} aria-controls="logo-previews" onClick={() => { setLogoAnimation(animation); setLogoReplayKey((key) => key + 1); }}>{copy.animations[animation].label}</button>
              ))}
            </div>
            <div className="logo-control-actions">
              <Button variant="ghost" size="sm" disabled={logoAnimation === 'static'} onClick={() => setLogoReplayKey((key) => key + 1)}><RotateCcw /> {copy.identity.replay}</Button>
              <Switch label={copy.identity.markOnly} checked={logoMarkOnly} onChange={(event) => { setLogoMarkOnly(event.target.checked); setLogoReplayKey((key) => key + 1); }} />
            </div>
          </div>
          <p className="logo-motion-note">{logoMarkOnly && logoAnimation === 'loading' ? copy.identity.loadingMark : copy.animations[logoAnimation].description}</p>
          <div className="logo-examples" id="logo-previews">
            <div className="logo-panel logo-panel--light" data-palette={palette} data-theme="light"><Logo animation={logoAnimation} replayKey={logoReplayKey} markOnly={logoMarkOnly} interactive interactionLabel={copy.identity.logoAction} /><span>{logoMarkOnly ? copy.identity.mark : copy.identity.wordmark} / LIGHT</span><a href={`${brandAssetBase}/${logoName}${logoSuffix}.svg`} download aria-label={copy.identity.download(copy.identity.lightBackground, downloadLabel, logoMarkOnly ? copy.identity.mark : copy.identity.wordmark)}><ArrowDownToLine size={16} /></a></div>
            <div className="logo-panel logo-panel--dark"><Logo variant="light" animation={logoAnimation} replayKey={logoReplayKey} markOnly={logoMarkOnly} interactive interactionLabel={copy.identity.logoAction} /><span>{logoMarkOnly ? copy.identity.mark : copy.identity.reversedWordmark} / DARK</span><a href={`${brandAssetBase}/${logoName}-light${logoSuffix}.svg`} download aria-label={copy.identity.download(copy.identity.darkBackground, downloadLabel, logoMarkOnly ? copy.identity.mark : copy.identity.reversedWordmark)}><ArrowDownToLine size={16} /></a></div>
          </div>
          {logoSequence && (
            <div className="logo-sequence">
              <div className="logo-sequence__heading"><span>{logoAnimation === 'daily' ? `${dailyPreset.period === 'day' ? copy.identity.day : copy.identity.night} · ${downloadLabel}` : downloadLabel}</span><span>{copy.identity.duration(sequenceDuration)}</span></div>
              <ol aria-label={copy.identity.sequence}>
                {logoSequence.steps.map((step, index) => (
                  <li key={`${step.motion}-${index}`}><span className="logo-sequence__index">{String(index + 1).padStart(2, '0')}</span><span>{copy.animations[step.motion as AnimationId].label}</span><small>{copy.identity.cycles(step.cycles ?? 1)}</small><small>{copy.identity.pause(step.pause)}</small></li>
                ))}
              </ol>
              <p>{logoAnimation === 'daily' && <>{dailyPreset.period === 'day' ? copy.identity.nextNight : copy.identity.nextDay}{' '}</>}{copy.identity.sequenceNote}</p>
            </div>
          )}
          <p className="section-note">{copy.identity.note}</p>
          <div className="logo-downloads" aria-label={copy.identity.downloads}>
            <a href={`${brandAssetBase}/molr-mark${logoSuffix}.svg`} download aria-label={copy.identity.downloadMark(copy.identity.forestMark, downloadLabel)}>{copy.identity.forestMark} · {downloadLabel} <ArrowDownToLine size={14} /></a>
            <a href={`${brandAssetBase}/molr-mark-light${logoSuffix}.svg`} download aria-label={copy.identity.downloadMark(copy.identity.reversedMark, downloadLabel)}>{copy.identity.reversedMark} · {downloadLabel} <ArrowDownToLine size={14} /></a>
          </div>
          <LogoConnectionShowcase locale={locale} palette={palette} markOnly={logoMarkOnly} />
        </section>

        <AppIconShowcase copy={copy.appIcon} svgAssetBase={brandAssetBase} />

        <section className="foundation-section" id="components" aria-labelledby="components-title">
          <div className="section-heading">
            <div><span className="section-number">04 / INTERFACE</span><h2 id="components-title">{copy.components.title}</h2></div>
            <p>{copy.components.description}<br />{copy.components.descriptionEnd}</p>
          </div>
          <ComponentsShowcase locale={locale} copy={copy} theme={theme} />
        </section>
        <InterfaceShowcase locale={locale} />
      </main>

      <footer className="site-footer"><Logo variant={logoVariant} animation="daily" interactive interactionLabel={copy.identity.logoAction} /><p>{copy.footer}</p><span className="mono">molr.ai / 2026</span></footer>
    </div>
  );
}
