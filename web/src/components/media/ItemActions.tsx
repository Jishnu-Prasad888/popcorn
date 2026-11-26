import { useState } from 'react';
import { Bookmark, Check, Heart, Layers, ListPlus, Plus } from 'lucide-react';

import {
  addToCollection,
  addToPlaylist,
  createCollection,
  createPlaylist,
  errorMessage,
  listCollections,
  listPlaylists,
  setInList,
  setPlayed,
} from '../../api/media';
import type { Card, Ref, UserState } from '../../api/media';
import { useResource } from '../../api/resources';
import { Dialog } from '../Dialog';
import { useToast } from '../ui/Toast';

const refOf = (c: Card): Ref => ({ library_id: c.library_id, type: c.type, id: c.id });

/** Watched, favourite and watchlist toggles, plus "add to". Optimistic. */
export function ItemActions({ item, onChange }: { item: Card; onChange?: () => void }) {
  const toast = useToast();
  const [state, setState] = useState<UserState | undefined>(item.state);
  const [addTo, setAddTo] = useState<'collection' | 'playlist' | null>(null);
  const ref = refOf(item);

  const toggle = async (key: 'played' | 'favorite' | 'watchlist') => {
    const prev = state;
    const on = !state?.[key];
    setState({ ...(state ?? emptyState()), [key]: on });
    try {
      if (key === 'played') await setPlayed([ref], on);
      else await setInList(key, ref, on);
      onChange?.();
    } catch (err) {
      setState(prev);
      toast({ message: errorMessage(err), tone: 'error' });
    }
  };

  const watchedLabel =
    item.type === 'show' || item.type === 'season' ? 'Mark all watched' : 'Mark watched';
  return (
    <>
      <button
        type="button"
        className="btn-round is-played"
        aria-pressed={Boolean(state?.played)}
        onClick={() => void toggle('played')}
        title={state?.played ? 'Mark unwatched' : watchedLabel}
        aria-label={state?.played ? 'Mark unwatched' : watchedLabel}
      >
        <Check size={20} strokeWidth={2.5} />
      </button>
      {item.type !== 'season' && (
        <>
          <button
            type="button"
            className="btn-round"
            aria-pressed={Boolean(state?.favorite)}
            onClick={() => void toggle('favorite')}
            title={state?.favorite ? 'Remove from favourites' : 'Add to favourites'}
            aria-label={state?.favorite ? 'Remove from favourites' : 'Add to favourites'}
          >
            <Heart size={19} fill={state?.favorite ? 'currentColor' : 'none'} />
          </button>
          <button
            type="button"
            className="btn-round"
            aria-pressed={Boolean(state?.watchlist)}
            onClick={() => void toggle('watchlist')}
            title={state?.watchlist ? 'Remove from watchlist' : 'Add to watchlist'}
            aria-label={state?.watchlist ? 'Remove from watchlist' : 'Add to watchlist'}
          >
            <Bookmark size={19} fill={state?.watchlist ? 'currentColor' : 'none'} />
          </button>
        </>
      )}
      {item.type !== 'season' && (
        <button
          type="button"
          className="btn-round"
          onClick={() => setAddTo('collection')}
          title="Add to collection"
          aria-label="Add to collection"
        >
          <Layers size={19} />
        </button>
      )}
      <button
        type="button"
        className="btn-round"
        onClick={() => setAddTo('playlist')}
        title="Add to playlist"
        aria-label="Add to playlist"
      >
        <ListPlus size={20} />
      </button>
      {addTo && (
        <AddToDialog kind={addTo} items={[ref]} title={item.title} onClose={() => setAddTo(null)} />
      )}
    </>
  );
}

function emptyState(): UserState {
  return {
    position_secs: 0,
    duration_secs: 0,
    played: false,
    play_count: 0,
    favorite: false,
    watchlist: false,
    unplayed_count: 0,
  };
}

/** Pick (or create) a collection or playlist to add items to. */
export function AddToDialog({
  kind,
  items,
  title,
  onClose,
}: {
  kind: 'collection' | 'playlist';
  items: Ref[];
  title: string;
  onClose: () => void;
}) {
  const toast = useToast();
  const lists = useResource(async () => {
    if (kind === 'collection')
      return (await listCollections()).collections.map((c) => ({
        id: c.id,
        name: c.name,
        count: c.item_count,
      }));
    return (await listPlaylists()).playlists.map((p) => ({
      id: p.id,
      name: p.name,
      count: p.item_count,
    }));
  }, [kind]);
  const [name, setName] = useState('');
  const [busy, setBusy] = useState(false);
  const noun = kind === 'collection' ? 'collection' : 'playlist';

  const add = async (id: string, listName: string) => {
    setBusy(true);
    try {
      if (kind === 'collection') await addToCollection(id, items);
      else await addToPlaylist(id, items);
      toast(`Added ${title} to ${listName}.`);
      onClose();
    } catch (err) {
      toast({ message: errorMessage(err), tone: 'error' });
    } finally {
      setBusy(false);
    }
  };

  const create = async () => {
    const n = name.trim();
    if (!n) return;
    setBusy(true);
    try {
      if (kind === 'collection') await createCollection(n, '', items);
      else await createPlaylist(n, '', items);
      toast(`Created ${n} with ${title}.`);
      onClose();
    } catch (err) {
      toast({ message: errorMessage(err), tone: 'error' });
    } finally {
      setBusy(false);
    }
  };

  return (
    <Dialog open title={`Add to ${noun}`} onClose={onClose} size="small">
      <form
        className="add-to-new"
        onSubmit={(e) => {
          e.preventDefault();
          void create();
        }}
      >
        <input
          className="input"
          value={name}
          onChange={(e) => setName(e.target.value)}
          placeholder={`New ${noun} name`}
          aria-label={`New ${noun} name`}
        />
        <button type="submit" className="primary-button" disabled={busy || !name.trim()}>
          <Plus size={16} /> Create
        </button>
      </form>
      {lists.loading && <p className="muted">Loading…</p>}
      {lists.error && <p className="error-text">{lists.error}</p>}
      {lists.data && lists.data.length > 0 && (
        <ul className="add-to-list">
          {lists.data.map((l) => (
            <li key={l.id}>
              <button type="button" disabled={busy} onClick={() => void add(l.id, l.name)}>
                <span>{l.name}</span>
                <span className="muted">{l.count}</span>
              </button>
            </li>
          ))}
        </ul>
      )}
      {lists.data?.length === 0 && (
        <p className="muted">You have no {noun}s yet. Name one above to create it.</p>
      )}
    </Dialog>
  );
}
