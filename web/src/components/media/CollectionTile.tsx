import { Link } from 'react-router-dom';
import { Layers, ListVideo } from 'lucide-react';

import { backdropOf, collectionPosterUrl, playlistPosterUrl, posterOf } from '../../api/media';
import type { Card, Collection, Playlist, SmartCollection } from '../../api/media';

/** A collection or playlist tile: its own artwork, or a fan of its posters. */
export function CollectionTile({
  collection,
  playlist,
  smart,
  covers,
}: {
  collection?: Collection;
  playlist?: Playlist;
  smart?: SmartCollection;
  covers?: Card[];
}) {
  const item = collection ?? playlist ?? smart!;
  const to = playlist ? `/playlists/${item.id}` : `/collections/${item.id}`;
  const own = collection ? collectionPosterUrl(collection) : playlist ? playlistPosterUrl(playlist) : null;
  const cards = covers ?? collection?.covers ?? playlist?.covers ?? smart?.covers ?? [];
  const Icon = playlist ? ListVideo : Layers;
  const count = item.item_count;
  return (
    <article className="collection-tile">
      <Link to={to} className="collection-frame" aria-label={`${item.name}, ${count} item${count === 1 ? '' : 's'}`}>
        {own ? (
          <img src={own} alt="" loading="lazy" />
        ) : cards.length > 0 ? (
          <span className="collection-fan" aria-hidden="true">
            {cards.slice(0, 4).map((c, i) => {
              const src = posterOf(c) ?? backdropOf(c);
              return src ? <img key={c.library_id + c.id + i} src={src} alt="" loading="lazy" /> : <span key={i} />;
            })}
          </span>
        ) : (
          <span className="card-fallback">
            <Icon size={28} strokeWidth={1.5} />
          </span>
        )}
      </Link>
      <div className="card-meta">
        <Link to={to} className="card-title">
          {item.name}
        </Link>
        <p className="card-sub">
          {count} item{count === 1 ? '' : 's'}
          {smart && <span>Automatic</span>}
        </p>
      </div>
    </article>
  );
}
