import { useState } from 'react';
import { Link } from 'react-router-dom';
import { RefreshCw, ScanLine, Sparkles } from 'lucide-react';

import { useLibraries } from '../api/libraries';
import {
  errorMessage,
  getCatalogDuplicates,
  getLibraryStats,
  getUnassigned,
  itemPath,
  listStreams,
  refreshMetadata,
  syncCatalog,
} from '../api/media';
import { useResource } from '../api/resources';
import { ErrorState, LoadingState } from '../components/States';
import { useToast } from '../components/ui/Toast';
import { formatDateTime } from '../lib/dates';
import './AdminPage.css';

function bytes(n: number) {
  if (n <= 0) return '0 B';
  const u = ['B', 'KB', 'MB', 'GB', 'TB', 'PB'];
  const i = Math.min(u.length - 1, Math.floor(Math.log(n) / Math.log(1024)));
  return `${(n / 1024 ** i).toFixed(i >= 3 ? 1 : 0)} ${u[i]}`;
}

function Breakdown({ title, data }: { title: string; data: Record<string, number> }) {
  const entries = Object.entries(data ?? {}).sort((a, b) => b[1] - a[1]);
  const total = entries.reduce((s, [, v]) => s + v, 0);
  if (!entries.length) return null;
  return (
    <div className="breakdown">
      <h3>{title}</h3>
      <ul>
        {entries.map(([k, v]) => (
          <li key={k}>
            <span className="breakdown-label">{k || 'Unknown'}</span>
            <span className="breakdown-bar">
              <span style={{ width: `${(v / total) * 100}%` }} />
            </span>
            <span className="breakdown-value">{v}</span>
          </li>
        ))}
      </ul>
    </div>
  );
}

function LibraryPanel({ id, name }: { id: string; name: string }) {
  const toast = useToast();
  const stats = useResource(() => getLibraryStats(id), [id]);
  const dups = useResource(() => getCatalogDuplicates(id), [id]);
  const unassigned = useResource(() => getUnassigned(id), [id]);
  const [busy, setBusy] = useState('');

  const run = async (label: string, fn: () => Promise<unknown>, done: string) => {
    setBusy(label);
    try {
      await fn();
      toast({ message: done, tone: 'success' });
      stats.reload();
    } catch (err) {
      toast({ message: errorMessage(err), tone: 'error' });
    } finally {
      setBusy('');
    }
  };

  const s = stats.data?.stats;
  const jobs = stats.data?.jobs ?? {};
  const pending = Object.values(jobs).reduce((a, b) => a + b, 0);

  return (
    <section className="admin-library">
      <header className="admin-library-head">
        <h2>{name}</h2>
        <div className="admin-actions">
          <button type="button" className="button button-sm" disabled={busy !== ''} onClick={() => void run('sync', () => syncCatalog(id), 'Rebuilding the catalog from the files.')}>
            <ScanLine size={15} /> Re-scan files
          </button>
          <button type="button" className="button button-sm" disabled={busy !== ''} onClick={() => void run('meta', () => refreshMetadata(id), 'Fetching missing metadata.')}>
            <Sparkles size={15} /> Fetch missing metadata
          </button>
          <button type="button" className="button button-sm" disabled={busy !== ''} onClick={() => void run('force', () => refreshMetadata(id, [], true), 'Refreshing all metadata. Locked fields are kept.')}>
            <RefreshCw size={15} /> Refresh all
          </button>
        </div>
      </header>
      {stats.loading && <LoadingState />}
      {stats.error && <ErrorState message={stats.error} onRetry={stats.reload} />}
      {s && (
        <>
          <dl className="stat-tiles">
            <div>
              <dt>Movies</dt>
              <dd>{s.movies}</dd>
            </div>
            <div>
              <dt>Shows</dt>
              <dd>{s.shows}</dd>
            </div>
            <div>
              <dt>Episodes</dt>
              <dd>{s.episodes}</dd>
            </div>
            <div>
              <dt>Storage</dt>
              <dd>{bytes(s.total_bytes)}</dd>
            </div>
            <div>
              <dt>Running time</dt>
              <dd>{s.total_secs > 0 ? `${Math.round(s.total_secs / 3600)} h` : '—'}</dd>
            </div>
            <div>
              <dt>Subtitle files</dt>
              <dd>{s.subtitles}</dd>
            </div>
          </dl>
          {(s.unmatched > 0 || s.unprobed > 0 || s.missing_files > 0 || pending > 0 || !stats.data?.ffprobe) && (
            <ul className="admin-notes">
              {!stats.data?.ffprobe && <li className="is-warn">ffprobe isn't installed, so codecs, durations and tracks can't be read. Install ffmpeg on the server.</li>}
              {s.unmatched > 0 && <li>{s.unmatched} titles have no metadata match. Open one and use Manage → Identify.</li>}
              {s.unprobed > 0 && <li>{s.unprobed} files are waiting to be analysed.</li>}
              {s.missing_files > 0 && <li className="is-warn">{s.missing_files} files are missing from disk.</li>}
              {pending > 0 && (
                <li>
                  {pending} background jobs queued: {Object.entries(jobs).map(([k, v]) => `${k} ${v}`).join(', ')}.
                </li>
              )}
            </ul>
          )}
          <div className="breakdowns">
            <Breakdown title="Resolution" data={s.resolutions} />
            <Breakdown title="Video codec" data={s.video_codecs} />
            <Breakdown title="Container" data={s.containers} />
          </div>
        </>
      )}
      {dups.data && dups.data.duplicates.length > 0 && (
        <div className="admin-sub">
          <h3>Possible duplicates</h3>
          <ul className="plain-list">
            {dups.data.duplicates.map((d, i) => (
              <li key={i}>
                {d.movies.map((m, j) => (
                  <span key={m.id}>
                    {j > 0 && ' and '}
                    <Link to={itemPath(m)}>
                      {m.title} {m.year ? `(${m.year})` : ''}
                    </Link>
                  </span>
                ))}
                <span className="muted"> — same {d.kind}</span>
              </li>
            ))}
          </ul>
        </div>
      )}
      {unassigned.data && unassigned.data.files.length > 0 && (
        <div className="admin-sub">
          <h3>Files not in the catalog ({unassigned.data.files.length})</h3>
          <p className="muted">These video files couldn't be placed as a movie or an episode. Rename them like “Title (Year).mkv” or “Show/Season 1/Show S01E01.mkv” and re-scan.</p>
          <ul className="plain-list mono-list">
            {unassigned.data.files.slice(0, 50).map((f) => (
              <li key={f.id}>{f.rel_path}</li>
            ))}
          </ul>
        </div>
      )}
    </section>
  );
}

export default function AdminPage() {
  const { libraries, loading } = useLibraries();
  const streams = useResource(listStreams, []);
  const total = libraries.length;

  return (
    <div className="page admin-page">
      <header className="page-head">
        <div>
          <h1 className="page-title">Server</h1>
          <p className="page-count">
            {total} librar{total === 1 ? 'y' : 'ies'}. <Link to="/libraries">Manage libraries</Link> or set up <Link to="/admin/metadata">metadata providers</Link>.
          </p>
        </div>
      </header>

      <section className="admin-streams">
        <h2 className="section-title">Playing now</h2>
        {streams.data && streams.data.streams.length === 0 && <p className="muted">Nobody is watching anything right now.</p>}
        {streams.data && streams.data.streams.length > 0 && (
          <ul className="plain-list">
            {streams.data.streams.map((s) => (
              <li key={s.session}>
                <strong>{s.username}</strong> is watching {s.title} <span className={`method-chip method-${s.method}`}>{s.method}</span>{' '}
                <span className="muted">since {formatDateTime(s.started_at)}</span>
              </li>
            ))}
          </ul>
        )}
      </section>

      {loading && <LoadingState />}
      {libraries.map((l) => (
        <LibraryPanel key={l.id} id={l.id} name={l.name} />
      ))}
      {!loading && total === 0 && (
        <div className="empty-media">
          <h2>No libraries yet</h2>
          <p>Add a folder of movies or TV shows to start.</p>
          <Link className="primary-button" to="/libraries">
            Add a library
          </Link>
        </div>
      )}
    </div>
  );
}
