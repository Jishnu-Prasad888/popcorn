import { Link } from 'react-router-dom';

import { useAuth } from '../auth/authContext';
import { getHome } from '../api/media';
import { useResource } from '../api/resources';
import { LandscapeCard, PosterCard, Row } from '../components/media/Cards';
import { Hero } from '../components/media/Hero';
import { CollectionTile } from '../components/media/CollectionTile';
import { ErrorState, LoadingState } from '../components/States';
import { usePreferences } from '../lib/preferences';

export default function HomePage() {
  const { user } = useAuth();
  const home = useResource(getHome, []);
  const { iface } = usePreferences();

  if (home.loading) return <div className="page"><LoadingState /></div>;
  if (home.error) return <div className="page"><ErrorState message={home.error} onRetry={home.reload} /></div>;
  const data = home.data!;

  if (data.counts.movies === 0 && data.counts.shows === 0) {
    return (
      <div className="page">
        <div className="empty-media">
          <h2>Nothing to watch yet</h2>
          <p>
            Movies and shows appear here once a library is added and scanned. Put movies in folders like
            <code> Movies/Heat (1995)/Heat.mkv</code> and shows like <code>TV/Dark/Season 01/Dark S01E01.mkv</code>.
          </p>
          {user?.role === 'admin' ? (
            <Link to="/libraries" className="primary-button">
              Add a library
            </Link>
          ) : (
            <p>Ask the server's administrator to give you access to a library.</p>
          )}
        </div>
      </div>
    );
  }

  const hidden = new Set(iface.home_rows.filter((r) => r.startsWith('-')).map((r) => r.slice(1)));
  const rows = data.rows.filter((r) => !hidden.has(r.id));
  const hero = rows.find((r) => r.kind === 'hero');

  return (
    <div className="home">
      {hero ? <Hero items={hero.items} /> : <div style={{ height: 'var(--space-5)' }} />}
      {rows
        .filter((r) => r.kind !== 'hero')
        .map((r) =>
          r.kind === 'landscape' ? (
            <Row key={r.id} title={r.title} link={r.link} landscape>
              {r.items.map((c) => (
                <LandscapeCard key={c.library_id + c.id} c={c} />
              ))}
            </Row>
          ) : (
            <Row key={r.id} title={r.title} link={r.link}>
              {r.items.map((c) => (
                <PosterCard key={c.library_id + c.id} c={c} />
              ))}
            </Row>
          ),
        )}
      {data.collections.length > 0 && !hidden.has('collections') && (
        <Row title="Your collections" link="/collections" landscape>
          {data.collections.map((col) => (
            <CollectionTile key={col.id} collection={col} onChanged={home.reload} />
          ))}
        </Row>
      )}
    </div>
  );
}
