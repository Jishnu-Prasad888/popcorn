import { Link } from 'react-router-dom';
import { Trash2 } from 'lucide-react';

import { deleteHistory, errorMessage, getHistory, getList, itemPath, episodeCode } from '../api/media';
import type { ListName } from '../api/media';
import { useResource } from '../api/resources';
import { CardGrid, GridSkeleton } from '../components/media/Cards';
import { ErrorState, LoadingState } from '../components/States';
import { useToast } from '../components/ui/Toast';
import { formatDateTime } from '../lib/dates';
import { usePreferences } from '../lib/preferences';

const COPY: Record<ListName, { title: string; empty: string; hint: string }> = {
  watchlist: {
    title: 'Watchlist',
    empty: 'Your watchlist is empty',
    hint: 'Save movies and shows for later with the bookmark button on their page.',
  },
  favorite: {
    title: 'Favourites',
    empty: 'No favourites yet',
    hint: 'Mark the titles you love with the heart button on their page.',
  },
};

/** The watchlist or favourites. */
export function ListPage({ list }: { list: ListName }) {
  const res = useResource(() => getList(list), [list]);
  const { iface } = usePreferences();
  const copy = COPY[list];
  return (
    <div className="page">
      <header className="page-head">
        <div>
          <h1 className="page-title">{copy.title}</h1>
          {res.data && <p className="page-count">{res.data.items.length} titles</p>}
        </div>
      </header>
      {res.loading && <GridSkeleton />}
      {res.error && <ErrorState message={res.error} onRetry={res.reload} />}
      {res.data?.items.length === 0 && (
        <div className="empty-media">
          <h2>{copy.empty}</h2>
          <p>{copy.hint}</p>
          <Link className="primary-button" to="/movies">
            Browse movies
          </Link>
        </div>
      )}
      {res.data && res.data.items.length > 0 && <CardGrid items={res.data.items} view={iface.view} />}
    </div>
  );
}

/** What the person watched, newest first. */
export function HistoryPage() {
  const res = useResource(() => getHistory(300), []);
  const toast = useToast();
  const remove = async (id: number) => {
    try {
      await deleteHistory(id);
      res.reload();
    } catch (err) {
      toast({ message: errorMessage(err), tone: 'error' });
    }
  };
  return (
    <div className="page">
      <header className="page-head">
        <h1 className="page-title">History</h1>
      </header>
      {res.loading && <LoadingState />}
      {res.error && <ErrorState message={res.error} onRetry={res.reload} />}
      {res.data?.entries.length === 0 && (
        <div className="empty-media">
          <h2>Nothing watched yet</h2>
          <p>Movies and episodes you finish are listed here, newest first.</p>
        </div>
      )}
      {res.data && res.data.entries.length > 0 && (
        <ol className="history-list">
          {res.data.entries.map((h) => (
            <li key={h.entry_id}>
              <time dateTime={h.watched_at}>{formatDateTime(h.watched_at)}</time>
              <Link to={itemPath(h.item)} className="history-title">
                {h.item.type === 'episode' ? (
                  <>
                    {h.item.show_title} <span className="muted">{episodeCode(h.item)} {h.item.title}</span>
                  </>
                ) : (
                  <>
                    {h.item.title} {h.item.year ? <span className="muted">{h.item.year}</span> : null}
                  </>
                )}
              </Link>
              <button type="button" className="icon-button" onClick={() => void remove(h.entry_id)} aria-label="Remove from history" title="Remove from history">
                <Trash2 size={16} />
              </button>
            </li>
          ))}
        </ol>
      )}
    </div>
  );
}
