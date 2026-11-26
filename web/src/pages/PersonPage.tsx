import { useParams } from 'react-router-dom';
import { User } from 'lucide-react';

import { getPerson, personImageUrl } from '../api/media';
import { useResource } from '../api/resources';
import { CardGrid } from '../components/media/Cards';
import { ErrorState, LoadingState } from '../components/States';
import '../components/media/detail.css';

export default function PersonPage() {
  const { lib = '', id = '' } = useParams();
  const res = useResource(() => getPerson(lib, id), [lib, id]);
  if (res.loading)
    return (
      <div className="page">
        <LoadingState />
      </div>
    );
  if (res.error || !res.data)
    return (
      <div className="page">
        <ErrorState message={res.error ?? 'Not found.'} onRetry={res.reload} />
      </div>
    );
  const p = res.data;
  const roles = new Set(
    Object.values(p.roles)
      .flat()
      .filter((r) => ['director', 'writer', 'creator', 'producer'].includes(r)),
  );
  const known = roles.size
    ? [...roles]
        .map(
          (r) =>
            ({ director: 'Director', writer: 'Writer', creator: 'Creator', producer: 'Producer' })[
              r
            ],
        )
        .join(', ')
    : 'Actor';
  const movies = p.items.filter((c) => c.type === 'movie');
  const shows = p.items.filter((c) => c.type === 'show');

  return (
    <div className="page">
      <header className="person-head">
        <span className="person-photo">
          {p.profile_url ? (
            <img src={personImageUrl(p.library_id, p.id)} alt="" />
          ) : (
            <User size={48} strokeWidth={1.25} />
          )}
        </span>
        <div>
          <h1 className="page-title">{p.name}</h1>
          <p className="page-count">
            {known}. {p.items.length} title{p.items.length === 1 ? '' : 's'} in your libraries.
          </p>
          {p.tmdb_id ? (
            <a
              href={`https://www.themoviedb.org/person/${p.tmdb_id}`}
              target="_blank"
              rel="noreferrer"
            >
              View on TMDB
            </a>
          ) : null}
        </div>
      </header>
      {movies.length > 0 && (
        <section className="detail-section">
          <h2 className="section-title">Movies</h2>
          <CardGrid items={movies} />
        </section>
      )}
      {shows.length > 0 && (
        <section className="detail-section">
          <h2 className="section-title">TV shows</h2>
          <CardGrid items={shows} />
        </section>
      )}
    </div>
  );
}
