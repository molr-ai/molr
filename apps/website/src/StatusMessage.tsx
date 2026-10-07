import { useEffect, useState } from 'react';
import { statusPhrases as phrases } from './site-content';
import type { StatusMode } from './site-mode';

const phraseDuration = 5200;

function initialPhrase() {
  const languages = navigator.languages.length ? navigator.languages : [navigator.language];
  for (const language of languages) {
    const base = language.toLowerCase().split(/[-_]/)[0];
    const index = phrases.findIndex(phrase => phrase.language.split('-')[0] === base);
    if (index !== -1) return index;
  }
  return 1;
}

export function StatusMessage({ mode, accessibleLabel }: { mode: StatusMode; accessibleLabel: string }) {
  const [frame, setFrame] = useState(() => ({ index: initialPhrase(), cycle: 0 }));

  useEffect(() => {
    const motion = window.matchMedia('(prefers-reduced-motion: reduce)');
    let timer: number | undefined;

    function restart() {
      window.clearInterval(timer);
      if (document.visibilityState === 'hidden') return;
      // Restart the visual phase after a hidden tab resumes, keeping the
      // animation and timer together instead of returning to faded-out text.
      setFrame(previous => ({ index: motion.matches ? initialPhrase() : previous.index, cycle: previous.cycle + 1 }));
      if (!motion.matches) {
        timer = window.setInterval(() => {
          setFrame(previous => ({ index: (previous.index + 1) % phrases.length, cycle: previous.cycle + 1 }));
        }, phraseDuration);
      }
    }

    function languageChanged() {
      setFrame(previous => ({ index: initialPhrase(), cycle: previous.cycle + 1 }));
      restart();
    }

    restart();
    document.addEventListener('visibilitychange', restart);
    window.addEventListener('languagechange', languageChanged);
    motion.addEventListener('change', restart);
    return () => {
      window.clearInterval(timer);
      document.removeEventListener('visibilitychange', restart);
      window.removeEventListener('languagechange', languageChanged);
      motion.removeEventListener('change', restart);
    };
  }, []);

  const phrase = phrases[frame.index];
  return (
    <h1 id="site-status-title" className="launch-message" aria-label={accessibleLabel} aria-live="off">
      <span
        key={frame.cycle}
        className="launch-phrase"
        lang={phrase.language}
        dir={phrase.language === 'ar' ? 'rtl' : 'ltr'}
        aria-hidden="true"
        style={{ animationDuration: `${phraseDuration}ms` }}
      >
        {phrase[mode]}
      </span>
    </h1>
  );
}
