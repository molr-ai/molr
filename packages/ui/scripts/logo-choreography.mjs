import choreography from '../src/logo-choreography.json' with { type: 'json' };

export const choreographyAnimations = [...Object.keys(choreography.motions), ...Object.keys(choreography.sequences)];

// Each channel describes a pose over normalized time. Every clip starts and
// ends in the resting pose, so the same clips work alone or in a fixed score.
function motionClips() {
  return {
    blink: { loop: true, eyes: [[0, 'open'], [0.93, 'open'], [0.95, 'blink'], [0.98, 'open'], [1, 'open']] },
    idle: {
      loop: true,
      head: [[0, 'rest'], [0.5, 'idle'], [1, 'rest']],
      tail: [[0, 'rest'], [0.5, 'idle'], [1, 'rest']],
    },
    curious: {
      head: [[0, 'rest'], [0.15, 'rest'], [0.42, 'curious'], [0.65, 'curious'], [1, 'rest']],
      angle: [[0, 0], [0.15, 0], [0.42, 6], [0.65, 6], [1, 0]],
    },
    sleepy: {
      // Keep the original 0.9 s lead-in, 1.38 s closing and 1.32 s opening;
      // extend only the relaxed, closed-eye hold from 2.4 s to 20.4 s.
      head: [[0, 'rest'], [0.0375, 'rest'], [0.095, 'soft'], [0.945, 'soft'], [1, 'rest']],
      tail: [[0, 'rest'], [0.0375, 'rest'], [0.095, 'sleepy'], [0.945, 'sleepy'], [1, 'rest']],
      eyes: [[0, 'open'], [0.0375, 'open'], [0.095, 'closed'], [0.945, 'closed'], [1, 'open']],
    },
    greet: {
      tail: [[0, 'rest'], [0.18, 'left'], [0.34, 'right'], [0.5, 'left'], [0.66, 'right'], [0.82, 'rest'], [1, 'rest']],
      y: [[0, 0], [0.22, 1.5], [0.48, 0], [1, 0]],
      eyes: [[0, 'open'], [0.38, 'open'], [0.42, 'blink'], [0.48, 'open'], [1, 'open']],
    },
    look: {
      gaze: [[0, 0], [0.08, 0], [0.28, -1.5], [0.42, -1.5], [0.67, 1.5], [0.8, 1.5], [1, 0]],
      angle: [[0, 0], [0.12, 0], [0.32, -2.5], [0.42, -2.5], [0.71, 2.5], [0.8, 2.5], [1, 0]],
    },
    listen: {
      head: [[0, 'rest'], [0.1, 'rest'], [0.3, 'listen'], [0.42, 'rest'], [0.54, 'listen'], [0.76, 'listen'], [1, 'rest']],
      angle: [[0, 0], [0.15, 0], [0.38, -2], [0.76, -2], [1, 0]],
      gaze: [[0, 0], [0.3, -1], [0.76, -1], [1, 0]],
    },
    stretch: {
      head: [[0, 'rest'], [0.22, 'idle'], [0.55, 'stretch'], [0.7, 'stretch'], [1, 'rest']],
      eyes: [[0, 'open'], [0.22, 'closed'], [0.35, 'closed'], [0.6, 'open'], [1, 'open']],
      o: [[0, 'rest'], [0.25, 'rest'], [0.58, 'stretch'], [0.7, 'stretch'], [1, 'rest']],
      l: [[0, 'rest'], [0.25, 'rest'], [0.58, 'stretch'], [0.7, 'stretch'], [1, 'rest']],
      tail: [[0, 'rest'], [0.25, 'idle'], [0.65, 'stretch'], [0.76, 'stretch'], [1, 'rest']],
    },
    nuzzle: {
      head: [[0, 'rest'], [0.3, 'soft'], [0.68, 'soft'], [1, 'rest']],
      eyes: [[0, 'open'], [0.26, 'closed'], [0.68, 'closed'], [1, 'open']],
      angle: [[0, 0], [0.35, 3.5], [0.5, 3.5], [0.7, 2], [1, 0]],
      x: [[0, 0], [0.35, 2], [0.5, 2], [0.7, 1.2], [1, 0]],
      tail: [[0, 'rest'], [0.4, 'curl'], [0.68, 'curl'], [1, 'rest']],
    },
    earflick: {
      head: [[0, 'rest'], [0.15, 'rest'], [0.28, 'listen'], [0.4, 'rest'], [0.53, 'rightEar'], [0.65, 'rest'], [1, 'rest']],
      eyes: [[0, 'open'], [0.7, 'open'], [0.76, 'blink'], [0.83, 'open'], [1, 'open']],
    },
    peek: {
      x: [[0, 0], [0.1, 0], [0.38, 2.5], [0.63, 2.5], [1, 0]],
      y: [[0, 0], [0.1, 0], [0.38, -0.8], [0.63, -0.8], [1, 0]],
      angle: [[0, 0], [0.38, -2.5], [0.63, -2.5], [1, 0]],
      eyes: [[0, 'open'], [0.32, 'wide'], [0.63, 'wide'], [1, 'open']],
      gaze: [[0, 0], [0.32, 0.8], [0.63, 0.8], [1, 0]],
      tail: [[0, 'rest'], [0.45, 'alert'], [0.63, 'alert'], [1, 'rest']],
    },
  };
}

function compileTracks(steps, clips) {
  const resting = { head: 'rest', tail: 'rest', o: 'rest', l: 'rest', eyes: 'open', angle: 0, x: 0, y: 0, gaze: 0 };
  const tracks = Object.fromEntries(Object.entries(resting).map(([channel, value]) => [channel, [[0, value]]]));
  let elapsed = 0;
  for (const { motion, duration, cycles = 1, pause = 0 } of steps) {
    if (!clips[motion] || !Number.isFinite(duration) || duration <= 0 || !Number.isFinite(pause) || pause < 0) {
      throw new Error(`Invalid logo choreography step: ${motion}`);
    }
    if (!Number.isSafeInteger(cycles) || cycles < 1 || (cycles > 1 && !clips[motion].loop)) {
      throw new Error(`Invalid cycle count for logo clip: ${motion}`);
    }
    for (const [channel, rest] of Object.entries(resting)) {
      const frames = clips[motion][channel] ?? [[0, rest], [1, rest]];
      if (frames[0][0] !== 0 || frames.at(-1)[0] !== 1 || frames[0][1] !== rest || frames.at(-1)[1] !== rest) {
        throw new Error(`Logo clip ${motion}.${channel} must start and finish at rest.`);
      }
      for (let cycle = 0; cycle < cycles; cycle++) {
        for (const [time, value] of frames) {
          const point = [Number((elapsed + (cycle + time) * duration).toFixed(6)), value];
          const previous = tracks[channel].at(-1);
          if (point[0] === previous[0]) {
            if (point[1] !== previous[1]) throw new Error(`Discontinuous logo clip: ${motion}.${channel}`);
          } else {
            if (point[0] < previous[0]) throw new Error(`Unordered logo clip: ${motion}.${channel}`);
            tracks[channel].push(point);
          }
        }
      }
      // Cycles run back-to-back; the longer pause happens once after the batch.
      if (pause) tracks[channel].push([Number((elapsed + duration * cycles + pause).toFixed(6)), rest]);
    }
    elapsed = Number((elapsed + duration * cycles + pause).toFixed(6));
  }
  if (!elapsed) throw new Error('Logo choreography must contain at least one step.');
  // Keep the ends of each hold, dropping redundant points inside it.
  for (const [channel, points] of Object.entries(tracks)) {
    tracks[channel] = points.filter((point, index) => index === 0 || index === points.length - 1 || point[1] !== points[index - 1][1] || point[1] !== points[index + 1][1]);
  }
  return { duration: elapsed, tracks, resting };
}

export function choreographyArtwork(preset, markOnly, animation, { cat, idleContours, animate, eyeContour, stroked }) {
  const clips = motionClips();
  const sequence = choreography.sequences[animation];
  const steps = sequence ? sequence.steps : [{ motion: animation, duration: choreography.motions[animation].duration }];
  const { duration, tracks, resting } = compileTracks(steps, clips);
  const repeat = sequence ? 'indefinite' : '1';
  const contours = {
    head: {
      rest: idleContours.M.rest,
      idle: idleContours.M.flex,
      soft: 'M8.75 41L12 16L24 22Q31 28 38 22L50 16L53.25 41',
      curious: 'M8.75 41L10 14L24 22Q31 28 38 22L52 9L53.25 41',
      listen: 'M8.75 41L10 10.5L24 22Q31 28 38 22L50 16L53.25 41',
      rightEar: 'M8.75 41L12 16L24 22Q31 28 38 22L52 10.5L53.25 41',
      stretch: 'M7.75 41L9 10L23 21Q31 26 39 21L53 10L54.25 41',
    },
    tail: {
      rest: cat.r,
      idle: idleContours.r.flex,
      sleepy: 'M134 41V31C134 24 141 21 151 21H157Q168 23 169 19',
      left: 'M134 41V31C134 24 141 21 151 21H157Q168 20 164 10',
      right: 'M134 41V31C134 24 141 21 151 21H157Q168 23 174 15',
      stretch: 'M134 41V30C134 23 141 19 151 19H157Q169 18 172 10',
      curl: 'M134 41V31C134 24 141 21 151 21H157Q165 26 162 20',
      alert: 'M134 41V31C134 24 141 21 151 21H157Q168 19 170 10',
    },
    o: { rest: cat.o, stretch: 'M92.833333 30.166667C92.833333 22.666667 87.833333 18.5 82 18.5C76.166667 18.5 71.166667 22.666667 71.166667 30.166667C71.166667 37.666667 76.166667 41 82 41C87.833333 41 92.833333 37.666667 92.833333 30.166667Z' },
    l: { rest: cat.l, stretch: 'M112 10V31Q112 41 121 41' },
  };

  function tween(channel, attribute, map = value => value, transform) {
    const frames = tracks[channel];
    if (frames.every(([, value]) => value === resting[channel])) return '';
    return animate(attribute, frames.map(([, value]) => map(value)), duration, frames.map(([time]) => Number((time / duration).toFixed(9))), { repeat, transform });
  }

  function path(letter, channel) {
    return `<path data-letter="${letter}" d="${contours[channel].rest}">${tween(channel, 'd', value => contours[channel][value])}</path>`;
  }

  function transformed(channel, type, map, content) {
    const motion = tween(channel, 'transform', map, type);
    return motion ? `<g data-motion-channel="${channel}">${motion}${content}</g>` : content;
  }

  const eyes = [23, 39].map((cx, index) => {
    const contour = state => eyeContour(cx, state);
    return `<path data-eye="${index ? 'right' : 'left'}" d="${eyeContour(cx)}">${tween('eyes', 'd', contour)}</path>`;
  }).join('\n');
  const gaze = transformed('gaze', 'translate', value => `${value} 0`, `<g fill="${preset.eye}">${eyes}</g>`);
  let face = `${stroked(preset, path('M', 'head'))}${gaze}`;
  face = transformed('angle', 'rotate', value => `${value} 31 36`, face);
  face = transformed('y', 'translate', value => `0 ${value}`, face);
  face = transformed('x', 'translate', value => `${value} 0`, face);
  const body = markOnly ? '' : stroked(preset, `${path('o', 'o')}${path('l', 'l')}${path('r', 'tail')}`);
  return `<g data-head="">${face}</g>\n${body}`;
}
