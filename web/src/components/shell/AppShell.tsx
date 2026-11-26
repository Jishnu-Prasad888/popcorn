import { useEffect, useRef, useState } from 'react';
import type { FormEvent } from 'react';
import { NavLink, Outlet, useLocation, useNavigate, useSearchParams } from 'react-router-dom';
import {
  Clapperboard,
  Clock3,
  Cog,
  Database,
  Film,
  Heart,
  HardDrive,
  Home,
  Layers,
  ListVideo,
  LogOut,
  PanelLeftClose,
  PanelLeftOpen,
  Search,
  ShieldCheck,
  Bookmark,
  Tv,
  Archive,
  Activity,
  Upload,
} from 'lucide-react';
import type { LucideIcon } from 'lucide-react';

import { useLibraries } from '../../api/libraries';
import { useAuth } from '../../auth/authContext';
import { UploadDialog } from '../uploads/UploadDialog';
import { UploadTray } from '../uploads/UploadTray';
import { usePreferences } from '../../lib/preferences';
import './AppShell.css';

interface NavItem {
  to: string;
  label: string;
  icon: LucideIcon;
  end?: boolean | undefined;
}

const BROWSE: NavItem[] = [
  { to: '/', label: 'Home', icon: Home, end: true },
  { to: '/movies', label: 'Movies', icon: Film },
  { to: '/shows', label: 'TV shows', icon: Tv },
  { to: '/search', label: 'Search', icon: Search },
];

const YOURS: NavItem[] = [
  { to: '/collections', label: 'Collections', icon: Layers },
  { to: '/playlists', label: 'Playlists', icon: ListVideo },
  { to: '/watchlist', label: 'Watchlist', icon: Bookmark },
  { to: '/favorites', label: 'Favourites', icon: Heart },
  { to: '/history', label: 'History', icon: Clock3 },
];

const ADMIN: NavItem[] = [
  { to: '/admin', label: 'Dashboard', icon: Activity, end: true },
  { to: '/libraries', label: 'Libraries', icon: HardDrive },
  { to: '/admin/metadata', label: 'Metadata', icon: Database },
  { to: '/permissions', label: 'Users & access', icon: ShieldCheck },
  { to: '/backups', label: 'Backups', icon: Archive },
];

/** Matches the stylesheet's breakpoint for the drawer layout. */
const NARROW = '(max-width: 899px)';
const COLLAPSED_KEY = 'popcorn.sidebar.collapsed';

function readCollapsed(): boolean {
  try {
    return localStorage.getItem(COLLAPSED_KEY) === '1';
  } catch {
    return false;
  }
}

function storeCollapsed(v: boolean) {
  try {
    if (v) localStorage.setItem(COLLAPSED_KEY, '1');
    else localStorage.removeItem(COLLAPSED_KEY);
  } catch {
    // Only means the choice isn't remembered.
  }
}

function NavGroup({
  title,
  items,
  onNavigate,
}: {
  title?: string;
  items: NavItem[];
  onNavigate?: () => void;
}) {
  return (
    <div className="nav-group">
      {title && <p className="nav-group-title">{title}</p>}
      <ul>
        {items.map((it) => (
          <li key={it.to}>
            <NavLink to={it.to} end={it.end ?? false} className="nav-link" onClick={onNavigate}>
              <it.icon size={18} strokeWidth={1.75} aria-hidden="true" />
              <span>{it.label}</span>
            </NavLink>
          </li>
        ))}
      </ul>
    </div>
  );
}

/** The wordmark: the name set in the display face with a signal underline. */
export function Wordmark() {
  return (
    <span className="wordmark">
      <Clapperboard size={20} strokeWidth={2} aria-hidden="true" />
      popcorn
    </span>
  );
}

function SearchBar() {
  const navigate = useNavigate();
  const location = useLocation();
  const [params] = useSearchParams();
  const onSearch = location.pathname === '/search';
  const [value, setValue] = useState(onSearch ? (params.get('q') ?? '') : '');
  const inputRef = useRef<HTMLInputElement>(null);

  // "/" focuses search from anywhere outside a text field.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const t = e.target as HTMLElement;
      if (e.key === '/' && !/^(INPUT|TEXTAREA|SELECT)$/.test(t.tagName) && !t.isContentEditable) {
        e.preventDefault();
        inputRef.current?.focus();
      }
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, []);

  // Typing on the search page updates results as you go.
  useEffect(() => {
    if (!onSearch) return;
    const t = window.setTimeout(() => {
      if ((params.get('q') ?? '') !== value)
        navigate(`/search?q=${encodeURIComponent(value)}`, { replace: true });
    }, 250);
    return () => window.clearTimeout(t);
  }, [value, onSearch, params, navigate]);

  const submit = (e: FormEvent) => {
    e.preventDefault();
    navigate(`/search?q=${encodeURIComponent(value.trim())}`);
  };

  return (
    <form className="shell-search" role="search" onSubmit={submit}>
      <Search size={17} aria-hidden="true" />
      <input
        ref={inputRef}
        type="search"
        value={value}
        onChange={(e) => setValue(e.target.value)}
        placeholder="Search titles, people, genres"
        aria-label="Search titles, people and genres"
      />
      <kbd aria-hidden="true">/</kbd>
    </form>
  );
}

export default function AppShell() {
  const { user, logout } = useAuth();
  const isAdmin = user?.role === 'admin';
  const [drawer, setDrawer] = useState(false);
  // On wide screens the sidebar is part of the layout and can be collapsed;
  // on narrow ones it is a drawer over the page. One pair of buttons drives both.
  const [collapsed, setCollapsed] = useState(() => readCollapsed());
  const location = useLocation();
  usePreferences(); // applies interface preferences once signed in
  const { libraries } = useLibraries();
  const [uploadOpen, setUploadOpen] = useState(false);

  // The drawer closes whenever the route changes.
  const [drawerPath, setDrawerPath] = useState(location.pathname);
  if (drawerPath !== location.pathname) {
    setDrawerPath(location.pathname);
    setDrawer(false);
  }

  const close = () => setDrawer(false);
  const narrow = () => window.matchMedia(NARROW).matches;
  const setSidebar = (open: boolean) => {
    if (narrow()) {
      setDrawer(open);
      return;
    }
    setCollapsed(!open);
    storeCollapsed(!open);
  };
  const sidebarOpen = drawer || !collapsed;

  return (
    <div
      className="shell"
      data-testid="app-shell"
      data-drawer={drawer ? 'open' : 'closed'}
      data-collapsed={collapsed}
    >
      <a className="skip-link" href="#main">
        Skip to content
      </a>
      <aside className="shell-sidebar" aria-label="Main navigation">
        <div className="shell-brand">
          <NavLink to="/" className="shell-brand-link" aria-label="Popcorn home">
            <Wordmark />
          </NavLink>
          <button
            type="button"
            className="icon-button shell-drawer-close"
            onClick={() => setSidebar(false)}
            aria-label="Hide sidebar"
            title="Hide sidebar"
          >
            <PanelLeftClose size={20} />
          </button>
        </div>
        <nav className="shell-nav">
          <NavGroup items={BROWSE} onNavigate={close} />
          <NavGroup title="Yours" items={YOURS} onNavigate={close} />
          {isAdmin && <NavGroup title="Server" items={ADMIN} onNavigate={close} />}
        </nav>
        <div className="shell-account">
          <NavLink to="/settings" className="nav-link" onClick={close}>
            <Cog size={18} strokeWidth={1.75} aria-hidden="true" />
            <span>Settings</span>
          </NavLink>
          <div className="shell-user">
            <span className="shell-avatar" aria-hidden="true">
              {user?.username.slice(0, 1).toUpperCase()}
            </span>
            <span className="shell-username">{user?.username}</span>
            <button
              type="button"
              className="icon-button"
              onClick={() => void logout()}
              aria-label="Sign out"
              title="Sign out"
            >
              <LogOut size={17} />
            </button>
          </div>
        </div>
      </aside>
      <div className="shell-scrim" onClick={close} aria-hidden="true" />
      <div className="shell-main">
        <header className="shell-topbar">
          <button
            type="button"
            className="icon-button shell-menu"
            onClick={() => setSidebar(true)}
            aria-label="Show sidebar"
            title="Show sidebar"
            aria-expanded={sidebarOpen}
          >
            <PanelLeftOpen size={22} />
          </button>
          <span className="shell-topbar-brand">
            <Wordmark />
          </span>
          <SearchBar />
          {libraries.length > 0 && (
            <button
              type="button"
              className="button shell-upload"
              onClick={() => setUploadOpen(true)}
              title="Upload files or folders"
              data-testid="upload-button"
            >
              <Upload size={17} aria-hidden="true" />
              <span>Upload</span>
            </button>
          )}
        </header>
        <main id="main" className="shell-content" tabIndex={-1}>
          <Outlet />
        </main>
      </div>
      <UploadTray />
      <UploadDialog open={uploadOpen} onClose={() => setUploadOpen(false)} />
      <nav className="shell-bottom-nav" aria-label="Quick navigation">
        {[...BROWSE.slice(0, 3), ...YOURS.slice(0, 1), ...YOURS.slice(2, 3)].map((it) => (
          <NavLink key={it.to} to={it.to} end={it.end ?? false} className="bottom-link">
            <it.icon size={20} strokeWidth={1.75} aria-hidden="true" />
            <span>{it.label}</span>
          </NavLink>
        ))}
      </nav>
    </div>
  );
}
