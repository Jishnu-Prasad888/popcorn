/**
 * The right-click menu on a poster or landscape card: play, mark watched,
 * lists, add to a collection or playlist and, for people who can edit,
 * metadata, matching and artwork (including uploading a new poster).
 *
 * Editor actions need the item's full record and whether this person may
 * edit it, so that is fetched when the menu opens, not for every card.
 */
import { useCallback, useRef, useState } from 'react';
import type { MouseEvent, ReactNode } from 'react';
import { useNavigate } from 'react-router-dom';

import {
  errorMessage,
  formatTimecode,
  getEpisode,
  getMovie,
  getShow,
  itemPath,
  playPath,
  refreshMetadata,
  removeArtwork,
  setInList,
  setPlayed,
  uploadArtwork,
} from '../../api/media';
import type { Art, Card, ItemType, Ref, UserState } from '../../api/media';
import { ArtworkDialog, EditDialog, MatchDialog } from '../manage/ManageDialogs';
import type { Manageable } from '../manage/ManageDialogs';
import { Menu } from '../ui/Menu';
import type { MenuAnchor, MenuEntry } from '../ui/Menu';
import { useToast } from '../ui/Toast';
import { AddToDialog } from './ItemActions';

type Panel = 'edit' | 'match' | 'art' | 'collection' | 'playlist' | null;

interface Loaded {
  item: Manageable;
  canEdit: boolean;
  fresh: Partial<Card>;
}

async function loadManageable(c: Card): Promise<Loaded | null> {
  const asValues = (x: object) => x as unknown as Record<string, unknown>;
  if (c.type === 'movie') {
    const { movie: m, can_edit } = await getMovie(c.library_id, c.id);
    return {
      canEdit: can_edit,
      fresh: m,
      item: {
        library_id: m.library_id,
        type: 'movie',
        id: m.id,
        title: m.title,
        year: m.year,
        art: m.art,
        locked_fields: m.locked_fields,
        values: asValues(m),
        files: m.files,
      },
    };
  }
  if (c.type === 'show') {
    const { show: s, can_edit } = await getShow(c.library_id, c.id);
    return {
      canEdit: can_edit,
      fresh: s,
      item: {
        library_id: s.library_id,
        type: 'show',
        id: s.id,
        title: s.title,
        year: s.year,
        art: s.art,
        locked_fields: s.locked_fields,
        values: asValues(s),
      },
    };
  }
  if (c.type === 'episode') {
    const { episode: e, can_edit } = await getEpisode(c.library_id, c.id);
    return {
      canEdit: can_edit,
      fresh: e,
      item: {
        library_id: e.library_id,
        type: 'episode',
        id: e.id,
        title: e.title,
        art: e.art,
        locked_fields: e.locked_fields,
        values: asValues(e),
        files: e.files,
      },
    };
  }
  return null;
}

/** The artwork a card's main image comes from: an episode poster is its show's. */
function imageTarget(
  c: Card,
  landscape: boolean,
): { type: ItemType; id: string; kind: 'poster' | 'thumb' | 'backdrop'; label: string } | null {
  if (c.type === 'episode') {
    if (landscape) return { type: 'episode', id: c.id, kind: 'thumb', label: 'thumbnail' };
    return c.show_id ? { type: 'show', id: c.show_id, kind: 'poster', label: 'show poster' } : null;
  }
  if (landscape) return { type: c.type, id: c.id, kind: 'backdrop', label: 'backdrop' };
  return { type: c.type, id: c.id, kind: 'poster', label: 'poster' };
}

const NO_OVERRIDE: Partial<Card> = {};

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

/**
 * Wires a card for its context menu. Returns the card as it should render
 * now (state and artwork follow what was done in the menu), the handler to
 * put on the card, and the menu and dialogs to render beside it.
 */
export function useCardMenu(
  c: Card,
  {
    landscape = false,
    onChanged,
  }: { landscape?: boolean; onChanged?: (() => void) | undefined } = {},
) {
  const navigate = useNavigate();
  const toast = useToast();
  const [anchor, setAnchor] = useState<MenuAnchor | null>(null);
  // What the menu changed, and the record it loaded, belong to one item: a
  // card reused for another item (the rotating home banner) starts clean.
  const key = `${c.library_id}/${c.type}/${c.id}`;
  const [ovState, setOvState] = useState<{ key: string; v: Partial<Card> }>({ key, v: {} });
  const override = ovState.key === key ? ovState.v : NO_OVERRIDE;
  const setOverride = useCallback(
    (f: (o: Partial<Card>) => Partial<Card>) =>
      setOvState((st) => ({ key, v: f(st.key === key ? st.v : {}) })),
    [key],
  );
  const [loadedState, setLoadedState] = useState<{ key: string; l: Loaded } | null>(null);
  const loaded = loadedState?.key === key ? loadedState.l : null;
  const [panel, setPanel] = useState<Panel>(null);
  const fileRef = useRef<HTMLInputElement>(null);

  const card: Card = { ...c, ...override };
  const state = card.state;
  const ref: Ref = { library_id: c.library_id, type: c.type, id: c.id };
  const target = imageTarget(card, landscape);

  const reload = useCallback(async () => {
    try {
      const l = await loadManageable(c);
      if (!l) return;
      setLoadedState({ key, l });
      const f = l.fresh;
      setOverride((o) => ({
        ...o,
        title: f.title ?? c.title,
        year: f.year,
        overview: f.overview,
        rating: f.rating,
        imdb_rating: f.imdb_rating,
        genres: f.genres ?? c.genres,
        ...(f.art ? { art: f.art } : {}),
        ...(f.show_art ? { show_art: f.show_art } : {}),
      }));
    } catch {
      // The menu still works without the editor actions.
    }
  }, [c, key, setOverride]);

  const onContextMenu = (e: MouseEvent<HTMLElement>) => {
    // Shift + right-click keeps the browser's own menu (copy, open in new tab).
    if (e.shiftKey) return;
    e.preventDefault();
    e.stopPropagation();
    // A keyboard context-menu key reports 0,0: open at the card instead.
    if (e.clientX === 0 && e.clientY === 0) {
      setAnchor({ element: e.currentTarget });
    } else {
      setAnchor({ x: e.clientX, y: e.clientY });
    }
    if (!loaded) void reload();
  };

  const toggle = async (key: 'played' | 'favorite' | 'watchlist') => {
    const prev = state;
    const on = !state?.[key];
    setOverride((o) => ({
      ...o,
      state: {
        ...(prev ?? emptyState()),
        [key]: on,
        ...(key === 'played' ? { position_secs: 0 } : {}),
      },
    }));
    try {
      if (key === 'played') await setPlayed([ref], on);
      else await setInList(key, ref, on);
      const what =
        key === 'played'
          ? on
            ? 'Marked watched'
            : 'Marked unwatched'
          : key === 'favorite'
            ? on
              ? 'Added to favourites'
              : 'Removed from favourites'
            : on
              ? 'Added to watchlist'
              : 'Removed from watchlist';
      toast({ message: what, tone: 'success' });
      onChanged?.();
    } catch (err) {
      setOverride((o) => ({ ...o, state: prev }));
      toast({ message: errorMessage(err), tone: 'error' });
    }
  };

  const bumpArt = (art: Art | undefined, has: boolean): Art => ({
    ...(art ?? { poster: false, backdrop: false, logo: false, thumb: false, version: 0 }),
    ...(target ? { [target.kind]: has } : {}),
    version: (art?.version ?? 0) + 1,
  });

  // A new version number makes the browser fetch the changed image.
  const applyArt = (has: boolean) => {
    if (!target) return;
    if (c.type === 'episode' && target.type === 'show')
      setOverride((o) => ({ ...o, show_art: bumpArt(o.show_art ?? c.show_art, has) }));
    else setOverride((o) => ({ ...o, art: bumpArt(o.art ?? c.art, has) }));
  };

  const upload = async (file: File) => {
    if (!target) return;
    try {
      await uploadArtwork(c.library_id, target.type, target.id, target.kind, file);
      applyArt(true);
      toast({ message: `New ${target.label} saved.`, tone: 'success' });
      onChanged?.();
    } catch (err) {
      toast({ message: errorMessage(err), tone: 'error' });
    }
  };

  const removeImage = async () => {
    if (!target) return;
    try {
      await removeArtwork(c.library_id, target.type, target.id, target.kind);
      applyArt(false);
      toast({ message: `Removed the ${target.label}.`, tone: 'success' });
      onChanged?.();
    } catch (err) {
      toast({ message: errorMessage(err), tone: 'error' });
    }
  };

  const refresh = async () => {
    try {
      await refreshMetadata(c.library_id, [{ type: c.type, id: c.id }], true);
      toast({ message: 'Refreshing metadata. Changes appear in a few seconds.' });
      window.setTimeout(() => {
        void reload();
        onChanged?.();
      }, 4000);
    } catch (err) {
      toast({ message: errorMessage(err), tone: 'error' });
    }
  };

  const playable = (c.type === 'movie' || c.type === 'episode') && c.available;
  const position = state && !state.played ? state.position_secs : 0;
  const hasImage = target
    ? target.type === 'show' && c.type === 'episode'
      ? card.show_art?.[target.kind]
      : card.art?.[target.kind]
    : false;
  const canEdit = loaded?.canEdit ?? false;
  const matchable = c.type === 'movie' || c.type === 'show';
  const noun = c.type === 'show' ? 'show' : c.type === 'episode' ? 'episode' : 'movie';

  const items: MenuEntry[] = [
    ...(playable
      ? [
          {
            id: 'play',
            label: position > 30 ? `Resume from ${formatTimecode(position)}` : 'Play',
            icon: 'video' as const,
            onSelect: () => navigate(playPath(c)),
          },
          ...(position > 30
            ? [
                {
                  id: 'restart',
                  label: 'Play from the beginning',
                  onSelect: () => navigate(playPath(c, { restart: true })),
                },
              ]
            : []),
        ]
      : []),
    {
      id: 'open',
      label:
        c.type === 'show' ? 'Open show' : c.type === 'episode' ? 'Open episode' : 'Open details',
      icon: 'info',
      onSelect: () => navigate(itemPath(c)),
    },
    ...(c.type === 'episode' && c.show_id
      ? [
          {
            id: 'show',
            label: 'Go to show',
            onSelect: () => navigate(`/show/${c.library_id}/${c.show_id}`),
          },
        ]
      : []),
    'separator',
    {
      id: 'played',
      label: state?.played
        ? 'Mark unwatched'
        : c.type === 'show'
          ? 'Mark all watched'
          : 'Mark watched',
      onSelect: () => void toggle('played'),
    },
    {
      id: 'favorite',
      label: state?.favorite ? 'Remove from favourites' : 'Add to favourites',
      onSelect: () => void toggle('favorite'),
    },
    {
      id: 'watchlist',
      label: state?.watchlist ? 'Remove from watchlist' : 'Add to watchlist',
      onSelect: () => void toggle('watchlist'),
    },
    { id: 'collection', label: 'Add to collection…', onSelect: () => setPanel('collection') },
    { id: 'playlist', label: 'Add to playlist…', onSelect: () => setPanel('playlist') },
    ...(canEdit && loaded
      ? ([
          'separator',
          { id: 'edit', label: 'Edit metadata…', icon: 'edit', onSelect: () => setPanel('edit') },
          ...(matchable
            ? [
                {
                  id: 'match',
                  label: 'Fix match…',
                  icon: 'search' as const,
                  onSelect: () => setPanel('match'),
                },
              ]
            : []),
          ...(matchable
            ? [
                {
                  id: 'refresh',
                  label: 'Refresh metadata',
                  icon: 'refresh' as const,
                  onSelect: () => void refresh(),
                },
              ]
            : []),
          ...(target
            ? [
                {
                  id: 'upload',
                  label: `Upload a new ${target.label}…`,
                  icon: 'upload' as const,
                  // Synchronous, inside the click, so the browser allows the file picker.
                  onSelect: () => fileRef.current?.click(),
                },
              ]
            : []),
          { id: 'art', label: 'Choose artwork…', onSelect: () => setPanel('art') },
          ...(target && hasImage
            ? [
                {
                  id: 'remove-art',
                  label: `Remove the ${target.label}`,
                  icon: 'trash' as const,
                  danger: true,
                  onSelect: () => void removeImage(),
                },
              ]
            : []),
        ] as MenuEntry[])
      : []),
  ];

  const afterSave = () => {
    void reload();
    onChanged?.();
  };

  const element: ReactNode = (
    <>
      <input
        ref={fileRef}
        type="file"
        accept="image/jpeg,image/png,image/webp"
        className="visually-hidden"
        tabIndex={-1}
        aria-hidden="true"
        onChange={(e) => {
          const f = e.target.files?.[0];
          e.target.value = '';
          if (f) void upload(f);
        }}
      />
      {anchor && (
        <Menu
          anchor={anchor}
          items={items}
          onClose={() => setAnchor(null)}
          label={`${card.title}, ${noun} actions`}
        />
      )}
      {panel === 'collection' && (
        <AddToDialog
          kind="collection"
          items={[ref]}
          title={card.title}
          onClose={() => setPanel(null)}
        />
      )}
      {panel === 'playlist' && (
        <AddToDialog
          kind="playlist"
          items={[ref]}
          title={card.title}
          onClose={() => setPanel(null)}
        />
      )}
      {loaded && panel === 'edit' && (
        <EditDialog item={loaded.item} onClose={() => setPanel(null)} onSaved={afterSave} />
      )}
      {loaded && panel === 'match' && (
        <MatchDialog item={loaded.item} onClose={() => setPanel(null)} onSaved={afterSave} />
      )}
      {loaded && panel === 'art' && (
        <ArtworkDialog item={loaded.item} onClose={() => setPanel(null)} onSaved={afterSave} />
      )}
    </>
  );

  return { card, onContextMenu, element };
}

/** The right-click menu on a cast or crew member. */
export function usePersonMenu(libraryId: string) {
  const navigate = useNavigate();
  const [open, setOpen] = useState<{ anchor: MenuAnchor; id: string; name: string } | null>(null);
  const openPerson = (e: MouseEvent<HTMLElement>, p: { person_id: string; name: string }) => {
    if (e.shiftKey) return;
    e.preventDefault();
    const anchor: MenuAnchor =
      e.clientX === 0 && e.clientY === 0
        ? { element: e.currentTarget }
        : { x: e.clientX, y: e.clientY };
    setOpen({ anchor, id: p.person_id, name: p.name });
  };
  const element = open ? (
    <Menu
      anchor={open.anchor}
      label={`${open.name} actions`}
      onClose={() => setOpen(null)}
      items={[
        {
          id: 'open',
          label: `Everything with ${open.name}`,
          icon: 'person',
          onSelect: () => navigate(`/person/${libraryId}/${open.id}`),
        },
        {
          id: 'search',
          label: `Search for ${open.name}`,
          icon: 'search',
          onSelect: () => navigate(`/search?q=${encodeURIComponent(open.name)}`),
        },
      ]}
    />
  ) : null;
  return { openPerson, element };
}
