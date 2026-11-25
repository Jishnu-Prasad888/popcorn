import { useState } from 'react';
import { Link, useParams } from 'react-router-dom';
import { Play, RotateCcw } from 'lucide-react';

import { formatTimecode, getMovie, playPath, progressOf } from '../api/media';
import { useResource } from '../api/resources';
import { PosterCard, Row, SignalBar } from '../components/media/Cards';
import { CastRow, Crew, DetailHeader, TechInfo } from '../components/media/Detail';
import { ItemActions } from '../components/media/ItemActions';
import { ManageMenu } from '../components/manage/ManageDialogs';
import { ErrorState, LoadingState } from '../components/States';
import { formatDateTime } from '../lib/dates';

export default function MovieDetailPage() {
  const { lib = '', id = '' } = useParams();
  const res = useResource(() => getMovie(lib, id), [lib, id]);
  const [fileId, setFileId] = useState('');

  if (res.loading && !res.data) return <div className="page"><LoadingState /></div>;
  if (res.error || !res.data) return <div className="page"><ErrorState message={res.error ?? 'Not found.'} onRetry={res.reload} /></div>;
  const { movie, related, history, can_edit } = res.data;
  const progress = progressOf(movie);
  const present = movie.files.filter((f) => f.status === 'present');
  const chosen = fileId || present[0]?.id;
  const playTo = (restart: boolean) => {
    const base = playPath(movie, { restart });
    return chosen && present.length > 1 ? `${base}${base.includes('?') ? '&' : '?'}file=${chosen}` : base;
  };
  const credits = movie.credits ?? [];

  return (
    <div className="detail">
      <DetailHeader
        item={movie}
        onChanged={res.reload}
        actions={
          <>
            {present.length === 0 ? (
              <span className="tech-missing">Not available right now: the disk holding this movie may be disconnected.</span>
            ) : progress > 0 ? (
              <>
                <Link className="btn-play" to={playTo(false)}>
                  <Play size={20} fill="currentColor" /> Resume {formatTimecode(movie.state?.position_secs ?? 0)}
                </Link>
                <Link className="btn-ghost" to={playTo(true)}>
                  <RotateCcw size={17} /> From the start
                </Link>
              </>
            ) : (
              <Link className="btn-play" to={playTo(false)}>
                <Play size={20} fill="currentColor" /> Play
              </Link>
            )}
            <ItemActions item={movie} onChange={res.reload} />
            {can_edit && (
              <ManageMenu
                onChanged={res.reload}
                item={{
                  library_id: movie.library_id,
                  type: 'movie',
                  id: movie.id,
                  title: movie.title,
                  year: movie.year,
                  art: movie.art,
                  locked_fields: movie.locked_fields,
                  values: movie as unknown as Record<string, unknown>,
                  files: movie.files,
                }}
              />
            )}
          </>
        }
      >
        {progress > 0 && (
          <div style={{ maxWidth: 360, marginTop: 'var(--space-4)' }}>
            <SignalBar value={progress} />
          </div>
        )}
        {present.length > 1 && (
          <label className="version-pick">
            <span className="muted">Version</span>
            <select className="select" value={chosen} onChange={(e) => setFileId(e.target.value)}>
              {present.map((f) => (
                <option key={f.id} value={f.id}>
                  {[f.edition || f.name, f.resolution, f.hdr].filter(Boolean).join(', ')}
                </option>
              ))}
            </select>
          </label>
        )}
        {movie.genres && movie.genres.length > 0 && (
          <div className="genre-chips" style={{ marginTop: 'var(--space-5)' }}>
            {movie.genres.map((g) => (
              <Link key={g} className="genre-chip" to={`/movies?genre=${encodeURIComponent(g)}`}>
                {g}
              </Link>
            ))}
          </div>
        )}
        <Crew credits={credits} libraryId={movie.library_id} />
      </DetailHeader>

      <div className="detail-body">
        <CastRow credits={credits} libraryId={movie.library_id} />
      </div>
      {related.length > 0 && (
        <Row title="More like this">
          {related.map((c) => (
            <PosterCard key={c.library_id + c.id} c={c} />
          ))}
        </Row>
      )}
      <div className="detail-body">
        <TechInfo files={movie.files} />
        {(movie.studios?.length || movie.countries?.length || movie.imdb_id) && (
          <section className="detail-section">
            <h2 className="section-title">Details</h2>
            <dl className="crew">
              {movie.release_date && (
                <div>
                  <dt>Released</dt>
                  <dd>{movie.release_date}</dd>
                </div>
              )}
              {movie.studios?.length ? (
                <div>
                  <dt>Studios</dt>
                  <dd>{movie.studios.join(', ')}</dd>
                </div>
              ) : null}
              {movie.countries?.length ? (
                <div>
                  <dt>Countries</dt>
                  <dd>{movie.countries.join(', ')}</dd>
                </div>
              ) : null}
              {movie.imdb_id && (
                <div>
                  <dt>Links</dt>
                  <dd>
                    <a href={`https://www.imdb.com/title/${movie.imdb_id}/`} target="_blank" rel="noreferrer">
                      IMDb
                    </a>
                    {movie.tmdb_id ? (
                      <>
                        {', '}
                        <a href={`https://www.themoviedb.org/movie/${movie.tmdb_id}`} target="_blank" rel="noreferrer">
                          TMDB
                        </a>
                      </>
                    ) : null}
                  </dd>
                </div>
              )}
            </dl>
          </section>
        )}
        {history.length > 0 && (
          <section className="detail-section">
            <h2 className="section-title">You watched this</h2>
            <ul className="plain-list">
              {history.map((h) => (
                <li key={h.entry_id}>{formatDateTime(h.watched_at)}</li>
              ))}
            </ul>
          </section>
        )}
      </div>
    </div>
  );
}
