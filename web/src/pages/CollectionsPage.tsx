import { useState } from 'react';
import type { ReactNode } from 'react';
import { Link, useNavigate, useParams } from 'react-router-dom';
import { ArrowDown, ArrowUp, Copy, GripVertical, ImagePlus, Pencil, Play, Plus, Repeat, Repeat1, Trash2, X } from 'lucide-react';

import {
  collectionPosterUrl,
  collectionQueue,
  createCollection,
  createPlaylist,
  deleteCollection,
  deletePlaylist,
  duplicateCollection,
  episodeCode,
  errorMessage,
  formatTimecode,
  getCollection,
  getPlaylist,
  itemPath,
  landscapeOf,
  listCollections,
  listPlaylists,
  playlistPosterUrl,
  posterOf,
  removeFromCollection,
  removeFromPlaylist,
  removeUserPoster,
  reorderCollection,
  reorderPlaylist,
  updateCollection,
  updatePlaylist,
  uploadUserPoster,
} from '../api/media';
import type { Card, PlaylistEntry } from '../api/media';
import { useResource } from '../api/resources';
import { CardMenuArea } from '../components/media/CardMenuArea';
import { CardGrid, SignalBar } from '../components/media/Cards';
import { CollectionTile } from '../components/media/CollectionTile';
import { ConfirmDialog, Dialog } from '../components/Dialog';
import { ErrorState, LoadingState } from '../components/States';
import { useToast } from '../components/ui/Toast';
import './CollectionsPage.css';

/* ---------- shared ---------- */

function NameDialog({
  title,
  initial = { name: '', description: '' },
  action,
  onSubmit,
  onClose,
}: {
  title: string;
  initial?: { name: string; description: string };
  action: string;
  onSubmit: (name: string, description: string) => Promise<void>;
  onClose: () => void;
}) {
  const [name, setName] = useState(initial.name);
  const [description, setDescription] = useState(initial.description);
  const [error, setError] = useState('');
  const submit = async () => {
    try {
      await onSubmit(name.trim(), description.trim());
      onClose();
    } catch (err) {
      setError(errorMessage(err));
    }
  };
  return (
    <Dialog
      open
      size="small"
      title={title}
      onClose={onClose}
      footer={
        <>
          <button type="button" className="button" onClick={onClose}>
            Cancel
          </button>
          <button type="button" className="primary-button" disabled={!name.trim()} onClick={() => void submit()}>
            {action}
          </button>
        </>
      }
    >
      <form
        className="name-form"
        onSubmit={(e) => {
          e.preventDefault();
          void submit();
        }}
      >
        <label className="field">
          <span className="field-label">Name</span>
          <input autoFocus value={name} onChange={(e) => setName(e.target.value)} />
        </label>
        <label className="field">
          <span className="field-label">Description</span>
          <textarea rows={3} value={description} onChange={(e) => setDescription(e.target.value)} />
        </label>
        {error && <p className="error-text">{error}</p>}
      </form>
    </Dialog>
  );
}

/** Drag rows to reorder; the arrow buttons do the same from the keyboard. */
function ReorderList<T>({ items, keyOf, render, onReorder }: { items: T[]; keyOf: (t: T) => string; render: (t: T, i: number) => ReactNode; onReorder: (next: T[]) => void }) {
  const [drag, setDrag] = useState<number | null>(null);
  const [over, setOver] = useState<number | null>(null);
  const move = (from: number, to: number) => {
    if (to < 0 || to >= items.length || from === to) return;
    const next = [...items];
    const [x] = next.splice(from, 1);
    next.splice(to, 0, x!);
    onReorder(next);
  };
  return (
    <ol className="reorder">
      {items.map((it, i) => (
        <li
          key={keyOf(it)}
          className="reorder-item"
          draggable
          data-over={over === i && drag !== null && drag !== i}
          onDragStart={(e) => {
            setDrag(i);
            e.dataTransfer.effectAllowed = 'move';
          }}
          onDragOver={(e) => {
            e.preventDefault();
            setOver(i);
          }}
          onDragEnd={() => {
            setDrag(null);
            setOver(null);
          }}
          onDrop={(e) => {
            e.preventDefault();
            if (drag !== null) move(drag, i);
            setDrag(null);
            setOver(null);
          }}
        >
          <span className="reorder-grip" aria-hidden="true">
            <GripVertical size={16} />
          </span>
          {render(it, i)}
          <span className="reorder-arrows">
            <button type="button" className="icon-button" disabled={i === 0} onClick={() => move(i, i - 1)} aria-label="Move up">
              <ArrowUp size={15} />
            </button>
            <button type="button" className="icon-button" disabled={i === items.length - 1} onClick={() => move(i, i + 1)} aria-label="Move down">
              <ArrowDown size={15} />
            </button>
          </span>
        </li>
      ))}
    </ol>
  );
}

function ItemLine({ c, extra }: { c: Card; extra?: ReactNode }) {
  const img = c.type === 'episode' ? landscapeOf(c) : posterOf(c);
  return (
    <CardMenuArea item={c}>
    <span className="item-line">
      <span className={`item-line-thumb ${c.type === 'episode' ? 'is-wide' : ''}`}>{img && <img src={img} alt="" loading="lazy" />}</span>
      <span className="item-line-text">
        <Link to={itemPath(c)}>{c.type === 'episode' ? c.show_title : c.title}</Link>
        <span className="muted">{c.type === 'episode' ? `${episodeCode(c)} ${c.title}` : [c.type === 'show' ? 'Show' : 'Movie', c.year].filter(Boolean).join(', ')}</span>
        {extra}
      </span>
    </span>
    </CardMenuArea>
  );
}

function PosterControl({ kind, id, has, onChanged }: { kind: 'collections' | 'playlists'; id: string; has: boolean; onChanged: () => void }) {
  const toast = useToast();
  return (
    <>
      <label className="btn-round" title="Upload artwork" aria-label="Upload artwork">
        <ImagePlus size={19} />
        <input
          type="file"
          accept="image/jpeg,image/png,image/webp"
          className="visually-hidden"
          onChange={async (e) => {
            const f = e.target.files?.[0];
            if (!f) return;
            try {
              await uploadUserPoster(kind, id, f);
              onChanged();
            } catch (err) {
              toast({ message: errorMessage(err), tone: 'error' });
            }
          }}
        />
      </label>
      {has && (
        <button
          type="button"
          className="btn-round"
          title="Remove artwork"
          aria-label="Remove artwork"
          onClick={async () => {
            await removeUserPoster(kind, id).catch(() => undefined);
            onChanged();
          }}
        >
          <X size={19} />
        </button>
      )}
    </>
  );
}

/* ---------- collections ---------- */

export function CollectionsPage() {
  const res = useResource(listCollections, []);
  const navigate = useNavigate();
  const [creating, setCreating] = useState(false);
  return (
    <div className="page">
      <header className="page-head">
        <div>
          <h1 className="page-title">Collections</h1>
          <p className="page-count">Group movies and shows your way, in the order you choose.</p>
        </div>
        <button type="button" className="primary-button" onClick={() => setCreating(true)}>
          <Plus size={17} /> New collection
        </button>
      </header>
      {res.loading && <LoadingState />}
      {res.error && <ErrorState message={res.error} onRetry={res.reload} />}
      {res.data && (
        <>
          {res.data.collections.length > 0 ? (
            <div className="collection-grid">
              {res.data.collections.map((c) => (
                <CollectionTile key={c.id} collection={c} onChanged={res.reload} />
              ))}
            </div>
          ) : (
            <div className="empty-media">
              <h2>No collections yet</h2>
              <p>Make one for a franchise, a mood or a movie night, then add titles from their pages.</p>
            </div>
          )}
          <h2 className="section-title" style={{ marginTop: 'var(--space-7)' }}>
            Automatic
          </h2>
          <div className="collection-grid">
            {res.data.smart.map((s) => (
              <CollectionTile key={s.id} smart={s} onChanged={res.reload} />
            ))}
          </div>
        </>
      )}
      {creating && (
        <NameDialog
          title="New collection"
          action="Create collection"
          onClose={() => setCreating(false)}
          onSubmit={async (n, d) => {
            const c = await createCollection(n, d);
            navigate(`/collections/${c.id}`);
          }}
        />
      )}
    </div>
  );
}

export function CollectionDetailPage() {
  const { id = '' } = useParams();
  const navigate = useNavigate();
  const toast = useToast();
  const res = useResource(() => getCollection(id), [id]);
  const [editing, setEditing] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const [arranging, setArranging] = useState(false);

  if (res.loading && !res.data) return <div className="page"><LoadingState /></div>;
  if (res.error || !res.data) return <div className="page"><ErrorState message={res.error ?? 'Not found.'} onRetry={res.reload} /></div>;
  const { collection: col, items } = res.data;
  const smart = Boolean(col.smart);
  const poster = smart ? null : collectionPosterUrl(col);

  const play = async () => {
    try {
      const q = await collectionQueue(id);
      if (!q.queue.length) {
        toast({ message: 'Nothing in this collection can be played.' });
        return;
      }
      const first = q.queue[0]!;
      navigate(`/play/${first.library_id}/${first.type}/${first.id}?queue=collection:${id}`);
    } catch (err) {
      toast({ message: errorMessage(err), tone: 'error' });
    }
  };

  const reorder = async (next: Card[]) => {
    try {
      await reorderCollection(id, next.map((c) => ({ library_id: c.library_id, type: c.type, id: c.id })));
      res.reload();
    } catch (err) {
      toast({ message: errorMessage(err), tone: 'error' });
    }
  };

  return (
    <div className="page">
      <header className="list-hero">
        <div className="list-hero-art">
          {poster ? <img src={poster} alt="" /> : <CollectionTile collection={{ ...col, covers: items.slice(0, 4) }} covers={items.slice(0, 4)} onChanged={() => navigate('/collections')} />}
        </div>
        <div>
          <p className="detail-eyebrow">{smart ? 'Automatic collection' : 'Collection'}</p>
          <h1 className="page-title">{col.name}</h1>
          {col.description && <p className="list-description">{col.description}</p>}
          <p className="page-count">
            {items.length} item{items.length === 1 ? '' : 's'}
          </p>
          <div className="detail-actions">
            <button type="button" className="btn-play" onClick={() => void play()} disabled={!items.length}>
              <Play size={20} fill="currentColor" /> Play all
            </button>
            {!smart && (
              <>
                <button type="button" className="btn-round" onClick={() => setArranging(!arranging)} aria-pressed={arranging} title="Arrange" aria-label="Arrange">
                  <GripVertical size={19} />
                </button>
                <button type="button" className="btn-round" onClick={() => setEditing(true)} title="Rename" aria-label="Rename">
                  <Pencil size={18} />
                </button>
                <PosterControl kind="collections" id={col.id} has={Boolean(poster)} onChanged={res.reload} />
                <button
                  type="button"
                  className="btn-round"
                  title="Duplicate"
                  aria-label="Duplicate"
                  onClick={async () => {
                    const c = await duplicateCollection(id).catch((err: unknown) => {
                      toast({ message: errorMessage(err), tone: 'error' });
                      return null;
                    });
                    if (c) navigate(`/collections/${c.id}`);
                  }}
                >
                  <Copy size={18} />
                </button>
                <button type="button" className="btn-round" onClick={() => setDeleting(true)} title="Delete" aria-label="Delete collection">
                  <Trash2 size={18} />
                </button>
              </>
            )}
          </div>
        </div>
      </header>

      {items.length === 0 && (
        <div className="empty-media">
          <h2>This collection is empty</h2>
          <p>Open any movie or show and use the collection button to add it here.</p>
        </div>
      )}
      {arranging && !smart ? (
        <ReorderList
          items={items}
          keyOf={(c) => c.library_id + c.type + c.id}
          onReorder={(n) => void reorder(n)}
          render={(c) => (
            <>
              <ItemLine c={c} />
              <button
                type="button"
                className="icon-button"
                aria-label={`Remove ${c.title}`}
                onClick={async () => {
                  await removeFromCollection(id, [{ library_id: c.library_id, type: c.type, id: c.id }]).catch(() => undefined);
                  res.reload();
                }}
              >
                <X size={16} />
              </button>
            </>
          )}
        />
      ) : (
        <CardGrid items={items} />
      )}

      {editing && (
        <NameDialog
          title="Rename collection"
          action="Save"
          initial={{ name: col.name, description: col.description }}
          onClose={() => setEditing(false)}
          onSubmit={async (n, d) => {
            await updateCollection(id, { name: n, description: d });
            res.reload();
          }}
        />
      )}
      <ConfirmDialog
        open={deleting}
        title={`Delete ${col.name}?`}
        message="The collection goes away; its movies and shows stay in your libraries."
        confirmLabel="Delete collection"
        destructive
        onCancel={() => setDeleting(false)}
        onConfirm={async () => {
          await deleteCollection(id);
          navigate('/collections');
        }}
      />
    </div>
  );
}

/* ---------- playlists ---------- */

export function PlaylistsPage() {
  const res = useResource(listPlaylists, []);
  const navigate = useNavigate();
  const [creating, setCreating] = useState(false);
  return (
    <div className="page">
      <header className="page-head">
        <div>
          <h1 className="page-title">Playlists</h1>
          <p className="page-count">Queues of movies and episodes that play one after another.</p>
        </div>
        <button type="button" className="primary-button" onClick={() => setCreating(true)}>
          <Plus size={17} /> New playlist
        </button>
      </header>
      {res.loading && <LoadingState />}
      {res.error && <ErrorState message={res.error} onRetry={res.reload} />}
      {res.data?.playlists.length === 0 && (
        <div className="empty-media">
          <h2>No playlists yet</h2>
          <p>Add movies, episodes, whole seasons or a collection from their pages, then play them in order.</p>
        </div>
      )}
      {res.data && res.data.playlists.length > 0 && (
        <div className="collection-grid">
          {res.data.playlists.map((p) => (
            <CollectionTile key={p.id} playlist={p} onChanged={res.reload} />
          ))}
        </div>
      )}
      {creating && (
        <NameDialog
          title="New playlist"
          action="Create playlist"
          onClose={() => setCreating(false)}
          onSubmit={async (n, d) => {
            const p = await createPlaylist(n, d);
            navigate(`/playlists/${p.id}`);
          }}
        />
      )}
    </div>
  );
}

export function PlaylistDetailPage() {
  const { id = '' } = useParams();
  const navigate = useNavigate();
  const toast = useToast();
  const res = useResource(() => getPlaylist(id), [id]);
  const [editing, setEditing] = useState(false);
  const [deleting, setDeleting] = useState(false);

  if (res.loading && !res.data) return <div className="page"><LoadingState /></div>;
  if (res.error || !res.data) return <div className="page"><ErrorState message={res.error ?? 'Not found.'} onRetry={res.reload} /></div>;
  const { playlist: pl, entries } = res.data;
  const poster = playlistPosterUrl(pl);
  const playable = entries.filter((e) => e.item?.available);

  const playEntry = (e: PlaylistEntry) => {
    void updatePlaylist(id, { current_entry: e.entry_id }).catch(() => undefined);
    navigate(`/play/${e.library_id}/${e.type}/${e.id}?queue=playlist:${id}&entry=${e.entry_id}`);
  };
  const resumeEntry = entries.find((e) => e.entry_id === pl.current_entry && e.item?.available) ?? playable[0];

  const patch = async (p: Parameters<typeof updatePlaylist>[1]) => {
    try {
      await updatePlaylist(id, p);
      res.reload();
    } catch (err) {
      toast({ message: errorMessage(err), tone: 'error' });
    }
  };

  const nextRepeat = pl.repeat === 'off' ? 'all' : pl.repeat === 'all' ? 'one' : 'off';
  const RepeatIcon = pl.repeat === 'one' ? Repeat1 : Repeat;

  return (
    <div className="page">
      <header className="list-hero">
        <div className="list-hero-art">{poster ? <img src={poster} alt="" /> : <CollectionTile playlist={{ ...pl, covers: entries.map((e) => e.item).filter((c): c is Card => Boolean(c)).slice(0, 4) }} onChanged={() => navigate('/playlists')} />}</div>
        <div>
          <p className="detail-eyebrow">Playlist</p>
          <h1 className="page-title">{pl.name}</h1>
          {pl.description && <p className="list-description">{pl.description}</p>}
          <p className="page-count">
            {entries.length} item{entries.length === 1 ? '' : 's'}
          </p>
          <div className="detail-actions">
            <button type="button" className="btn-play" disabled={!resumeEntry} onClick={() => resumeEntry && playEntry(resumeEntry)}>
              <Play size={20} fill="currentColor" /> {pl.current_entry && resumeEntry?.entry_id === pl.current_entry ? 'Continue' : 'Play'}
            </button>
            <button type="button" className="btn-round" aria-pressed={pl.repeat !== 'off'} onClick={() => void patch({ repeat: nextRepeat })} title={`Repeat: ${pl.repeat}`} aria-label={`Repeat ${pl.repeat}; switch to ${nextRepeat}`}>
              <RepeatIcon size={18} />
            </button>
            <button type="button" className="btn-round" onClick={() => setEditing(true)} title="Rename" aria-label="Rename">
              <Pencil size={18} />
            </button>
            <PosterControl kind="playlists" id={pl.id} has={Boolean(poster)} onChanged={res.reload} />
            <button type="button" className="btn-round" onClick={() => setDeleting(true)} title="Delete" aria-label="Delete playlist">
              <Trash2 size={18} />
            </button>
          </div>
        </div>
      </header>

      {entries.length === 0 ? (
        <div className="empty-media">
          <h2>This playlist is empty</h2>
          <p>Use the add-to-playlist button on a movie, episode, season or show to queue it here.</p>
        </div>
      ) : (
        <ReorderList
          items={entries}
          keyOf={(e) => e.entry_id}
          onReorder={async (next) => {
            try {
              await reorderPlaylist(id, next.map((e) => e.entry_id));
              res.reload();
            } catch (err) {
              toast({ message: errorMessage(err), tone: 'error' });
            }
          }}
          render={(e) => (
            <>
              {e.item ? (
                <ItemLine
                  c={e.item}
                  extra={
                    e.position_secs > 0 ? (
                      <span className="entry-resume">
                        <SignalBar value={e.item.duration_secs ? e.position_secs / e.item.duration_secs : 0.1} /> {formatTimecode(e.position_secs)}
                      </span>
                    ) : null
                  }
                />
              ) : (
                <span className="item-line muted">This title is no longer available.</span>
              )}
              {e.entry_id === pl.current_entry && <span className="now-chip">Current</span>}
              {e.item?.available && (
                <button type="button" className="icon-button" onClick={() => playEntry(e)} aria-label="Play from here">
                  <Play size={16} />
                </button>
              )}
              <button
                type="button"
                className="icon-button"
                aria-label="Remove from playlist"
                onClick={async () => {
                  await removeFromPlaylist(id, [e.entry_id]).catch(() => undefined);
                  res.reload();
                }}
              >
                <X size={16} />
              </button>
            </>
          )}
        />
      )}

      {editing && (
        <NameDialog
          title="Rename playlist"
          action="Save"
          initial={{ name: pl.name, description: pl.description }}
          onClose={() => setEditing(false)}
          onSubmit={async (n, d) => {
            await updatePlaylist(id, { name: n, description: d });
            res.reload();
          }}
        />
      )}
      <ConfirmDialog
        open={deleting}
        title={`Delete ${pl.name}?`}
        message="The playlist goes away; its titles stay in your libraries."
        confirmLabel="Delete playlist"
        destructive
        onCancel={() => setDeleting(false)}
        onConfirm={async () => {
          await deletePlaylist(id);
          navigate('/playlists');
        }}
      />
    </div>
  );
}
