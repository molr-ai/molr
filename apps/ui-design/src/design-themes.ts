import { usePalettePreference } from '@molr/ui';
import type { Theme } from '@molr/ui';
export { palettes as designThemes } from '@molr/ui';
export type { Palette as DesignPalette } from '@molr/ui';
export const useDesignPalette = (theme: Theme) => usePalettePreference('molr-ui-design-palette', theme, `${import.meta.env.BASE_URL}brand`);
