export const siteModes = ['coming-soon', 'maintenance', 'live'] as const;
export type SiteMode = (typeof siteModes)[number];
export type StatusMode = Exclude<SiteMode, 'live'>;

export function resolveSiteMode(value: string | undefined): SiteMode {
  const mode = value?.trim() || 'coming-soon';
  if (siteModes.includes(mode as SiteMode)) return mode as SiteMode;
  throw new Error(`Invalid VITE_SITE_MODE: "${mode}". Expected coming-soon, maintenance, or live.`);
}
