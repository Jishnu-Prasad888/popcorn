import { useEffect, useState } from 'react';
import type { ReactNode } from 'react';
import { Link } from 'react-router-dom';
import { Info, Play, RotateCcw } from 'lucide-react';

import {
  backdropOf,
  formatRuntime,
  itemPath,
  logoOf,
  playPath,
  progressOf,
  ratingOf,
} from '../../api/media';
import type { Card } from '../../api/media';
import { SignalBar } from './Cards';
import { useCardMenu } from './CardMenu';

/** The facts line under a title: year, runtime, rating, age rating, genres. */
export function Facts({ c }: { c: Card }) {
  const rating = c.imdb_rating;
  const facts: ReactNode[] = [];
  if (c.year) facts.push(<span key="y">{c.year}</span>);
  const runtime = formatRuntime(c.runtime_mins, c.type === 'movie' ? c.duration_secs : undefined);
  if (runtime) facts.push(<span key="r">{runtime}</span>);
  if (c.type === 'show' && c.season_count) {
    facts.push(
      <span key="s">
        {c.season_count} season{c.season_count === 1 ? '' : 's'}
      </span>,
    );
  }
  if (c.content_rating)
    facts.push(
      <span key="c" className="content-rating">
        {c.content_rating}
      </span>,
    );
  if (rating) {
    facts.push(
      <span key="i" className="rating-imdb">
        <b>IMDb</b>
        {rating.toFixed(1)}
      </span>,
    );
  } else if (ratingOf(c)) {
    facts.push(
      <span key="t" className="card-rating">
        {ratingOf(c)!.toFixed(1)}
      </span>,
    );
  }
  if (c.genres?.length) facts.push(<span key="g">{c.genres.slice(0, 3).join(', ')}</span>);
  return <p className="hero-facts">{facts}</p>;
}

/**
 * The home page's cinematic opener: a backdrop, the title (or its logo) set
 * large, and the way to start watching. Several featured titles rotate.
 */
export function Hero({ items }: { items: Card[] }) {
  const [index, setIndex] = useState(0);
  const [paused, setPaused] = useState(false);
  const c = items[index % items.length];

  useEffect(() => {
    if (items.length < 2 || paused) return;
    const reduced = window.matchMedia('(prefers-reduced-motion: reduce)').matches;
    if (reduced) return;
    const t = window.setInterval(() => setIndex((i) => (i + 1) % items.length), 9000);
    return () => window.clearInterval(t);
  }, [items.length, paused]);

  if (!c) return null;
  return <HeroSlide c={c} items={items} index={index} setIndex={setIndex} setPaused={setPaused} />;
}

function HeroSlide({
  c: item,
  items,
  index,
  setIndex,
  setPaused,
}: {
  c: Card;
  items: Card[];
  index: number;
  setIndex: (i: number) => void;
  setPaused: (p: boolean) => void;
}) {
  const { card: c, onContextMenu, element } = useCardMenu(item, { landscape: true });
  const backdrop = backdropOf(c);
  const logo = logoOf(c);
  const progress = progressOf(c);
  const resumable = progress > 0;

  return (
    <section
      onContextMenu={onContextMenu}
      className="hero"
      aria-roledescription="carousel"
      aria-label="Featured"
      onMouseEnter={() => setPaused(true)}
      onMouseLeave={() => setPaused(false)}
      onFocus={() => setPaused(true)}
      onBlur={() => setPaused(false)}
    >
      {element}
      <div className="hero-backdrop" key={c.library_id + c.id}>
        {backdrop && <img src={backdrop} alt="" />}
      </div>
      <div className="hero-body">
        {logo ? (
          <img className="hero-logo" src={logo} alt={c.title} />
        ) : (
          <h1 className="hero-title">{c.title}</h1>
        )}
        {logo && <h1 className="visually-hidden">{c.title}</h1>}
        <Facts c={c} />
        {c.overview && <p className="hero-overview">{c.overview}</p>}
        {resumable && (
          <div style={{ maxWidth: 320, marginBottom: 'var(--space-4)' }}>
            <SignalBar value={progress} />
          </div>
        )}
        <div className="hero-actions">
          {c.type === 'show' ? (
            <Link className="btn-play" to={itemPath(c)}>
              <Play size={20} fill="currentColor" /> Watch
            </Link>
          ) : (
            <Link className="btn-play" to={playPath(c)}>
              {resumable ? <RotateCcw size={20} /> : <Play size={20} fill="currentColor" />}
              {resumable ? 'Resume' : 'Play'}
            </Link>
          )}
          <Link className="btn-ghost" to={itemPath(c)}>
            <Info size={18} /> Details
          </Link>
        </div>
        {items.length > 1 && (
          <div className="hero-dots" role="tablist" aria-label="Featured titles">
            {items.map((it, i) => (
              <button
                key={it.library_id + it.id}
                type="button"
                role="tab"
                className="hero-dot"
                aria-current={i === index}
                aria-selected={i === index}
                aria-label={it.title}
                onClick={() => setIndex(i)}
              />
            ))}
          </div>
        )}
      </div>
    </section>
  );
}
