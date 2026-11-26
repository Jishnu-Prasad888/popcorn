/**
 * Pure helpers for the upload queue: building destination paths, reading
 * dropped folders, and turning a failed request into a sentence a person can
 * act on. Kept apart from the React provider so they can be tested alone.
 */

import { ApiError } from '../api/client';

export type UploadStatus = 'queued' | 'uploading' | 'done' | 'failed' | 'cancelled';

export interface UploadItem {
  id: string;
  libraryId: string;
  libraryName: string;
  file: File;
  /** File name shown on the row. */
  name: string;
  /** Destination relative to the library root, folders included. */
  destPath: string;
  size: number;
  loaded: number;
  status: UploadStatus;
  /** Why it failed, written for the person reading the tray. */
  error?: string | undefined;
  /** False when retrying cannot help (too large, disallowed name). */
  retryable?: boolean | undefined;
}

/** A picked or dropped file and its path inside whatever folder held it. */
export interface PickedFile {
  file: File;
  /** "Show/Season 1/e01.mkv" for a folder pick, the bare name otherwise. */
  relPath: string;
}

/** Files every OS scatters through folders; nobody means to upload them. */
const JUNK = /^(\.ds_store|thumbs\.db|desktop\.ini|\._.*)$/i;

export function isJunkFile(name: string): boolean {
  return JUNK.test(name);
}

/** Joins a destination folder and a relative path with single slashes. */
export function joinDest(folder: string, relPath: string): string {
  const parts = [...folder.split(/[\\/]+/), ...relPath.split(/[\\/]+/)]
    .map((p) => p.trim())
    .filter((p) => p !== '' && p !== '.');
  return parts.join('/');
}

/** The folder part of a library-relative path ("" for the top folder). */
export function parentOf(path: string): string {
  const i = path.lastIndexOf('/');
  return i < 0 ? '' : path.slice(0, i);
}

/** Files chosen through an <input type="file">, folder picks included. */
export function fromInput(list: FileList | null): PickedFile[] {
  if (!list) return [];
  return Array.from(list).map((file) => ({
    file,
    relPath: file.webkitRelativePath || file.name,
  }));
}

/**
 * Files dropped from the desktop. A dropped folder arrives as a directory
 * entry, so it is walked (in batches, as readEntries returns them) to reach
 * every file inside, keeping each one's path within the folder.
 */
export async function fromDrop(data: DataTransfer): Promise<PickedFile[]> {
  const entries: FileSystemEntry[] = [];
  const loose: File[] = [];
  for (const item of Array.from(data.items)) {
    if (item.kind !== 'file') continue;
    const entry = item.webkitGetAsEntry?.();
    if (entry) entries.push(entry);
    else {
      const f = item.getAsFile();
      if (f) loose.push(f);
    }
  }
  if (entries.length === 0) {
    return [...loose, ...Array.from(data.files)].map((file) => ({ file, relPath: file.name }));
  }
  const out: PickedFile[] = loose.map((file) => ({ file, relPath: file.name }));
  for (const entry of entries) await walk(entry, '', out);
  return out;
}

async function walk(entry: FileSystemEntry, prefix: string, out: PickedFile[]): Promise<void> {
  const path = prefix ? `${prefix}/${entry.name}` : entry.name;
  if (entry.isFile) {
    const file = await new Promise<File>((resolve, reject) =>
      (entry as FileSystemFileEntry).file(resolve, reject),
    );
    out.push({ file, relPath: path });
    return;
  }
  if (!entry.isDirectory) return;
  const reader = (entry as FileSystemDirectoryEntry).createReader();
  for (;;) {
    const batch = await new Promise<FileSystemEntry[]>((resolve, reject) =>
      reader.readEntries(resolve, reject),
    );
    if (batch.length === 0) break;
    for (const child of batch) await walk(child, path, out);
  }
}

export function formatBytes(n: number): string {
  if (!Number.isFinite(n) || n < 0) return '';
  if (n < 1024) return `${n} B`;
  const units = ['KB', 'MB', 'GB', 'TB'];
  let v = n / 1024;
  let i = 0;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  return `${v < 10 ? v.toFixed(1) : Math.round(v)} ${units[i]}`;
}

export function formatDuration(seconds: number): string {
  if (!Number.isFinite(seconds) || seconds <= 0) return '';
  if (seconds < 60) return `${Math.max(1, Math.round(seconds))} sec`;
  const m = Math.round(seconds / 60);
  if (m < 60) return `${m} min`;
  const h = Math.floor(m / 60);
  return `${h} hr ${m % 60} min`;
}

/** The reason shown on a failed row, and whether retrying could help. */
export function describeFailure(
  err: unknown,
  maxBytes: number,
): { message: string; retryable: boolean } {
  if (err instanceof ApiError) {
    switch (err.status) {
      case 401:
        return { message: 'Your session expired. Sign in again, then retry.', retryable: true };
      case 403:
        return { message: 'You don’t have permission to add files here.', retryable: false };
      case 409:
        if (/offline/i.test(err.message)) {
          return { message: 'The library’s drive is offline.', retryable: true };
        }
        return { message: 'A file with this name is already in that folder.', retryable: false };
      case 413:
        return {
          message: `Larger than the ${formatBytes(maxBytes)} upload limit.`,
          retryable: false,
        };
      case 400:
        if (/path/i.test(err.message)) {
          return { message: 'This name or folder isn’t allowed in a library.', retryable: false };
        }
        return { message: err.message, retryable: false };
      default:
        if (err.status >= 500) {
          return {
            message: 'The server couldn’t save the file. Try again later.',
            retryable: true,
          };
        }
        return { message: err.message, retryable: true };
    }
  }
  if (err instanceof Error && err.message) {
    return { message: err.message, retryable: true };
  }
  return { message: 'The upload failed.', retryable: true };
}
