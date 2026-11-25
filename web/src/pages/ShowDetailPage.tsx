import { useState } from 'react';
import { Link, useParams, useSearchParams } from 'react-router-dom';
import { ArrowDown, ArrowUp, Check, ListOrdered, Play } from 'lucide-react';

import {
  episodeCode,
  errorMessage,
  formatRuntime,
  formatTimecode,
  getSeasonEpisodes,
  getShow,
  landscapeOf,
  playPath,
  progressOf,
  reorderSeasons,
  setPlayed,
} from '../api/media';
import type { Card, Season } from '../api/media';
import { useResource } from '../api/resources';
import { PosterCard, Row, SignalBar } from '../components/media/Cards';
import { CastRow, Crew, DetailHeader } from '../components/media/Detail';
import { ItemActions } from '../components/media/ItemActions';
import { ManageMenu } from '../components/manage/ManageDialogs';
import { Dialog } from '../components/Dialog';
import { ErrorState, LoadingState } from '../components/States';
import { useCardMenu } from '../components/media/CardMenu';
import { useToast } from '../components/ui/Toast';

function seasonLabel(s: Season) {
  if (s.season_number === 0) return 'Specials';
  return s.title && !/^season \d+$/i.test(s.title) ? s.title : `Season ${s.season_number}`;
}

export default function ShowDetailPage() {
  const { lib = '', id = '' } = useParams();
  const [params, setParams] = useSearchParams();
  const toast = useToast();
  const res = useResource(() => getShow(lib, id), [lib, id]);
  const [ordering, setOrdering] = useState(false);

  const show = res.data?.show;
  const seasons = show?.seasons ?? [];
  const defaultSeason = (() => {
    const next = res.data?.next_up;
    if (next?.season_id) return next.season_id;
    return (seasons.find((s) => s.season_number > 0) ?? seasons[0])?.id ?? '';
  })();
  const seasonId = params.get('season') ?? defaultSeason;
  const episodes = useResource(() => getSeasonEpisodes(lib, seasonId), [lib, seasonId, res.data], Boolean(seasonId));

  if (res.loading && !res.data) return <div className="page"><LoadingState /></div>;
  if (res.error || !show) return <div className="page"><ErrorState message={res.error ?? 'Not found.'} onRetry={res.reload} /></div>;
  const next = res.data!.next_up;
  const canEdit = res.data!.can_edit;
  const season = seasons.find((s) => s.id === seasonId);
  const credits = show.credits ?? [];

  const reloadAll = () => {
    res.reload();
    episodes.reload();
  };

  const toggleEpisode = async (e: Card) => {
    try {
      await setPlayed([{ library_id: lib, type: 'episode', id: e.id }], !e.state?.played);
      reloadAll();
    } catch (err) {
      toast({ message: errorMessage(err), tone: 'error' });
    }
  };

  return (
    <div className="detail">
      <DetailHeader
        item={show}
        onChanged={reloadAll}
        eyebrow={
          <>
            {show.status && <span>{show.status}</span>}
            {show.category && show.category !== 'tv' && <span> {show.category === 'anime' ? 'Anime' : 'Web series'}</span>}
          </>
        }
        actions={
          <>
            {next ? (
              <Link className="btn-play" to={playPath(next, { queue: `show:${show.id}` })}>
                <Play size={20} fill="currentColor" />
                {progressOf(next) > 0 ? 'Resume' : next.season_number === 1 && next.episode_number === 1 && !show.state?.last_played_at ? 'Play' : 'Continue'}{' '}
                {episodeCode(next)}
              </Link>
            ) : (
              <span className="played-chip">
                <Check size={14} strokeWidth={3} /> You've watched every episode
              </span>
            )}
            <ItemActions item={show} onChange={reloadAll} />
            {canEdit && (
              <ManageMenu
                onChanged={reloadAll}
                item={{
                  library_id: show.library_id,
                  type: 'show',
                  id: show.id,
                  title: show.title,
                  year: show.year,
                  art: show.art,
                  locked_fields: show.locked_fields,
                  values: show as unknown as Record<string, unknown>,
                }}
              />
            )}
          </>
        }
      >
        {next && (
          <p className="next-up">
            Next up: <strong>{episodeCode(next)} {next.title}</strong>
            {next.state && next.state.position_secs > 0 && <span className="muted">{formatTimecode(next.state.position_secs)} in</span>}
          </p>
        )}
        {show.genres && show.genres.length > 0 && (
          <div className="genre-chips" style={{ marginTop: 'var(--space-5)' }}>
            {show.genres.map((g) => (
              <Link key={g} className="genre-chip" to={`/shows?genre=${encodeURIComponent(g)}`}>
                {g}
              </Link>
            ))}
          </div>
        )}
        <Crew credits={credits} libraryId={show.library_id} />
      </DetailHeader>

      <div className="detail-body">
        <section className="detail-section">
          <div className="season-tabs" role="tablist" aria-label="Seasons">
            {seasons.map((s) => (
              <button
                key={s.id}
                type="button"
                role="tab"
                className="season-tab"
                aria-selected={s.id === seasonId}
                onClick={() => setParams({ season: s.id }, { replace: true })}
              >
                {seasonLabel(s)}
                {s.state?.played && <Check size={14} strokeWidth={3} className="season-tab-done" aria-label="watched" />}
              </button>
            ))}
          </div>
          {season && (
            <div className="season-summary">
              <p>
                {season.episode_count} episode{season.episode_count === 1 ? '' : 's'}
                {season.episode_total && season.episode_total > season.episode_count ? ` of ${season.episode_total}` : ''}
                {season.air_date ? `, ${season.air_date.slice(0, 4)}` : ''}
                {season.overview ? <><br />{season.overview}</> : null}
              </p>
              <div className="detail-actions">
                <ItemActions key={season.id + String(season.state?.played)} item={{ ...show, type: 'season', id: season.id, title: seasonLabel(season), state: season.state }} onChange={reloadAll} />
                {canEdit && seasons.length > 1 && (
                  <button type="button" className="btn-round" onClick={() => setOrdering(true)} aria-label="Change season order" title="Change season order">
                    <ListOrdered size={19} />
                  </button>
                )}
              </div>
            </div>
          )}
          {episodes.error && <ErrorState message={episodes.error} onRetry={episodes.reload} />}
          {!episodes.data && !episodes.error && <LoadingState />}
          {episodes.data && (
            <ol className="episode-list">
              {episodes.data.episodes.map((e) => (
                <EpisodeRow key={e.id} e={e} showId={show.id} onToggle={() => void toggleEpisode(e)} onChanged={reloadAll} />
              ))}
            </ol>
          )}
        </section>
        <CastRow credits={credits} libraryId={show.library_id} />
      </div>
      {res.data!.related.length > 0 && (
        <Row title="More like this">
          {res.data!.related.map((c) => (
            <PosterCard key={c.library_id + c.id} c={c} />
          ))}
        </Row>
      )}
      {ordering && <SeasonOrderDialog lib={lib} showId={show.id} seasons={seasons} onClose={() => setOrdering(false)} onSaved={reloadAll} />}
    </div>
  );
}

function EpisodeRow({ e: item, showId, onToggle, onChanged }: { e: Card; showId: string; onToggle: () => void; onChanged: () => void }) {
  const { card: e, onContextMenu, element } = useCardMenu(item, { landscape: true, onChanged });
  const still = landscapeOf(e);
  const meta = [e.air_date, formatRuntime(e.runtime_mins, e.duration_secs), e.resolution].filter(Boolean).join('  /  ');
  return (
    <li className="episode-row" onContextMenu={onContextMenu}>
      {element}
      <Link to={e.available ? playPath(e, { queue: `show:${showId}` }) : `/episode/${e.library_id}/${e.id}`} className="episode-still" aria-label={`Play ${episodeCode(e)} ${e.title}`}>
        {still ? <img src={still} alt="" loading="lazy" /> : <span className="card-fallback" />}
        <span className="landscape-play" aria-hidden="true">
          <Play size={22} fill="currentColor" />
        </span>
        <SignalBar value={progressOf(e)} className="landscape-signal" />
      </Link>
      <div>
        <div className="episode-head">
          <span className="episode-number">{e.episode_number ?? ''}{e.episode_end && e.episode_end > (e.episode_number ?? 0) ? `–${e.episode_end}` : ''}</span>
          <h3 className="episode-title">
            <Link to={`/episode/${e.library_id}/${e.id}`}>{e.title}</Link>
          </h3>
        </div>
        <p className="episode-meta">
          {meta}
          {!e.available && <span className="tech-missing"> Not available</span>}
        </p>
        {e.overview && <p className="episode-overview">{e.overview}</p>}
      </div>
      <div className="episode-side">
        <button type="button" className="icon-button" aria-pressed={Boolean(e.state?.played)} onClick={onToggle} aria-label={e.state?.played ? `Mark ${episodeCode(e)} unwatched` : `Mark ${episodeCode(e)} watched`} title={e.state?.played ? 'Watched' : 'Mark watched'}>
          <Check size={18} strokeWidth={e.state?.played ? 3 : 2} />
        </button>
      </div>
    </li>
  );
}

function SeasonOrderDialog({ lib, showId, seasons, onClose, onSaved }: { lib: string; showId: string; seasons: Season[]; onClose: () => void; onSaved: () => void }) {
  const toast = useToast();
  const [order, setOrder] = useState(seasons);
  const move = (i: number, d: number) => {
    const next = [...order];
    const [s] = next.splice(i, 1);
    next.splice(i + d, 0, s!);
    setOrder(next);
  };
  const save = async () => {
    try {
      await reorderSeasons(lib, showId, order.map((s) => s.id));
      onSaved();
      onClose();
    } catch (err) {
      toast({ message: errorMessage(err), tone: 'error' });
    }
  };
  return (
    <Dialog
      open
      size="small"
      title="Season order"
      onClose={onClose}
      footer={
        <>
          <button type="button" className="button" onClick={onClose}>
            Cancel
          </button>
          <button type="button" className="primary-button" onClick={() => void save()}>
            Save order
          </button>
        </>
      }
    >
      <ol className="reorder-list">
        {order.map((s, i) => (
          <li key={s.id}>
            <span>{seasonLabel(s)}</span>
            <button type="button" className="icon-button" disabled={i === 0} onClick={() => move(i, -1)} aria-label={`Move ${seasonLabel(s)} up`}>
              <ArrowUp size={16} />
            </button>
            <button type="button" className="icon-button" disabled={i === order.length - 1} onClick={() => move(i, 1)} aria-label={`Move ${seasonLabel(s)} down`}>
              <ArrowDown size={16} />
            </button>
          </li>
        ))}
      </ol>
    </Dialog>
  );
}
