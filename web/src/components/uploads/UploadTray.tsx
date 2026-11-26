/**
 * The upload tray: a panel in the bottom corner, like a file-sync app's, with
 * one row per file. Each row shows where the file is going, how far along it
 * is, and — when it fails — why, with a retry where retrying could help.
 *
 * The list scrolls inside a fixed maximum height, so a folder of a thousand
 * files never takes over the page. The header summarises the whole batch and
 * the panel can be collapsed to just that header.
 */

import { useEffect, useRef, useState } from 'react';
import {
  AlertCircle,
  Ban,
  CheckCircle2,
  ChevronDown,
  ChevronUp,
  Clock3,
  FileVideo,
  File as FileIcon,
  RotateCcw,
  X,
} from 'lucide-react';

import { ConfirmDialog } from '../Dialog';
import { useUploads } from '../../uploads/UploadProvider';
import { formatBytes, formatDuration, parentOf } from '../../uploads/uploads';
import type { UploadItem } from '../../uploads/uploads';
import './UploadTray.css';

const VIDEO = /\.(mkv|mp4|m4v|avi|mov|wmv|webm|ts|m2ts|mpg|mpeg|flv|ogv)$/i;

/** Smoothed bytes per second across every running upload. */
function useThroughput(items: UploadItem[], active: boolean): number {
  const [rate, setRate] = useState(0);
  const sample = useRef<{ at: number; bytes: number } | null>(null);
  const itemsRef = useRef(items);
  useEffect(() => {
    itemsRef.current = items;
  }, [items]);

  useEffect(() => {
    if (!active) {
      sample.current = null;
      return;
    }
    const tick = () => {
      const bytes = itemsRef.current.reduce(
        (n, it) => n + (it.status === 'uploading' || it.status === 'done' ? it.loaded : 0),
        0,
      );
      const now = performance.now();
      const prev = sample.current;
      sample.current = { at: now, bytes };
      if (!prev) return;
      const delta = bytes - prev.bytes;
      // A retry or a cleared row makes the total jump down; skip that tick.
      if (delta < 0) return;
      const instant = delta / ((now - prev.at) / 1000);
      setRate((r) => (r === 0 ? instant : r * 0.7 + instant * 0.3));
    };
    tick();
    const t = window.setInterval(tick, 1000);
    return () => window.clearInterval(t);
  }, [active]);

  return active ? rate : 0;
}

function StatusIcon({ item }: { item: UploadItem }) {
  switch (item.status) {
    case 'done':
      return <CheckCircle2 className="upload-status-icon is-done" size={18} aria-hidden="true" />;
    case 'failed':
      return <AlertCircle className="upload-status-icon is-failed" size={18} aria-hidden="true" />;
    case 'cancelled':
      return <Ban className="upload-status-icon is-muted" size={18} aria-hidden="true" />;
    case 'queued':
      return <Clock3 className="upload-status-icon is-muted" size={18} aria-hidden="true" />;
    case 'uploading': {
      const pct = item.size ? item.loaded / item.size : 0;
      const c = 2 * Math.PI * 8;
      return (
        <svg className="upload-ring" viewBox="0 0 20 20" width="20" height="20" aria-hidden="true">
          <circle cx="10" cy="10" r="8" className="upload-ring-track" />
          <circle
            cx="10"
            cy="10"
            r="8"
            className="upload-ring-fill"
            strokeDasharray={c}
            strokeDashoffset={c * (1 - pct)}
          />
        </svg>
      );
    }
  }
}

function describe(item: UploadItem): string {
  const where = parentOf(item.destPath);
  const place = where ? `${item.libraryName} / ${where}` : item.libraryName;
  switch (item.status) {
    case 'queued':
      return `Waiting · ${formatBytes(item.size)} · ${place}`;
    case 'uploading':
      return `${formatBytes(item.loaded)} of ${formatBytes(item.size)} · ${place}`;
    case 'done':
      return `Uploaded to ${place}`;
    case 'cancelled':
      return 'Cancelled';
    case 'failed':
      return item.error ?? 'The upload failed.';
  }
}

function UploadRow({ item }: { item: UploadItem }) {
  const { cancel, retry } = useUploads();
  const Icon = VIDEO.test(item.name) ? FileVideo : FileIcon;
  const pct = item.size ? Math.round((item.loaded / item.size) * 100) : 0;
  const canCancel = item.status === 'queued' || item.status === 'uploading';
  const canRetry =
    item.status === 'cancelled' || (item.status === 'failed' && item.retryable !== false);

  return (
    <li className="upload-row" data-status={item.status} title={item.destPath}>
      <Icon className="upload-file-icon" size={20} strokeWidth={1.6} aria-hidden="true" />
      <div className="upload-row-text">
        <span className="upload-name">{item.name}</span>
        <span className="upload-meta" role={item.status === 'failed' ? 'alert' : undefined}>
          {item.status === 'failed' && <span className="visually-hidden">Failed: </span>}
          {describe(item)}
        </span>
      </div>
      <div className="upload-row-end">
        <span className="upload-row-state">
          {item.status === 'uploading' && <span className="upload-pct">{pct}%</span>}
          <StatusIcon item={item} />
        </span>
        {canCancel && (
          <button
            type="button"
            className="upload-row-action"
            onClick={() => cancel(item.id)}
            aria-label={`Cancel ${item.name}`}
            title="Cancel"
          >
            <X size={16} />
          </button>
        )}
        {canRetry && (
          <button
            type="button"
            className="upload-row-action"
            onClick={() => retry(item.id)}
            aria-label={`Retry ${item.name}`}
            title="Retry"
          >
            <RotateCcw size={15} />
          </button>
        )}
      </div>
      {item.status === 'uploading' && (
        <span className="upload-row-bar" aria-hidden="true">
          <span style={{ width: `${pct}%` }} />
        </span>
      )}
    </li>
  );
}

export function UploadTray() {
  const { items, cancelAll, retryFailed, clearFinished, dismissAll } = useUploads();
  const [collapsed, setCollapsed] = useState(false);
  const [confirmClose, setConfirmClose] = useState(false);

  const counts = { queued: 0, uploading: 0, done: 0, failed: 0, cancelled: 0 };
  let total = 0;
  let sent = 0;
  for (const it of items) {
    counts[it.status]++;
    if (it.status === 'queued' || it.status === 'uploading') {
      total += it.size;
      sent += it.loaded;
    }
  }
  const remaining = counts.queued + counts.uploading;
  const active = remaining > 0;
  const rate = useThroughput(items, active);
  const retryable = items.filter((it) => it.status === 'failed' && it.retryable !== false).length;

  // A new batch opens the panel again.
  const [seen, setSeen] = useState(items.length);
  if (items.length !== seen) {
    if (items.length > seen) setCollapsed(false);
    setSeen(items.length);
  }

  if (items.length === 0) return null;

  let title: string;
  if (active) title = `Uploading ${remaining} ${remaining === 1 ? 'file' : 'files'}`;
  else if (counts.failed > 0) title = `${counts.done} uploaded, ${counts.failed} failed`;
  else if (counts.done > 0)
    title = `${counts.done} ${counts.done === 1 ? 'upload' : 'uploads'} complete`;
  else title = 'Uploads cancelled';

  const eta = active && rate > 0 ? formatDuration((total - sent) / rate) : '';
  const overall = total ? Math.round((sent / total) * 100) : 0;

  const close = () => {
    if (active) setConfirmClose(true);
    else clearFinished();
  };

  return (
    <section
      className="upload-tray"
      aria-label="Uploads"
      data-collapsed={collapsed}
      data-testid="upload-tray"
    >
      <header className="upload-tray-head">
        <h2 className="upload-tray-title" aria-live="polite">
          {title}
        </h2>
        <button
          type="button"
          className="upload-head-button"
          onClick={() => setCollapsed((c) => !c)}
          aria-expanded={!collapsed}
          aria-label={collapsed ? 'Show uploads' : 'Hide uploads'}
          title={collapsed ? 'Show uploads' : 'Hide uploads'}
        >
          {collapsed ? <ChevronUp size={18} /> : <ChevronDown size={18} />}
        </button>
        <button
          type="button"
          className="upload-head-button"
          onClick={close}
          aria-label="Close uploads"
          title="Close"
        >
          <X size={18} />
        </button>
      </header>

      {active && (
        <div
          className="upload-tray-progress"
          role="progressbar"
          aria-label="Overall upload progress"
          aria-valuemin={0}
          aria-valuemax={100}
          aria-valuenow={overall}
        >
          <span style={{ width: `${overall}%` }} />
        </div>
      )}

      {!collapsed && (
        <>
          <div className="upload-tray-bar">
            <span className="upload-tray-summary">
              {active
                ? `${formatBytes(sent)} of ${formatBytes(total)}${eta ? ` · ${eta} left` : ''}`
                : `${items.length} ${items.length === 1 ? 'file' : 'files'}`}
            </span>
            {retryable > 0 && (
              <button type="button" className="link-button" onClick={retryFailed}>
                Retry {retryable} failed
              </button>
            )}
            {active ? (
              <button type="button" className="link-button" onClick={cancelAll}>
                Cancel all
              </button>
            ) : (
              <button type="button" className="link-button" onClick={clearFinished}>
                Clear
              </button>
            )}
          </div>
          <ul className="upload-list">
            {items.map((it) => (
              <UploadRow key={it.id} item={it} />
            ))}
          </ul>
        </>
      )}

      <ConfirmDialog
        open={confirmClose}
        title="Cancel uploads?"
        message={`${remaining} ${remaining === 1 ? 'file hasn’t' : 'files haven’t'} finished uploading. Closing cancels ${remaining === 1 ? 'it' : 'them'}.`}
        confirmLabel="Cancel uploads"
        cancelLabel="Keep uploading"
        destructive
        onCancel={() => setConfirmClose(false)}
        onConfirm={() => {
          setConfirmClose(false);
          dismissAll();
        }}
      />
    </section>
  );
}
