import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import type { CSSProperties, KeyboardEvent as ReactKeyboardEvent, ReactNode } from 'react';
import { Link, useNavigate, useParams, useSearchParams } from 'react-router-dom';
import {
  ArrowLeft,
  AudioLines,
  Captions,
  ChevronRight,
  Gauge,
  Info,
  Keyboard,
  Maximize,
  Minimize,
  Pause,
  PictureInPicture2,
  Play,
  Redo2,
  Settings2,
  SkipForward,
  Undo2,
  Volume1,
  Volume2,
  VolumeX,
  X,
} from 'lucide-react';

import {
  collectionQueue,
  decide,
  episodeCode,
  errorMessage,
  formatTimecode,
  getPlayInfo,
  getPlaylist,
  keyframeAt,
  landscapeOf,
  playPath,
  reportProgress,
  stopStream,
  updatePlaylist,
} from '../api/media';
import type { Card, Decision, PlayableType, PlayInfo, Ref, SubtitleStyle, SubtitleTrack } from '../api/media';
import { audioLabel, codecName } from '../lib/mediaFormat';
import { activeCues, clientCaps, parseVTT, sessionId } from '../lib/playback';
import type { Cue } from '../lib/playback';
import { updatePlayback } from '../lib/preferences';
import './PlayerPage.css';

const QUALITIES: [string, string][] = [
  ['original', 'Original'],
  ['2160p', '4K'],
  ['1080p', '1080p'],
  ['720p', '720p'],
  ['480p', '480p'],
  ['360p', '360p'],
];
const NO_CUES: Cue[] = [];
const SPEEDS = [0.5, 0.75, 1, 1.25, 1.5, 1.75, 2];

type Panel = null | 'menu' | 'audio' | 'subtitles' | 'quality' | 'speed' | 'style' | 'sync' | 'info' | 'keys';

interface QueueEntry extends Ref {
  entry?: string;
  position_secs?: number;
}

/** Resolves the play queue named by ?queue= into references, in order. */
async function loadQueue(queue: string): Promise<{ items: QueueEntry[]; repeat?: string } | null> {
  const [kind, id] = queue.split(':');
  if (kind === 'collection' && id) {
    return { items: (await collectionQueue(id)).queue };
  }
  if (kind === 'playlist' && id) {
    const p = await getPlaylist(id);
    return {
      items: p.entries.map((e) => ({ library_id: e.library_id, type: e.type, id: e.id, entry: e.entry_id, position_secs: e.position_secs })),
      repeat: p.playlist.repeat,
    };
  }
  return null;
}

function styleFor(s: SubtitleStyle): CSSProperties {
  const bg = s.background === 'box' ? `rgb(0 0 0 / ${s.opacity / 100})` : 'transparent';
  const fonts: Record<string, string> = {
    sans: 'var(--font-ui)',
    serif: 'Georgia, "Times New Roman", serif',
    mono: 'var(--font-mono)',
    rounded: '"Chakra Petch", var(--font-ui)',
  };
  return {
    '--sub-size': `${s.size / 100}`,
    '--sub-color': s.color,
    '--sub-bg': bg,
    '--sub-bottom': `${s.position}%`,
    '--sub-shadow': s.background === 'shadow' ? '0 0 4px #000, 0 2px 3px #000, 0 0 10px rgb(0 0 0 / 0.8)' : 'none',
    '--sub-font': fonts[s.font] ?? fonts.sans,
  } as CSSProperties;
}

/** Each item gets a fresh player, so no state leaks from one to the next. */
export default function PlayerPage() {
  const { lib = '', type = 'movie', id = '' } = useParams();
  return <Player key={`${lib}/${type}/${id}`} lib={lib} type={type} id={id} />;
}

function Player({ lib, type, id }: { lib: string; type: string; id: string }) {
  const [params] = useSearchParams();
  const navigate = useNavigate();
  const queueName = params.get('queue') ?? '';
  const entryParam = params.get('entry') ?? '';

  const videoRef = useRef<HTMLVideoElement>(null);
  const stageRef = useRef<HTMLDivElement>(null);
  const session = useMemo(sessionId, []);

  const [info, setInfo] = useState<PlayInfo | null>(null);
  const [error, setError] = useState('');
  const [fileId, setFileId] = useState(params.get('file') ?? '');
  const [audio, setAudio] = useState(-1);
  const [subtitle, setSubtitle] = useState('');
  const [quality, setQuality] = useState('original');
  const [audioDelay, setAudioDelay] = useState(0);
  const [subDelay, setSubDelay] = useState(0);
  const [speed, setSpeed] = useState(1);
  const [style, setStyle] = useState<SubtitleStyle | null>(null);
  const [decision, setDecision] = useState<Decision | null>(null);
  const [src, setSrc] = useState('');
  const [offset, setOffset] = useState(0);
  const [baseUrl, setBaseUrl] = useState('');
  const [method, setMethod] = useState<Decision['method']>('direct');
  const [loadedCues, setLoadedCues] = useState<{ id: string; cues: Cue[] }>({ id: '', cues: [] });

  const [time, setTime] = useState(0);
  const [buffered, setBuffered] = useState(0);
  const [playing, setPlaying] = useState(false);
  const [waiting, setWaiting] = useState(true);
  const [volume, setVolume] = useState(() => Number(localStorage.getItem('popcorn.volume') ?? '1'));
  const [muted, setMuted] = useState(false);
  const [panel, setPanel] = useState<Panel>(null);
  const [idle, setIdle] = useState(false);
  const [fullscreen, setFullscreen] = useState(false);
  const [countdown, setCountdown] = useState<number | null>(null);
  const [countdownDismissed, setCountdownDismissed] = useState(false);
  const [toast, setToast] = useState('');
  const [queue, setQueue] = useState<{ items: QueueEntry[]; repeat?: string } | null>(null);
  const introSkipped = useRef(false);
  const creditsSkipped = useRef(false);
  const startAt = useRef(0);
  const lastReport = useRef(0);

  const duration = info?.files.find((f) => f.id === (fileId || info.file_id))?.duration_secs ?? info?.item.duration_secs ?? 0;
  const file = info?.files.find((f) => f.id === (fileId || info?.file_id));
  const prefs = info?.preferences;

  // ---------- load ----------
  useEffect(() => {
    let cancelled = false;
    getPlayInfo(lib, type as PlayableType, id, params.get('file') ?? undefined)
      .then((pi) => {
        if (cancelled) return;
        setInfo(pi);
        setFileId(pi.file_id);
        setAudio(pi.audio);
        setSubtitle(pi.subtitle);
        setQuality(pi.preferences.quality || 'original');
        setSpeed(pi.preferences.speed || 1);
        setStyle(pi.preferences.subtitle_style);
        startAt.current = params.get('restart') ? 0 : pi.resume_secs;
      })
      .catch((err: unknown) => !cancelled && setError(errorMessage(err)));
    return () => {
      cancelled = true;
    };
    // params is read once per item.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [lib, type, id]);

  useEffect(() => {
    if (!queueName) return;
    let cancelled = false;
    loadQueue(queueName)
      .then((q) => !cancelled && setQueue(q))
      .catch(() => undefined);
    return () => {
      cancelled = true;
    };
  }, [queueName]);

  // A playlist entry resumes where it was left in the playlist.
  useEffect(() => {
    if (!queue || !entryParam || params.get('restart')) return;
    const e = queue.items.find((x) => x.entry === entryParam);
    if (e?.position_secs && e.position_secs > 30 && time < 1) startAt.current = e.position_secs;
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [queue, entryParam]);

  // ---------- choose the stream ----------
  const subTrack = info?.subtitles.find((s) => s.id === subtitle);
  const burn = subTrack?.image_based ? subTrack.type_index : -1;

  const activeFile = fileId || info?.file_id || '';

  // Starts a converted stream at `at`. When video is copied, ffmpeg can only
  // begin on a keyframe, so the stream really starts a little earlier: the
  // server reports that point (zero), the clock is offset by it, and the
  // element skips forward to `at`.
  const restartSeq = useRef(0);
  const restartAt = useCallback(
    async (url: string, at: number, videoCopy: boolean) => {
      const seq = ++restartSeq.current;
      let zero = at;
      if (videoCopy && at > 0) zero = await keyframeAt(lib, activeFile, at).then((r) => r.zero, () => at);
      // A later seek replaced this one while the keyframe was looked up.
      if (seq !== restartSeq.current) return;
      setOffset(zero);
      setTime(at);
      setBaseUrl(url);
      startAt.current = at - zero > 0.25 ? at - zero : 0;
      setSrc(`${url}&start=${at.toFixed(3)}&session=${session}`);
    },
    [lib, activeFile, session],
  );

  const loadStream = useCallback(
    async (at: number) => {
      if (!info || audio < 0) return;
      const caps = clientCaps();
      const p: Record<string, string | number> = { audio, vc: caps.vc, ac: caps.ac, ct: caps.ct };
      if (quality !== 'original') p.quality = quality;
      if (burn >= 0) p.burn = burn;
      if (audioDelay) p.adelay = audioDelay;
      if (prefs?.max_bitrate) p.bitrate = prefs.max_bitrate;
      if (prefs?.passthrough?.length) p.pt = prefs.passthrough.join(',');
      try {
        const d = await decide(lib, fileId || info.file_id, p);
        setDecision(d.decision);
        setMethod(d.decision.method);
        if (d.decision.method === 'direct') {
          setOffset(0);
          setBaseUrl(d.url);
          setSrc(d.url);
          startAt.current = at;
        } else {
          await restartAt(d.url, at, d.decision.video_copy);
        }
      } catch (err) {
        setError(errorMessage(err));
      }
    },
    [info, audio, quality, burn, audioDelay, prefs, lib, fileId, restartAt],
  );

  // Where a seek is heading until the video gets there. Until then the old
  // stream's clock is stale, so position and the time display use this.
  const pendingSeek = useRef<number | null>(null);
  const position = useCallback(() => pendingSeek.current ?? offset + (videoRef.current?.currentTime ?? 0), [offset]);

  // (Re)start when the stream inputs change, keeping the position.
  const streamKey = `${fileId}|${audio}|${quality}|${burn}|${audioDelay}`;
  const lastStreamKey = useRef('');
  useEffect(() => {
    if (!info || audio < 0) return;
    if (lastStreamKey.current === streamKey) return;
    const first = lastStreamKey.current === '';
    lastStreamKey.current = streamKey;
    void loadStream(first ? startAt.current : position());
  }, [info, audio, streamKey, loadStream, position]);

  // Apply the start point once the element knows its media.
  const onLoaded = () => {
    const v = videoRef.current;
    if (!v) return;
    if (startAt.current > 0) {
      v.currentTime = startAt.current;
      startAt.current = 0;
    } else {
      pendingSeek.current = null;
    }
    v.playbackRate = speed;
    void v.play().catch(() => setPlaying(false));
  };

  // ---------- subtitles ----------
  useEffect(() => {
    if (!subTrack || subTrack.image_based || !subTrack.url) return;
    const trackId = subTrack.id;
    let cancelled = false;
    fetch(subTrack.url)
      .then((r) => (r.ok ? r.text() : Promise.reject(new Error('Subtitle unavailable'))))
      .then((t) => !cancelled && setLoadedCues({ id: trackId, cues: parseVTT(t) }))
      .catch(() => !cancelled && flash('This subtitle could not be loaded.'));
    return () => {
      cancelled = true;
    };
  }, [subTrack]);

  const cues = loadedCues.id === subtitle ? loadedCues.cues : NO_CUES;
  const shownCues = useMemo(() => activeCues(cues, time - subDelay / 1000), [cues, time, subDelay]);

  // ---------- seeking ----------
  const restartTimer = useRef(0);
  useEffect(() => () => window.clearTimeout(restartTimer.current), []);
  const seek = useCallback(
    (to: number) => {
      const v = videoRef.current;
      if (!v) return;
      const t = Math.max(0, Math.min(duration > 0 ? duration - 1 : to, to));
      pendingSeek.current = t;
      setTime(t);
      window.clearTimeout(restartTimer.current);
      if (method === 'direct') {
        v.currentTime = t;
        return;
      }
      // Within what this stream has already delivered, seek in place;
      // otherwise restart the stream there.
      const rel = t - offset;
      let inBuffer = false;
      for (let i = 0; i < v.buffered.length; i++) {
        if (rel >= v.buffered.start(i) && rel <= v.buffered.end(i) - 0.5) inBuffer = true;
      }
      if (rel >= 0 && inBuffer) {
        v.currentTime = rel;
        return;
      }
      // Restarting the stream is costly: held or repeated arrow keys and a
      // dragged bar settle first, then one restart goes to where they ended.
      restartTimer.current = window.setTimeout(() => void restartAt(baseUrl, t, decision?.video_copy ?? false), 350);
    },
    [duration, method, offset, baseUrl, decision, restartAt],
  );

  const skip = useCallback((d: number) => seek(position() + d), [seek, position]);

  // ---------- progress ----------
  const audioLang = file?.streams?.find((s) => s.kind === 'audio' && s.type_index === audio)?.language ?? '';
  const subLang = subtitle ? (subTrack?.language ?? '') : 'off';
  const queueEntry = queue?.items.find((x) => x.entry === entryParam);

  const report = useCallback(
    (ended = false) => {
      if (!info || duration <= 0) return;
      const pos = position();
      if (pos < 1 && !ended) return;
      const body = {
        library_id: lib,
        type: type as PlayableType,
        id,
        file_id: fileId,
        position_secs: ended ? duration : pos,
        duration_secs: duration,
        audio_language: audioLang,
        subtitle_language: subLang,
        ended,
        ...(queueName.startsWith('playlist:') && queueEntry ? { playlist_id: queueName.split(':')[1], playlist_entry: queueEntry.entry } : {}),
      };
      lastReport.current = Date.now();
      void reportProgress(body).catch(() => undefined);
    },
    [info, duration, position, lib, type, id, fileId, audioLang, subLang, queueName, queueEntry],
  );

  const reportRef = useRef(report);
  useEffect(() => {
    reportRef.current = report;
  });

  useEffect(() => {
    const onHide = () => {
      if (document.visibilityState === 'hidden') reportRef.current();
    };
    document.addEventListener('visibilitychange', onHide);
    return () => {
      document.removeEventListener('visibilitychange', onHide);
      reportRef.current();
      void stopStream(session).catch(() => undefined);
    };
  }, [session]);

  // ---------- next item ----------
  const next = useMemo((): { to: string; card: Card | null; label: string } | null => {
    if (queue && queue.items.length) {
      const idx = queue.items.findIndex((x) => (entryParam ? x.entry === entryParam : x.type === type && x.id === id));
      let n: QueueEntry | undefined;
      if (queue.repeat === 'one' && idx >= 0) {
        n = queue.items[idx];
      } else {
        n = queue.items[idx + 1] ?? (queue.repeat === 'all' ? queue.items[0] : undefined);
      }
      if (!n) return null;
      const qs = new URLSearchParams({ queue: queueName });
      if (n.entry) qs.set('entry', n.entry);
      const same = n.id === info?.next?.id;
      return { to: `/play/${n.library_id}/${n.type}/${n.id}?${qs}`, card: same ? (info?.next ?? null) : null, label: same && info?.next ? `${episodeCode(info.next)} ${info.next.title}` : 'Next in queue' };
    }
    if (info?.next) {
      return { to: playPath(info.next, queueName ? { queue: queueName } : {}), card: info.next, label: `${episodeCode(info.next)} ${info.next.title}` };
    }
    return null;
  }, [queue, entryParam, type, id, queueName, info]);

  const goNext = useCallback(() => {
    if (!next) return;
    report();
    if (queueName.startsWith('playlist:')) {
      const e = new URLSearchParams(next.to.split('?')[1]).get('entry');
      if (e) void updatePlaylist(queueName.split(':')[1] ?? '', { current_entry: e }).catch(() => undefined);
    }
    navigate(next.to, { replace: true });
  }, [next, navigate, report, queueName]);

  // ---------- markers ----------
  const m = info?.markers ?? {};
  const inIntro = m.intro_start != null && m.intro_end != null && time >= m.intro_start && time < m.intro_end - 1;
  const creditsAt = m.credits_start ?? (duration > 0 ? duration - Math.min(30, duration * 0.05) : Infinity);
  const inCredits = time >= creditsAt && duration > 0 && time < duration;

  useEffect(() => {
    if (inIntro && prefs?.skip_intro === 'auto' && !introSkipped.current && m.intro_end != null) {
      introSkipped.current = true;
      seek(m.intro_end);
      flash('Skipped the intro');
    }
  }, [inIntro, prefs, m.intro_end, seek]);

  useEffect(() => {
    if (!inCredits || countdownDismissed || countdown !== null) return;
    if (next && prefs?.autoplay_next) {
      // Playback position comes from the video element, an external clock.
      // eslint-disable-next-line react-hooks/set-state-in-effect
      setCountdown(prefs.next_countdown || 10);
    } else if (prefs?.skip_credits === 'auto' && !creditsSkipped.current && next) {
      creditsSkipped.current = true;
      goNext();
    }
  }, [inCredits, countdownDismissed, countdown, next, prefs, goNext]);

  useEffect(() => {
    if (countdown === null) return;
    if (countdown <= 0) {
      goNext();
      return;
    }
    if (!playing) return;
    const t = window.setTimeout(() => setCountdown((c) => (c === null ? null : c - 1)), 1000);
    return () => window.clearTimeout(t);
  }, [countdown, playing, goNext]);

  // ---------- video events ----------
  const onTime = () => {
    const v = videoRef.current;
    if (!v) return;
    if (pendingSeek.current !== null) return;
    setTime(offset + v.currentTime);
    if (v.buffered.length) setBuffered(offset + v.buffered.end(v.buffered.length - 1));
    if (Date.now() - lastReport.current > 10000 && !v.paused) report();
  };

  const onEnded = () => {
    report(true);
    if (next && prefs?.autoplay_next !== false) goNext();
    else setPlaying(false);
  };

  // ---------- controls ----------
  const togglePlay = useCallback(() => {
    const v = videoRef.current;
    if (!v) return;
    if (v.paused) void v.play();
    else {
      v.pause();
      report();
    }
  }, [report]);

  const setVol = (val: number) => {
    const v = videoRef.current;
    const x = Math.max(0, Math.min(1, val));
    setVolume(x);
    setMuted(x === 0);
    if (v) {
      v.volume = x;
      v.muted = x === 0;
    }
    localStorage.setItem('popcorn.volume', String(x));
  };

  useEffect(() => {
    const v = videoRef.current;
    if (v) {
      v.volume = volume;
      v.muted = muted;
    }
  }, [volume, muted, src]);

  useEffect(() => {
    if (videoRef.current) videoRef.current.playbackRate = speed;
  }, [speed]);

  const toggleFullscreen = useCallback(() => {
    const el = stageRef.current;
    if (!el) return;
    if (document.fullscreenElement) void document.exitFullscreen();
    else void el.requestFullscreen?.().catch(() => undefined);
  }, []);

  useEffect(() => {
    const on = () => setFullscreen(Boolean(document.fullscreenElement));
    document.addEventListener('fullscreenchange', on);
    return () => document.removeEventListener('fullscreenchange', on);
  }, []);

  const pip = async () => {
    const v = videoRef.current as (HTMLVideoElement & { requestPictureInPicture?: () => Promise<unknown> }) | null;
    try {
      if (document.pictureInPictureElement) await document.exitPictureInPicture();
      else await v?.requestPictureInPicture?.();
    } catch {
      flash('Picture-in-picture is not available here.');
    }
  };

  const flashTimer = useRef(0);
  function flash(msg: string) {
    setToast(msg);
    window.clearTimeout(flashTimer.current);
    flashTimer.current = window.setTimeout(() => setToast(''), 1800);
  }

  // Controls hide after a few seconds of stillness while playing.
  const idleTimer = useRef(0);
  const wake = useCallback(() => {
    setIdle(false);
    window.clearTimeout(idleTimer.current);
    idleTimer.current = window.setTimeout(() => setIdle(true), 3000);
  }, []);
  useEffect(() => () => window.clearTimeout(idleTimer.current), []);

  const textSubs = useMemo(() => info?.subtitles ?? [], [info]);
  const cycleSubs = useCallback(() => {
    const ids = ['', ...textSubs.map((s) => s.id)];
    const i = ids.indexOf(subtitle);
    const n = ids[(i + 1) % ids.length] ?? '';
    setSubtitle(n);
    const t = textSubs.find((s) => s.id === n);
    flash(t ? `Subtitles: ${trackName(t)}` : 'Subtitles off');
  }, [textSubs, subtitle]);

  const cycleAudio = useCallback(() => {
    const list = file?.streams?.filter((s) => s.kind === 'audio') ?? [];
    if (list.length < 2) return;
    const i = list.findIndex((s) => s.type_index === audio);
    const n = list[(i + 1) % list.length]!;
    setAudio(n.type_index);
    flash(`Audio: ${audioLabel(n)}`);
  }, [file, audio]);

  // ---------- keyboard ----------
  const onKey = useCallback(
    (e: KeyboardEvent) => {
      const t = e.target as HTMLElement;
      // Text fields keep their keys; the seek and volume sliders don't, so
      // the arrows still skip after clicking the bar (and don't nudge it 1 s).
      const slider = t instanceof HTMLInputElement && t.type === 'range';
      if (/^(SELECT|TEXTAREA)$/.test(t.tagName) || (t.tagName === 'INPUT' && !slider)) return;
      const step = e.shiftKey ? 30 : 10;
      const k = e.key;
      let handled = true;
      if (k === ' ' || k === 'k') togglePlay();
      else if (k === 'ArrowLeft' || k === 'j') skip(-(k === 'j' ? (prefs?.skip_backward ?? 10) : step));
      else if (k === 'ArrowRight' || k === 'l') skip(k === 'l' ? (prefs?.skip_forward ?? 30) : step);
      else if (k === 'ArrowUp') setVol(volume + 0.05);
      else if (k === 'ArrowDown') setVol(volume - 0.05);
      else if (k === 'm') setMuted((x) => !x);
      else if (k === 'f') toggleFullscreen();
      else if (k === 'c') cycleSubs();
      else if (k === 'a') cycleAudio();
      else if (k === 'i') setPanel((p) => (p === 'info' ? null : 'info'));
      else if (k === 'p') void pip();
      else if (k === 'n' || (k === 'N' && e.shiftKey)) goNext();
      else if (k === 's' && (inIntro || inCredits)) {
        if (inIntro && m.intro_end != null) seek(m.intro_end);
        else goNext();
      } else if (k === '>' || k === '.') {
        const s = SPEEDS[Math.min(SPEEDS.length - 1, SPEEDS.indexOf(speed) + 1)] ?? speed;
        setSpeed(s);
        flash(`Speed ${s}×`);
      } else if (k === '<' || k === ',') {
        const s = SPEEDS[Math.max(0, SPEEDS.indexOf(speed) - 1)] ?? speed;
        setSpeed(s);
        flash(`Speed ${s}×`);
      } else if (k === 'g') setSubDelay((d) => d - 100);
      else if (k === 'h') setSubDelay((d) => d + 100);
      else if (k === '?') setPanel((p) => (p === 'keys' ? null : 'keys'));
      else if (k === 'Escape') {
        if (panel) setPanel(null);
        else if (!document.fullscreenElement) navigate(-1);
        else handled = false;
      } else if (/^[0-9]$/.test(k) && duration > 0) seek((Number(k) / 10) * duration);
      else handled = false;
      if (handled) {
        e.preventDefault();
        wake();
      }
    },
    // pip and setVol are stable enough for a key handler re-created on render.
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [togglePlay, skip, prefs, volume, toggleFullscreen, cycleSubs, cycleAudio, goNext, inIntro, inCredits, m.intro_end, seek, speed, panel, navigate, duration, wake],
  );

  useEffect(() => {
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [onKey]);

  useEffect(() => {
    if (subDelay) flash(`Subtitle delay ${subDelay > 0 ? '+' : ''}${subDelay} ms`);
  }, [subDelay]);

  // ---------- render ----------
  if (error) {
    return (
      <div className="player player-error">
        <div>
          <h1>This can't be played right now</h1>
          <p>{error}</p>
          <button type="button" className="btn-ghost" onClick={() => navigate(-1)}>
            <ArrowLeft size={17} /> Go back
          </button>
        </div>
      </div>
    );
  }

  const item = info?.item;
  const title = item?.type === 'episode' ? item.show_title : item?.title;
  const subtitleLine = item?.type === 'episode' ? `${episodeCode(item)} ${item.title}` : item?.year ? String(item.year) : '';
  const pct = (x: number) => (duration > 0 ? `${Math.min(100, (x / duration) * 100)}%` : '0%');
  const audioTracks = file?.streams?.filter((s) => s.kind === 'audio') ?? [];
  const showControls = !playing || !idle || panel !== null;
  const VolIcon = muted || volume === 0 ? VolumeX : volume < 0.5 ? Volume1 : Volume2;

  return (
    <div
      ref={stageRef}
      className="player"
      data-idle={!showControls}
      onMouseMove={wake}
      onTouchStart={wake}
      onKeyDown={(e: ReactKeyboardEvent) => e.stopPropagation()}
      style={style ? styleFor(style) : undefined}
    >
      {src && (
        <video
          ref={videoRef}
          className="player-video"
          src={src}
          playsInline
          autoPlay
          poster={item ? (landscapeOf(item) ?? undefined) : undefined}
          onLoadedMetadata={onLoaded}
          onTimeUpdate={onTime}
          onPlay={() => setPlaying(true)}
          onPause={() => setPlaying(false)}
          onWaiting={() => setWaiting(true)}
          onPlaying={() => setWaiting(false)}
          onCanPlay={() => setWaiting(false)}
          onSeeked={() => {
            pendingSeek.current = null;
          }}
          onEnded={onEnded}
          onError={() => {
            const v = videoRef.current;
            if (method === 'direct' && info?.ffmpeg) {
              // The browser refused the original file: convert it instead.
              const at = v?.currentTime ?? startAt.current;
              void (async () => {
                const caps = clientCaps();
                const d = await decide(lib, fileId, { audio, vc: caps.vc, ac: caps.ac, ct: caps.ct, method: 'remux' }).catch(() => null);
                if (!d) return;
                setDecision({ ...d.decision, reasons: [...d.decision.reasons, 'the browser could not open the original file'] });
                setMethod(d.decision.method === 'direct' ? 'remux' : d.decision.method);
                await restartAt(d.url, at, d.decision.video_copy);
              })();
            } else {
              setError(method === 'direct' ? 'This browser cannot play this file, and ffmpeg is not installed on the server to convert it.' : 'The stream stopped unexpectedly. The file may have gone offline.');
            }
          }}
          onClick={togglePlay}
          onDoubleClick={toggleFullscreen}
        />
      )}

      <div className="player-subs" aria-live="off">
        {shownCues.map((c, i) => (
          // Cue text is sanitised to <i>, <b>, <u> and <br> by parseVTT.
          <p key={i} style={c.line !== undefined && c.line < 50 ? { order: -1 } : undefined}>
            <span dangerouslySetInnerHTML={{ __html: c.html }} />
          </p>
        ))}
      </div>

      {(waiting || !info) && (
        <div className="player-spinner" role="status" aria-label="Loading">
          <span />
        </div>
      )}

      {toast && <div className="player-flash">{toast}</div>}

      <header className="player-top">
        <button type="button" className="player-icon" onClick={() => navigate(-1)} aria-label="Back">
          <ArrowLeft size={24} />
        </button>
        <div className="player-titles">
          <strong>{title}</strong>
          <span>{subtitleLine}</span>
        </div>
        {decision && (
          <span className={`player-method method-${method}`} title={decision.reasons.join('; ') || 'Playing the original file'}>
            {method === 'direct' ? 'Direct play' : method === 'remux' ? 'Remux' : 'Transcode'}
          </span>
        )}
      </header>

      {inIntro && prefs?.skip_intro !== 'off' && m.intro_end != null && (
        <button type="button" className="player-skip" onClick={() => seek(m.intro_end!)}>
          Skip intro <ChevronRight size={18} />
        </button>
      )}
      {inCredits && next && countdown === null && prefs?.skip_credits !== 'off' && !countdownDismissed && (
        <button type="button" className="player-skip" onClick={goNext}>
          {item?.type === 'episode' ? 'Next episode' : 'Play next'} <SkipForward size={18} />
        </button>
      )}

      {countdown !== null && next && (
        <aside className="player-next" aria-live="polite">
          {next.card && landscapeOf(next.card) && <img src={landscapeOf(next.card)!} alt="" />}
          <div>
            <p className="player-next-label">Up next in {countdown}</p>
            <p className="player-next-title">{next.label}</p>
            <div className="player-next-actions">
              <button type="button" className="btn-play" onClick={goNext}>
                <Play size={18} fill="currentColor" /> Play now
              </button>
              <button
                type="button"
                className="btn-ghost"
                onClick={() => {
                  setCountdown(null);
                  setCountdownDismissed(true);
                }}
              >
                Cancel
              </button>
            </div>
          </div>
          <span className="player-next-ring" style={{ animationDuration: `${prefs?.next_countdown ?? 10}s` }} />
        </aside>
      )}

      <div className="player-bottom">
        <div className="player-seek">
          <span className="player-time">{formatTimecode(time)}</span>
          <div className="player-track">
            <span className="player-buffer" style={{ left: pct(offset), width: `calc(${pct(buffered)} - ${pct(offset)})` }} />
            <span className="player-played" style={{ width: pct(time) }} />
            {m.intro_start != null && m.intro_end != null && (
              <span className="player-marker" style={{ left: pct(m.intro_start), width: `calc(${pct(m.intro_end)} - ${pct(m.intro_start)})` }} title="Intro" />
            )}
            {m.credits_start != null && <span className="player-marker player-marker-credits" style={{ left: pct(m.credits_start), right: 0 }} title="Credits" />}
            <input
              type="range"
              min={0}
              max={Math.max(1, duration)}
              step={1}
              value={Math.min(time, duration)}
              onChange={(e) => seek(Number(e.target.value))}
              aria-label="Seek"
              aria-valuetext={`${formatTimecode(time)} of ${formatTimecode(duration)}`}
            />
          </div>
          <span className="player-time">-{formatTimecode(Math.max(0, duration - time))}</span>
        </div>
        <div className="player-controls">
          <button type="button" className="player-icon player-icon-big" onClick={togglePlay} aria-label={playing ? 'Pause' : 'Play'}>
            {playing ? <Pause size={30} fill="currentColor" /> : <Play size={30} fill="currentColor" />}
          </button>
          <button type="button" className="player-icon" onClick={() => skip(-(prefs?.skip_backward ?? 10))} aria-label={`Back ${prefs?.skip_backward ?? 10} seconds`}>
            <Undo2 size={22} />
            <small>{prefs?.skip_backward ?? 10}</small>
          </button>
          <button type="button" className="player-icon" onClick={() => skip(prefs?.skip_forward ?? 30)} aria-label={`Forward ${prefs?.skip_forward ?? 30} seconds`}>
            <Redo2 size={22} />
            <small>{prefs?.skip_forward ?? 30}</small>
          </button>
          {next && (
            <button type="button" className="player-icon" onClick={goNext} aria-label="Next" title={next.label}>
              <SkipForward size={22} />
            </button>
          )}
          <div className="player-volume">
            <button type="button" className="player-icon" onClick={() => setMuted((x) => !x)} aria-label={muted ? 'Unmute' : 'Mute'}>
              <VolIcon size={22} />
            </button>
            <input type="range" min={0} max={1} step={0.02} value={muted ? 0 : volume} onChange={(e) => setVol(Number(e.target.value))} aria-label="Volume" />
          </div>
          <span className="player-spacer" />
          <button type="button" className="player-icon" onClick={() => setPanel(panel === 'subtitles' ? null : 'subtitles')} aria-label="Subtitles" aria-pressed={Boolean(subtitle)}>
            <Captions size={22} />
          </button>
          {audioTracks.length > 1 && (
            <button type="button" className="player-icon" onClick={() => setPanel(panel === 'audio' ? null : 'audio')} aria-label="Audio track">
              <AudioLines size={22} />
            </button>
          )}
          <button type="button" className="player-icon" onClick={() => setPanel(panel ? null : 'menu')} aria-label="Playback settings" aria-expanded={panel !== null}>
            <Settings2 size={22} />
          </button>
          <button type="button" className="player-icon" onClick={() => void pip()} aria-label="Picture in picture">
            <PictureInPicture2 size={22} />
          </button>
          <button type="button" className="player-icon" onClick={toggleFullscreen} aria-label={fullscreen ? 'Exit full screen' : 'Full screen'}>
            {fullscreen ? <Minimize size={22} /> : <Maximize size={22} />}
          </button>
        </div>
      </div>

      {panel && info && (
        <div className="player-panel" role="dialog" aria-label="Playback settings">
          <header>
            {panel !== 'menu' && panel !== 'subtitles' && panel !== 'audio' ? (
              <button type="button" className="player-panel-back" onClick={() => setPanel('menu')}>
                <ArrowLeft size={16} /> {PANEL_TITLES[panel]}
              </button>
            ) : (
              <strong>{PANEL_TITLES[panel]}</strong>
            )}
            <button type="button" className="player-icon" onClick={() => setPanel(null)} aria-label="Close">
              <X size={18} />
            </button>
          </header>
          <div className="player-panel-body">
            {panel === 'menu' && (
              <ul className="pp-menu">
                <MenuRow icon={<AudioLines size={18} />} label="Audio" value={audioTracks.find((a) => a.type_index === audio) ? audioLabel(audioTracks.find((a) => a.type_index === audio)!) : '—'} onClick={() => setPanel('audio')} />
                <MenuRow icon={<Captions size={18} />} label="Subtitles" value={subTrack ? trackName(subTrack) : 'Off'} onClick={() => setPanel('subtitles')} />
                <MenuRow icon={<Settings2 size={18} />} label="Quality" value={QUALITIES.find(([q]) => q === quality)?.[1] ?? quality} onClick={() => setPanel('quality')} />
                <MenuRow icon={<Gauge size={18} />} label="Speed" value={`${speed}×`} onClick={() => setPanel('speed')} />
                <MenuRow icon={<Captions size={18} />} label="Subtitle style" value={`${style?.size ?? 100}%`} onClick={() => setPanel('style')} />
                <MenuRow icon={<Undo2 size={18} />} label="Sync" value={subDelay || audioDelay ? 'Adjusted' : 'In sync'} onClick={() => setPanel('sync')} />
                {info.files.length > 1 && (
                  <li className="pp-versions">
                    <span>Version</span>
                    <select value={fileId} onChange={(e) => setFileId(e.target.value)} aria-label="Version">
                      {info.files.map((f) => (
                        <option key={f.id} value={f.id}>
                          {[f.edition || f.name, f.resolution].filter(Boolean).join(', ')}
                        </option>
                      ))}
                    </select>
                  </li>
                )}
                <MenuRow icon={<Info size={18} />} label="Stream info" value="" onClick={() => setPanel('info')} />
                <MenuRow icon={<Keyboard size={18} />} label="Keyboard shortcuts" value="?" onClick={() => setPanel('keys')} />
              </ul>
            )}
            {panel === 'audio' && (
              <Choices
                items={audioTracks.map((a) => ({ id: String(a.type_index), label: audioLabel(a), hint: a.is_default ? 'Default' : '' }))}
                value={String(audio)}
                onPick={(v) => {
                  setAudio(Number(v));
                  const t = audioTracks.find((a) => String(a.type_index) === v);
                  if (t?.language) void updatePlayback({ audio_languages: dedupeFront(prefs?.audio_languages ?? [], t.language) }).catch(() => undefined);
                }}
              />
            )}
            {panel === 'subtitles' && (
              <>
                <Choices
                  items={[{ id: '', label: 'Off', hint: '' }, ...textSubs.map((s) => ({ id: s.id, label: trackName(s), hint: subtitleHint(s) }))]}
                  value={subtitle}
                  onPick={setSubtitle}
                />
                {textSubs.length === 0 && <p className="pp-note">This title has no subtitles. Editors can add a file from the title's Manage menu.</p>}
              </>
            )}
            {panel === 'quality' && (
              <>
                <Choices items={QUALITIES.map(([q, l]) => ({ id: q, label: l, hint: q === 'original' && file?.resolution ? file.resolution : '' }))} value={quality} onPick={setQuality} />
                <p className="pp-note">Lower qualities are transcoded on the server, which uses more of its CPU.</p>
              </>
            )}
            {panel === 'speed' && <Choices items={SPEEDS.map((s) => ({ id: String(s), label: s === 1 ? 'Normal' : `${s}×`, hint: '' }))} value={String(speed)} onPick={(v) => setSpeed(Number(v))} />}
            {panel === 'style' && style && (
              <StylePanel
                style={style}
                onChange={(s) => {
                  setStyle(s);
                  void updatePlayback({ subtitle_style: s }).catch(() => undefined);
                }}
              />
            )}
            {panel === 'sync' && (
              <div className="pp-sync">
                <Stepper label="Subtitle delay" value={subDelay} unit="ms" step={100} onChange={setSubDelay} hint="Later subtitles: +. Keys G and H." />
                <Stepper label="Audio delay" value={audioDelay} unit="ms" step={50} onChange={setAudioDelay} hint="Applied on the server; the stream restarts." />
              </div>
            )}
            {panel === 'info' && (
              <dl className="pp-info">
                <dt>Playback</dt>
                <dd>{method === 'direct' ? 'Direct play' : method === 'remux' ? 'Remux (copying streams)' : 'Transcode'}</dd>
                {decision?.reasons.length ? (
                  <>
                    <dt>Why</dt>
                    <dd>{decision.reasons.join('; ')}</dd>
                  </>
                ) : null}
                <dt>Video</dt>
                <dd>
                  {[codecName(file?.video_codec), file?.resolution, file?.hdr, file?.frame_rate ? `${file.frame_rate.toFixed(3).replace(/\.?0+$/, '')} fps` : '', decision && method !== 'direct' ? (decision.video_copy ? 'copied' : 'encoded to H.264') : '']
                    .filter(Boolean)
                    .join(', ')}
                </dd>
                <dt>Audio</dt>
                <dd>
                  {audioTracks.find((a) => a.type_index === audio) ? audioLabel(audioTracks.find((a) => a.type_index === audio)!) : '—'}
                  {decision && method !== 'direct' ? (decision.audio_copy ? ', copied' : ', encoded to AAC') : ''}
                </dd>
                <dt>Container</dt>
                <dd>{file?.container}</dd>
                <dt>Bitrate</dt>
                <dd>{file?.bitrate ? `${(file.bitrate / 1e6).toFixed(1)} Mb/s` : '—'}</dd>
                <dt>File</dt>
                <dd className="pp-path">{file?.rel_path}</dd>
              </dl>
            )}
            {panel === 'keys' && (
              <dl className="pp-keys">
                {KEYS.map(([k, d]) => (
                  <div key={k}>
                    <dt>
                      <kbd>{k}</kbd>
                    </dt>
                    <dd>{d}</dd>
                  </div>
                ))}
              </dl>
            )}
          </div>
        </div>
      )}
      {!info && !error && (
        <Link to="/" className="visually-hidden">
          Home
        </Link>
      )}
    </div>
  );
}

const PANEL_TITLES: Record<Exclude<Panel, null>, string> = {
  menu: 'Playback',
  audio: 'Audio',
  subtitles: 'Subtitles',
  quality: 'Quality',
  speed: 'Speed',
  style: 'Subtitle style',
  sync: 'Sync',
  info: 'Stream info',
  keys: 'Keyboard shortcuts',
};

const KEYS: [string, string][] = [
  ['Space / K', 'Play or pause'],
  ['← →', 'Back or forward 10 s (Shift: 30 s)'],
  ['J / L', 'Skip back or forward'],
  ['↑ ↓', 'Volume'],
  ['M', 'Mute'],
  ['F', 'Full screen'],
  ['C', 'Cycle subtitles'],
  ['A', 'Cycle audio tracks'],
  ['G / H', 'Subtitle delay −/+ 100 ms'],
  ['< >', 'Slower or faster'],
  ['S', 'Skip intro or credits'],
  ['N', 'Next episode'],
  ['P', 'Picture in picture'],
  ['I', 'Stream info'],
  ['0–9', 'Jump to 0–90%'],
  ['Esc', 'Close menu or leave'],
];

function dedupeFront(list: string[], lang: string) {
  return [lang, ...list.filter((l) => l !== lang)];
}

function trackName(s: SubtitleTrack) {
  return [s.language_name || 'Unknown', s.title && s.title !== s.language_name ? s.title : ''].filter(Boolean).join(', ');
}

function subtitleHint(s: SubtitleTrack) {
  return [s.is_forced && 'Forced', s.is_hearing_impaired && 'SDH', s.image_based && 'Burned in', s.source !== 'embedded' && (s.source === 'upload' ? 'Added' : 'External')].filter(Boolean).join(', ');
}

function MenuRow({ icon, label, value, onClick }: { icon: ReactNode; label: string; value: string; onClick: () => void }) {
  return (
    <li>
      <button type="button" className="pp-row" onClick={onClick}>
        {icon}
        <span className="pp-row-label">{label}</span>
        <span className="pp-row-value">{value}</span>
        <ChevronRight size={16} />
      </button>
    </li>
  );
}

function Choices({ items, value, onPick }: { items: { id: string; label: string; hint: string }[]; value: string; onPick: (id: string) => void }) {
  return (
    <ul className="pp-choices" role="radiogroup">
      {items.map((it) => (
        <li key={it.id}>
          <button type="button" role="radio" aria-checked={it.id === value} className="pp-choice" onClick={() => onPick(it.id)}>
            <span className="pp-dot" aria-hidden="true" />
            <span className="pp-row-label">{it.label}</span>
            {it.hint && <span className="pp-row-value">{it.hint}</span>}
          </button>
        </li>
      ))}
    </ul>
  );
}

function Stepper({ label, value, unit, step, onChange, hint }: { label: string; value: number; unit: string; step: number; onChange: (v: number) => void; hint: string }) {
  return (
    <div className="pp-stepper">
      <span className="pp-row-label">{label}</span>
      <div>
        <button type="button" className="player-icon" onClick={() => onChange(value - step)} aria-label={`${label} minus ${step} ${unit}`}>
          −
        </button>
        <output>
          {value > 0 ? '+' : ''}
          {value} {unit}
        </output>
        <button type="button" className="player-icon" onClick={() => onChange(value + step)} aria-label={`${label} plus ${step} ${unit}`}>
          +
        </button>
        {value !== 0 && (
          <button type="button" className="pp-reset" onClick={() => onChange(0)}>
            Reset
          </button>
        )}
      </div>
      <p className="pp-note">{hint}</p>
    </div>
  );
}

function StylePanel({ style, onChange }: { style: SubtitleStyle; onChange: (s: SubtitleStyle) => void }) {
  const colors = ['#ffffff', '#f1fa8c', '#8be9fd', '#50fa7b', '#ff79c6'];
  return (
    <div className="pp-style">
      <label>
        <span className="pp-row-label">Size</span>
        <input type="range" min={50} max={250} step={10} value={style.size} onChange={(e) => onChange({ ...style, size: Number(e.target.value) })} />
        <output>{style.size}%</output>
      </label>
      <label>
        <span className="pp-row-label">Position</span>
        <input type="range" min={0} max={40} step={1} value={style.position} onChange={(e) => onChange({ ...style, position: Number(e.target.value) })} />
        <output>{style.position}%</output>
      </label>
      <div className="pp-style-row">
        <span className="pp-row-label">Colour</span>
        <span className="pp-swatches">
          {colors.map((c) => (
            <button key={c} type="button" style={{ background: c }} aria-label={c} aria-pressed={style.color === c} onClick={() => onChange({ ...style, color: c })} />
          ))}
        </span>
      </div>
      <div className="pp-style-row">
        <span className="pp-row-label">Background</span>
        <span className="pp-segs">
          {(['shadow', 'box', 'none'] as const).map((b) => (
            <button key={b} type="button" aria-pressed={style.background === b} onClick={() => onChange({ ...style, background: b })}>
              {b === 'shadow' ? 'Shadow' : b === 'box' ? 'Box' : 'None'}
            </button>
          ))}
        </span>
      </div>
      {style.background === 'box' && (
        <label>
          <span className="pp-row-label">Box opacity</span>
          <input type="range" min={10} max={100} step={5} value={style.opacity} onChange={(e) => onChange({ ...style, opacity: Number(e.target.value) })} />
          <output>{style.opacity}%</output>
        </label>
      )}
      <div className="pp-style-row">
        <span className="pp-row-label">Font</span>
        <span className="pp-segs">
          {(['sans', 'serif', 'mono', 'rounded'] as const).map((f) => (
            <button key={f} type="button" aria-pressed={style.font === f} onClick={() => onChange({ ...style, font: f })}>
              {f === 'sans' ? 'Sans' : f === 'serif' ? 'Serif' : f === 'mono' ? 'Mono' : 'Techno'}
            </button>
          ))}
        </span>
      </div>
      <p className="pp-preview" style={styleFor(style)}>
        <span>The quick brown fox</span>
      </p>
    </div>
  );
}

