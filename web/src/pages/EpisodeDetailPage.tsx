import { useState } from 'react';
import { Link, useNavigate, useParams } from 'react-router-dom';
import { ChevronLeft, ChevronRight, MoveRight, Play, RotateCcw } from 'lucide-react';

import {
  episodeCode,
  errorMessage,
  formatRuntime,
  formatTimecode,
  getEpisode,
  landscapeOf,
  moveEpisode,
  playPath,
  progressOf,
} from '../api/media';
import { useResource } from '../api/resources';
import { SignalBar } from '../components/media/Cards';
import { CastRow, TechInfo } from '../components/media/Detail';
import { ItemActions } from '../components/media/ItemActions';
import { ManageMenu } from '../components/manage/ManageDialogs';
import { Dialog } from '../components/Dialog';
import { ErrorState, LoadingState } from '../components/States';
import { CardMenuArea } from '../components/media/CardMenuArea';
import { useToast } from '../components/ui/Toast';
import '../components/media/detail.css';

export default function EpisodeDetailPage() {
  const { lib = '', id = '' } = useParams();
  const res = useResource(() => getEpisode(lib, id), [lib, id]);
  const [moving, setMoving] = useState(false);

  if (res.loading && !res.data)
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
  const { episode: e, previous_id, next_id, can_edit } = res.data;
  const still = landscapeOf(e);
  const progress = progressOf(e);
  const present = e.files.some((f) => f.status === 'present');

  return (
    <div className="detail">
      <CardMenuArea item={e} onChanged={res.reload} landscape>
        <section className="detail-head">
          <div className="detail-backdrop" aria-hidden="true">
            {still && <img src={still} alt="" />}
          </div>
          <div className="detail-body" style={{ maxWidth: 980 }}>
            <div className="detail-eyebrow">
              <Link to={`/show/${e.library_id}/${e.show_id}?season=${e.season_id}`}>
                {e.show_title}
              </Link>
              <span>
                {' '}
                {e.season_number === 0 ? 'Special' : `Season ${e.season_number}`}, episode{' '}
                {e.episode_number}
              </span>
            </div>
            <h1 className="detail-title">{e.title}</h1>
            <p className="hero-facts">
              <span className="episode-number">{episodeCode(e)}</span>
              {e.air_date && <span>{e.air_date}</span>}
              {formatRuntime(e.runtime_mins, e.duration_secs) && (
                <span>{formatRuntime(e.runtime_mins, e.duration_secs)}</span>
              )}
              {e.rating ? <span className="card-rating">{e.rating.toFixed(1)}</span> : null}
            </p>
            {e.overview && <p className="detail-overview">{e.overview}</p>}
            <div className="detail-actions">
              {present ? (
                <>
                  <Link className="btn-play" to={playPath(e, { queue: `show:${e.show_id}` })}>
                    <Play size={20} fill="currentColor" />{' '}
                    {progress > 0
                      ? `Resume ${formatTimecode(e.state?.position_secs ?? 0)}`
                      : 'Play'}
                  </Link>
                  {progress > 0 && (
                    <Link
                      className="btn-ghost"
                      to={playPath(e, { restart: true, queue: `show:${e.show_id}` })}
                    >
                      <RotateCcw size={17} /> From the start
                    </Link>
                  )}
                </>
              ) : (
                <span className="tech-missing">
                  Not available right now: the disk may be disconnected.
                </span>
              )}
              <ItemActions item={e} onChange={res.reload} />
              {can_edit && (
                <>
                  <ManageMenu
                    onChanged={res.reload}
                    item={{
                      library_id: e.library_id,
                      type: 'episode',
                      id: e.id,
                      title: e.title,
                      art: e.art,
                      locked_fields: e.locked_fields,
                      values: e as unknown as Record<string, unknown>,
                      files: e.files,
                    }}
                  />
                  <button
                    type="button"
                    className="btn-round"
                    onClick={() => setMoving(true)}
                    aria-label="Change season or episode number"
                    title="Change season or episode number"
                  >
                    <MoveRight size={19} />
                  </button>
                </>
              )}
            </div>
            {progress > 0 && (
              <div style={{ maxWidth: 360, marginTop: 'var(--space-4)' }}>
                <SignalBar value={progress} />
              </div>
            )}
            <nav
              className="detail-actions"
              style={{ marginTop: 'var(--space-6)' }}
              aria-label="Episodes"
            >
              {previous_id && (
                <Link className="btn-ghost" to={`/episode/${e.library_id}/${previous_id}`}>
                  <ChevronLeft size={17} /> Previous episode
                </Link>
              )}
              {next_id && (
                <Link className="btn-ghost" to={`/episode/${e.library_id}/${next_id}`}>
                  Next episode <ChevronRight size={17} />
                </Link>
              )}
            </nav>
          </div>
        </section>
      </CardMenuArea>
      <div className="detail-body">
        <CastRow credits={e.credits ?? []} libraryId={e.library_id} />
        <TechInfo files={e.files} />
      </div>
      {moving && (
        <MoveDialog
          lib={lib}
          id={e.id}
          season={e.season_number}
          number={e.episode_number ?? 1}
          onClose={() => setMoving(false)}
          onSaved={res.reload}
        />
      )}
    </div>
  );
}

function MoveDialog({
  lib,
  id,
  season,
  number,
  onClose,
  onSaved,
}: {
  lib: string;
  id: string;
  season: number;
  number: number;
  onClose: () => void;
  onSaved: () => void;
}) {
  const toast = useToast();
  const navigate = useNavigate();
  const [s, setS] = useState(String(season));
  const [n, setN] = useState(String(number));
  const save = async () => {
    try {
      await moveEpisode(lib, id, Number(s), Number(n));
      toast({ message: 'Episode moved. It stays here even after a rescan.', tone: 'success' });
      onSaved();
      onClose();
      navigate(0);
    } catch (err) {
      toast({ message: errorMessage(err), tone: 'error' });
    }
  };
  return (
    <Dialog
      open
      size="small"
      title="Season and episode number"
      onClose={onClose}
      footer={
        <>
          <button type="button" className="button" onClick={onClose}>
            Cancel
          </button>
          <button type="button" className="primary-button" onClick={() => void save()}>
            Move episode
          </button>
        </>
      }
    >
      <div className="edit-grid">
        <label className="field">
          <span className="field-label">Season (0 for specials)</span>
          <input type="number" min={0} value={s} onChange={(e) => setS(e.target.value)} />
        </label>
        <label className="field">
          <span className="field-label">Episode</span>
          <input type="number" min={1} value={n} onChange={(e) => setN(e.target.value)} />
        </label>
      </div>
    </Dialog>
  );
}
