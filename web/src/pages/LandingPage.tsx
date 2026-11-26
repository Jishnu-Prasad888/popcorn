import { Link } from 'react-router-dom';

import { Wordmark } from '../components/shell/AppShell';
import './LandingPage.css';

/**
 * Public page at /landing: what Popcorn is, and why it exists.
 *
 * Built around one picture from the subject itself: a title paused partway
 * through, the way the home screen shows it, with the resume bar and the
 * next episode lined up. The rest stays quiet: why it was made, three
 * promises, what it does, and where your watching data lives.
 *
 * Outside the app shell. Links go only to routes that exist: /setup and
 * /login.
 */

interface Group {
  title: string;
  lines: string[];
}

const WHAT_IT_DOES: Group[] = [
  {
    title: 'Watch',
    lines: [
      'Movies and TV in the browser, on any screen in the house.',
      'Pick up where you stopped, on whichever device you’re on.',
      'Subtitles and audio tracks, converted on the fly when a browser can’t play a file.',
    ],
  },
  {
    title: 'Organise',
    lines: [
      'Posters, plots, cast and episode guides, matched for you.',
      'Collections, playlists, a watchlist and favourites.',
      'Fix a wrong match or pick your own artwork.',
    ],
  },
  {
    title: 'Add',
    lines: [
      'Upload files or whole folders from the browser.',
      'Or point it at the drives you already have.',
      'Seasons and episodes sorted out from folder names.',
    ],
  },
  {
    title: 'Keep',
    lines: [
      'An account for everyone at home, each with their own history.',
      'Decide who sees which library.',
      'Back up the server and every library, then restore it.',
    ],
  },
];

function NowPlaying() {
  return (
    <figure
      className="landing-figure"
      aria-label="A show paused partway through an episode, as Popcorn shows it"
    >
      <div className="landing-screen">
        <div className="landing-frame" aria-hidden="true">
          <span className="landing-sun" />
          <span className="landing-hills" />
        </div>
        <div className="landing-screen-body">
          <p className="landing-eyebrow">Continue watching</p>
          <p className="landing-show">The Long Coast</p>
          <p className="landing-episode">S1 · E4 — “Low Tide”</p>
          <div className="landing-resume">
            <span className="landing-resume-bar" aria-hidden="true">
              <span />
            </span>
            <span className="landing-resume-time">31 min left</span>
          </div>
        </div>
      </div>
      <ul className="landing-up-next" aria-label="Up next">
        <li>
          <span className="landing-thumb" aria-hidden="true" />
          <span>
            <strong>E5 — “Breakwater”</strong>
            <small>Up next · 48 min</small>
          </span>
        </li>
        <li>
          <span className="landing-thumb is-alt" aria-hidden="true" />
          <span>
            <strong>E6 — “Harbour Lights”</strong>
            <small>52 min</small>
          </span>
        </li>
      </ul>
    </figure>
  );
}

export default function LandingPage() {
  return (
    <div className="landing" data-testid="landing-page">
      <header className="landing-nav">
        <Link to="/landing" className="landing-nav-brand" aria-label="Popcorn">
          <Wordmark />
        </Link>
        <Link to="/login" className="button ghost-button">
          Sign in
        </Link>
      </header>

      <main>
        <section className="landing-hero" aria-labelledby="landing-title">
          <div className="landing-hero-copy">
            <h1 id="landing-title" className="landing-title">
              Your movies. <span className="landing-title-accent">Your server.</span>
            </h1>
            <p className="landing-lede">
              Popcorn turns the films and shows you already have into a streaming service of your
              own, running on a computer at home. It remembers where everyone stopped, and nobody
              else is watching.
            </p>
            <div className="landing-actions">
              <Link to="/setup" className="button primary-button landing-cta">
                Set up Popcorn
              </Link>
              <Link to="/login" className="button landing-cta">
                Sign in
              </Link>
            </div>
          </div>
          <NowPlaying />
        </section>

        <section className="landing-why" aria-labelledby="landing-why-title">
          <h2 id="landing-why-title" className="landing-heading">
            Why Popcorn exists
          </h2>
          <div className="landing-essay">
            <p>
              A film collection ends up scattered: rips on one drive, downloads on another, a few
              old DVDs’ worth on a laptop nobody uses. Meanwhile the streaming services rotate their
              catalogues, raise their prices, and forget what you were halfway through the moment a
              title leaves.
            </p>
            <p>
              Popcorn puts the collection you own back in one place that belongs to you. It plays
              your files where they already are, and keeps your watch history, lists and artwork
              beside them, not on someone else’s server.
            </p>
          </div>

          <dl className="landing-promises">
            <div>
              <dt>Your files stay where they are.</dt>
              <dd>
                Popcorn reads your folders as they are. Nothing is renamed or re-encoded, and every
                file still plays without it.
              </dd>
            </div>
            <div>
              <dt>Everyone gets their own place.</dt>
              <dd>
                Each person has their own progress, history and watchlist, and sees only the
                libraries you share with them.
              </dd>
            </div>
            <div>
              <dt>It runs on your own computer.</dt>
              <dd>No subscription, no ads, no account with anyone else.</dd>
            </div>
          </dl>
        </section>

        <section className="landing-does" aria-labelledby="landing-does-title">
          <h2 id="landing-does-title" className="landing-heading">
            What it does
          </h2>
          <div className="landing-groups">
            {WHAT_IT_DOES.map((group) => (
              <div key={group.title} className="landing-group">
                <h3>{group.title}</h3>
                <ul>
                  {group.lines.map((line) => (
                    <li key={line}>{line}</li>
                  ))}
                </ul>
              </div>
            ))}
          </div>
        </section>

        <section className="landing-portable" aria-labelledby="landing-portable-title">
          <div className="landing-portable-copy">
            <h2 id="landing-portable-title" className="landing-heading">
              Take it with you
            </h2>
            <p>
              What Popcorn knows about a library lives in one small folder next to the films. Move
              the drive to another computer running Popcorn and the posters, matches and artwork
              come with it.
            </p>
          </div>
          <pre
            className="landing-tree"
            aria-label="A library folder: your films and shows, plus a .popcorn folder holding its catalogue"
          >
            {`Media/
├── Movies/
│   └── Arrival (2016).mkv
├── Shows/
│   └── The Long Coast/
│       └── Season 1/
└── .popcorn/   `}
            <span className="landing-tree-note">catalogue, artwork, matches</span>
          </pre>
        </section>

        <section className="landing-closing" aria-labelledby="landing-closing-title">
          <h2 id="landing-closing-title" className="landing-heading">
            Start your library
          </h2>
          <p>Set it up in a few minutes, on a computer you already have.</p>
          <div className="landing-actions">
            <Link to="/setup" className="button primary-button landing-cta">
              Set up Popcorn
            </Link>
            <Link to="/login" className="button landing-cta">
              Sign in
            </Link>
          </div>
        </section>
      </main>

      <footer className="landing-footer">
        <Wordmark />
        <span>Your own movie and TV server, kept at home.</span>
      </footer>
    </div>
  );
}
