/**
 * Choose where uploads go, then pick files or a whole folder — or drop them.
 *
 * A folder keeps its structure: dropping "Severance/Season 1/…" into the
 * "Shows" folder of a library creates "Shows/Severance/Season 1/…". The
 * dialog closes as soon as files are queued; the tray takes it from there.
 */

import { useEffect, useRef, useState } from 'react';
import type { DragEvent } from 'react';
import { FilePlus2, FolderPlus, UploadCloud } from 'lucide-react';

import { useLibraries } from '../../api/libraries';
import { Dialog } from '../Dialog';
import { useToast } from '../ui/Toast';
import { useUploads } from '../../uploads/UploadProvider';
import { formatBytes, fromDrop, fromInput } from '../../uploads/uploads';
import type { PickedFile } from '../../uploads/uploads';

export function UploadDialog({ open, onClose }: { open: boolean; onClose: () => void }) {
  const { libraries, libraryId } = useLibraries();
  const { enqueue, maxBytes } = useUploads();
  const toast = useToast();
  const online = libraries.filter((l) => l.status === 'online');
  const [target, setTarget] = useState(libraryId ?? '');
  const [folder, setFolder] = useState('');
  const [over, setOver] = useState(false);
  const filesRef = useRef<HTMLInputElement>(null);
  const folderRef = useRef<HTMLInputElement>(null);

  // Folder picking is a non-standard attribute React doesn't type.
  useEffect(() => {
    folderRef.current?.setAttribute('webkitdirectory', '');
    folderRef.current?.setAttribute('directory', '');
  });

  const chosen = online.find((l) => l.id === target) ?? online[0];

  const queue = (files: PickedFile[]) => {
    if (!chosen) return;
    if (files.length === 0) {
      toast({ message: 'That folder is empty.', tone: 'neutral' });
      return;
    }
    const n = enqueue({ libraryId: chosen.id, libraryName: chosen.name, folder }, files);
    if (n === 0) toast({ message: 'Nothing to upload there.', tone: 'neutral' });
    onClose();
  };

  const onDrop = (e: DragEvent) => {
    e.preventDefault();
    setOver(false);
    void fromDrop(e.dataTransfer)
      .then(queue)
      .catch(() => toast({ message: 'Couldn’t read the dropped files.', tone: 'error' }));
  };

  return (
    <Dialog open={open} title="Upload" onClose={onClose} size="medium" testId="upload-dialog">
      {online.length === 0 ? (
        <p>No library is online to upload into.</p>
      ) : (
        <div className="upload-dialog">
          <div className="upload-dialog-fields">
            {online.length > 1 && (
              <label className="field">
                <span className="field-label">Library</span>
                <select value={chosen?.id ?? ''} onChange={(e) => setTarget(e.target.value)}>
                  {online.map((l) => (
                    <option key={l.id} value={l.id}>
                      {l.name}
                    </option>
                  ))}
                </select>
              </label>
            )}
            <label className="field">
              <span className="field-label">Folder in {chosen?.name}</span>
              <input
                type="text"
                value={folder}
                onChange={(e) => setFolder(e.target.value)}
                placeholder="Top folder"
                spellCheck={false}
                autoComplete="off"
              />
              <span className="field-hint">
                Optional, for example <code>Movies</code> or <code>Shows/Severance</code>. Missing
                folders are created.
              </span>
            </label>
          </div>

          <div
            className="upload-drop"
            data-over={over}
            onDragEnter={(e) => {
              e.preventDefault();
              setOver(true);
            }}
            onDragOver={(e) => {
              e.preventDefault();
              e.dataTransfer.dropEffect = 'copy';
            }}
            onDragLeave={(e) => {
              if (!e.currentTarget.contains(e.relatedTarget as Node | null)) setOver(false);
            }}
            onDrop={onDrop}
          >
            <UploadCloud
              className="upload-drop-icon"
              size={36}
              strokeWidth={1.5}
              aria-hidden="true"
            />
            <p>Drop files or folders here</p>
            <div className="upload-drop-actions">
              <button
                type="button"
                className="button primary-button"
                onClick={() => filesRef.current?.click()}
              >
                <FilePlus2 size={17} aria-hidden="true" />
                Choose files
              </button>
              <button type="button" className="button" onClick={() => folderRef.current?.click()}>
                <FolderPlus size={17} aria-hidden="true" />
                Choose folder
              </button>
            </div>
            <p className="upload-drop-limit">Up to {formatBytes(maxBytes)} per file</p>
          </div>

          <input
            ref={filesRef}
            type="file"
            multiple
            hidden
            data-testid="upload-files-input"
            onChange={(e) => {
              queue(fromInput(e.target.files));
              e.target.value = '';
            }}
          />
          <input
            ref={folderRef}
            type="file"
            hidden
            data-testid="upload-folder-input"
            onChange={(e) => {
              queue(fromInput(e.target.files));
              e.target.value = '';
            }}
          />
        </div>
      )}
    </Dialog>
  );
}
