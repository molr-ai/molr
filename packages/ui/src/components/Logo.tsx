import { useMemo, useState } from 'react';
import { useDailyLogoPreset } from '../logo-daily';
import { getBundledLogo, logoDataUrl } from '../logo-assets';
import { InteractiveLogo } from './InteractiveLogo';
import { useBrandAssetBase } from '../locale';

export type LogoProps = {
  /** Directory containing the exported brand files; inherits UIProvider by default. */
  assetBaseUrl?: string;
  variant?: 'dark' | 'light';
  animation?: 'static' | 'blink' | 'idle' | 'curious' | 'sleepy' | 'greet' | 'loading'
    | 'look' | 'listen' | 'stretch' | 'nuzzle' | 'earflick' | 'peek' | 'alive' | 'playful' | 'daily';
  replayKey?: number;
  interactive?: boolean;
  interactionLabel?: string;
  markOnly?: boolean;
  className?: string;
};

export function Logo({ variant = 'dark', animation = 'static', replayKey, interactive = false, interactionLabel, assetBaseUrl, markOnly = false, className = '' }: LogoProps) {
  const inheritedBase = useBrandAssetBase();
  const assetBase = (assetBaseUrl ?? inheritedBase).replace(/\/+$/, '');
  const dailyPreset = useDailyLogoPreset(animation === 'daily');
  const selectedAnimation = animation === 'daily' ? dailyPreset.animation : animation;
  const name = markOnly ? 'molr-mark' : 'molr-wordmark';
  const suffix = variant === 'light' ? '-light' : '';
  const motion = selectedAnimation === 'static' ? '' : `-${selectedAnimation}`;
  // A fresh image URL restarts the embedded SVG timeline, including when
  // the browser would otherwise reuse a cached one-shot animation.
  const replay = animation !== 'static' && replayKey !== undefined ? `?replay=${replayKey}` : '';
  const slot = animation === 'daily' ? `${replay ? '&' : '?'}period=${dailyPreset.slotKey}` : '';
  const src = `${assetBase}/${name}${suffix}${motion}.svg${replay}${slot}`;
  const bundled = useMemo(() => getBundledLogo(assetBase, `${name}${suffix}${motion}.svg`), [assetBase, name, suffix, motion]);
  const stillAsset = `${assetBase}/${name}${suffix}.svg`;
  const stillSource = useMemo(() => getBundledLogo(assetBase, `${name}${suffix}.svg`), [assetBase, name, suffix]);
  const still = useMemo(() => stillSource ? logoDataUrl(stillSource, stillAsset) : stillAsset, [stillSource, stillAsset]);
  const image = useMemo(() => bundled ? logoDataUrl(bundled, src) : src, [bundled, src]);
  const [failedSrc, setFailedSrc] = useState<string>();
  const [loadedSrc, setLoadedSrc] = useState<string>();
  const fallback = useMemo(() => logoDataUrl(stillSource ?? getBundledLogo('/brand', `${name}${suffix}.svg`)!, stillAsset), [stillSource, name, suffix, stillAsset]);
  const displayed = failedSrc === image ? fallback : image;

  const picture = (
    <picture className={`logo logo--${variant} ${className}`} role="img" aria-label="Molr" data-ready={loadedSrc === displayed ? true : undefined}>
      {animation !== 'static' && animation !== 'loading' && (
        <source media="(prefers-reduced-motion: reduce)" srcSet={failedSrc === image ? fallback : still} />
      )}
      <img
        className={markOnly ? 'logo__symbol' : 'logo__wordmark'}
        src={displayed}
        onLoad={() => setLoadedSrc(displayed)}
        onError={() => setFailedSrc(image)}
        width={markOnly ? 64 : 183}
        height={markOnly ? 64 : 54}
        alt=""
      />
    </picture>
  );

  if (interactive && (selectedAnimation === 'alive' || selectedAnimation === 'playful')) {
    return <InteractiveLogo key={src} src={src} source={bundled} sequence={selectedAnimation} variant={variant} markOnly={markOnly} className={className} fallback={picture} interactionLabel={interactionLabel} />;
  }
  return picture;
}
