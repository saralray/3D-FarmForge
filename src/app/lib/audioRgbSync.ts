/* Music sync for the RGB wave (Navigation's logo toggle). Captures system
   sound via display capture (the picker's "share audio" checkbox) or falls
   back to the microphone, measures bass-weighted loudness every frame, and
   drives --rgb-hue / --rgb-pos / --rgb-level on <html>. The .rgb-music CSS
   overrides in theme.css read those vars in place of the fixed-speed
   keyframes, so the whole effect speeds up and glows with the beat. */

const MUSIC_CLASS = 'rgb-music';

export type AudioRgbSource = 'system' | 'microphone';

let cleanup: (() => void) | null = null;

async function captureStream(): Promise<{ stream: MediaStream; source: AudioRgbSource }> {
  // Processing kills the dynamics the beat detection needs, so disable it.
  const audio = { echoCancellation: false, noiseSuppression: false, autoGainControl: false };
  if (navigator.mediaDevices.getDisplayMedia) {
    try {
      // video: true is required by the API; the track is never rendered but
      // is kept alive so the browser's "stop sharing" bar can end the sync.
      const stream = await navigator.mediaDevices.getDisplayMedia({ video: true, audio });
      if (stream.getAudioTracks().length > 0) return { stream, source: 'system' };
      // Shared without ticking "share audio" — drop it and try the mic.
      stream.getTracks().forEach((t) => t.stop());
    } catch {
      // Picker cancelled or unsupported — fall through to the microphone.
    }
  }
  const stream = await navigator.mediaDevices.getUserMedia({ audio });
  return { stream, source: 'microphone' };
}

export async function startAudioRgbSync(onEnded?: () => void): Promise<AudioRgbSource> {
  stopAudioRgbSync();
  const { stream, source } = await captureStream();
  const ctx = new AudioContext();
  void ctx.resume();
  const analyser = ctx.createAnalyser();
  analyser.fftSize = 512;
  analyser.smoothingTimeConstant = 0.5;
  ctx.createMediaStreamSource(stream).connect(analyser);
  const bins = new Uint8Array(analyser.frequencyBinCount);

  const root = document.documentElement;
  root.classList.add(MUSIC_CLASS);

  let raf = 0;
  let phase = 0; // hue angle in degrees; also scrolls the gradients
  let peak = 0.05; // rolling peak for auto-gain, so quiet sources still hit 1
  let level = 0; // smoothed normalized loudness 0..1
  let baseline = 0; // slow-moving floor; level spikes above it = beats
  let flash = 0; // beat flash: snaps to 1 on a hit, decays fast
  let last = performance.now();

  const frame = (now: number) => {
    const dt = Math.min((now - last) / 1000, 0.1);
    last = now;
    analyser.getByteFrequencyData(bins);

    // Beats live in the low bins, so weight bass over the full spectrum.
    const bassEnd = Math.max(1, bins.length >> 3);
    let bass = 0;
    for (let i = 0; i < bassEnd; i++) bass += bins[i];
    bass /= bassEnd * 255;
    let all = 0;
    for (let i = 0; i < bins.length; i++) all += bins[i];
    all /= bins.length * 255;
    const loud = Math.min(1, bass * 0.7 + all * 0.6);

    // Auto-gain: normalize against a slowly decaying rolling peak so the
    // full 0..1 range is used whether the source is loud or quiet.
    peak = Math.max(peak - dt * 0.04, loud, 0.05);
    const norm = Math.min(1, loud / peak);

    // Fast attack, slow release: flashes land on the beat, then decay.
    level = norm > level ? norm : level + (norm - level) * Math.min(1, dt * 5);
    baseline += (level - baseline) * Math.min(1, dt * 2);
    // A hit is the level jumping above its own rolling floor; flash snaps
    // up instantly and decays exponentially for a strobe-like hit.
    flash = Math.max(flash * Math.exp(-dt * 6), Math.min(1, (level - baseline) * 3.5));

    // Idle drift at near-silence; loudness and beat hits spin it faster.
    phase = (phase + dt * (30 + level * 420 + flash * 1100)) % 360;

    root.style.setProperty('--rgb-hue', `${phase.toFixed(1)}deg`);
    root.style.setProperty('--rgb-pos', `${((phase / 360) * 200).toFixed(2)}%`);
    root.style.setProperty('--rgb-level', Math.min(1, level * 1.4).toFixed(3));
    root.style.setProperty('--rgb-pulse', flash.toFixed(3));
    raf = requestAnimationFrame(frame);
  };
  raf = requestAnimationFrame(frame);

  const stopAll = () => {
    cancelAnimationFrame(raf);
    stream.getTracks().forEach((t) => t.stop());
    void ctx.close();
    root.classList.remove(MUSIC_CLASS);
    root.style.removeProperty('--rgb-hue');
    root.style.removeProperty('--rgb-pos');
    root.style.removeProperty('--rgb-level');
    root.style.removeProperty('--rgb-pulse');
    cleanup = null;
  };
  cleanup = stopAll;
  // The "stop sharing" bar (or unplugging the mic) ends tracks out-of-band.
  stream.getTracks().forEach((t) =>
    t.addEventListener('ended', () => {
      if (cleanup === stopAll) {
        stopAll();
        onEnded?.();
      }
    })
  );
  return source;
}

export function stopAudioRgbSync() {
  cleanup?.();
}
