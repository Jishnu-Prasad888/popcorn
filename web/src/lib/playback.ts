/**
 * Browser playback helpers: what this browser can decode (sent to the
 * server so it decides between direct play, remux and transcode) and a
 * WebVTT parser for the subtitle overlay, which is drawn by Popcorn rather
 * than the browser so delay and styling can be applied.
 */

export interface ClientCaps {
  vc: string;
  ac: string;
  ct: string;
}

let cached: ClientCaps | null = null;

/** Probes the browser's decoders once. */
export function clientCaps(): ClientCaps {
  if (cached) return cached;
  const v = document.createElement('video');
  const ok = (type: string) => {
    try {
      const ms = (window as unknown as { MediaSource?: { isTypeSupported(t: string): boolean } }).MediaSource;
      return v.canPlayType(type) !== '' || Boolean(ms?.isTypeSupported(type));
    } catch {
      return false;
    }
  };
  const video: [string, string][] = [
    ['h264', 'video/mp4; codecs="avc1.640028"'],
    ['hevc', 'video/mp4; codecs="hvc1.1.6.L120.90"'],
    ['vp9', 'video/webm; codecs="vp9"'],
    ['vp8', 'video/webm; codecs="vp8"'],
    ['av1', 'video/mp4; codecs="av01.0.08M.08"'],
  ];
  const audio: [string, string][] = [
    ['aac', 'audio/mp4; codecs="mp4a.40.2"'],
    ['mp3', 'audio/mpeg'],
    ['opus', 'audio/webm; codecs="opus"'],
    ['vorbis', 'audio/webm; codecs="vorbis"'],
    ['flac', 'audio/flac'],
    ['ac3', 'audio/mp4; codecs="ac-3"'],
    ['eac3', 'audio/mp4; codecs="ec-3"'],
  ];
  const containers: [string, string][] = [
    ['mp4', 'video/mp4'],
    ['webm', 'video/webm'],
    ['mkv', 'video/x-matroska'],
  ];
  const pick = (list: [string, string][]) => list.filter(([, t]) => ok(t)).map(([n]) => n);
  const vc = pick(video);
  const ac = pick(audio);
  const ct = pick(containers);
  // Every browser Popcorn supports plays H.264/AAC in MP4, even when a probe
  // is inconclusive (some test environments report nothing).
  cached = {
    vc: (vc.length ? vc : ['h264']).join(','),
    ac: (ac.length ? ac : ['aac', 'mp3']).join(','),
    ct: (ct.length ? ct : ['mp4']).join(','),
  };
  return cached;
}

export interface Cue {
  start: number;
  end: number;
  /** Lines of text; simple tags (<i>, <b>, <u>) are kept, others stripped. */
  html: string;
  /** Line position as a percentage from the top, when the cue sets one. */
  line?: number;
}

function parseTime(s: string): number {
  const m = s.trim().match(/^(?:(\d+):)?(\d{1,2}):(\d{2})[.,](\d{1,3})$/);
  if (!m) return Number.NaN;
  return Number(m[1] ?? 0) * 3600 + Number(m[2]) * 60 + Number(m[3]) + Number((m[4] ?? '0').padEnd(3, '0')) / 1000;
}

const ALLOWED = /^\/?(i|b|u)$/i;

function sanitize(text: string): string {
  const escaped = text.replace(/&(?!(amp|lt|gt|nbsp|quot|#\d+);)/g, '&amp;');
  return escaped
    .replace(/<([^>]*)>/g, (_, inner: string) => {
      const tag = inner.trim().split(/[\s.]/)[0] ?? '';
      return ALLOWED.test(tag) ? `<${tag.toLowerCase()}>` : '';
    })
    .replace(/\{\\[^}]*\}/g, '')
    .replace(/\r?\n/g, '<br>');
}

/** Parses WebVTT into cues sorted by start time. */
export function parseVTT(src: string): Cue[] {
  const cues: Cue[] = [];
  const blocks = src.replace(/\r\n?/g, '\n').split(/\n{2,}/);
  for (const block of blocks) {
    const lines = block.split('\n');
    const timing = lines.findIndex((l) => l.includes('-->'));
    if (timing < 0) continue;
    const [a = '', rest = ''] = (lines[timing] ?? '').split('-->');
    const [b = '', ...settings] = rest.trim().split(/\s+/);
    const start = parseTime(a);
    const end = parseTime(b);
    if (!Number.isFinite(start) || !Number.isFinite(end)) continue;
    const text = lines.slice(timing + 1).join('\n').trim();
    if (!text) continue;
    const cue: Cue = { start, end, html: sanitize(text) };
    const line = settings.find((s) => s.startsWith('line:'));
    if (line) {
      const v = line.slice(5).split(',')[0] ?? '';
      if (v.endsWith('%')) cue.line = Number.parseFloat(v);
    }
    cues.push(cue);
  }
  cues.sort((x, y) => x.start - y.start);
  return cues;
}

/** Cues active at time t (binary search over start, then a short scan). */
export function activeCues(cues: Cue[], t: number): Cue[] {
  let lo = 0;
  let hi = cues.length;
  while (lo < hi) {
    const mid = (lo + hi) >> 1;
    if (cues[mid]!.start <= t) lo = mid + 1;
    else hi = mid;
  }
  const out: Cue[] = [];
  // Look back over cues that started earlier and may still be showing.
  for (let i = lo - 1; i >= 0 && i >= lo - 30; i--) {
    const c = cues[i]!;
    if (c.end > t) out.unshift(c);
  }
  return out;
}

/** A random id for a playback session. */
export function sessionId(): string {
  const a = new Uint8Array(8);
  crypto.getRandomValues(a);
  return Array.from(a, (b) => b.toString(16).padStart(2, '0')).join('');
}
