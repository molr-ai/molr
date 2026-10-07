import choreography from './logo-choreography.json';

export type LogoSequence = keyof typeof choreography.sequences;

export function getLogoTimeline(sequence: LogoSequence) {
  let cursor = 0;
  const steps = choreography.sequences[sequence].steps.map(step => {
    const start = cursor;
    const activeEnd = Number((start + step.duration * (step.cycles ?? 1)).toFixed(6));
    const end = Number((activeEnd + step.pause).toFixed(6));
    cursor = end;
    return { motion: step.motion, label: step.label, start, activeEnd, end };
  });
  return { steps, duration: cursor };
}

export function getLogoPlayback(timeline: ReturnType<typeof getLogoTimeline>, elapsed: number) {
  if (!Number.isFinite(timeline.duration) || timeline.duration <= 0 || !timeline.steps.length || !Number.isFinite(elapsed)) return null;
  const position = Math.max(0, elapsed) % timeline.duration;
  const index = timeline.steps.findIndex(step => Number.isFinite(step.start) && Number.isFinite(step.activeEnd) && Number.isFinite(step.end) && step.start <= position && step.start <= step.activeEnd && step.activeEnd <= step.end && position < step.end);
  if (index === -1) return null;
  const step = timeline.steps[index];
  const waiting = position >= step.activeEnd;
  const next = timeline.steps[(index + 1) % timeline.steps.length];
  if (!Number.isFinite(next.start) || next.start < 0 || next.start >= timeline.duration) return null;
  return { index, motion: step.motion, label: step.label, waiting, nextMotion: next.motion, nextLabel: next.label, nextStart: next.start };
}
