import assert from 'node:assert/strict';
import { readFile, readdir } from 'node:fs/promises';
import { test } from 'node:test';

const brand = new URL('../public/brand/', import.meta.url);
const bundled = JSON.parse(await readFile(new URL('../src/logo-assets.json', import.meta.url), 'utf8'));
const subtract = (a, b) => a.map((value, i) => value - b[i]);

function segments(path) {
  let end = [0, 0];
  return [...path.matchAll(/([MLVHCQ])([^MLVHCQ]*)/g)].flatMap(([, command, coordinates]) => {
    const values = coordinates.trim().split(/[\s,]+/).map(Number);
    const start = end;
    end = command === 'V' ? [start[0], values[0]] : command === 'H' ? [values[0], start[1]] : values.slice(-2);
    if (command === 'M') return [];
    const curved = command === 'C' || command === 'Q';
    return [{
      incoming: subtract(curved ? values.slice(0, 2) : end, start),
      outgoing: subtract(end, curved ? values.slice(-4, -2) : start),
    }];
  });
}

function checkJoins(path, letter, context) {
  const parts = segments(path);
  // M's two ear tips intentionally turn sharply. Its central bend and all
  // r joins must have matching directions, including between keyframes.
  const joins = letter === 'M' ? [2, 3] : [1, 2, 3];
  for (const index of joins) {
    const a = parts[index - 1].outgoing;
    const b = parts[index].incoming;
    const length = Math.hypot(...a) * Math.hypot(...b);
    assert(length > 0, `${context}: collapsed tangent at join ${index}`);
    assert(a[0] * b[0] + a[1] * b[1] > 0, `${context}: reversed tangent at join ${index}`);
    assert(Math.abs(a[0] * b[1] - a[1] * b[0]) / length < 1e-6, `${context}: kink at join ${index}`);
  }
}

test('all exported and bundled logo poses keep their smooth joins during morphing', async () => {
  for (const filename of (await readdir(brand)).filter(name => name.endsWith('.svg'))) {
    const source = await readFile(new URL(filename, brand), 'utf8');
    if (filename in bundled) assert.equal(source, bundled[filename], `${filename}: stale bundled asset`);
    for (const [, letter, rest, content = ''] of source.matchAll(/<path data-letter="(M|r)" d="([^"]+)"(?:\/>|>([\s\S]*?)<\/path>)/g)) {
      checkJoins(rest, letter, `${filename} ${letter} rest`);
      for (const [, values] of content.matchAll(/<animate attributeName="d"[^>]*values="([^"]+)"/g)) {
        const frames = values.split(';');
        for (const [index, frame] of frames.entries()) {
          checkJoins(frame, letter, `${filename} ${letter} frame ${index}`);
          if (!index) continue;
          const previous = frames[index - 1];
          assert.deepEqual(frame.match(/[A-Z]/g), previous.match(/[A-Z]/g), `${filename}: incompatible morph commands`);
          const numbers = frame.match(/-?\d+(?:\.\d+)?/g).map(Number);
          for (const progress of [.25, .5, .75]) {
            let position = 0;
            const between = previous.replace(/-?\d+(?:\.\d+)?/g, value => String(Number(value) * (1 - progress) + numbers[position++] * progress));
            checkJoins(between, letter, `${filename} ${letter} frame ${index} at ${progress}`);
          }
        }
      }
    }
  }
});
