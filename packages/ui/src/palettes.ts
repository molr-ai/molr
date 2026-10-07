export const palettes = [
  { id: 'forest', zh: '森林', en: 'Forest', description: { zh: '苔绿与青柠，自然又轻快', en: 'Moss and lime. Naturally bright.' } },
  { id: 'ocean', zh: '海盐蓝', en: 'Ocean', description: { zh: '海蓝与薄雾，清晰而宁静', en: 'Sea blue and mist. Calm clarity.' } },
  { id: 'iris', zh: '鸢尾紫', en: 'Iris', description: { zh: '墨紫与薰衣草，柔和的灵感', en: 'Ink and lavender. Quiet inspiration.' } },
  { id: 'sand', zh: '暖砂', en: 'Sand', description: { zh: '咖啡与杏金，温暖的专注', en: 'Coffee and apricot. Warm focus.' } },
  { id: 'rose', zh: '玫瑰', en: 'Rose', description: { zh: '莓果与花瓣，克制的浪漫', en: 'Berry and petals. Understated warmth.' } },
  { id: 'monochrome', zh: '素墨', en: 'Monochrome', description: { zh: '黑白与灰阶，简洁而纯粹', en: 'Black, white and gray. Simple and focused.' } },
] as const;
export type Palette = typeof palettes[number]['id'];

/** SVG inks are fixed within each palette; Logo's light/dark variant matches its surface. */
export const paletteBrandColors = {
  forest: { ink: '#263b31', paper: '#f5f6ef', accent: '#d5f478' },
  ocean: { ink: '#244b66', paper: '#eaf3f9', accent: '#a8dcf4' },
  iris: { ink: '#493960', paper: '#f2edf9', accent: '#d8c5fa' },
  sand: { ink: '#59412e', paper: '#f7efe3', accent: '#f4d295' },
  monochrome: { ink: '#262626', paper: '#f5f5f5', accent: '#d4d4d4' },
  rose: { ink: '#673e4c', paper: '#f9edf1', accent: '#f6c5d3' },
} as const;

export function paletteAssetBase(palette: keyof typeof paletteBrandColors, base = '/brand') {
  return palette === 'forest' ? base : `${base}/themes/${palette}`;
}
