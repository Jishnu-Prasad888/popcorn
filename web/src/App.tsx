import { Suspense } from 'react';
import { Navigate, Route, Routes } from 'react-router-dom';

import RequireAuth from './auth/RequireAuth';
import { LibrariesProvider } from './api/libraries';
import AppShell from './components/shell/AppShell';
import { ToastProvider } from './components/ui/Toast';
import { lazyPage } from './lib/lazyPage';
import BrowsePage from './pages/BrowsePage';
import ForbiddenPage from './pages/ForbiddenPage';
import HomePage from './pages/HomePage';
import LoginPage from './pages/LoginPage';
import MovieDetailPage from './pages/MovieDetailPage';
import SearchPage from './pages/SearchPage';
import SetupPage from './pages/SetupPage';
import ShowDetailPage from './pages/ShowDetailPage';

/* Pages most visits never touch load on demand, keeping the first load small. */
const PlayerPage = lazyPage(() => import('./pages/PlayerPage'));
const EpisodeDetailPage = lazyPage(() => import('./pages/EpisodeDetailPage'));
const PersonPage = lazyPage(() => import('./pages/PersonPage'));
const CollectionsPage = lazyPage(() => import('./pages/CollectionsPage').then((m) => ({ default: m.CollectionsPage })));
const CollectionDetailPage = lazyPage(() => import('./pages/CollectionsPage').then((m) => ({ default: m.CollectionDetailPage })));
const PlaylistsPage = lazyPage(() => import('./pages/CollectionsPage').then((m) => ({ default: m.PlaylistsPage })));
const PlaylistDetailPage = lazyPage(() => import('./pages/CollectionsPage').then((m) => ({ default: m.PlaylistDetailPage })));
const WatchlistPage = lazyPage(() => import('./pages/ListPage').then((m) => ({ default: () => <m.ListPage list="watchlist" /> })));
const FavoritesPage = lazyPage(() => import('./pages/ListPage').then((m) => ({ default: () => <m.ListPage list="favorite" /> })));
const HistoryPage = lazyPage(() => import('./pages/ListPage').then((m) => ({ default: m.HistoryPage })));
const SettingsPage = lazyPage(() => import('./pages/SettingsPage'));
const AdminPage = lazyPage(() => import('./pages/AdminPage'));
const MetadataSettingsPage = lazyPage(() => import('./pages/MetadataSettingsPage'));
const BackupsPage = lazyPage(() => import('./pages/BackupsPage'));
const LibrariesPage = lazyPage(() => import('./pages/LibrariesPage'));
const PermissionsPage = lazyPage(() => import('./pages/PermissionsPage'));

/** Shown for the instant a lazily loaded page is fetched. */
function RouteFallback() {
  return (
    <p className="visually-hidden" role="status">
      Loading…
    </p>
  );
}

export default function App() {
  return (
    <LibrariesProvider>
      <ToastProvider>
        <Suspense fallback={<RouteFallback />}>
          <Routes>
            <Route path="/setup" element={<SetupPage />} />
            <Route path="/signup" element={<SetupPage />} />
            <Route path="/login" element={<LoginPage />} />

            <Route element={<RequireAuth />}>
              <Route path="/403" element={<ForbiddenPage />} />
              {/* The player takes the whole screen, outside the shell. */}
              <Route path="/play/:lib/:type/:id" element={<PlayerPage />} />
              <Route element={<AppShell />}>
                <Route path="/" element={<HomePage />} />
                <Route path="/movies" element={<BrowsePage type="movie" />} />
                <Route path="/shows" element={<BrowsePage type="show" />} />
                <Route path="/movie/:lib/:id" element={<MovieDetailPage />} />
                <Route path="/show/:lib/:id" element={<ShowDetailPage />} />
                <Route path="/episode/:lib/:id" element={<EpisodeDetailPage />} />
                <Route path="/person/:lib/:id" element={<PersonPage />} />
                <Route path="/search" element={<SearchPage />} />

                <Route path="/collections" element={<CollectionsPage />} />
                <Route path="/collections/:id" element={<CollectionDetailPage />} />
                <Route path="/playlists" element={<PlaylistsPage />} />
                <Route path="/playlists/:id" element={<PlaylistDetailPage />} />
                <Route path="/watchlist" element={<WatchlistPage />} />
                <Route path="/favorites" element={<FavoritesPage />} />
                <Route path="/history" element={<HistoryPage />} />
                <Route path="/settings" element={<SettingsPage />} />

                <Route path="/admin" element={<AdminPage />} />
                <Route path="/admin/metadata" element={<MetadataSettingsPage />} />
                <Route path="/libraries" element={<LibrariesPage />} />
                <Route path="/permissions" element={<PermissionsPage />} />
                <Route path="/backups" element={<BackupsPage />} />
              </Route>
            </Route>

            <Route path="*" element={<Navigate to="/" replace />} />
          </Routes>
        </Suspense>
      </ToastProvider>
    </LibrariesProvider>
  );
}
