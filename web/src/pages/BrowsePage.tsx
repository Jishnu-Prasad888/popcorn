import { useEffect, useMemo, useRef, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import { ArrowDownWideNarrow, ArrowUpNarrowWide, LayoutGrid, List, X } from 'lucide-react';

import { getFacets, listMovies, listShows } from '../api/media';
import type { Listing } from '../api/media';
import { useLibraries } from '../api/libraries';
import { useResource } from '../api/resources';
import { CardGrid, GridSkeleton } from '../components/media/Cards';
import { ErrorState } from '../components/States';
import { updateInterface, usePreferences } from '../lib/preferences';

const PAGE = 60;

const SORTS: Record<'movie' | 'show', [string, string][]> = {
  movie: [
    ['title', 'Title'],
    ['release', 'Release date'],
    ['rating', 'Rating'],
    ['added', 'Date added'],
    ['runtime', 'Runtime'],
    ['size', 'File size'],
    ['played', 'Last watched'],
  ],
  show: [
    ['title', 'Title'],
    ['release', 'First aired'],
    ['rating', 'Rating'],
    ['added', 'Date added'],
    ['updated', 'Latest episode'],
    ['played', 'Last watched'],
  ],
};

const WATCHED: [string, string][] = [
  ['', 'All'],
  ['unwatched', 'Unwatched'],
  ['in_progress', 'In progress'],
  ['watched', 'Watched'],
  ['favorite', 'Favourites'],
];

const RESOLUTIONS: [string, string][] = [
  ['', 'Any quality'],
  ['4k', '4K'],
  ['1080p', '1080p'],
  ['720p', '720p'],
  ['sd', 'SD'],
];

const RATINGS: [string, string][] = [
  ['', 'Any rating'],
  ['8', '8+'],
  ['7', '7+'],
  ['6', '6+'],
  ['5', '5+'],
];

const CATEGORIES: [string, string][] = [
  ['', 'All shows'],
  ['tv', 'TV'],
  ['anime', 'Anime'],
  ['web', 'Web series'],
];

const LANGUAGE_NAMES = new Intl.DisplayNames(['en'], { type: 'language' });
const languageName = (code: string) => {
  try {
    return LANGUAGE_NAMES.of(code) ?? code;
  } catch {
    return code;
  }
};

/** Filters, sort and view live in the URL, so a filtered view can be shared and comes back on Back. */
const FILTER_KEYS = [
  'genre',
  'decade',
  'year',
  'min_rating',
  'language',
  'resolution',
  'watched',
  'category',
  'person',
  'director',
  'actor',
  'studio',
  'lib',
];

export default function BrowsePage({ type }: { type: 'movie' | 'show' }) {
  const [params, setParams] = useSearchParams();
  const { iface } = usePreferences();
  const { libraries } = useLibraries();
  const view = iface.view === 'list' ? 'list' : 'grid';
  const sort = params.get('sort') ?? 'title';
  const order = params.get('order') ?? '';
  const facets = useResource(() => getFacets(type), [type]);

  const query = useMemo(() => {
    const q: Record<string, string> = { sort };
    if (order) q.order = order;
    for (const k of FILTER_KEYS) {
      const v = params.get(k);
      if (v) q[k] = v;
    }
    return q;
  }, [params, sort, order]);

  const [pages, setPages] = useState(1);
  const queryKey = JSON.stringify(query) + type;
  const [lastKey, setLastKey] = useState(queryKey);
  if (lastKey !== queryKey) {
    setLastKey(queryKey);
    setPages(1);
  }

  const list = useResource<Listing>(
    () => (type === 'movie' ? listMovies : listShows)({ ...query, limit: PAGE * pages, offset: 0 }),
    [queryKey, pages],
  );
  // Keep showing the previous page while the next loads.
  const [shown, setShown] = useState<Listing | null>(null);
  if (list.data && list.data !== shown) setShown(list.data);
  const data = list.data ?? shown;

  const set = (key: string, value: string) => {
    const next = new URLSearchParams(params);
    if (value) next.set(key, value);
    else next.delete(key);
    if (key === 'decade' && value) next.delete('year');
    setParams(next, { replace: true });
  };

  const sentinel = useRef<HTMLDivElement>(null);
  const hasMore = data ? data.items.length < data.total : false;
  useEffect(() => {
    const el = sentinel.current;
    if (!el || !hasMore || list.loading) return;
    const io = new IntersectionObserver(
      (entries) => {
        if (entries.some((e) => e.isIntersecting)) setPages((p) => p + 1);
      },
      { rootMargin: '800px' },
    );
    io.observe(el);
    return () => io.disconnect();
  }, [hasMore, list.loading]);

  const active = FILTER_KEYS.filter((k) => params.get(k));
  const title = type === 'movie' ? 'Movies' : 'TV shows';
  const decades = useMemo(() => {
    const ys = facets.data?.years ?? [];
    return [...new Set(ys.map((y) => Math.floor(y / 10) * 10))].sort((a, b) => b - a);
  }, [facets.data]);

  const sel = (key: string, label: string, options: [string, string][]) => (
    <select
      className="select"
      aria-label={label}
      value={params.get(key) ?? ''}
      onChange={(e) => set(key, e.target.value)}
      data-active={Boolean(params.get(key))}
    >
      {options.map(([v, l]) => (
        <option key={v} value={v}>
          {l}
        </option>
      ))}
    </select>
  );

  const libsOfKind = libraries.filter((l) => {
    const kind = (l as { kind?: string }).kind ?? 'mixed';
    return kind === 'mixed' || kind === (type === 'movie' ? 'movies' : 'shows');
  });

  return (
    <div className="page">
      <header className="page-head">
        <div>
          <h1 className="page-title">{title}</h1>
          <p className="page-count">
            {data
              ? `${data.total} ${type === 'movie' ? (data.total === 1 ? 'movie' : 'movies') : data.total === 1 ? 'show' : 'shows'}`
              : ' '}
          </p>
        </div>
      </header>

      <div className="toolbar" role="toolbar" aria-label={`Filter and sort ${title.toLowerCase()}`}>
        {libsOfKind.length > 1 &&
          sel('lib', 'Library', [
            ['', 'All libraries'],
            ...libsOfKind.map((l): [string, string] => [l.id, l.name]),
          ])}
        {type === 'show' && sel('category', 'Category', CATEGORIES)}
        {sel('genre', 'Genre', [
          ['', 'All genres'],
          ...(facets.data?.genres ?? []).map((g): [string, string] => [g, g]),
        ])}
        {sel('decade', 'Decade', [
          ['', 'Any year'],
          ...decades.map((d): [string, string] => [String(d), `${d}s`]),
        ])}
        {sel('min_rating', 'Rating', RATINGS)}
        {sel('language', 'Language', [
          ['', 'Any language'],
          ...(facets.data?.languages ?? []).map((l): [string, string] => [l, languageName(l)]),
        ])}
        {type === 'movie' && sel('resolution', 'Quality', RESOLUTIONS)}
        {sel('watched', 'Watched', WATCHED)}
        {active.length > 0 && (
          <button
            type="button"
            className="chip-button"
            onClick={() => {
              const next = new URLSearchParams();
              if (params.get('sort')) next.set('sort', params.get('sort')!);
              setParams(next, { replace: true });
            }}
          >
            <X size={14} /> Clear filters
          </button>
        )}
        <span className="toolbar-spacer" />
        <select
          className="select"
          aria-label="Sort by"
          value={sort}
          onChange={(e) => set('sort', e.target.value)}
        >
          {SORTS[type].map(([v, l]) => (
            <option key={v} value={v}>
              {l}
            </option>
          ))}
        </select>
        <button
          type="button"
          className="icon-button"
          onClick={() => set('order', isDesc(sort, order) ? 'asc' : 'desc')}
          aria-label={
            isDesc(sort, order)
              ? 'Descending; switch to ascending'
              : 'Ascending; switch to descending'
          }
          title={isDesc(sort, order) ? 'Descending' : 'Ascending'}
        >
          {isDesc(sort, order) ? (
            <ArrowDownWideNarrow size={18} />
          ) : (
            <ArrowUpNarrowWide size={18} />
          )}
        </button>
        <span className="seg" role="group" aria-label="View">
          <button
            type="button"
            aria-pressed={view === 'grid'}
            onClick={() => void updateInterface({ view: 'grid' })}
            aria-label="Grid"
          >
            <LayoutGrid size={16} />
          </button>
          <button
            type="button"
            aria-pressed={view === 'list'}
            onClick={() => void updateInterface({ view: 'list' })}
            aria-label="List"
          >
            <List size={16} />
          </button>
        </span>
      </div>

      {params.get('person') ||
      params.get('director') ||
      params.get('actor') ||
      params.get('studio') ? (
        <p className="muted" style={{ marginTop: 0 }}>
          With{' '}
          {params.get('person') ??
            params.get('director') ??
            params.get('actor') ??
            params.get('studio')}
        </p>
      ) : null}

      {list.error && !data && <ErrorState message={list.error} onRetry={list.reload} />}
      {!data && !list.error && <GridSkeleton />}
      {data && data.items.length === 0 && (
        <div className="empty-media">
          <h2>
            {active.length ? 'Nothing matches these filters' : `No ${title.toLowerCase()} yet`}
          </h2>
          <p>
            {active.length
              ? 'Loosen a filter or clear them all to see more.'
              : `${title} appear here once a library with ${type === 'movie' ? 'movie files' : 'episodes'} is scanned.`}
          </p>
        </div>
      )}
      {data && data.items.length > 0 && <CardGrid items={data.items} view={view} />}
      <div ref={sentinel} className="load-more" aria-hidden={!hasMore}>
        {hasMore && list.loading && <span className="muted">Loading more…</span>}
      </div>
    </div>
  );
}

function isDesc(sort: string, order: string): boolean {
  if (order) return order === 'desc';
  return ['release', 'rating', 'added', 'size', 'played', 'updated'].includes(sort);
}
