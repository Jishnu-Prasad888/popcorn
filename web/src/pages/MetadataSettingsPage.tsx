import { useState } from 'react';
import { CheckCircle2, KeyRound } from 'lucide-react';

import {
  errorMessage,
  getMetadataSettings,
  saveMetadataSettings,
  testMetadata,
} from '../api/media';
import { useResource } from '../api/resources';
import { ErrorState, LoadingState } from '../components/States';
import { Toggle } from '../components/ui/Toggle';
import { useToast } from '../components/ui/Toast';
import './AdminPage.css';

export default function MetadataSettingsPage() {
  const res = useResource(getMetadataSettings, []);
  const toast = useToast();
  const [tmdb, setTmdb] = useState('');
  const [omdb, setOmdb] = useState('');
  // Null until edited: the saved values show until then.
  const [languageEdit, setLanguage] = useState<string | null>(null);
  const [regionEdit, setRegion] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const [test, setTest] = useState<{ ok: boolean; error?: string } | null>(null);

  if (res.loading && !res.data)
    return (
      <div className="page">
        <LoadingState />
      </div>
    );
  if (res.error || !res.data)
    return (
      <div className="page">
        <ErrorState message={res.error ?? 'Could not load.'} onRetry={res.reload} />
      </div>
    );
  const s = res.data;
  const language = languageEdit ?? (s.language || 'en-US');
  const region = regionEdit ?? (s.region || '');

  const save = async (patch: Parameters<typeof saveMetadataSettings>[0], msg = 'Saved.') => {
    setSaving(true);
    try {
      await saveMetadataSettings(patch);
      toast({ message: msg, tone: 'success' });
      res.reload();
    } catch (err) {
      toast({ message: errorMessage(err), tone: 'error' });
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="page admin-page" style={{ maxWidth: 820 }}>
      <header className="page-head">
        <div>
          <h1 className="page-title">Metadata providers</h1>
          <p className="page-count">
            Posters, plots, cast and ratings come from TMDB. OMDb adds IMDb ratings. Both keys are
            free.
          </p>
        </div>
      </header>

      <form
        className="meta-form"
        onSubmit={(e) => {
          e.preventDefault();
          const patch: Parameters<typeof saveMetadataSettings>[0] = { language, region };
          if (tmdb) patch.tmdb_key = tmdb;
          if (omdb) patch.omdb_key = omdb;
          void save(patch, 'Provider settings saved.').then(() => {
            setTmdb('');
            setOmdb('');
          });
        }}
      >
        <label className="field">
          <span className="field-label">
            TMDB API key{' '}
            {s.tmdb_key_set && (
              <span className="key-set">
                <CheckCircle2 size={14} /> set {s.tmdb_key_hint && `(…${s.tmdb_key_hint})`}
              </span>
            )}
          </span>
          <input
            type="password"
            autoComplete="off"
            value={tmdb}
            onChange={(e) => setTmdb(e.target.value)}
            placeholder={
              s.tmdb_key_set ? 'Leave empty to keep the current key' : 'v3 API key or v4 read token'
            }
          />
          <span className="field-hint">
            Create one at{' '}
            <a href="https://www.themoviedb.org/settings/api" target="_blank" rel="noreferrer">
              themoviedb.org/settings/api
            </a>
            .
          </span>
        </label>
        <label className="field">
          <span className="field-label">
            OMDb API key{' '}
            {s.omdb_key_set && (
              <span className="key-set">
                <CheckCircle2 size={14} /> set {s.omdb_key_hint && `(…${s.omdb_key_hint})`}
              </span>
            )}
          </span>
          <input
            type="password"
            autoComplete="off"
            value={omdb}
            onChange={(e) => setOmdb(e.target.value)}
            placeholder={s.omdb_key_set ? 'Leave empty to keep the current key' : 'Optional'}
          />
          <span className="field-hint">
            For IMDb ratings. Get one at{' '}
            <a href="https://www.omdbapi.com/apikey.aspx" target="_blank" rel="noreferrer">
              omdbapi.com
            </a>
            .
          </span>
        </label>
        <div className="meta-row">
          <label className="field">
            <span className="field-label">Language</span>
            <input
              value={language}
              onChange={(e) => setLanguage(e.target.value)}
              placeholder="en-US"
            />
            <span className="field-hint">
              Titles and plots in this language when TMDB has them.
            </span>
          </label>
          <label className="field">
            <span className="field-label">Region</span>
            <input
              value={region}
              onChange={(e) => setRegion(e.target.value.toUpperCase())}
              placeholder="IN"
              maxLength={2}
            />
            <span className="field-hint">For release dates and age ratings.</span>
          </label>
        </div>
        <div className="meta-buttons">
          <button type="submit" className="primary-button" disabled={saving}>
            <KeyRound size={16} /> Save
          </button>
          <button
            type="button"
            className="button"
            disabled={!s.tmdb_key_set}
            onClick={async () => {
              setTest(null);
              setTest(
                await testMetadata().catch((err: unknown) => ({
                  ok: false,
                  error: errorMessage(err),
                })),
              );
            }}
          >
            Test connection
          </button>
          {test && (
            <span className={test.ok ? 'test-ok' : 'error-text'}>
              {test.ok ? 'TMDB answered. Metadata will work.' : test.error}
            </span>
          )}
        </div>
      </form>

      <section className="meta-toggles">
        <div className="setting-row">
          <div className="setting-text">
            <span className="setting-label">Fetch metadata automatically</span>
            <span className="setting-hint">
              Match new movies and shows as soon as they're found.
            </span>
          </div>
          <Toggle
            label="Fetch metadata automatically"
            hideLabel
            checked={s.auto}
            onChange={(v) => void save({ auto: v })}
          />
        </div>
        <div className="setting-row">
          <div className="setting-text">
            <span className="setting-label">Make episode thumbnails from the video</span>
            <span className="setting-hint">
              When TMDB has no still for an episode, take a frame with ffmpeg.
            </span>
          </div>
          <Toggle
            label="Make episode thumbnails"
            hideLabel
            checked={s.generate_thumbs}
            onChange={(v) => void save({ generate_thumbs: v })}
          />
        </div>
      </section>
    </div>
  );
}
