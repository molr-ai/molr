// Native taskbar artwork uses fixed red/white pixels, independent of WebUI themes.
// A 16px design rendered at 2x remains legible in Windows' small overlay area.
const digits = [
  '111101101101111', '010110010010111', '111001111100111', '111001111001111',
  '101101111001001', '111100111001111', '111100111101111', '111001010010010',
  '111101111101111', '111101111001111',
];
export function badgeBitmap(count: number): Buffer {
  const text = count > 99 ? '99+' : String(count);
  const pixels = Buffer.alloc(32 * 32 * 4);
  const left = Math.floor((16 - (text.length * 4 - 1)) / 2);
  for (let y = 0; y < 32; y++) for (let x = 0; x < 32; x++) {
    const px = Math.floor(x / 2), py = Math.floor(y / 2);
    if ((x - 15.5) ** 2 + (y - 15.5) ** 2 > 15.5 ** 2) continue;
    const index = Math.floor((px - left) / 4), column = (px - left) % 4, row = py - 5;
    const char = text[index];
    const glyph = char === '+' ? '000010111010000' : digits[Number(char)];
    const white = index >= 0 && index < text.length && column < 3 && row >= 0 && row < 5 && glyph?.[row * 3 + column] === '1';
    // nativeImage bitmap pixels are BGRA on Windows.
    pixels.set(white ? [255, 255, 255, 255] : [55, 55, 210, 255], (y * 32 + x) * 4);
  }
  return pixels;
}
