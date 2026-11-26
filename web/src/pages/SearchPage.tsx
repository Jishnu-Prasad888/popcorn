import { Link, useSearchParams } from 'react-router-dom';
import { User } from 'lucide-react';

import { personImageUrl, search } from '../api/media';
import { useResource } from '../api/resources';
import { LandscapeCard, PosterCard, Row } from '../components/media/Cards';
import { ErrorState, LoadingState } from '../components/States';
import '../components/media/detail.css';

const SUGGESTIONS = [
  'Action',
  'Comedy',
  'Drama',
  'Animation',
  'Science Fiction',
  'Thriller',
  'Documentary',
  'Horror',
];

export default function SearchPage() {
  const [params] = useSearchParams();
  const q = (params.get('q') ?? '').trim();
  const res = useResource(() => search(q), [q], q.length > 0);

  return (
    <div className="search-page">
      <div className="page">
        <h1 className="page-title">{q ? `Results for “${q}”` : 'Search'}</h1>
        {!q && (
          <>
            <p className="page-count">
              Find movies, shows, episodes and people. Press / anywhere to search.
            </p>
            <div className="genre-chips" style={{ marginTop: 'var(--space-5)' }}>
              {SUGGESTIONS.map((g) => (
                <Link key={g} className="genre-chip" to={`/search?q=${encodeURIComponent(g)}`}>
                  {g}
                </Link>
              ))}
            </div>
          </>
        )}
        {q && res.loading && <LoadingState label="Searching…" />}
        {q && res.error && <ErrorState message={res.error} onRetry={res.reload} />}
      </div>
      {res.data && (
        <>
          {res.data.movies.length +
            res.data.shows.length +
            res.data.episodes.length +
            res.data.people.length ===
            0 && (
            <div className="page">
              <div className="empty-media">
                <h2>Nothing matches “{q}”</h2>
                <p>
                  Check the spelling, try the original title, or search for an actor or director.
                </p>
              </div>
            </div>
          )}
          {res.data.movies.length > 0 && (
            <Row title={`Movies (${res.data.movies.length})`}>
              {res.data.movies.map((c) => (
                <PosterCard key={c.library_id + c.id} c={c} />
              ))}
            </Row>
          )}
          {res.data.shows.length > 0 && (
            <Row title={`TV shows (${res.data.shows.length})`}>
              {res.data.shows.map((c) => (
                <PosterCard key={c.library_id + c.id} c={c} />
              ))}
            </Row>
          )}
          {res.data.people.length > 0 && (
            <section className="row" aria-label="People">
              <header className="row-head">
                <h2 className="row-title">People</h2>
              </header>
              <ul className="cast-row" style={{ padding: '0 var(--content-gutter)' }}>
                {res.data.people.map((p) => (
                  <li key={p.library_id + p.id}>
                    <Link to={`/person/${p.library_id}/${p.id}`} className="cast-card">
                      <span className="cast-photo">
                        {p.profile_url ? (
                          <img src={personImageUrl(p.library_id, p.id)} alt="" loading="lazy" />
                        ) : (
                          <User size={26} strokeWidth={1.5} />
                        )}
                      </span>
                      <span className="cast-name">{p.name}</span>
                      <span className="cast-character">
                        {p.credits} title{p.credits === 1 ? '' : 's'}
                      </span>
                    </Link>
                  </li>
                ))}
              </ul>
            </section>
          )}
          {res.data.episodes.length > 0 && (
            <Row title={`Episodes (${res.data.episodes.length})`} landscape>
              {res.data.episodes.map((c) => (
                <LandscapeCard key={c.library_id + c.id} c={c} />
              ))}
            </Row>
          )}
        </>
      )}
    </div>
  );
}
