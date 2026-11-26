import { useRef, useState } from 'react';
import type { ReactNode } from 'react';
import { Link } from 'react-router-dom';
import { Check, ChevronLeft, ChevronRight, Film, Play, Star, Tv } from 'lucide-react';

import {
  episodeCode,
  formatRuntime,
  itemPath,
  landscapeOf,
  playPath,
  posterOf,
  progressOf,
  ratingOf,
} from '../../api/media';
import type { Card } from '../../api/media';
import { useCardMenu } from './CardMenu';
import './media.css';

/** A thin purple-to-pink bar showing how far into an item someone is. */
export function SignalBar({ value, className = '' }: { value: number; className?: string }) {
  if (value <= 0) return null;
  return (
    <span
      className={`signal-bar ${className}`}
      role="progressbar"
      aria-valuenow={Math.round(value * 100)}
      aria-valuemin={0}
      aria-valuemax={100}
      aria-label="Watched"
    >
      <span style={{ width: `${Math.max(3, value * 100)}%` }} />
    </span>
  );
}

function Img({ src, alt, fallback }: { src: string | null; alt: string; fallback: ReactNode }) {
  const [failed, setFailed] = useState(false);
  if (!src || failed) return <span className="card-fallback">{fallback}</span>;
  return (
    <img src={src} alt={alt} loading="lazy" decoding="async" onError={() => setFailed(true)} />
  );
}

function Placeholder({ c }: { c: Card }) {
  const Icon = c.type === 'movie' ? Film : Tv;
  return (
    <>
      <Icon size={28} strokeWidth={1.5} aria-hidden="true" />
      <span className="card-fallback-title">
        {c.type === 'episode' ? (c.show_title ?? c.title) : c.title}
      </span>
    </>
  );
}

function subtitleFor(c: Card): string {
  switch (c.type) {
    case 'episode':
      return [episodeCode(c), c.title].filter(Boolean).join(' ');
    case 'show': {
      const n = c.season_count ?? 0;
      return [c.year, n ? `${n} season${n === 1 ? '' : 's'}` : ''].filter(Boolean).join(', ');
    }
    default:
      return [c.year, formatRuntime(c.runtime_mins, c.duration_secs)].filter(Boolean).join(', ');
  }
}

function Badge({ c }: { c: Card }) {
  const st = c.state;
  if (st?.played) {
    return (
      <span className="card-badge card-badge-played" title="Watched">
        <Check size={13} strokeWidth={3} aria-label="Watched" />
      </span>
    );
  }
  if (
    c.type === 'show' &&
    st &&
    st.unplayed_count > 0 &&
    st.unplayed_count < (c.episode_count ?? 0)
  ) {
    return (
      <span className="card-badge" title={`${st.unplayed_count} unwatched`}>
        {st.unplayed_count}
      </span>
    );
  }
  return null;
}

/** A 2:3 poster card for a movie or show (or an episode, by its show). */
export function PosterCard({ c: item, sub }: { c: Card; sub?: string }) {
  const { card: c, onContextMenu, element } = useCardMenu(item);
  const title = c.type === 'episode' ? (c.show_title ?? c.title) : c.title;
  const rating = ratingOf(c);
  return (
    <article className="poster-card" onContextMenu={onContextMenu}>
      {element}
      <div className="poster-wrap">
        <Link to={itemPath(c)} className="poster-frame" aria-label={title}>
          <Img src={posterOf(c)} alt="" fallback={<Placeholder c={c} />} />
          <Badge c={c} />
          {!c.available && <span className="card-offline">Offline</span>}
          <SignalBar value={progressOf(c)} className="poster-signal" />
        </Link>
        {(c.type === 'movie' || c.type === 'episode') && c.available && (
          <Link to={playPath(c)} className="card-play" aria-label={`Play ${title}`}>
            <Play size={18} fill="currentColor" />
          </Link>
        )}
      </div>
      <div className="card-meta">
        <Link to={itemPath(c)} className="card-title">
          {title}
        </Link>
        <p className="card-sub">
          {sub ?? subtitleFor(c)}
          {rating ? (
            <span className="card-rating">
              <Star size={11} fill="currentColor" aria-hidden="true" />
              {rating.toFixed(1)}
            </span>
          ) : null}
        </p>
      </div>
    </article>
  );
}

/** A 16:9 card: Continue Watching, Next Up and episode lists. */
export function LandscapeCard({ c: item, playable = true }: { c: Card; playable?: boolean }) {
  const { card: c, onContextMenu, element } = useCardMenu(item, { landscape: true });
  const title = c.type === 'episode' ? (c.show_title ?? c.title) : c.title;
  const sub =
    c.type === 'episode'
      ? subtitleFor(c)
      : [c.year, formatRuntime(c.runtime_mins, c.duration_secs)].filter(Boolean).join(', ');
  const to = playable && c.available ? playPath(c) : itemPath(c);
  return (
    <article className="landscape-card" onContextMenu={onContextMenu}>
      {element}
      <Link
        to={to}
        className="landscape-frame"
        aria-label={`${playable ? 'Play' : 'Open'} ${title}${c.type === 'episode' ? `, ${sub}` : ''}`}
      >
        <Img src={landscapeOf(c)} alt="" fallback={<Placeholder c={c} />} />
        {playable && (
          <span className="landscape-play" aria-hidden="true">
            <Play size={22} fill="currentColor" />
          </span>
        )}
        <Badge c={c} />
        <SignalBar value={progressOf(c)} className="landscape-signal" />
      </Link>
      <div className="card-meta">
        <Link to={itemPath(c)} className="card-title">
          {title}
        </Link>
        <p className="card-sub">{sub}</p>
      </div>
    </article>
  );
}

/** A horizontally scrolling row with a title and arrow buttons. */
export function Row({
  title,
  link,
  children,
  landscape = false,
}: {
  title: string;
  link?: string | undefined;
  children: ReactNode;
  landscape?: boolean;
}) {
  const ref = useRef<HTMLDivElement>(null);
  const scroll = (dir: number) => {
    const el = ref.current;
    if (el) el.scrollBy({ left: dir * el.clientWidth * 0.85, behavior: 'smooth' });
  };
  return (
    <section className="row" aria-label={title}>
      <header className="row-head">
        <h2 className="row-title">{link ? <Link to={link}>{title}</Link> : title}</h2>
        <div className="row-arrows">
          <button
            type="button"
            className="icon-button"
            onClick={() => scroll(-1)}
            aria-label={`Scroll ${title} back`}
          >
            <ChevronLeft size={20} />
          </button>
          <button
            type="button"
            className="icon-button"
            onClick={() => scroll(1)}
            aria-label={`Scroll ${title} forward`}
          >
            <ChevronRight size={20} />
          </button>
        </div>
      </header>
      <div className={`row-track ${landscape ? 'row-track-landscape' : ''}`} ref={ref}>
        {children}
      </div>
    </section>
  );
}

/** A responsive grid of poster cards, or a list. */
export function CardGrid({ items, view = 'grid' }: { items: Card[]; view?: string }) {
  if (view === 'list') {
    return (
      <ul className="card-list">
        {items.map((c) => (
          <li key={c.library_id + c.id}>
            <ListRow c={c} />
          </li>
        ))}
      </ul>
    );
  }
  return (
    <div className="card-grid">
      {items.map((c) => (
        <PosterCard key={c.library_id + c.id} c={c} />
      ))}
    </div>
  );
}

function ListRow({ c: item }: { c: Card }) {
  const { card: c, onContextMenu, element } = useCardMenu(item);
  const rating = ratingOf(c);
  return (
    <div className="list-row" onContextMenu={onContextMenu}>
      {element}
      <Link to={itemPath(c)} className="list-thumb" aria-hidden="true" tabIndex={-1}>
        <Img src={posterOf(c)} alt="" fallback={<Placeholder c={c} />} />
      </Link>
      <div className="list-body">
        <Link to={itemPath(c)} className="card-title">
          {c.title}
        </Link>
        <p className="card-sub">
          {[
            c.year,
            (c.genres ?? []).slice(0, 3).join(', '),
            formatRuntime(c.runtime_mins, c.duration_secs),
          ]
            .filter(Boolean)
            .join('  /  ')}
        </p>
        {c.overview && <p className="list-overview">{c.overview}</p>}
        <SignalBar value={progressOf(c)} />
      </div>
      <div className="list-side">
        {rating ? (
          <span className="card-rating">
            <Star size={12} fill="currentColor" aria-hidden="true" />
            {rating.toFixed(1)}
          </span>
        ) : null}
        {c.resolution && <span className="tech-chip">{c.resolution}</span>}
        {c.state?.played && (
          <span className="played-chip">
            <Check size={12} strokeWidth={3} aria-hidden="true" /> Watched
          </span>
        )}
      </div>
    </div>
  );
}

/** Placeholder cards while a grid loads. */
export function GridSkeleton({ count = 12 }: { count?: number }) {
  return (
    <div className="card-grid" aria-hidden="true">
      {Array.from({ length: count }, (_, i) => (
        <div key={i} className="poster-card">
          <span className="poster-frame skeleton" />
          <span className="skeleton skeleton-line" />
        </div>
      ))}
    </div>
  );
}
