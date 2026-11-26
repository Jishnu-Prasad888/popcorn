/**
 * The movie and TV API: catalog browsing, playback, per-person state, lists,
 * collections, playlists and metadata management. Types mirror the Go JSON.
 */
import { API_BASE, apiDelete, apiGet, apiPatch, apiPost, apiPut, ApiError } from './client';

export type ItemType = 'movie' | 'show' | 'season' | 'episode';
export type PlayableType = 'movie' | 'episode';

export interface Art {
  poster: boolean;
  backdrop: boolean;
  logo: boolean;
  thumb: boolean;
  version: number;
}

export interface UserState {
  position_secs: number;
  duration_secs: number;
  played: boolean;
  play_count: number;
  last_played_at?: string | undefined;
  favorite: boolean;
  watchlist: boolean;
  unplayed_count: number;
}

export interface Card {
  type: ItemType;
  id: string;
  library_id: string;
  title: string;
  original_title?: string | undefined;
  year?: number | undefined;
  release_date?: string | undefined;
  rating?: number | undefined;
  imdb_rating?: number | undefined;
  runtime_mins?: number | undefined;
  genres: string[] | null;
  content_rating?: string | undefined;
  overview?: string | undefined;
  tagline?: string | undefined;
  category?: string | undefined;
  added_at: string;
  updated_at?: string | undefined;
  art: Art;
  match_status?: string | undefined;
  resolution?: string | undefined;
  height?: number | undefined;
  size_bytes?: number | undefined;
  duration_secs?: number | undefined;
  file_count?: number | undefined;
  available: boolean;
  season_count?: number | undefined;
  episode_count?: number | undefined;
  show_id?: string | undefined;
  show_title?: string | undefined;
  season_id?: string | undefined;
  season_number: number;
  episode_number?: number | undefined;
  episode_end?: number | undefined;
  air_date?: string | undefined;
  show_art?: Art | undefined;
  state?: UserState | undefined;
}

export interface Credit {
  person_id: string;
  name: string;
  role: string;
  character?: string | undefined;
  profile_url?: string | undefined;
}

export interface StreamInfo {
  index: number;
  kind: 'video' | 'audio' | 'subtitle' | string;
  type_index: number;
  codec: string;
  profile?: string | undefined;
  language?: string | undefined;
  language_name?: string | undefined;
  title?: string | undefined;
  channels?: number | undefined;
  channel_layout?: string | undefined;
  sample_rate?: number | undefined;
  bitrate?: number | undefined;
  width?: number | undefined;
  height?: number | undefined;
  is_default: boolean;
  is_forced: boolean;
  is_hearing_impaired: boolean;
  image_based?: boolean | undefined;
}

export interface ExternalSubtitle {
  id: string;
  source: 'sidecar' | 'upload';
  path: string;
  format: string;
  language?: string | undefined;
  language_name?: string | undefined;
  title?: string | undefined;
  is_forced: boolean;
  is_hearing_impaired: boolean;
}

export interface Chapter {
  start: number;
  end: number;
  title?: string | undefined;
}

export interface FileInfo {
  id: string;
  rel_path: string;
  name: string;
  edition?: string | undefined;
  size_bytes: number;
  mod_time: string;
  status: string;
  manual: boolean;
  container?: string | undefined;
  duration_secs?: number | undefined;
  bitrate?: number | undefined;
  width?: number | undefined;
  height?: number | undefined;
  resolution?: string | undefined;
  video_codec?: string | undefined;
  video_profile?: string | undefined;
  pix_fmt?: string | undefined;
  frame_rate?: number | undefined;
  hdr?: string | undefined;
  probe_error?: string | undefined;
  probed: boolean;
  streams: StreamInfo[] | null;
  subtitles: ExternalSubtitle[] | null;
  chapters: Chapter[] | null;
}

interface DetailExtras {
  original_language?: string | undefined;
  languages: string[] | null;
  countries: string[] | null;
  studios: string[] | null;
  imdb_id?: string | undefined;
  tmdb_id?: number | undefined;
  vote_count?: number | undefined;
  imdb_votes?: number | undefined;
  locked_fields: string[] | null;
  metadata_at?: string | undefined;
  folder?: string | undefined;
  credits: Credit[] | null;
}

export interface Movie extends Card, DetailExtras {
  files: FileInfo[];
}

export interface Season {
  id: string;
  show_id: string;
  library_id: string;
  season_number: number;
  title: string;
  overview?: string | undefined;
  air_date?: string | undefined;
  episode_count: number;
  episode_total?: number | undefined;
  sort_order: number;
  art: Art;
  locked_fields: string[] | null;
  state?: UserState | undefined;
}

export interface Show extends Card, DetailExtras {
  first_air_date?: string | undefined;
  last_air_date?: string | undefined;
  status?: string | undefined;
  seasons: Season[];
}

export interface Episode extends Card {
  intro_start?: number | undefined;
  intro_end?: number | undefined;
  credits_start?: number | undefined;
  sort_order: number;
  locked_fields: string[] | null;
  credits: Credit[] | null;
  files: FileInfo[];
}

export interface Person {
  id: string;
  library_id: string;
  name: string;
  tmdb_id?: number | undefined;
  profile_url?: string | undefined;
  items: Card[];
  roles: Record<string, string[]>;
}

export interface PersonHit {
  id: string;
  library_id: string;
  name: string;
  profile_url?: string | undefined;
  credits: number;
}

export interface Ref {
  library_id: string;
  type: ItemType;
  id: string;
}

export interface Collection {
  id: string;
  name: string;
  description: string;
  poster_version: number;
  item_count: number;
  created_at: string;
  updated_at: string;
  preview: Ref[];
  covers?: Card[] | undefined;
  smart?: boolean | undefined;
}

export interface SmartCollection {
  id: string;
  name: string;
  description: string;
  item_count: number;
  covers: Card[];
}

export interface Playlist {
  id: string;
  name: string;
  description: string;
  shuffle: boolean;
  repeat: 'off' | 'all' | 'one' | string;
  current_entry?: string | undefined;
  poster_version: number;
  item_count: number;
  created_at: string;
  updated_at: string;
  preview: Ref[];
  covers?: Card[] | undefined;
}

export interface PlaylistEntry extends Ref {
  entry_id: string;
  position: number;
  position_secs: number;
  added_at: string;
  item: Card | null;
}

export interface HistoryEntry extends Ref {
  entry_id: number;
  show_id?: string | undefined;
  watched_at: string;
  watched_secs: number;
  item: Card;
}

export interface SubtitleStyle {
  size: number;
  color: string;
  background: 'none' | 'shadow' | 'box' | string;
  opacity: number;
  position: number;
  font: 'sans' | 'serif' | 'mono' | 'rounded' | string;
}

export interface PlaybackPrefs {
  audio_languages: string[];
  subtitle_languages: string[];
  subtitle_mode: 'auto' | 'always' | 'forced' | 'off' | string;
  prefer_original_audio: boolean;
  prefer_sdh: boolean;
  autoplay_next: boolean;
  next_countdown: number;
  skip_intro: 'off' | 'button' | 'auto' | string;
  skip_credits: 'off' | 'button' | 'auto' | string;
  quality: string;
  max_bitrate: number;
  speed: number;
  skip_forward: number;
  skip_backward: number;
  passthrough: string[];
  subtitle_style: SubtitleStyle;
}

export interface InterfacePrefs {
  density: 'compact' | 'comfortable' | 'spacious' | string;
  card_size: 'small' | 'medium' | 'large' | string;
  view: 'grid' | 'list' | string;
  motion: 'full' | 'reduced' | string;
  accent: 'purple' | 'pink' | 'cyan' | 'green' | string;
  home_rows: string[];
}

export interface Facets {
  genres: string[];
  years: number[];
  languages: string[];
  studios: string[];
}

export interface Stats {
  movies: number;
  shows: number;
  seasons: number;
  episodes: number;
  video_files: number;
  missing_files: number;
  total_bytes: number;
  movie_bytes: number;
  episode_bytes: number;
  total_secs: number;
  unmatched: number;
  unprobed: number;
  resolutions: Record<string, number>;
  video_codecs: Record<string, number>;
  containers: Record<string, number>;
  subtitles: number;
}

export interface HomeRow {
  id: string;
  title: string;
  kind: 'hero' | 'cards' | 'landscape' | string;
  items: Card[];
  link?: string | undefined;
}

export interface SubtitleTrack {
  id: string;
  source: 'embedded' | 'sidecar' | 'upload';
  language?: string | undefined;
  language_name: string;
  title?: string | undefined;
  format: string;
  is_forced: boolean;
  is_hearing_impaired: boolean;
  is_default: boolean;
  image_based: boolean;
  type_index: number;
  url?: string | undefined;
}

export interface PlayInfo {
  item: Card;
  files: FileInfo[];
  file_id: string;
  subtitles: SubtitleTrack[];
  audio: number;
  subtitle: string;
  resume_secs: number;
  markers: {
    intro_start?: number | null;
    intro_end?: number | null;
    credits_start?: number | null;
  };
  next: Card | null;
  previous: Card | null;
  preferences: PlaybackPrefs;
  ffmpeg: boolean;
}

export interface Decision {
  method: 'direct' | 'remux' | 'transcode';
  video_copy: boolean;
  audio_copy: boolean;
  reasons: string[];
}

export interface ProviderResult {
  provider: string;
  id: string;
  kind: 'movie' | 'show';
  title: string;
  original_title?: string | undefined;
  year?: number | undefined;
  overview?: string | undefined;
  poster_url?: string | undefined;
  rating?: number | undefined;
}

export interface MetadataSettings {
  tmdb_key_set: boolean;
  omdb_key_set: boolean;
  tmdb_key_hint?: string | undefined;
  omdb_key_hint?: string | undefined;
  language: string;
  region: string;
  auto: boolean;
  generate_thumbs: boolean;
}

export interface StreamSession {
  session: string;
  user_id: string;
  username: string;
  title: string;
  method: string;
  started_at: string;
}

export interface Listing {
  items: Card[];
  total: number;
}

/* ---------- URLs ---------- */

/** The URL of an item's artwork, or null when it has none. */
export function artUrl(
  libraryId: string,
  type: ItemType,
  id: string,
  kind: 'poster' | 'backdrop' | 'logo' | 'thumb',
  art: Art | undefined,
): string | null {
  if (!art || !art[kind]) return null;
  return `${API_BASE}/libraries/${libraryId}/art/${type}/${id}/${kind}?v=${art.version}`;
}

/** The best poster-shaped image for a card. */
export function posterOf(c: Card): string | null {
  if (c.type === 'episode') {
    return c.show_id ? artUrl(c.library_id, 'show', c.show_id, 'poster', c.show_art) : null;
  }
  return artUrl(c.library_id, c.type, c.id, 'poster', c.art);
}

/** The best 16:9 image for a card: an episode still, else a backdrop. */
export function landscapeOf(c: Card): string | null {
  if (c.type === 'episode') {
    return (
      artUrl(c.library_id, 'episode', c.id, 'thumb', c.art) ??
      (c.show_id ? artUrl(c.library_id, 'show', c.show_id, 'backdrop', c.show_art) : null)
    );
  }
  return (
    artUrl(c.library_id, c.type, c.id, 'backdrop', c.art) ??
    artUrl(c.library_id, c.type, c.id, 'thumb', c.art)
  );
}

export function backdropOf(c: Card): string | null {
  if (c.type === 'episode' && c.show_id) {
    return artUrl(c.library_id, 'show', c.show_id, 'backdrop', c.show_art) ?? landscapeOf(c);
  }
  return artUrl(c.library_id, c.type, c.id, 'backdrop', c.art);
}

export function logoOf(c: Card): string | null {
  if (c.type === 'episode' && c.show_id)
    return artUrl(c.library_id, 'show', c.show_id, 'logo', c.show_art);
  return artUrl(c.library_id, c.type, c.id, 'logo', c.art);
}

export const personImageUrl = (libraryId: string, personId: string) =>
  `${API_BASE}/libraries/${libraryId}/people/${personId}/image`;

/** The app route of an item's page. */
export function itemPath(c: {
  type: ItemType;
  library_id: string;
  id: string;
  show_id?: string | undefined;
  season_id?: string | undefined;
}): string {
  switch (c.type) {
    case 'movie':
      return `/movie/${c.library_id}/${c.id}`;
    case 'show':
      return `/show/${c.library_id}/${c.id}`;
    case 'season':
      return `/show/${c.library_id}/${c.show_id}?season=${c.id}`;
    case 'episode':
      return `/episode/${c.library_id}/${c.id}`;
  }
}

export function playPath(
  c: { type: ItemType; library_id: string; id: string },
  opts: { restart?: boolean; queue?: string } = {},
): string {
  const q = new URLSearchParams();
  if (opts.restart) q.set('restart', '1');
  if (opts.queue) q.set('queue', opts.queue);
  const qs = q.toString();
  return `/play/${c.library_id}/${c.type}/${c.id}${qs ? `?${qs}` : ''}`;
}

/* ---------- formatting ---------- */

export function formatRuntime(mins?: number, secs?: number): string {
  const m = mins && mins > 0 ? mins : secs ? Math.round(secs / 60) : 0;
  if (!m) return '';
  const h = Math.floor(m / 60);
  return h ? `${h}h ${m % 60}m` : `${m}m`;
}

export function formatTimecode(secs: number): string {
  if (!Number.isFinite(secs) || secs < 0) secs = 0;
  const s = Math.floor(secs % 60);
  const m = Math.floor((secs / 60) % 60);
  const h = Math.floor(secs / 3600);
  const pad = (n: number) => String(n).padStart(2, '0');
  return h ? `${h}:${pad(m)}:${pad(s)}` : `${m}:${pad(s)}`;
}

export function episodeCode(
  c: Pick<Card, 'season_number' | 'episode_number' | 'episode_end'>,
): string {
  if (c.episode_number === undefined) return '';
  const pad = (n: number) => String(n).padStart(2, '0');
  const end =
    c.episode_end && c.episode_end > (c.episode_number ?? 0) ? `–E${pad(c.episode_end)}` : '';
  return `S${pad(c.season_number)}E${pad(c.episode_number)}${end}`;
}

/** 0..1 progress through an item, or 0. */
export function progressOf(c: Card): number {
  const st = c.state;
  if (!st || st.played) return 0;
  const d = st.duration_secs || c.duration_secs || (c.runtime_mins ?? 0) * 60;
  return d > 0 ? Math.min(1, st.position_secs / d) : 0;
}

export function ratingOf(c: Card): number | undefined {
  return c.imdb_rating || c.rating || undefined;
}

export function errorMessage(err: unknown): string {
  if (err instanceof ApiError) return err.message;
  if (err instanceof Error) return err.message;
  return 'Something went wrong.';
}

/* ---------- requests ---------- */

const qs = (params: Record<string, string | number | boolean | undefined | null>) => {
  const q = new URLSearchParams();
  for (const [k, v] of Object.entries(params)) {
    if (v !== undefined && v !== null && v !== '' && v !== false) q.set(k, String(v));
  }
  const s = q.toString();
  return s ? `?${s}` : '';
};

export type ListParams = Record<string, string | number | undefined>;

export const getHome = () =>
  apiGet<{ rows: HomeRow[]; collections: Collection[]; counts: { movies: number; shows: number } }>(
    '/home',
  );
export const listMovies = (p: ListParams = {}) => apiGet<Listing>(`/movies${qs(p)}`);
export const listShows = (p: ListParams = {}) => apiGet<Listing>(`/shows${qs(p)}`);
export const getFacets = (type: 'movie' | 'show') => apiGet<Facets>(`/facets?type=${type}`);
export const search = (q: string) =>
  apiGet<{ movies: Card[]; shows: Card[]; episodes: Card[]; people: PersonHit[] }>(
    `/search${qs({ q })}`,
  );

export const getMovie = (lib: string, id: string) =>
  apiGet<{ movie: Movie; related: Card[]; history: HistoryEntry[]; can_edit: boolean }>(
    `/libraries/${lib}/movies/${id}`,
  );
export const getShow = (lib: string, id: string) =>
  apiGet<{ show: Show; next_up: Card | null; related: Card[]; can_edit: boolean }>(
    `/libraries/${lib}/shows/${id}`,
  );
export const getSeasonEpisodes = (lib: string, seasonId: string) =>
  apiGet<{ season: Season; episodes: Card[] }>(`/libraries/${lib}/seasons/${seasonId}/episodes`);
export const getEpisode = (lib: string, id: string) =>
  apiGet<{ episode: Episode; previous_id: string; next_id: string; can_edit: boolean }>(
    `/libraries/${lib}/episodes/${id}`,
  );
export const getPerson = (lib: string, id: string) =>
  apiGet<Person>(`/libraries/${lib}/people/${id}`);

export const getPlayInfo = (lib: string, type: PlayableType, id: string, fileId?: string) =>
  apiGet<PlayInfo>(`/libraries/${lib}/play/${type}/${id}${qs({ file: fileId })}`);
export const decide = (lib: string, fileId: string, params: ListParams) =>
  apiGet<{ decision: Decision; url: string; audio: number; duration_secs: number }>(
    `/libraries/${lib}/files/${fileId}/decide${qs(params)}`,
  );
/** Where a stream that copies video really starts when asked for `at`:
 * the keyframe it lands on, and the source time that is 0 in the stream. */
export const keyframeAt = (lib: string, fileId: string, at: number) =>
  apiGet<{ keyframe: number; zero: number }>(
    `/libraries/${lib}/files/${fileId}/keyframe${qs({ at: at.toFixed(3) })}`,
  );
export const reportProgress = (body: {
  library_id: string;
  type: PlayableType;
  id: string;
  file_id?: string | undefined;
  position_secs: number;
  duration_secs: number;
  audio_language?: string | undefined;
  subtitle_language?: string | undefined;
  ended?: boolean | undefined;
  playlist_id?: string | undefined;
  playlist_entry?: string | undefined;
}) => apiPost<UserState>('/playback/progress', body);
export const stopStream = (session: string) => apiPost<void>('/playback/stop', { session });
export const setPlayed = (items: Ref[], played: boolean) =>
  apiPost<{ updated: number }>('/playback/played', { items, played });
export const clearProgress = (ref: Ref) => apiDelete<void>('/playback/progress', ref);

export type ListName = 'favorite' | 'watchlist';
export const setInList = (list: ListName, ref: Ref, on: boolean) =>
  (on ? apiPut<void> : apiDelete<void>)(
    `/me/lists/${list}/${ref.library_id}/${ref.type}/${ref.id}`,
  );
export const getList = (list: ListName) => apiGet<{ items: Card[] }>(`/me/lists/${list}`);
export const getHistory = (limit = 100, offset = 0) =>
  apiGet<{ entries: HistoryEntry[] }>(`/me/history${qs({ limit, offset })}`);
export const deleteHistory = (id: number) => apiDelete<void>(`/me/history/${id}`);
export const getPreferences = () =>
  apiGet<{ playback: PlaybackPrefs; interface: InterfacePrefs }>('/me/preferences');
export const savePreferences = (body: { playback?: PlaybackPrefs; interface?: InterfacePrefs }) =>
  apiPut<{ playback: PlaybackPrefs; interface: InterfacePrefs }>('/me/preferences', body);

export const listCollections = () =>
  apiGet<{ collections: Collection[]; smart: SmartCollection[] }>('/collections');
export const getCollection = (id: string) =>
  apiGet<{ collection: Collection; items: Card[] }>(`/collections/${id}`);
export const createCollection = (name: string, description = '', items: Ref[] = []) =>
  apiPost<Collection>('/collections', { name, description, items });
export const updateCollection = (id: string, patch: { name?: string; description?: string }) =>
  apiPatch<Collection>(`/collections/${id}`, patch);
export const deleteCollection = (id: string) => apiDelete<void>(`/collections/${id}`);
export const duplicateCollection = (id: string, name?: string) =>
  apiPost<Collection>(`/collections/${id}/duplicate`, { name });
export const addToCollection = (id: string, items: Ref[]) =>
  apiPost<void>(`/collections/${id}/items`, { items });
export const removeFromCollection = (id: string, items: Ref[]) =>
  apiDelete<void>(`/collections/${id}/items`, { items });
export const reorderCollection = (id: string, items: Ref[]) =>
  apiPut<void>(`/collections/${id}/order`, { items });
export const collectionQueue = (id: string) => apiGet<{ queue: Ref[] }>(`/collections/${id}/queue`);
export const collectionPosterUrl = (c: Collection) =>
  c.poster_version > 0 ? `${API_BASE}/collections/${c.id}/poster?v=${c.poster_version}` : null;
export const playlistPosterUrl = (p: Playlist) =>
  p.poster_version > 0 ? `${API_BASE}/playlists/${p.id}/poster?v=${p.poster_version}` : null;

export const listPlaylists = () => apiGet<{ playlists: Playlist[] }>('/playlists');
export const getPlaylist = (id: string) =>
  apiGet<{ playlist: Playlist; entries: PlaylistEntry[] }>(`/playlists/${id}`);
export const createPlaylist = (name: string, description = '', items: Ref[] = []) =>
  apiPost<Playlist>('/playlists', { name, description, items });
export const updatePlaylist = (
  id: string,
  patch: {
    name?: string;
    description?: string;
    shuffle?: boolean;
    repeat?: string;
    current_entry?: string;
  },
) => apiPatch<Playlist>(`/playlists/${id}`, patch);
export const deletePlaylist = (id: string) => apiDelete<void>(`/playlists/${id}`);
export const addToPlaylist = (id: string, items: Ref[], collection?: string) =>
  apiPost<{ added: number }>(`/playlists/${id}/items`, { items, collection });
export const removeFromPlaylist = (id: string, entryIds: string[]) =>
  apiDelete<void>(`/playlists/${id}/items`, { entry_ids: entryIds });
export const reorderPlaylist = (id: string, entryIds: string[]) =>
  apiPut<void>(`/playlists/${id}/order`, { entry_ids: entryIds });
export const shufflePlaylist = (id: string) => apiPost<void>(`/playlists/${id}/shuffle`);

/** Uploads raw bytes (artwork, subtitles). */
export async function uploadBytes(
  path: string,
  body: Blob,
  method: 'PUT' | 'POST' = 'PUT',
): Promise<unknown> {
  const res = await fetch(`${API_BASE}${path}`, {
    method,
    body,
    headers: { Accept: 'application/json' },
  });
  if (!res.ok) {
    let message = `Upload failed (${res.status}).`;
    try {
      const env = (await res.json()) as { error?: { message?: string } };
      if (env.error?.message) message = env.error.message;
    } catch {
      /* not JSON */
    }
    throw new Error(message);
  }
  return res.status === 204 ? undefined : res.json();
}
export const uploadUserPoster = (kind: 'collections' | 'playlists', id: string, file: Blob) =>
  uploadBytes(`/${kind}/${id}/poster`, file);
export const removeUserPoster = (kind: 'collections' | 'playlists', id: string) =>
  apiDelete<void>(`/${kind}/${id}/poster`);

/* ---------- management ---------- */

export const getLibraryStats = (lib: string) =>
  apiGet<{ stats: Stats; jobs: Record<string, number>; ffprobe: boolean }>(
    `/libraries/${lib}/stats`,
  );
export const getCatalogDuplicates = (lib: string) =>
  apiGet<{ duplicates: { kind: string; movies: Card[] }[] }>(
    `/libraries/${lib}/catalog/duplicates`,
  );
export const getUnassigned = (lib: string) =>
  apiGet<{ files: { id: string; rel_path: string }[] }>(`/libraries/${lib}/catalog/unassigned`);
export const updateLibrary = (lib: string, patch: { name?: string; kind?: string }) =>
  apiPatch<unknown>(`/libraries/${lib}`, patch);
export const syncCatalog = (lib: string) => apiPost<void>(`/libraries/${lib}/catalog/sync`);
export const refreshMetadata = (
  lib: string,
  items: { type: ItemType; id: string }[] = [],
  force = false,
) => apiPost<{ queued: number }>(`/libraries/${lib}/metadata/refresh`, { items, force });
export const getMetadataSettings = () => apiGet<MetadataSettings>('/settings/metadata');
export const saveMetadataSettings = (
  body: Partial<{
    tmdb_key: string;
    omdb_key: string;
    language: string;
    region: string;
    auto: boolean;
    generate_thumbs: boolean;
  }>,
) => apiPut<MetadataSettings>('/settings/metadata', body);
export const testMetadata = () =>
  apiPost<{ ok: boolean; error?: string }>('/settings/metadata/test');
export const providerSearch = (type: 'movie' | 'show', q: string, year?: number) =>
  apiGet<{ results: ProviderResult[] }>(`/metadata/search${qs({ type, q, year })}`);
export const matchItem = (lib: string, type: 'movie' | 'show', id: string, providerId: string) =>
  apiPost<void>(`/libraries/${lib}/items/${type}/${id}/match`, { provider_id: providerId });
export const unmatchItem = (lib: string, type: 'movie' | 'show', id: string) =>
  apiPost<void>(`/libraries/${lib}/items/${type}/${id}/unmatch`);
export const editItem = (
  lib: string,
  type: ItemType,
  id: string,
  changes: Record<string, unknown>,
) => apiPatch<void>(`/libraries/${lib}/items/${type}/${id}`, changes);
export const unlockFields = (lib: string, type: ItemType, id: string, fields: string[]) =>
  apiPost<void>(`/libraries/${lib}/items/${type}/${id}/unlock`, { fields });
export const uploadArtwork = (lib: string, type: ItemType, id: string, kind: string, file: Blob) =>
  uploadBytes(`/libraries/${lib}/art/${type}/${id}/${kind}`, file);
export const removeArtwork = (lib: string, type: ItemType, id: string, kind: string) =>
  apiDelete<void>(`/libraries/${lib}/art/${type}/${id}/${kind}`);
export const generateArtwork = (lib: string, type: ItemType, id: string, kind: string, at = 0) =>
  apiPost<void>(`/libraries/${lib}/art/${type}/${id}/${kind}/generate`, { at });
export const attachFile = (lib: string, movieId: string, fileId: string, edition: string) =>
  apiPost<void>(`/libraries/${lib}/movies/${movieId}/files`, { file_id: fileId, edition });
export const setEdition = (lib: string, fileId: string, edition: string) =>
  apiPut<void>(`/libraries/${lib}/files/${fileId}/edition`, { edition });
export const splitFile = (lib: string, fileId: string) =>
  apiPost<{ movie_id: string }>(`/libraries/${lib}/files/${fileId}/split`);
export const releaseFile = (lib: string, fileId: string) =>
  apiPost<void>(`/libraries/${lib}/files/${fileId}/release`);
export const mergeMovies = (lib: string, target: string, others: string[]) =>
  apiPost<void>(`/libraries/${lib}/movies/${target}/merge`, { others });
export const moveEpisode = (lib: string, id: string, season: number, number: number) =>
  apiPost<void>(`/libraries/${lib}/episodes/${id}/move`, { season, number });
export const reorderSeasons = (lib: string, showId: string, ids: string[]) =>
  apiPut<void>(`/libraries/${lib}/shows/${showId}/order`, { ids });
export const reorderEpisodes = (lib: string, seasonId: string, ids: string[]) =>
  apiPut<void>(`/libraries/${lib}/seasons/${seasonId}/order`, { ids });
export const uploadSubtitle = (
  lib: string,
  fileId: string,
  file: File,
  info: { language?: string; title?: string; forced?: boolean; sdh?: boolean },
) =>
  uploadBytes(
    `/libraries/${lib}/files/${fileId}/subtitles${qs({ name: file.name, language: info.language, title: info.title, forced: info.forced, sdh: info.sdh })}`,
    file,
    'POST',
  ) as Promise<{ id: string }>;
export const updateSubtitle = (
  lib: string,
  subId: string,
  info: { language: string; title: string; forced: boolean; sdh: boolean },
) => apiPatch<void>(`/libraries/${lib}/subtitles/${subId}`, info);
export const replaceSubtitle = (lib: string, subId: string, file: Blob) =>
  uploadBytes(`/libraries/${lib}/subtitles/${subId}/content`, file);
export const renameSubtitle = (lib: string, subId: string, name: string) =>
  apiPost<{ path: string }>(`/libraries/${lib}/subtitles/${subId}/rename`, { name });
export const deleteSubtitle = (lib: string, subId: string) =>
  apiDelete<void>(`/libraries/${lib}/subtitles/${subId}`);
export const listStreams = () => apiGet<{ streams: StreamSession[] }>('/streams');
