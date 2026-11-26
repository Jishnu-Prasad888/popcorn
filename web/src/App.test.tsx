import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, describe, expect, it, vi } from 'vitest';

import App from './App';
import AuthProvider from './auth/AuthProvider';

const admin = {
  id: 'u1',
  username: 'jishnu',
  role: 'admin' as const,
  created_at: '2026-01-01T00:00:00Z',
};

const originalFetch = globalThis.fetch;

function json(data: unknown, status = 200): Response {
  return new Response(JSON.stringify(data), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

/** Mock the handful of endpoints the shell and Home page touch. `usersStatus`
 * lets a test make a page request fail with 403. */
function mockFetch(options: {
  authenticated: boolean;
  bootstrapRequired?: boolean;
  usersStatus?: number;
}) {
  const fn = vi.fn(async (input: URL | RequestInfo) => {
    const url = String(input);
    if (url.endsWith('/api/v1/auth/status')) {
      return json({
        bootstrap_required: options.bootstrapRequired ?? false,
        authenticated: options.authenticated,
        ...(options.authenticated ? { user: admin } : {}),
      });
    }
    if (url.endsWith('/api/v1/users')) {
      if (options.usersStatus && options.usersStatus !== 200) {
        return json(
          {
            error: {
              code: 'FORBIDDEN',
              message: 'You are not allowed to list accounts.',
              request_id: 'mock',
            },
          },
          options.usersStatus,
        );
      }
      return json({ users: [admin] });
    }
    if (url.endsWith('/api/v1/health')) {
      return json({ status: 'ok', database: 'ok' });
    }
    if (url.endsWith('/api/v1/me/preferences')) {
      return json({
        playback: {
          audio_languages: ['eng'],
          subtitle_languages: ['eng'],
          subtitle_mode: 'auto',
          prefer_original_audio: false,
          prefer_sdh: false,
          autoplay_next: true,
          next_countdown: 10,
          skip_intro: 'button',
          skip_credits: 'button',
          quality: 'original',
          max_bitrate: 0,
          speed: 1,
          skip_forward: 30,
          skip_backward: 10,
          passthrough: [],
          subtitle_style: {
            size: 100,
            color: '#ffffff',
            background: 'shadow',
            opacity: 60,
            position: 8,
            font: 'sans',
          },
        },
        interface: {
          density: 'comfortable',
          card_size: 'medium',
          view: 'grid',
          motion: 'full',
          accent: 'purple',
          home_rows: [],
        },
      });
    }
    if (url.endsWith('/api/v1/home')) {
      return json({ rows: [], collections: [], counts: { movies: 0, shows: 0 } });
    }
    if (url.endsWith('/api/v1/libraries')) {
      return json({ libraries: [] });
    }
    if (url.endsWith('/api/v1/version')) {
      return json({ version: '0.1.0', commit: 'abc', built_at: '2026-01-01T00:00:00Z' });
    }
    return json({ error: { code: 'NOT_FOUND', message: 'missing', request_id: 'mock' } }, 404);
  });
  globalThis.fetch = fn as unknown as typeof fetch;
  return fn;
}

function setup(path: string) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <AuthProvider>
        <App />
      </AuthProvider>
    </MemoryRouter>,
  );
}

describe('App routing', () => {
  afterEach(() => {
    globalThis.fetch = originalFetch;
    vi.restoreAllMocks();
  });

  it('sends an anonymous deep link to the sign-in page', async () => {
    mockFetch({ authenticated: false });
    setup('/movies');

    expect(await screen.findByRole('heading', { name: 'Sign in to Popcorn' })).toBeInTheDocument();
  });

  it('sends a brand-new server to the first-run setup page', async () => {
    mockFetch({ authenticated: false, bootstrapRequired: true });
    setup('/movies');

    expect(
      await screen.findByRole('heading', { name: 'Create your admin account' }),
    ).toBeInTheDocument();
  });

  it('serves the setup page on both /setup and /signup', async () => {
    mockFetch({ authenticated: false, bootstrapRequired: true });
    setup('/signup');

    expect(
      await screen.findByRole('heading', { name: 'Create your admin account' }),
    ).toBeInTheDocument();
  });

  it('renders the settings page inside the shell for a signed-in admin', async () => {
    mockFetch({ authenticated: true });
    setup('/settings');

    expect(await screen.findByRole('heading', { name: 'Settings' })).toBeInTheDocument();
    expect(await screen.findByTestId('app-shell')).toBeInTheDocument();
    expect(screen.getAllByRole('link', { name: 'Settings' }).length).toBeGreaterThan(0);
    expect(screen.getByRole('button', { name: 'Sign out' })).toBeInTheDocument();
    expect(screen.getAllByText('jishnu').length).toBeGreaterThan(0);
  });

  it('shows the access-denied page when a page request is refused', async () => {
    mockFetch({ authenticated: true, usersStatus: 403 });
    setup('/permissions');

    expect(await screen.findByTestId('forbidden-page')).toBeInTheDocument();
    expect(screen.getByText('You are not allowed to list accounts.')).toBeInTheDocument();
  });

  it('falls back to home for unknown routes', async () => {
    mockFetch({ authenticated: true });
    setup('/does-not-exist');

    expect(await screen.findByTestId('app-shell')).toBeInTheDocument();
    expect(
      await screen.findByRole('heading', { name: 'Nothing to watch yet' }),
    ).toBeInTheDocument();
  });
});
