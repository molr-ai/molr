export type SoundState = 'ready' | 'blocked' | 'unsupported';

/** Audio is unlocked by a user gesture, never queued for a later page resume. */
export class MessageSound {
  private context?: AudioContext;
  private voices = new Set<OscillatorNode>();
  private disposed = false;
  constructor(private changed: (state: SoundState) => void = () => {}) {}
  state(): SoundState {
    return typeof AudioContext === 'undefined' ? 'unsupported' : this.context?.state === 'running' ? 'ready' : 'blocked';
  }
  async unlock(): Promise<boolean> {
    if (this.disposed || this.state() === 'unsupported') return false;
    try {
      this.context ??= new AudioContext();
      this.context.onstatechange = () => { if (!this.disposed) this.changed(this.state()); };
      // Start resume synchronously inside the click/key handler. Some browsers
      // leave resume pending when blocked, so don't leave the settings UI busy.
      const resume = this.context.resume();
      let timeout: ReturnType<typeof setTimeout> | undefined;
      try { await Promise.race([resume, new Promise<void>(resolve => { timeout = setTimeout(resolve, 1200); })]); }
      finally { clearTimeout(timeout); }
      if (this.disposed) return false;
      this.changed(this.state()); return this.state() === 'ready';
    } catch { if (!this.disposed) this.changed('blocked'); return false; }
  }
  play(): boolean {
    const context = this.context;
    if (this.disposed || !context || context.state !== 'running') return false;
    this.stop();
    try {
      // A short, low-volume two-note chime; no media download or tracking URL.
      for (const [offset, frequency] of [[0, 660], [0.12, 880]]) {
        const tone = context.createOscillator(), gain = context.createGain(), start = context.currentTime + offset;
        tone.type = 'sine'; tone.frequency.value = frequency;
        gain.gain.setValueAtTime(0, start);
        gain.gain.linearRampToValueAtTime(0.06, start + 0.015);
        gain.gain.exponentialRampToValueAtTime(0.001, start + 0.16);
        tone.connect(gain); gain.connect(context.destination); this.voices.add(tone);
        tone.onended = () => { tone.disconnect(); gain.disconnect(); this.voices.delete(tone); };
        tone.start(start); tone.stop(start + 0.18);
      }
      return true;
    } catch { this.stop(); return false; }
  }
  stop() { for (const voice of this.voices) { try { voice.stop(); } catch { /* Already ended. */ } } this.voices.clear(); }
  dispose() { this.disposed = true; this.stop(); if (this.context) { this.context.onstatechange = null; void this.context.close().catch(() => {}); } }
}
