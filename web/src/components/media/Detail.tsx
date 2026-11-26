import type { ReactNode } from 'react';
import { Link } from 'react-router-dom';
import { User } from 'lucide-react';

import { backdropOf, logoOf, personImageUrl, posterOf } from '../../api/media';
import type { Card, Credit, FileInfo } from '../../api/media';
import { audioLabel, codecName } from '../../lib/mediaFormat';
import { formatBytes } from '../../api/types';
import { Facts } from './Hero';
import { useCardMenu, usePersonMenu } from './CardMenu';
import './detail.css';

/** The top of a detail page: backdrop, poster, title, facts and actions. */
export function DetailHeader({
  item,
  eyebrow,
  actions,
  children,
  onChanged,
}: {
  item: Card;
  eyebrow?: ReactNode;
  actions?: ReactNode;
  children?: ReactNode;
  /** Called after the right-click menu changes the item, to reload the page. */
  onChanged?: () => void;
}) {
  const { onContextMenu, element } = useCardMenu(item, { onChanged });
  const backdrop = backdropOf(item);
  const poster = posterOf(item);
  const logo = logoOf(item);
  return (
    <section className="detail-head" onContextMenu={onContextMenu}>
      {element}
      <div className="detail-backdrop" aria-hidden="true">
        {backdrop && <img src={backdrop} alt="" />}
      </div>
      <div className="detail-head-inner">
        <div className="detail-poster">
          {poster ? <img src={poster} alt="" /> : <span className="card-fallback" />}
        </div>
        <div className="detail-main">
          {eyebrow && <div className="detail-eyebrow">{eyebrow}</div>}
          {logo ? (
            <>
              <img className="detail-logo" src={logo} alt="" />
              <h1 className="visually-hidden">{item.title}</h1>
            </>
          ) : (
            <h1 className="detail-title">{item.title}</h1>
          )}
          {item.original_title && item.original_title !== item.title && (
            <p className="detail-original">{item.original_title}</p>
          )}
          <Facts c={item} />
          {item.tagline && <p className="detail-tagline">{item.tagline}</p>}
          {item.overview && <p className="detail-overview">{item.overview}</p>}
          {actions && <div className="detail-actions">{actions}</div>}
          {children}
        </div>
      </div>
    </section>
  );
}

/** Directors, writers and creators as a definition list. */
export function Crew({ credits, libraryId }: { credits: Credit[]; libraryId: string }) {
  const groups: [string, string[]][] = [
    ['Directed by', ['director']],
    ['Created by', ['creator']],
    ['Written by', ['writer']],
  ];
  const rows = groups
    .map(([label, roles]) => [label, credits.filter((c) => roles.includes(c.role))] as const)
    .filter(([, list]) => list.length > 0);
  if (!rows.length) return null;
  return (
    <dl className="crew">
      {rows.map(([label, list]) => (
        <div key={label}>
          <dt>{label}</dt>
          <dd>
            {list.slice(0, 4).map((c, i) => (
              <span key={c.person_id}>
                {i > 0 && ', '}
                <Link to={`/person/${libraryId}/${c.person_id}`}>{c.name}</Link>
              </span>
            ))}
          </dd>
        </div>
      ))}
    </dl>
  );
}

/** The cast as a row of photos. */
export function CastRow({ credits, libraryId }: { credits: Credit[]; libraryId: string }) {
  const cast = credits.filter((c) => c.role === 'cast').slice(0, 30);
  const { openPerson, element } = usePersonMenu(libraryId);
  if (!cast.length) return null;
  return (
    <section className="detail-section">
      {element}
      <h2 className="section-title">Cast</h2>
      <ul className="cast-row">
        {cast.map((c) => (
          <li key={c.person_id + (c.character ?? '')} onContextMenu={(e) => openPerson(e, c)}>
            <Link to={`/person/${libraryId}/${c.person_id}`} className="cast-card">
              <span className="cast-photo">
                {c.profile_url ? (
                  <img src={personImageUrl(libraryId, c.person_id)} alt="" loading="lazy" />
                ) : (
                  <User size={26} strokeWidth={1.5} />
                )}
              </span>
              <span className="cast-name">{c.name}</span>
              {c.character && <span className="cast-character">{c.character}</span>}
            </Link>
          </li>
        ))}
      </ul>
    </section>
  );
}

/** Versions, streams and subtitles of each file. */
export function TechInfo({ files }: { files: FileInfo[] }) {
  if (!files.length) return null;
  return (
    <section className="detail-section">
      <h2 className="section-title">{files.length > 1 ? `${files.length} versions` : 'File'}</h2>
      <div className="tech-files">
        {files.map((f) => {
          const video = f.streams?.find((s) => s.kind === 'video');
          const audio = f.streams?.filter((s) => s.kind === 'audio') ?? [];
          const subs = [
            ...(f.streams
              ?.filter((s) => s.kind === 'subtitle')
              .map((s) => ({
                key: `e${s.type_index}`,
                label: s.language_name || 'Unknown',
                flags: [
                  s.is_forced && 'Forced',
                  s.is_hearing_impaired && 'SDH',
                  codecName(s.codec),
                ],
              })) ?? []),
            ...(f.subtitles?.map((s) => ({
              key: s.id,
              label: s.language_name || 'Unknown',
              flags: [
                s.is_forced && 'Forced',
                s.is_hearing_impaired && 'SDH',
                s.format.toUpperCase(),
                s.source === 'upload' ? 'Added' : 'External',
              ],
            })) ?? []),
          ];
          return (
            <article key={f.id} className="tech-file" data-missing={f.status !== 'present'}>
              <header>
                <strong>{f.edition || f.name}</strong>
                {f.status !== 'present' && (
                  <span className="tech-missing">Not available: the disk may be offline</span>
                )}
              </header>
              <div className="tech-chips">
                {f.resolution && <span className="tech-chip">{f.resolution}</span>}
                {f.hdr && <span className="tech-chip">{f.hdr}</span>}
                {video && <span className="tech-chip">{codecName(video.codec)}</span>}
                {f.container && (
                  <span className="tech-chip">
                    {(f.container.split(',')[0] ?? '').toUpperCase()}
                  </span>
                )}
                {f.bitrate ? (
                  <span className="tech-chip">{(f.bitrate / 1e6).toFixed(1)} Mb/s</span>
                ) : null}
                <span className="tech-chip">{formatBytes(f.size_bytes)}</span>
              </div>
              <dl className="tech-streams">
                {audio.length > 0 && (
                  <div>
                    <dt>Audio</dt>
                    <dd>
                      {audio.map((a) => (
                        <span key={a.index}>{audioLabel(a)}</span>
                      ))}
                    </dd>
                  </div>
                )}
                {subs.length > 0 && (
                  <div>
                    <dt>Subtitles</dt>
                    <dd>
                      {subs.map((s) => (
                        <span key={s.key}>
                          {s.label}
                          <span className="muted"> {s.flags.filter(Boolean).join(', ')}</span>
                        </span>
                      ))}
                    </dd>
                  </div>
                )}
                <div>
                  <dt>Path</dt>
                  <dd className="tech-path">{f.rel_path}</dd>
                </div>
                {f.probe_error && (
                  <div>
                    <dt>Probe</dt>
                    <dd className="error-text">{f.probe_error}</dd>
                  </div>
                )}
              </dl>
            </article>
          );
        })}
      </div>
    </section>
  );
}
