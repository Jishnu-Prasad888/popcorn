/**
 * Interface preferences applied to the document: card size, density, motion
 * and accent. The palette itself is fixed (Techno Dracula, `styles/tokens.css`);
 * these only scale and tint it. A copy is kept in localStorage so the first
 * paint already matches; the server holds the source of truth per person.
 */

import type { InterfacePrefs } from '../api/media';

export const INTERFACE_STORAGE_KEY = 'popcorn.interface';

export const DEFAULT_INTERFACE: InterfacePrefs = {
  density: 'comfortable',
  card_size: 'medium',
  view: 'grid',
  motion: 'full',
  accent: 'purple',
  home_rows: [],
};

export const ACCENTS: Record<string, string> = {
  purple: '#bd93f9',
  pink: '#ff79c6',
  cyan: '#8be9fd',
  green: '#50fa7b',
};

export function readStoredInterface(): InterfacePrefs {
  try {
    const raw = window.localStorage.getItem(INTERFACE_STORAGE_KEY);
    if (raw) return { ...DEFAULT_INTERFACE, ...(JSON.parse(raw) as Partial<InterfacePrefs>) };
  } catch {
    /* storage unavailable */
  }
  return DEFAULT_INTERFACE;
}

export function applyInterface(p: InterfacePrefs): void {
  const root = document.documentElement;
  root.dataset.cardSize = p.card_size;
  root.dataset.density = p.density;
  root.dataset.motion = p.motion;
  const accent = ACCENTS[p.accent] ?? '#bd93f9';
  root.style.setProperty('--color-primary', accent);
  root.style.setProperty('--color-primary-ink', accent);
  root.style.setProperty('--color-primary-strong', accent);
  try {
    window.localStorage.setItem(INTERFACE_STORAGE_KEY, JSON.stringify(p));
  } catch {
    /* storage unavailable */
  }
}
