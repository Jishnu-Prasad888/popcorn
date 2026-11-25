/**
 * Metadata management for people with edit access: edit fields, fix the
 * match, refresh, replace artwork, organise versions and manage subtitles.
 */
import { useState } from 'react';
import type { ReactNode } from 'react';
import { Lock, Search, SlidersHorizontal, Trash2, Unlock, Upload, Wand2 } from 'lucide-react';

import {
  artUrl,
  deleteSubtitle,
  editItem,
  errorMessage,
  generateArtwork,
  matchItem,
  providerSearch,
  refreshMetadata,
  releaseFile,
  removeArtwork,
  renameSubtitle,
  replaceSubtitle,
  setEdition,
  splitFile,
  unlockFields,
  unmatchItem,
  updateSubtitle,
  uploadArtwork,
  uploadSubtitle,
} from '../../api/media';
import type { Art, ExternalSubtitle, FileInfo, ItemType, ProviderResult } from '../../api/media';
import { Dialog } from '../Dialog';
import { useToast } from '../ui/Toast';
import { Menu, useMenuButton } from '../ui/Menu';
import './manage.css';

export interface Manageable {
  library_id: string;
  type: ItemType;
  id: string;
  title: string;
  year?: number | undefined;
  art: Art;
  locked_fields: string[] | null;
  /** Current values of editable fields. */
  values: Record<string, unknown>;
  files?: FileInfo[];
}

type Panel = 'edit' | 'match' | 'art' | 'files' | 'subs' | null;

/** The "Manage" button on detail pages, and the dialogs it opens. */
export function ManageMenu({ item, onChanged }: { item: Manageable; onChanged: () => void }) {
  const toast = useToast();
  const [panel, setPanel] = useState<Panel>(null);
  const menu = useMenuButton();
  const matchable = item.type === 'movie' || item.type === 'show';
  const refresh = async () => {
    try {
      await refreshMetadata(item.library_id, [{ type: item.type, id: item.id }], true);
      toast({ message: 'Refreshing metadata. Changes appear in a few seconds.' });
      window.setTimeout(onChanged, 4000);
    } catch (err) {
      toast({ message: errorMessage(err), tone: 'error' });
    }
  };
  return (
    <>
      <button type="button" className="btn-round" onClick={menu.toggle} aria-haspopup="menu" aria-expanded={menu.open} aria-label="Manage" title="Manage">
        <SlidersHorizontal size={19} />
      </button>
      {menu.anchor && (
        <Menu
          anchor={menu.anchor}
          onClose={menu.close}
          label="Manage"
          items={[
            { id: 'edit', label: 'Edit metadata', onSelect: () => setPanel('edit') },
            ...(matchable ? [{ id: 'match', label: 'Fix match', onSelect: () => setPanel('match') }] : []),
            ...(matchable ? [{ id: 'refresh', label: 'Refresh metadata', onSelect: () => void refresh() }] : []),
            { id: 'art', label: 'Artwork', onSelect: () => setPanel('art') },
            ...(item.files?.length ? [{ id: 'files', label: 'Versions and files', onSelect: () => setPanel('files') }] : []),
            ...(item.files?.length ? [{ id: 'subs', label: 'Subtitles', onSelect: () => setPanel('subs') }] : []),
          ]}
        />
      )}
      {panel === 'edit' && <EditDialog item={item} onClose={() => setPanel(null)} onSaved={onChanged} />}
      {panel === 'match' && <MatchDialog item={item} onClose={() => setPanel(null)} onSaved={onChanged} />}
      {panel === 'art' && <ArtworkDialog item={item} onClose={() => setPanel(null)} onSaved={onChanged} />}
      {panel === 'files' && <FilesDialog item={item} onClose={() => setPanel(null)} onSaved={onChanged} />}
      {panel === 'subs' && <SubtitlesDialog item={item} onClose={() => setPanel(null)} onSaved={onChanged} />}
    </>
  );
}

type FieldKind = 'text' | 'long' | 'int' | 'float' | 'list' | 'date' | 'secs';
const FIELDS: Record<ItemType, [string, string, FieldKind][]> = {
  movie: [
    ['title', 'Title', 'text'],
    ['original_title', 'Original title', 'text'],
    ['sort_title', 'Sort as', 'text'],
    ['year', 'Year', 'int'],
    ['release_date', 'Release date', 'date'],
    ['tagline', 'Tagline', 'text'],
    ['overview', 'Overview', 'long'],
    ['genres', 'Genres', 'list'],
    ['runtime_mins', 'Runtime (minutes)', 'int'],
    ['content_rating', 'Age rating', 'text'],
    ['rating', 'TMDB rating', 'float'],
    ['imdb_rating', 'IMDb rating', 'float'],
    ['imdb_id', 'IMDb id', 'text'],
    ['original_language', 'Original language', 'text'],
    ['studios', 'Studios', 'list'],
    ['countries', 'Countries', 'list'],
  ],
  show: [
    ['title', 'Title', 'text'],
    ['original_title', 'Original title', 'text'],
    ['sort_title', 'Sort as', 'text'],
    ['year', 'Year', 'int'],
    ['first_air_date', 'First aired', 'date'],
    ['status', 'Status', 'text'],
    ['category', 'Category (tv, anime, web)', 'text'],
    ['overview', 'Overview', 'long'],
    ['genres', 'Genres', 'list'],
    ['content_rating', 'Age rating', 'text'],
    ['rating', 'TMDB rating', 'float'],
    ['imdb_rating', 'IMDb rating', 'float'],
    ['imdb_id', 'IMDb id', 'text'],
    ['original_language', 'Original language', 'text'],
    ['studios', 'Networks and studios', 'list'],
  ],
  season: [
    ['title', 'Title', 'text'],
    ['air_date', 'Air date', 'date'],
    ['overview', 'Overview', 'long'],
  ],
  episode: [
    ['title', 'Title', 'text'],
    ['air_date', 'Air date', 'date'],
    ['overview', 'Overview', 'long'],
    ['runtime_mins', 'Runtime (minutes)', 'int'],
    ['rating', 'Rating', 'float'],
    ['intro_start', 'Intro starts (seconds)', 'secs'],
    ['intro_end', 'Intro ends (seconds)', 'secs'],
    ['credits_start', 'Credits start (seconds)', 'secs'],
  ],
};

function toText(v: unknown, kind: FieldKind): string {
  if (v === undefined || v === null) return '';
  if (kind === 'list') return Array.isArray(v) ? v.join(', ') : String(v);
  return String(v);
}

function fromText(s: string, kind: FieldKind): unknown {
  const t = s.trim();
  if (!t) return null;
  switch (kind) {
    case 'int':
      return Number.parseInt(t, 10);
    case 'float':
    case 'secs':
      return Number.parseFloat(t);
    case 'list':
      return t.split(',').map((x) => x.trim()).filter(Boolean);
    default:
      return t;
  }
}

function EditDialog({ item, onClose, onSaved }: { item: Manageable; onClose: () => void; onSaved: () => void }) {
  const toast = useToast();
  const fields = FIELDS[item.type];
  const [values, setValues] = useState<Record<string, string>>(() =>
    Object.fromEntries(fields.map(([k, , kind]) => [k, toText(item.values[k], kind)])),
  );
  const [locked, setLocked] = useState(new Set(item.locked_fields ?? []));
  const [busy, setBusy] = useState(false);

  const save = async () => {
    const changes: Record<string, unknown> = {};
    for (const [k, , kind] of fields) {
      if (values[k] !== toText(item.values[k], kind)) changes[k] = fromText(values[k] ?? '', kind);
    }
    if (!Object.keys(changes).length) {
      onClose();
      return;
    }
    setBusy(true);
    try {
      await editItem(item.library_id, item.type, item.id, changes);
      toast({ message: 'Saved. Edited fields are locked, so a refresh keeps them.', tone: 'success' });
      onSaved();
      onClose();
    } catch (err) {
      toast({ message: errorMessage(err), tone: 'error' });
    } finally {
      setBusy(false);
    }
  };

  const unlock = async (k: string) => {
    try {
      await unlockFields(item.library_id, item.type, item.id, [k]);
      const next = new Set(locked);
      next.delete(k);
      setLocked(next);
      onSaved();
    } catch (err) {
      toast({ message: errorMessage(err), tone: 'error' });
    }
  };

  return (
    <Dialog
      open
      size="large"
      title={`Edit ${item.title}`}
      onClose={onClose}
      footer={
        <>
          <button type="button" className="button" onClick={onClose}>
            Cancel
          </button>
          <button type="button" className="primary-button" onClick={() => void save()} disabled={busy}>
            Save changes
          </button>
        </>
      }
    >
      <div className="edit-grid">
        {fields.map(([k, label, kind]) => (
          <label key={k} className={`field ${kind === 'long' ? 'edit-wide' : ''}`}>
            <span className="field-label edit-label">
              {label}
              {locked.has(k) && (
                <button type="button" className="lock-button" onClick={() => void unlock(k)} title="Locked: refreshes keep your value. Unlock to let refreshes update it.">
                  <Lock size={12} /> Locked
                  <Unlock size={12} className="lock-alt" />
                </button>
              )}
            </span>
            {kind === 'long' ? (
              <textarea rows={5} value={values[k]} onChange={(e) => setValues({ ...values, [k]: e.target.value })} />
            ) : (
              <input
                type={kind === 'date' ? 'date' : kind === 'text' || kind === 'list' ? 'text' : 'number'}
                step={kind === 'float' ? '0.1' : kind === 'secs' ? '0.5' : undefined}
                value={values[k]}
                onChange={(e) => setValues({ ...values, [k]: e.target.value })}
              />
            )}
            {kind === 'list' && <span className="field-hint">Separate with commas.</span>}
          </label>
        ))}
      </div>
    </Dialog>
  );
}

function MatchDialog({ item, onClose, onSaved }: { item: Manageable; onClose: () => void; onSaved: () => void }) {
  const toast = useToast();
  const kind = item.type === 'show' ? 'show' : 'movie';
  const [q, setQ] = useState(item.title);
  const [year, setYear] = useState(item.year ? String(item.year) : '');
  const [results, setResults] = useState<ProviderResult[] | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');

  const run = async () => {
    setBusy(true);
    setError('');
    try {
      const r = await providerSearch(kind, q, year ? Number(year) : undefined);
      setResults(r.results);
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  };

  const pick = async (r: ProviderResult) => {
    setBusy(true);
    try {
      await matchItem(item.library_id, kind, item.id, r.id);
      toast({ message: `Matched to ${r.title}${r.year ? ` (${r.year})` : ''}.`, tone: 'success' });
      onSaved();
      onClose();
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  };

  const unmatch = async () => {
    try {
      await unmatchItem(item.library_id, kind, item.id);
      toast({ message: 'Match removed.' });
      onSaved();
      onClose();
    } catch (err) {
      setError(errorMessage(err));
    }
  };

  return (
    <Dialog
      open
      size="large"
      title="Fix match"
      onClose={onClose}
      footer={
        <button type="button" className="button" onClick={() => void unmatch()}>
          Remove match
        </button>
      }
    >
      <form
        className="match-form"
        onSubmit={(e) => {
          e.preventDefault();
          void run();
        }}
      >
        <input value={q} onChange={(e) => setQ(e.target.value)} aria-label="Title, IMDb id (tt…) or tmdb:<id>" placeholder="Title, IMDb id (tt0133093) or tmdb:603" />
        <input value={year} onChange={(e) => setYear(e.target.value)} aria-label="Year" placeholder="Year" type="number" style={{ width: 100 }} />
        <button type="submit" className="primary-button" disabled={busy || !q.trim()}>
          <Search size={16} /> Search
        </button>
      </form>
      {error && <p className="error-text">{error}</p>}
      {results?.length === 0 && <p className="muted">No titles found. Try the original title, fewer words, or the IMDb id.</p>}
      {results && results.length > 0 && (
        <ul className="match-results">
          {results.map((r) => (
            <li key={r.id}>
              <button type="button" disabled={busy} onClick={() => void pick(r)}>
                {r.poster_url ? <img src={r.poster_url} alt="" loading="lazy" /> : <span className="match-noposter" />}
                <span>
                  <strong>
                    {r.title}
                    {r.year ? ` (${r.year})` : ''}
                  </strong>
                  {r.original_title && r.original_title !== r.title && <span className="muted"> {r.original_title}</span>}
                  {r.overview && <span className="match-overview">{r.overview}</span>}
                </span>
              </button>
            </li>
          ))}
        </ul>
      )}
    </Dialog>
  );
}

const ART_KINDS: Record<ItemType, [string, string][]> = {
  movie: [
    ['poster', 'Poster'],
    ['backdrop', 'Backdrop'],
    ['logo', 'Logo'],
  ],
  show: [
    ['poster', 'Poster'],
    ['backdrop', 'Backdrop'],
    ['logo', 'Logo'],
  ],
  season: [['poster', 'Poster']],
  episode: [['thumb', 'Still']],
};

function ArtworkDialog({ item, onClose, onSaved }: { item: Manageable; onClose: () => void; onSaved: () => void }) {
  const toast = useToast();
  const [art, setArt] = useState(item.art);
  const bump = (kind: string, has: boolean) => setArt({ ...art, [kind]: has, version: art.version + 1 });

  const act = async (fn: () => Promise<unknown>, kind: string, has: boolean, msg: string) => {
    try {
      await fn();
      bump(kind, has);
      toast({ message: msg, tone: 'success' });
      onSaved();
    } catch (err) {
      toast({ message: errorMessage(err), tone: 'error' });
    }
  };

  return (
    <Dialog open size="large" title="Artwork" onClose={onClose}>
      <div className="art-grid">
        {ART_KINDS[item.type].map(([kind, label]) => {
          const src = artUrl(item.library_id, item.type, item.id, kind as 'poster', art);
          return (
            <figure key={kind} className={`art-slot art-${kind}`}>
              <div className="art-preview">{src ? <img src={src} alt={`Current ${label.toLowerCase()}`} /> : <span className="muted">No {label.toLowerCase()}</span>}</div>
              <figcaption>
                <strong>{label}</strong>
                <span className="art-actions">
                  <label className="button button-sm">
                    <Upload size={14} /> Upload
                    <input
                      type="file"
                      accept="image/jpeg,image/png,image/webp"
                      className="visually-hidden"
                      onChange={(e) => {
                        const f = e.target.files?.[0];
                        if (f) void act(() => uploadArtwork(item.library_id, item.type, item.id, kind, f), kind, true, `${label} replaced.`);
                      }}
                    />
                  </label>
                  {(item.type === 'episode' || item.type === 'movie') && kind !== 'logo' && (
                    <button type="button" className="button button-sm" onClick={() => void act(() => generateArtwork(item.library_id, item.type, item.id, kind), kind, true, `${label} taken from the video.`)}>
                      <Wand2 size={14} /> From video
                    </button>
                  )}
                  {src && (
                    <button type="button" className="button button-sm" onClick={() => void act(() => removeArtwork(item.library_id, item.type, item.id, kind), kind, false, `${label} removed.`)} aria-label={`Remove ${label.toLowerCase()}`}>
                      <Trash2 size={14} />
                    </button>
                  )}
                </span>
              </figcaption>
            </figure>
          );
        })}
      </div>
      <p className="field-hint">Uploaded artwork wins over provider artwork, and a refresh keeps it.</p>
    </Dialog>
  );
}

function FilesDialog({ item, onClose, onSaved }: { item: Manageable; onClose: () => void; onSaved: () => void }) {
  const toast = useToast();
  const [editions, setEditions] = useState<Record<string, string>>(() =>
    Object.fromEntries((item.files ?? []).map((f) => [f.id, f.edition ?? ''])),
  );
  const run = async (fn: () => Promise<unknown>, msg: string, close = false) => {
    try {
      await fn();
      toast({ message: msg, tone: 'success' });
      onSaved();
      if (close) onClose();
    } catch (err) {
      toast({ message: errorMessage(err), tone: 'error' });
    }
  };
  return (
    <Dialog open size="large" title="Versions and files" onClose={onClose}>
      <ul className="files-manage">
        {(item.files ?? []).map((f) => (
          <li key={f.id}>
            <p className="tech-path">{f.rel_path}</p>
            <div className="files-manage-row">
              {item.type === 'movie' && (
                <>
                  <input
                    value={editions[f.id]}
                    onChange={(e) => setEditions({ ...editions, [f.id]: e.target.value })}
                    placeholder="Edition, e.g. Director's Cut"
                    aria-label={`Edition of ${f.name}`}
                  />
                  <button type="button" className="button button-sm" onClick={() => void run(() => setEdition(item.library_id, f.id, editions[f.id] ?? ''), 'Edition saved.')}>
                    Save
                  </button>
                  {(item.files?.length ?? 0) > 1 && (
                    <button type="button" className="button button-sm" onClick={() => void run(() => splitFile(item.library_id, f.id), 'Made a separate movie from this file.', true)}>
                      Split off
                    </button>
                  )}
                </>
              )}
              {f.manual && (
                <button type="button" className="button button-sm" onClick={() => void run(() => releaseFile(item.library_id, f.id), 'Released: the file is placed automatically again.', true)}>
                  Undo manual placement
                </button>
              )}
            </div>
          </li>
        ))}
      </ul>
      <p className="field-hint">
        Several files of one title are versions: the player lets people choose. Episode numbers are fixed from the episode's own page.
      </p>
    </Dialog>
  );
}

const SUB_LANGS = ['eng', 'hin', 'kan', 'tam', 'tel', 'mal', 'jpn', 'kor', 'chi', 'spa', 'fre', 'ger', 'ita', 'por', 'rus', 'ara'];
const LANG_NAMES = new Intl.DisplayNames(['en'], { type: 'language' });
const langName = (c: string) => {
  const two: Record<string, string> = { eng: 'en', hin: 'hi', kan: 'kn', tam: 'ta', tel: 'te', mal: 'ml', jpn: 'ja', kor: 'ko', chi: 'zh', spa: 'es', fre: 'fr', ger: 'de', ita: 'it', por: 'pt', rus: 'ru', ara: 'ar' };
  try {
    return LANG_NAMES.of(two[c] ?? c) ?? c;
  } catch {
    return c;
  }
};

function SubtitlesDialog({ item, onClose, onSaved }: { item: Manageable; onClose: () => void; onSaved: () => void }) {
  const toast = useToast();
  const files = (item.files ?? []).filter((f) => f.status === 'present');
  const [fileId, setFileId] = useState(files[0]?.id ?? '');
  const file = files.find((f) => f.id === fileId);
  const [subs, setSubs] = useState<ExternalSubtitle[]>(file?.subtitles ?? []);
  const [lang, setLang] = useState('eng');
  const [forced, setForced] = useState(false);
  const [sdh, setSdh] = useState(false);

  const run = async (fn: () => Promise<unknown>, msg: string, apply?: () => void) => {
    try {
      await fn();
      apply?.();
      toast({ message: msg, tone: 'success' });
      onSaved();
    } catch (err) {
      toast({ message: errorMessage(err), tone: 'error' });
    }
  };

  const embedded = file?.streams?.filter((s) => s.kind === 'subtitle') ?? [];
  return (
    <Dialog open size="large" title="Subtitles" onClose={onClose}>
      {files.length > 1 && (
        <label className="field">
          <span className="field-label">Version</span>
          <select
            value={fileId}
            onChange={(e) => {
              setFileId(e.target.value);
              setSubs(files.find((f) => f.id === e.target.value)?.subtitles ?? []);
            }}
          >
            {files.map((f) => (
              <option key={f.id} value={f.id}>
                {f.edition || f.name}
              </option>
            ))}
          </select>
        </label>
      )}
      {!file && <p className="muted">No file of this title is available right now.</p>}
      {file && (
        <>
          <h3 className="manage-subhead">Add a subtitle file</h3>
          <div className="sub-upload">
            <select value={lang} onChange={(e) => setLang(e.target.value)} aria-label="Language">
              {SUB_LANGS.map((l) => (
                <option key={l} value={l}>
                  {langName(l)}
                </option>
              ))}
            </select>
            <label className="check">
              <input type="checkbox" checked={forced} onChange={(e) => setForced(e.target.checked)} /> Forced
            </label>
            <label className="check">
              <input type="checkbox" checked={sdh} onChange={(e) => setSdh(e.target.checked)} /> SDH
            </label>
            <label className="primary-button">
              <Upload size={16} /> Choose file
              <input
                type="file"
                accept=".srt,.ass,.ssa,.vtt,.sub"
                className="visually-hidden"
                onChange={(e) => {
                  const f = e.target.files?.[0];
                  if (!f) return;
                  void run(
                    async () => {
                      const r = await uploadSubtitle(item.library_id, file.id, f, { language: lang, forced, sdh });
                      setSubs((s) => [...s, { id: r.id, source: 'upload', path: f.name, format: f.name.split('.').pop() ?? '', language: lang, language_name: langName(lang), is_forced: forced, is_hearing_impaired: sdh }]);
                    },
                    `Added ${f.name}.`,
                  );
                  e.target.value = '';
                }}
              />
            </label>
          </div>
          <p className="field-hint">SRT, ASS, SSA, VTT and SUB files. They are stored in the library's .popcorn folder; your media folders are not touched.</p>

          <h3 className="manage-subhead">External and added</h3>
          {subs.length === 0 && <p className="muted">None. Subtitle files next to the video (Movie.en.srt) are found automatically.</p>}
          <ul className="sub-list">
            {subs.map((s) => (
              <SubRow key={s.id} lib={item.library_id} sub={s} run={run} onRemoved={() => setSubs((x) => x.filter((y) => y.id !== s.id))} />
            ))}
          </ul>

          {embedded.length > 0 && (
            <>
              <h3 className="manage-subhead">In the file</h3>
              <ul className="sub-list">
                {embedded.map((s) => (
                  <li key={s.index} className="sub-row">
                    <span>
                      {s.language_name || 'Unknown'} <span className="muted">{[s.title, s.codec, s.is_forced && 'forced', s.is_hearing_impaired && 'SDH', s.image_based && 'image: burned in when chosen'].filter(Boolean).join(', ')}</span>
                    </span>
                  </li>
                ))}
              </ul>
            </>
          )}
        </>
      )}
    </Dialog>
  );
}

function SubRow({
  lib,
  sub,
  run,
  onRemoved,
}: {
  lib: string;
  sub: ExternalSubtitle;
  run: (fn: () => Promise<unknown>, msg: string, apply?: () => void) => Promise<void>;
  onRemoved: () => void;
}) {
  const [lang, setLang] = useState(sub.language ?? '');
  const [title, setTitle] = useState(sub.title ?? '');
  const [forced, setForced] = useState(sub.is_forced);
  const [sdh, setSdh] = useState(sub.is_hearing_impaired);
  const [name, setName] = useState('');
  return (
    <li className="sub-row">
      <div className="sub-row-head">
        <strong>{sub.path.split('/').pop()}</strong>
        <span className="muted">{sub.source === 'upload' ? 'Added' : 'Next to the video'}</span>
      </div>
      <div className="sub-row-edit">
        <input value={lang} onChange={(e) => setLang(e.target.value)} aria-label="Language code" placeholder="Language (eng, hin…)" style={{ width: 150 }} />
        <input value={title} onChange={(e) => setTitle(e.target.value)} aria-label="Title" placeholder="Title" />
        <label className="check">
          <input type="checkbox" checked={forced} onChange={(e) => setForced(e.target.checked)} /> Forced
        </label>
        <label className="check">
          <input type="checkbox" checked={sdh} onChange={(e) => setSdh(e.target.checked)} /> SDH
        </label>
        <button type="button" className="button button-sm" onClick={() => void run(() => updateSubtitle(lib, sub.id, { language: lang, title, forced, sdh }), 'Subtitle updated.')}>
          Save
        </button>
        <label className="button button-sm">
          Replace
          <input
            type="file"
            accept=".srt,.ass,.ssa,.vtt,.sub"
            className="visually-hidden"
            onChange={(e) => {
              const f = e.target.files?.[0];
              if (f) void run(() => replaceSubtitle(lib, sub.id, f), 'Subtitle replaced.');
            }}
          />
        </label>
        <button type="button" className="button button-sm" onClick={() => void run(() => deleteSubtitle(lib, sub.id), sub.source === 'upload' ? 'Subtitle removed.' : 'Subtitle file deleted.', onRemoved)} aria-label="Remove subtitle">
          <Trash2 size={14} />
        </button>
      </div>
      {sub.source === 'sidecar' && (
        <div className="sub-row-edit">
          <input value={name} onChange={(e) => setName(e.target.value)} placeholder="New file name" aria-label="New file name" />
          <button type="button" className="button button-sm" disabled={!name.trim()} onClick={() => void run(() => renameSubtitle(lib, sub.id, name.trim()), 'Subtitle file renamed.')}>
            Rename file
          </button>
        </div>
      )}
    </li>
  );
}

export function ManageSection({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section className="manage-section">
      <h3 className="manage-subhead">{title}</h3>
      {children}
    </section>
  );
}
