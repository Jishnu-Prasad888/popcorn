/* eslint-disable react-refresh/only-export-components --
 * The provider and the hook that reads it are one unit.
 */
/**
 * The upload queue, shared by the whole app so uploads keep going while the
 * person moves between pages (or opens the player).
 *
 * Files upload a few at a time; each one is its own request with its own
 * progress, so one failure never takes the rest down with it. Every file
 * keeps a row with its state and, when it fails, the reason.
 */

import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
} from 'react';
import type { ReactNode } from 'react';

import { getSystemCapabilities, uploadFileWithProgress } from '../api/queries';
import { useAuth } from '../auth/authContext';
import { describeFailure, formatBytes, isJunkFile, joinDest } from './uploads';
import type { PickedFile, UploadItem } from './uploads';

/** Uploads at once: enough to keep the link busy, few enough to finish files. */
const CONCURRENCY = 3;
const FALLBACK_LIMIT = 2 * 1024 ** 3;

export interface UploadTarget {
  libraryId: string;
  libraryName: string;
  /** Folder inside the library; "" is the top folder. */
  folder: string;
}

export interface UploadsState {
  items: UploadItem[];
  /** Bytes the server accepts per file. */
  maxBytes: number;
  /** Adds files to the queue; returns how many rows were added. */
  enqueue: (target: UploadTarget, files: PickedFile[]) => number;
  cancel: (id: string) => void;
  cancelAll: () => void;
  retry: (id: string) => void;
  retryFailed: () => void;
  /** Drops finished, failed and cancelled rows. */
  clearFinished: () => void;
  /** Cancels whatever is still running and empties the list. */
  dismissAll: () => void;
}

const UploadsContext = createContext<UploadsState | null>(null);

let nextId = 1;

export function UploadProvider({ children }: { children: ReactNode }) {
  const { user } = useAuth();
  const [items, setItems] = useState<UploadItem[]>([]);
  const [maxBytes, setMaxBytes] = useState(FALLBACK_LIMIT);
  // The queue's source of truth lives in a ref so the scheduler can read and
  // change it synchronously; every change is mirrored into state for render.
  const store = useRef<UploadItem[]>([]);
  const controllers = useRef(new Map<string, AbortController>());
  const limitRef = useRef(maxBytes);
  useLayoutEffect(() => {
    limitRef.current = maxBytes;
  }, [maxBytes]);

  useEffect(() => {
    if (!user) return;
    let live = true;
    getSystemCapabilities()
      .then((caps) => {
        if (live && caps.max_upload_bytes && caps.max_upload_bytes > 0)
          setMaxBytes(caps.max_upload_bytes);
      })
      .catch(() => {
        // The fallback is the server's default; the server enforces its own limit anyway.
      });
    return () => {
      live = false;
    };
  }, [user]);

  const commit = useCallback((next: UploadItem[]) => {
    store.current = next;
    setItems(next);
  }, []);

  const patch = useCallback(
    (id: string, change: Partial<UploadItem>) => {
      if (!store.current.some((it) => it.id === id)) return;
      commit(store.current.map((it) => (it.id === id ? { ...it, ...change } : it)));
    },
    [commit],
  );

  // A finished upload starts the next one through this, since the scheduler
  // can't name itself inside its own definition.
  const pumpRef = useRef<() => void>(() => {});

  // Starts waiting files until CONCURRENCY are running. Called whenever a
  // file is added, finishes, fails, or is cancelled or retried.
  const pump = useCallback(() => {
    const running = store.current.filter((it) => it.status === 'uploading').length;
    const next = store.current
      .filter((it) => it.status === 'queued')
      .slice(0, CONCURRENCY - running);
    if (next.length === 0) return;
    const ids = new Set(next.map((it) => it.id));
    commit(
      store.current.map((it) => (ids.has(it.id) ? { ...it, status: 'uploading', loaded: 0 } : it)),
    );

    for (const item of next) {
      const controller = new AbortController();
      controllers.current.set(item.id, controller);
      uploadFileWithProgress(item.libraryId, item.file, item.destPath, {
        signal: controller.signal,
        onProgress: ({ loaded, total }) => {
          // The request carries a little multipart framing on top of the file.
          patch(item.id, { loaded: Math.min(item.size, Math.round((loaded / total) * item.size)) });
        },
      })
        .then(() => patch(item.id, { status: 'done', loaded: item.size, error: undefined }))
        .catch((err: unknown) => {
          if (err instanceof DOMException && err.name === 'AbortError') {
            patch(item.id, { status: 'cancelled' });
            return;
          }
          const { message, retryable } = describeFailure(err, limitRef.current);
          patch(item.id, { status: 'failed', error: message, retryable });
        })
        .finally(() => {
          controllers.current.delete(item.id);
          pumpRef.current();
        });
    }
  }, [commit, patch]);

  useLayoutEffect(() => {
    pumpRef.current = pump;
  }, [pump]);

  // Leaving the page would abort every upload in flight; ask first.
  const active = items.some((it) => it.status === 'uploading' || it.status === 'queued');
  useEffect(() => {
    if (!active) return;
    const onUnload = (e: BeforeUnloadEvent) => e.preventDefault();
    window.addEventListener('beforeunload', onUnload);
    return () => window.removeEventListener('beforeunload', onUnload);
  }, [active]);

  const dismissAll = useCallback(() => {
    store.current = [];
    controllers.current.forEach((c) => c.abort());
    controllers.current.clear();
    setItems([]);
  }, []);

  // Signing out stops everything and forgets the list.
  const [owner, setOwner] = useState(user?.id ?? null);
  if ((user?.id ?? null) !== owner) {
    setOwner(user?.id ?? null);
    if (items.length) setItems([]);
  }
  useEffect(() => {
    if (user) return;
    store.current = [];
    controllers.current.forEach((c) => c.abort());
    controllers.current.clear();
  }, [user]);

  const enqueue = useCallback(
    (target: UploadTarget, files: PickedFile[]) => {
      const limit = limitRef.current;
      const pending = new Set(
        store.current
          .filter((it) => it.status === 'queued' || it.status === 'uploading')
          .map((it) => `${it.libraryId}:${it.destPath}`),
      );
      const added: UploadItem[] = [];
      for (const { file, relPath } of files) {
        if (isJunkFile(file.name)) continue;
        const destPath = joinDest(target.folder, relPath);
        const key = `${target.libraryId}:${destPath}`;
        const item: UploadItem = {
          id: `u${nextId++}`,
          libraryId: target.libraryId,
          libraryName: target.libraryName,
          file,
          name: file.name,
          destPath,
          size: file.size,
          loaded: 0,
          status: 'queued',
        };
        if (file.size > limit) {
          item.status = 'failed';
          item.error = `Larger than the ${formatBytes(limit)} upload limit.`;
          item.retryable = false;
        } else if (pending.has(key)) {
          item.status = 'failed';
          item.error = 'This file is already in the upload list.';
          item.retryable = false;
        }
        pending.add(key);
        added.push(item);
      }
      if (added.length) {
        commit([...store.current, ...added]);
        pump();
      }
      return added.length;
    },
    [commit, pump],
  );

  const cancel = useCallback(
    (id: string) => {
      const controller = controllers.current.get(id);
      if (controller) {
        controller.abort();
        return;
      }
      commit(
        store.current.map((it) =>
          it.id === id && it.status === 'queued' ? { ...it, status: 'cancelled' } : it,
        ),
      );
    },
    [commit],
  );

  const cancelAll = useCallback(() => {
    commit(
      store.current.map((it) => (it.status === 'queued' ? { ...it, status: 'cancelled' } : it)),
    );
    controllers.current.forEach((c) => c.abort());
  }, [commit]);

  const requeueWhere = useCallback(
    (match: (it: UploadItem) => boolean) => {
      commit(
        store.current.map((it) =>
          match(it) ? { ...it, status: 'queued', loaded: 0, error: undefined } : it,
        ),
      );
      pump();
    },
    [commit, pump],
  );

  const retry = useCallback(
    (id: string) =>
      requeueWhere((it) => it.id === id && (it.status === 'failed' || it.status === 'cancelled')),
    [requeueWhere],
  );

  const retryFailed = useCallback(
    () => requeueWhere((it) => it.status === 'failed' && it.retryable !== false),
    [requeueWhere],
  );

  const clearFinished = useCallback(() => {
    commit(store.current.filter((it) => it.status === 'queued' || it.status === 'uploading'));
  }, [commit]);

  const value = useMemo<UploadsState>(
    () => ({
      items,
      maxBytes,
      enqueue,
      cancel,
      cancelAll,
      retry,
      retryFailed,
      clearFinished,
      dismissAll,
    }),
    [items, maxBytes, enqueue, cancel, cancelAll, retry, retryFailed, clearFinished, dismissAll],
  );

  return <UploadsContext.Provider value={value}>{children}</UploadsContext.Provider>;
}

export function useUploads(): UploadsState {
  const ctx = useContext(UploadsContext);
  if (!ctx) throw new Error('useUploads must be used inside UploadProvider');
  return ctx;
}
