import { useState } from 'react';
import type { ReactNode } from 'react';
import { ArrowDown, ArrowUp, Plus, X } from 'lucide-react';

import type { PlaybackPrefs } from '../api/media';
import { useToast } from '../components/ui/Toast';
import { LoadingState } from '../components/States';
import { Toggle } from '../components/ui/Toggle';
import { ACCENTS } from '../lib/theme';
import { updateInterface, updatePlayback, usePreferences } from '../lib/preferences';
import './SettingsPage.css';

/** ISO 639-2 codes the server stores, with readable names. */
const LANGUAGES: [string, string][] = [
  ['eng', 'English'],
  ['hin', 'Hindi'],
  ['kan', 'Kannada'],
  ['tam', 'Tamil'],
  ['tel', 'Telugu'],
  ['mal', 'Malayalam'],
  ['mar', 'Marathi'],
  ['ben', 'Bengali'],
  ['jpn', 'Japanese'],
  ['kor', 'Korean'],
  ['chi', 'Chinese'],
  ['spa', 'Spanish'],
  ['fre', 'French'],
  ['ger', 'German'],
  ['ita', 'Italian'],
  ['por', 'Portuguese'],
  ['rus', 'Russian'],
  ['ara', 'Arabic'],
  ['tur', 'Turkish'],
  ['tha', 'Thai'],
];
const nameOf = (code: string) => LANGUAGES.find(([c]) => c === code)?.[1] ?? code;

function Section({ title, hint, children }: { title: string; hint?: string; children: ReactNode }) {
  return (
    <section className="settings-section">
      <header>
        <h2>{title}</h2>
        {hint && <p>{hint}</p>}
      </header>
      <div className="settings-rows">{children}</div>
    </section>
  );
}

function SettingRow({ label, hint, children }: { label: string; hint?: string; children: ReactNode }) {
  return (
    <div className="setting-row">
      <div className="setting-text">
        <span className="setting-label">{label}</span>
        {hint && <span className="setting-hint">{hint}</span>}
      </div>
      <div className="setting-control">{children}</div>
    </div>
  );
}

function Choice<T extends string>({ value, options, onChange, label }: { value: T; options: [T, string][]; onChange: (v: T) => void; label: string }) {
  return (
    <span className="seg" role="radiogroup" aria-label={label}>
      {options.map(([v, l]) => (
        <button key={v} type="button" role="radio" aria-checked={value === v} aria-pressed={value === v} onClick={() => onChange(v)}>
          {l}
        </button>
      ))}
    </span>
  );
}

/** An ordered list of languages: first match wins. */
function LanguageList({ value, onChange, label }: { value: string[]; onChange: (v: string[]) => void; label: string }) {
  const [adding, setAdding] = useState('');
  const move = (i: number, d: number) => {
    const next = [...value];
    const [x] = next.splice(i, 1);
    next.splice(i + d, 0, x!);
    onChange(next);
  };
  return (
    <div className="lang-list">
      <ol aria-label={label}>
        {value.map((l, i) => (
          <li key={l}>
            <span className="lang-rank">{i + 1}</span>
            <span className="lang-name">{nameOf(l)}</span>
            <button type="button" className="icon-button" disabled={i === 0} onClick={() => move(i, -1)} aria-label={`Move ${nameOf(l)} up`}>
              <ArrowUp size={15} />
            </button>
            <button type="button" className="icon-button" disabled={i === value.length - 1} onClick={() => move(i, 1)} aria-label={`Move ${nameOf(l)} down`}>
              <ArrowDown size={15} />
            </button>
            <button type="button" className="icon-button" onClick={() => onChange(value.filter((x) => x !== l))} aria-label={`Remove ${nameOf(l)}`}>
              <X size={15} />
            </button>
          </li>
        ))}
      </ol>
      <div className="lang-add">
        <select className="select" value={adding} onChange={(e) => setAdding(e.target.value)} aria-label={`Add a language to ${label.toLowerCase()}`}>
          <option value="">Add a language…</option>
          {LANGUAGES.filter(([c]) => !value.includes(c)).map(([c, n]) => (
            <option key={c} value={c}>
              {n}
            </option>
          ))}
        </select>
        <button
          type="button"
          className="button button-sm"
          disabled={!adding}
          onClick={() => {
            onChange([...value, adding]);
            setAdding('');
          }}
        >
          <Plus size={15} /> Add
        </button>
      </div>
    </div>
  );
}

const HOME_ROWS: [string, string][] = [
  ['continue', 'Continue Watching'],
  ['next_up', 'Next Up'],
  ['recent_movies', 'Recently added movies'],
  ['recent_shows', 'New episodes'],
  ['watchlist', 'Your watchlist'],
  ['because', 'Because you watched'],
  ['top_rated', 'Top rated'],
  ['collections', 'Your collections'],
];

export default function SettingsPage() {
  const { playback, iface, loaded } = usePreferences();
  const toast = useToast();

  if (!loaded || !playback) return <div className="page"><LoadingState /></div>;

  const set = (patch: Partial<PlaybackPrefs>) => {
    updatePlayback(patch).catch((err: unknown) => toast({ message: err instanceof Error ? err.message : 'Could not save.', tone: 'error' }));
  };
  const hidden = new Set(iface.home_rows.filter((r) => r.startsWith('-')).map((r) => r.slice(1)));
  const toggleRow = (id: string, on: boolean) => {
    const next = new Set(hidden);
    if (on) next.delete(id);
    else next.add(id);
    void updateInterface({ home_rows: [...next].map((r) => `-${r}`) });
  };

  return (
    <div className="page settings-page">
      <header className="page-head">
        <div>
          <h1 className="page-title">Settings</h1>
          <p className="page-count">These are yours: other people on this server have their own. Changes save as you make them.</p>
        </div>
      </header>

      <Section title="Languages" hint="When a title has several audio or subtitle tracks, the first language in your list that the title has is chosen.">
        <SettingRow label="Audio languages" hint="In order of preference.">
          <LanguageList label="Audio languages" value={playback.audio_languages} onChange={(v) => set({ audio_languages: v })} />
        </SettingRow>
        <SettingRow label="Prefer the original language" hint="Play a film in its own language (Japanese anime, Korean drama) when the file has it, ahead of your list.">
          <Toggle label="Prefer the original language" hideLabel checked={playback.prefer_original_audio} onChange={(v) => set({ prefer_original_audio: v })} />
        </SettingRow>
        <SettingRow label="Subtitle languages">
          <LanguageList label="Subtitle languages" value={playback.subtitle_languages} onChange={(v) => set({ subtitle_languages: v })} />
        </SettingRow>
        <SettingRow label="When to show subtitles">
          <select className="select" value={playback.subtitle_mode} onChange={(e) => set({ subtitle_mode: e.target.value })} aria-label="When to show subtitles">
            <option value="auto">When the audio isn't in one of my languages</option>
            <option value="always">Always</option>
            <option value="forced">Only forced (signs and foreign lines)</option>
            <option value="off">Never</option>
          </select>
        </SettingRow>
        <SettingRow label="Prefer SDH subtitles" hint="Subtitles for the deaf and hard of hearing, which also describe sounds.">
          <Toggle label="Prefer SDH subtitles" hideLabel checked={playback.prefer_sdh} onChange={(v) => set({ prefer_sdh: v })} />
        </SettingRow>
      </Section>

      <Section title="Episodes">
        <SettingRow label="Play the next episode automatically">
          <Toggle label="Play the next episode automatically" hideLabel checked={playback.autoplay_next} onChange={(v) => set({ autoplay_next: v })} />
        </SettingRow>
        <SettingRow label="Countdown before the next episode">
          <select className="select" value={playback.next_countdown} onChange={(e) => set({ next_countdown: Number(e.target.value) })} aria-label="Countdown before the next episode">
            {[5, 10, 15, 20, 30].map((s) => (
              <option key={s} value={s}>
                {s} seconds
              </option>
            ))}
          </select>
        </SettingRow>
        <SettingRow label="Skip intros" hint="Uses intro markers from chapters, or ones set on the episode.">
          <Choice label="Skip intros" value={playback.skip_intro} options={[['button', 'Show a button'], ['auto', 'Automatically'], ['off', 'Never']]} onChange={(v) => set({ skip_intro: v })} />
        </SettingRow>
        <SettingRow label="Skip credits">
          <Choice label="Skip credits" value={playback.skip_credits} options={[['button', 'Show a button'], ['auto', 'Automatically'], ['off', 'Never']]} onChange={(v) => set({ skip_credits: v })} />
        </SettingRow>
      </Section>

      <Section title="Playback">
        <SettingRow label="Quality" hint="Anything below Original is transcoded by the server.">
          <select className="select" value={playback.quality} onChange={(e) => set({ quality: e.target.value })} aria-label="Quality">
            <option value="original">Original</option>
            <option value="2160p">4K</option>
            <option value="1080p">1080p</option>
            <option value="720p">720p</option>
            <option value="480p">480p</option>
          </select>
        </SettingRow>
        <SettingRow label="Bitrate limit" hint="For slow connections. Files above it are transcoded down.">
          <select className="select" value={playback.max_bitrate} onChange={(e) => set({ max_bitrate: Number(e.target.value) })} aria-label="Bitrate limit">
            <option value={0}>No limit</option>
            <option value={40000}>40 Mb/s</option>
            <option value={20000}>20 Mb/s</option>
            <option value={8000}>8 Mb/s</option>
            <option value={4000}>4 Mb/s</option>
            <option value={2000}>2 Mb/s</option>
          </select>
        </SettingRow>
        <SettingRow label="Default speed">
          <select className="select" value={playback.speed} onChange={(e) => set({ speed: Number(e.target.value) })} aria-label="Default speed">
            {[0.75, 1, 1.25, 1.5, 2].map((s) => (
              <option key={s} value={s}>
                {s === 1 ? 'Normal' : `${s}×`}
              </option>
            ))}
          </select>
        </SettingRow>
        <SettingRow label="Skip back and forward">
          <span className="inline-controls">
            <select className="select" value={playback.skip_backward} onChange={(e) => set({ skip_backward: Number(e.target.value) })} aria-label="Skip back">
              {[5, 10, 15, 30].map((s) => (
                <option key={s} value={s}>
                  Back {s} s
                </option>
              ))}
            </select>
            <select className="select" value={playback.skip_forward} onChange={(e) => set({ skip_forward: Number(e.target.value) })} aria-label="Skip forward">
              {[10, 15, 30, 60].map((s) => (
                <option key={s} value={s}>
                  Forward {s} s
                </option>
              ))}
            </select>
          </span>
        </SettingRow>
        <SettingRow label="Surround pass-through" hint="Only if this device sends audio to a receiver that decodes these formats. Otherwise they are converted to AAC.">
          <span className="inline-controls">
            {['ac3', 'eac3', 'dts', 'truehd'].map((c) => (
              <label key={c} className="check">
                <input
                  type="checkbox"
                  checked={playback.passthrough.includes(c)}
                  onChange={(e) => set({ passthrough: e.target.checked ? [...playback.passthrough, c] : playback.passthrough.filter((x) => x !== c) })}
                />
                {{ ac3: 'Dolby Digital', eac3: 'Dolby Digital+', dts: 'DTS', truehd: 'TrueHD' }[c]}
              </label>
            ))}
          </span>
        </SettingRow>
      </Section>

      <Section title="Appearance">
        <SettingRow label="Accent">
          <span className="accent-swatches" role="radiogroup" aria-label="Accent">
            {Object.entries(ACCENTS).map(([name, hex]) => (
              <button key={name} type="button" role="radio" aria-checked={iface.accent === name} aria-label={name} style={{ background: hex }} onClick={() => void updateInterface({ accent: name })} />
            ))}
          </span>
        </SettingRow>
        <SettingRow label="Card size">
          <Choice label="Card size" value={iface.card_size} options={[['small', 'Small'], ['medium', 'Medium'], ['large', 'Large']]} onChange={(v) => void updateInterface({ card_size: v })} />
        </SettingRow>
        <SettingRow label="Density">
          <Choice label="Density" value={iface.density} options={[['compact', 'Compact'], ['comfortable', 'Comfortable']]} onChange={(v) => void updateInterface({ density: v })} />
        </SettingRow>
        <SettingRow label="Library view">
          <Choice label="Library view" value={iface.view} options={[['grid', 'Grid'], ['list', 'List']]} onChange={(v) => void updateInterface({ view: v })} />
        </SettingRow>
        <SettingRow label="Reduce motion" hint="Turns off zooms, fades and the rotating home banner.">
          <Toggle label="Reduce motion" hideLabel checked={iface.motion === 'reduced'} onChange={(v) => void updateInterface({ motion: v ? 'reduced' : 'full' })} />
        </SettingRow>
      </Section>

      <Section title="Home page" hint="Choose which rows appear on Home.">
        {HOME_ROWS.map(([id, label]) => (
          <SettingRow key={id} label={label}>
            <Toggle label={label} hideLabel checked={!hidden.has(id)} onChange={(v) => toggleRow(id, v)} />
          </SettingRow>
        ))}
      </Section>
    </div>
  );
}
