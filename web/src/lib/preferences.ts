/**
 * The signed-in person's playback and interface preferences, shared by the
 * shell, the settings page and the player. Loaded once per session and saved
 * back to the server on change.
 */
import { useEffect, useSyncExternalStore } from 'react';

import { getPreferences, savePreferences } from '../api/media';
import type { InterfacePrefs, PlaybackPrefs } from '../api/media';
import { applyInterface, readStoredInterface } from './theme';

interface PrefsState {
  playback: PlaybackPrefs | null;
  iface: InterfacePrefs;
  loaded: boolean;
}

let state: PrefsState = { playback: null, iface: readStoredInterface(), loaded: false };
let loading: Promise<void> | null = null;
const listeners = new Set<() => void>();

function set(next: Partial<PrefsState>) {
  state = { ...state, ...next };
  listeners.forEach((l) => l());
}

function subscribe(l: () => void) {
  listeners.add(l);
  return () => listeners.delete(l);
}

export function loadPreferences(force = false): Promise<void> {
  if (loading && !force) return loading;
  loading = getPreferences()
    .then((p) => {
      applyInterface(p.interface);
      set({ playback: p.playback, iface: p.interface, loaded: true });
    })
    .catch(() => {
      set({ loaded: true });
    });
  return loading;
}

export async function updatePlayback(patch: Partial<PlaybackPrefs>): Promise<void> {
  if (!state.playback) await loadPreferences();
  if (!state.playback) return;
  const next = { ...state.playback, ...patch };
  set({ playback: next });
  const saved = await savePreferences({ playback: next });
  set({ playback: saved.playback });
}

export async function updateInterface(patch: Partial<InterfacePrefs>): Promise<void> {
  const next = { ...state.iface, ...patch };
  applyInterface(next);
  set({ iface: next });
  const saved = await savePreferences({ interface: next });
  set({ iface: saved.interface });
}

export function usePreferences(): PrefsState {
  const s = useSyncExternalStore(subscribe, () => state);
  useEffect(() => {
    void loadPreferences();
  }, []);
  return s;
}
