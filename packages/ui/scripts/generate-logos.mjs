import { mkdir, writeFile } from 'node:fs/promises';
import { parseArgs } from 'node:util';
import { Resvg } from '@resvg/resvg-js';
import appIconSizes from '../src/app-icon-sizes.json' with { type: 'json' };
import { headContour, tailContour } from './logo-contours.mjs';
import { choreographyAnimations, choreographyArtwork } from './logo-choreography.mjs';

const destination = new URL('../public/brand/', import.meta.url);

// Brand palette. Any channel can be overridden to re-theme the whole asset
// matrix. npm swallows "--flag value" pairs, so either export env vars:
//   MOLR_INK='#1b2a22' MOLR_ACCENT='#c8f25d' npm run brand:generate
// or call the script directly:
//   node packages/ui/scripts/generate-logos.mjs --ink '#1b2a22'
const { values: args } = parseArgs({
  options: {
    ink: { type: 'string' },
    paper: { type: 'string' },
    accent: { type: 'string' },
  },
});
const palette = {
  ink: args.ink ?? process.env.MOLR_INK ?? '#263b31',
  paper: args.paper ?? process.env.MOLR_PAPER ?? '#f5f6ef',
  accent: args.accent ?? process.env.MOLR_ACCENT ?? '#d5f478',
};
for (const [channel, value] of Object.entries(palette)) {
  if (!/^#[0-9a-fA-F]{3,8}$/.test(value)) {
    throw new Error(`Invalid ${channel} color "${value}": expected a hex value like #263b31.`);
  }
}

// Two built-in presets. Artwork for light backgrounds keeps strokes and eyes
// in ink; artwork for dark backgrounds flips to paper strokes with accent
// eyes — the "cat eyes in the dark" moment. The icon always pairs the accent
// canvas with ink artwork.
const presets = [
  { suffix: '', titleTone: '', stroke: palette.ink, eye: palette.ink },
  { suffix: '-light', titleTone: ', light', stroke: palette.paper, eye: palette.accent },
];

// Final approved geometry: four letterforms and two dots inside M.
// The dots hint at eyes; M/l and the continuous tail share a top line.
// M legs splay by 1.25 units (about 2.5 degrees). The original o curves
// are uniformly scaled by 5/6 and translated by (12, 3.5), retaining a
// seven-unit stroke like every other letter; eyes are eight units wide.
const cat = {
  M: headContour(),
  o: 'M92 30.583333C92 23.5 87.833333 20.166667 82 20.166667C76.166667 20.166667 72 23.5 72 30.583333C72 37.666667 76.166667 41 82 41C87.833333 41 92 37.666667 92 30.583333Z',
  l: 'M112 12V32Q112 41 121 41',
  r: tailContour(),
};

// Keep the face still while the ear tips fold inward and the tail curls.
// Matching path commands let SVG interpolate the contours smoothly.
const idleContours = {
  M: {
    rest: cat.M,
    flex: headContour({ leftEar: [13, 17], rightEar: [49, 17] }),
  },
  r: {
    rest: cat.r,
    flex: tailContour({ bendX: 166, tip: [165, 17] }),
  },
};

function catPaths(preset, markOnly = false, idle = false) {
  const letters = markOnly ? [['M', cat.M]] : Object.entries(cat);
  const paths = letters.map(([letter, path]) => {
    const contour = idle && idleContours[letter];
    if (!contour) return `    <path data-letter="${letter}" d="${path}"/>`;
    return `    <path data-letter="${letter}" d="${contour.rest}">\n      <animate attributeName="d" dur="2.8s" repeatCount="indefinite" calcMode="spline" keyTimes="0;0.5;1" keySplines="0.45 0 0.55 1;0.45 0 0.55 1" values="${contour.rest};${contour.flex};${contour.rest}"/>\n    </path>`;
  }).join('\n');
  return `  <g stroke="${preset.stroke}" stroke-width="7" stroke-linecap="round" stroke-linejoin="round">\n${paths}\n  </g>\n  <g fill="${preset.eye}">\n    <circle data-eye="left" cx="23" cy="36" r="4"/>\n    <circle data-eye="right" cx="39" cy="36" r="4"/>\n  </g>`;
}

// Blinking variants: eyes squash once every 4 s (about 200 ms) via embedded
// CSS so the animation also runs inside <img>. Honors reduced motion.
const blinkStyle = `  <style>
    [data-eye]{animation:molr-blink 4s infinite;transform-box:fill-box;transform-origin:center}
    @keyframes molr-blink{0%,93%{transform:scaleY(1)}95%{transform:scaleY(.1)}98%,100%{transform:scaleY(1)}}
    @media (prefers-reduced-motion:reduce){[data-eye]{animation:none}}
  </style>\n`;

// SMIL path morphing also plays inside <img>. Reduced motion shows the
// original artwork instead, since CSS animation:none does not stop SMIL.
const idleStyle = `  <style>
    [data-idle-still]{display:none}
    @media (prefers-reduced-motion:reduce){[data-idle-motion]{display:none}[data-idle-still]{display:inline}}
  </style>\n`;

function motionStyle(extra = '') {
  return `  <style>
    [data-motion-still]{display:none}${extra ? `\n    ${extra}` : ''}
    @media (prefers-reduced-motion:reduce){[data-motion-art]{display:none}[data-motion-still]{display:inline}}
  </style>\n`;
}

function animate(attribute, values, duration, times, { repeat = 'indefinite', transform, linear = false } = {}) {
  const element = transform ? 'animateTransform' : 'animate';
  const timing = linear ? 'calcMode="linear"' : `calcMode="spline" keySplines="${values.slice(1).map(() => '0.45 0 0.55 1').join(';')}"`;
  return `<${element} attributeName="${attribute}"${transform ? ` type="${transform}"` : ''} dur="${duration}s" repeatCount="${repeat}" fill="freeze" ${timing} keyTimes="${times.join(';')}" values="${values.join(';')}"/>`;
}

function morph(letter, values, duration, times, options) {
  return `<path data-letter="${letter}" d="${values[0]}">${animate('d', values, duration, times, options)}</path>`;
}

function stroked(preset, content) {
  return `<g stroke="${preset.stroke}" stroke-width="7" stroke-linecap="round" stroke-linejoin="round">${content}</g>`;
}

// Matching cubic segments keep every eye state compatible for SMIL morphing.
function eyeContour(cx, state = 'open') {
  if (state === 'closed') return `M${cx + 4} 35.644C${cx + 2.756} 37.067 ${cx + 1.511} 37.778 ${cx} 37.778C${cx - 1.511} 37.778 ${cx - 2.756} 37.067 ${cx - 4} 35.644C${cx - 3.022} 36.089 ${cx - 1.689} 36.444 ${cx} 36.444C${cx + 1.689} 36.444 ${cx + 3.022} 36.089 ${cx + 4} 35.644Z`;
  const rx = state === 'wide' ? 4.5 : 4;
  const ry = state === 'blink' ? 0.4 : rx;
  const k = 0.5522847498;
  return `M${cx + rx} 36C${cx + rx} ${36 + ry * k} ${cx + rx * k} ${36 + ry} ${cx} ${36 + ry}C${cx - rx * k} ${36 + ry} ${cx - rx} ${36 + ry * k} ${cx - rx} 36C${cx - rx} ${36 - ry * k} ${cx - rx * k} ${36 - ry} ${cx} ${36 - ry}C${cx + rx * k} ${36 - ry} ${cx + rx} ${36 - ry * k} ${cx + rx} 36Z`;
}

function characterArtwork(preset, markOnly, animation) {
  const rest = idleContours.M.rest;
  let head = `<path data-letter="M" d="${cat.M}"/>`;
  let tail = `<path data-letter="r" d="${cat.r}"/>`;
  let headMotion = '';

  if (animation === 'curious') {
    const times = [0, 0.15, 0.42, 0.65, 1];
    const perk = headContour({ leftEar: [10, 14], rightEar: [52, 9] });
    head = morph('M', [rest, rest, perk, perk, rest], 1.8, times, { repeat: '1' });
    headMotion = animate('transform', ['0 31 36', '0 31 36', '6 31 36', '6 31 36', '0 31 36'], 1.8, times, { repeat: '1', transform: 'rotate' });
  } else if (animation === 'greet') {
    const left = tailContour({ bendX: 168, tip: [164, 10] });
    const right = tailContour({ bendX: 168, tip: [174, 15] });
    tail = morph('r', [cat.r, left, right, left, right, cat.r, cat.r], 2.4, [0, 0.18, 0.34, 0.5, 0.66, 0.82, 1], { repeat: '1' });
    headMotion = animate('transform', ['0 0', '0 1.5', '0 0', '0 0'], 2.4, [0, 0.22, 0.48, 1], { repeat: '1', transform: 'translate' });
  }

  const eyes = [23, 39].map((cx, index) => {
    const side = index === 0 ? 'left' : 'right';
    if (animation === 'greet') {
      return `<ellipse data-eye="${side}" cx="${cx}" cy="36" rx="4" ry="4">${animate('ry', [4, 4, 0.4, 4, 4], 2.4, [0, 0.38, 0.42, 0.48, 1], { repeat: '1' })}</ellipse>`;
    }
    return `<circle data-eye="${side}" cx="${cx}" cy="36" r="4"/>`;
  }).join('\n');

  // Moving the face as one group keeps both eyes attached to the head.
  const face = `<g data-head="">${headMotion}${stroked(preset, head)}<g fill="${preset.eye}">${eyes}</g></g>`;
  const body = markOnly ? '' : stroked(preset, `<path data-letter="o" d="${cat.o}"/><path data-letter="l" d="${cat.l}"/>${tail}`);
  return `${face}\n${body}`;
}

function loadingArtwork(preset, markOnly) {
  // The closed o carries the loading loop. Head and tail move more slowly,
  // with a slight lag in the tail; eyes stay anchored inside the face.
  const rest = idleContours.M.rest;
  const soft = headContour({ leftEar: [11.5, 14], rightEar: [50.5, 14] });
  const headValues = [rest, soft, rest];
  const headTimes = [0, 0.5, 1];
  const head = morph('M', headValues, 3.2, headTimes);
  const sway = tailContour({ bendX: 169, tip: [171, 15] });
  const tail = morph('r', [cat.r, sway, cat.r], 3.2, [0, 0.6, 1]);

  // The standalone M has no o, so its trace follows the same morph as its
  // underlying contour. This keeps the moving segment on the outline.
  const traceShape = markOnly ? rest : cat.o;
  const traceMorph = markOnly ? animate('d', headValues, 3.2, headTimes) : '';
  const trace = `<path data-loading-trace="${markOnly ? 'M' : 'o'}" d="${traceShape}" pathLength="100" stroke-dasharray="26 74">${traceMorph}${animate('stroke-dashoffset', [0, markOnly ? -100 : 100], 1.6, [0, 1], { linear: true })}</path>`;
  const headOutline = markOnly ? `<g data-loading-track="">${head}</g>${trace}` : head;
  const eyes = `<g fill="${preset.eye}"><circle data-eye="left" cx="23" cy="36" r="4"/><circle data-eye="right" cx="39" cy="36" r="4"/></g>`;
  const face = `<g data-head="">${stroked(preset, headOutline)}${eyes}</g>`;
  const body = markOnly ? '' : stroked(preset, `<g data-loading-track=""><path data-letter="o" d="${cat.o}"/></g>${trace}<path data-letter="l" d="${cat.l}"/>${tail}`);
  return `${face}\n${body}`;
}

const animations = {
  static: { suffix: '', title: '', style: '' },
  blink: { suffix: '-blink', title: ', blinking', style: blinkStyle },
  idle: { suffix: '-idle', title: ', idle', style: idleStyle },
  curious: { suffix: '-curious', title: ', curious', style: motionStyle() },
  greet: { suffix: '-greet', title: ', greeting', style: motionStyle() },
  // Loading communicates an active connection attempt, so keep it running
  // under reduced motion. Decorative animations retain their static fallback.
  loading: { suffix: '-loading', title: ', loading', style: '  <style>\n    [data-motion-still]{display:none}\n    [data-loading-track]{opacity:.24}\n  </style>\n' },
  ...Object.fromEntries(choreographyAnimations.map(name => [name, { suffix: `-${name}`, title: `, ${name}`, style: motionStyle() }])),
};

function catArtwork(preset, markOnly, animation) {
  if (choreographyAnimations.includes(animation)) {
    const artwork = choreographyArtwork(preset, markOnly, animation, { cat, idleContours, animate, eyeContour, stroked });
    return `  <g data-motion-art="">\n${artwork}\n  </g>\n  <g data-motion-still="">\n${catPaths(preset, markOnly)}\n  </g>`;
  }
  if (['curious', 'greet', 'loading'].includes(animation)) {
    const artwork = animation === 'loading' ? loadingArtwork(preset, markOnly) : characterArtwork(preset, markOnly, animation);
    return `  <g data-motion-art="">\n${artwork}\n  </g>\n  <g data-motion-still="">\n${catPaths(preset, markOnly)}\n  </g>`;
  }
  if (animation !== 'idle') return catPaths(preset, markOnly);
  return `  <g data-idle-motion="">\n${catPaths(preset, markOnly, true)}\n  </g>\n  <g data-idle-still="">\n${catPaths(preset, markOnly)}\n  </g>`;
}

function catWordmark(preset, animation = 'static') {
  const motion = animations[animation];
  return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 183 54" fill="none">\n  <title>Molr — Abstract cat${preset.titleTone}${motion.title}</title>\n  <desc>A compact M with subtly splayed legs contains two eyes. The original rounded o is uniformly reduced; the tail tip aligns with the tops of M and l.</desc>\n${motion.style}${catArtwork(preset, false, animation)}\n</svg>\n`;
}

function catMark(preset, animation = 'static') {
  const motion = animations[animation];
  return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64" fill="none">\n  <title>Molr — Cat-ear M${preset.titleTone}${motion.title}</title>\n${motion.style}  <g transform="translate(1 4)">\n${catArtwork(preset, true, animation)}\n  </g>\n</svg>\n`;
}

function catIcon(radius) {
  return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 80 80" fill="none">\n  <title>Molr — Cat-ear M icon</title>\n  <rect width="80" height="80" rx="${radius}" fill="${palette.accent}"/>\n  <g transform="translate(9 12)">\n${catPaths({ stroke: palette.ink, eye: palette.ink }, true)}\n  </g>\n</svg>\n`;
}

await mkdir(destination, { recursive: true });

const bundledLogos = {};
for (const preset of presets) {
  for (const [animation, motion] of Object.entries(animations)) {
    for (const [name, render] of [['wordmark', catWordmark], ['mark', catMark]]) {
      const filename = `molr-${name}${preset.suffix}${motion.suffix}.svg`;
      bundledLogos[filename] = render(preset, animation);
      await writeFile(new URL(filename, destination), bundledLogos[filename]);
    }
  }
}
// Ship the same artwork with the UI code: connection indicators must survive
// the server becoming unreachable, even before their first appearance.
await writeFile(new URL('../src/logo-assets.json', import.meta.url), `${JSON.stringify(bundledLogos, null, 2)}\n`);
const iconVariants = [{ suffix: '', radius: 22 }, { suffix: '-square', radius: 0 }];
for (const { suffix, radius } of iconVariants) {
  const icon = catIcon(radius);
  await writeFile(new URL(`molr-icon${suffix}.svg`, destination), icon);
  for (const size of appIconSizes) {
    const png = new Resvg(icon, { fitTo: { mode: 'width', value: size } }).render().asPng();
    await writeFile(new URL(`molr-icon${suffix}-${size}.png`, destination), png);
  }
}
console.log(`Generated ${presets.length * Object.keys(animations).length * 2 + iconVariants.length} Molr SVG assets and ${appIconSizes.length * iconVariants.length} PNG app icons.`);
if (args.ink || args.paper || args.accent || process.env.MOLR_INK || process.env.MOLR_PAPER || process.env.MOLR_ACCENT) {
  console.log('Custom palette applied:', palette);
}
