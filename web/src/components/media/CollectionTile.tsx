import { useState } from 'react';
import type { MouseEvent } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import { Layers, ListVideo } from 'lucide-react';

import {
  backdropOf,
  collectionPosterUrl,
  collectionQueue,
  deleteCollection,
  deletePlaylist,
  duplicateCollection,
  errorMessage,
  getPlaylist,
  playlistPosterUrl,
  posterOf,
} from '../../api/media';
import type { Card, Collection, Playlist, SmartCollection } from '../../api/media';
import { ConfirmDialog } from '../Dialog';
import { Menu } from '../ui/Menu';
import type { MenuAnchor, MenuEntry } from '../ui/Menu';
import { useToast } from '../ui/Toast';

/** A collection or playlist tile: its own artwork, or a fan of its posters. */
export function CollectionTile({
  collection,
  playlist,
  smart,
  covers,
  onChanged,
}: {
  collection?: Collection;
  playlist?: Playlist;
  smart?: SmartCollection;
  covers?: Card[];
  /** Called after the right-click menu deletes or duplicates. */
  onChanged?: () => void;
}) {
  const item = collection ?? playlist ?? smart!;
  const navigate = useNavigate();
  const toast = useToast();
  const [anchor, setAnchor] = useState<MenuAnchor | null>(null);
  const [confirming, setConfirming] = useState(false);
  const to = playlist ? `/playlists/${item.id}` : `/collections/${item.id}`;
  const own = collection ? collectionPosterUrl(collection) : playlist ? playlistPosterUrl(playlist) : null;
  const cards = covers ?? collection?.covers ?? playlist?.covers ?? smart?.covers ?? [];
  const Icon = playlist ? ListVideo : Layers;
  const count = item.item_count;
  const noun = playlist ? 'playlist' : 'collection';
  // A custom collection is one the person made; smart ones are built for them.
  const custom = Boolean(collection && !collection.smart);

  const play = async () => {
    try {
      if (playlist) {
        const p = await getPlaylist(item.id);
        const e = p.entries.find((x) => x.entry_id === p.playlist.current_entry && x.item?.available) ?? p.entries.find((x) => x.item?.available);
        if (!e) {
          toast({ message: 'Nothing in this playlist can be played.' });
          return;
        }
        navigate(`/play/${e.library_id}/${e.type}/${e.id}?queue=playlist:${item.id}&entry=${e.entry_id}`);
        return;
      }
      const q = await collectionQueue(item.id);
      const first = q.queue[0];
      if (!first) {
        toast({ message: 'Nothing in this collection can be played.' });
        return;
      }
      navigate(`/play/${first.library_id}/${first.type}/${first.id}?queue=collection:${item.id}`);
    } catch (err) {
      toast({ message: errorMessage(err), tone: 'error' });
    }
  };

  const duplicate = async () => {
    try {
      const c = await duplicateCollection(item.id);
      toast({ message: `Made a copy: ${c.name}.`, tone: 'success' });
      onChanged?.();
    } catch (err) {
      toast({ message: errorMessage(err), tone: 'error' });
    }
  };

  const remove = async () => {
    try {
      if (playlist) await deletePlaylist(item.id);
      else await deleteCollection(item.id);
      setConfirming(false);
      toast({ message: `Deleted ${item.name}.`, tone: 'success' });
      onChanged?.();
    } catch (err) {
      toast({ message: errorMessage(err), tone: 'error' });
    }
  };

  const onContextMenu = (e: MouseEvent<HTMLElement>) => {
    if (e.shiftKey) return;
    e.preventDefault();
    setAnchor(e.clientX === 0 && e.clientY === 0 ? { element: e.currentTarget } : { x: e.clientX, y: e.clientY });
  };

  const items: MenuEntry[] = [
    { id: 'play', label: count ? 'Play all' : 'Play all (empty)', icon: 'video', disabled: !count, onSelect: () => void play() },
    { id: 'open', label: `Open ${noun}`, icon: 'info', onSelect: () => navigate(to) },
    ...(custom ? [{ id: 'duplicate', label: 'Duplicate', icon: 'copy' as const, onSelect: () => void duplicate() }] : []),
    ...(custom || playlist ? (['separator', { id: 'delete', label: `Delete ${noun}…`, icon: 'trash', danger: true, onSelect: () => setConfirming(true) }] as MenuEntry[]) : []),
  ];

  return (
    <article className="collection-tile" onContextMenu={onContextMenu}>
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
      {anchor && <Menu anchor={anchor} items={items} label={`${item.name}, ${noun} actions`} onClose={() => setAnchor(null)} />}
      <ConfirmDialog
        open={confirming}
        title={`Delete ${item.name}?`}
        message={`The ${noun} goes away; its titles stay in your libraries.`}
        confirmLabel={`Delete ${noun}`}
        destructive
        onCancel={() => setConfirming(false)}
        onConfirm={() => void remove()}
      />
    </article>
  );
}
