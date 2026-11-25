import type { ReactNode } from 'react';

import type { Card } from '../../api/media';
import { useCardMenu } from './CardMenu';

/**
 * Gives whatever it wraps the item's right-click menu, without changing the
 * layout (the wrapper is `display: contents`).
 */
export function CardMenuArea({
  item,
  onChanged,
  landscape = false,
  children,
}: {
  item: Card;
  onChanged?: (() => void) | undefined;
  landscape?: boolean;
  children: ReactNode;
}) {
  const { onContextMenu, element } = useCardMenu(item, { landscape, onChanged });
  return (
    <div style={{ display: 'contents' }} onContextMenu={onContextMenu}>
      {element}
      {children}
    </div>
  );
}
