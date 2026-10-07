import { paletteAssetBase } from '@molr/ui/palettes';
import type { Palette } from '@molr/ui/palettes';
export { paletteBrandColors as designBrandColors } from '@molr/ui/palettes';
export const designBrandAssetBase = (palette: Palette) => paletteAssetBase(palette, `${import.meta.env.BASE_URL}brand`);
