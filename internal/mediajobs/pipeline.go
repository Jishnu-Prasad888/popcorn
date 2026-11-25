// Package mediajobs runs the media server's background work for each
// library: after a scan, a catalog sync works out the movies, shows and
// episodes; new video files are probed with ffprobe; new items get metadata
// and artwork from the configured provider; and items with no artwork get a
// frame from the video.
package mediajobs

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"path/filepath"
	"sync"

	"github.com/Jishnu-Prasad888/popcorn/internal/catalog"
	"github.com/Jishnu-Prasad888/popcorn/internal/jobs"
	"github.com/Jishnu-Prasad888/popcorn/internal/probe"
	"github.com/Jishnu-Prasad888/popcorn/internal/provider"
	"github.com/Jishnu-Prasad888/popcorn/internal/stream"
)

// Pipeline wires the media jobs into each library's job worker.
type Pipeline struct {
	Logger   *slog.Logger
	Settings *Settings
	// KindOf returns a library's kind (movies, shows, mixed).
	KindOf func(ctx context.Context, libraryID string) string
	// OnChange, when set, is told a library's catalog changed, so open pages
	// can refresh.
	OnChange func(libraryID string)

	mu      sync.Mutex
	changed map[string]bool
}

// Register adds the media job handlers to a library's worker.
func (p *Pipeline) Register(w *jobs.Worker, q *jobs.Queue, db *sql.DB, libraryID, root string) {
	popcornDir := filepath.Join(root, ".popcorn")
	w.SetExclusive(jobs.KindCatalog)
	w.SetExclusive(jobs.KindMetadata)
	w.SetExclusive(jobs.KindThumbnails)
	w.Register(jobs.KindCatalog, func(ctx context.Context, job *jobs.Job) error {
		return p.runSync(ctx, q, db, libraryID, root)
	})
	w.Register(jobs.KindProbe, func(ctx context.Context, job *jobs.Job) error {
		return p.runProbe(ctx, q, db, libraryID, root, job)
	})
	w.Register(jobs.KindMetadata, func(ctx context.Context, job *jobs.Job) error {
		return p.runMetadata(ctx, q, db, libraryID, popcornDir, job)
	})
	w.Register(jobs.KindThumbnails, func(ctx context.Context, job *jobs.Job) error {
		return p.runThumbnails(ctx, q, db, libraryID, root, popcornDir)
	})
}

// AfterScan queues a catalog sync after a library scan.
func (p *Pipeline) AfterScan(ctx context.Context, q *jobs.Queue) {
	EnqueueSync(ctx, q, p.Logger)
}

// EnqueueSync queues a catalog sync unless one is already waiting.
func EnqueueSync(ctx context.Context, q *jobs.Queue, logger *slog.Logger) {
	queued, err := pendingKinds(ctx, q.RawDB(), jobs.KindCatalog)
	if err == nil && queued > 0 {
		return
	}
	if _, err := q.Enqueue(ctx, jobs.KindCatalog, map[string]any{}); err != nil {
		logger.Warn("enqueue catalog sync", "error", err)
	}
}

func pendingKinds(ctx context.Context, db *sql.DB, kind string) (int, error) {
	var n int
	err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM index_jobs WHERE kind = ? AND status = 'queued'`, kind).Scan(&n)
	return n, err
}

// pendingPayloadKeys returns a payload field of every queued or running job
// of a kind, so the same work is not queued twice.
func pendingPayloadKeys(ctx context.Context, db *sql.DB, kind, field string) (map[string]bool, error) {
	rows, err := db.QueryContext(ctx, `SELECT payload FROM index_jobs WHERE kind = ? AND status IN ('queued', 'running', 'failed')`, kind)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := map[string]bool{}
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var m map[string]any
		if json.Unmarshal([]byte(raw), &m) == nil {
			if v, ok := m[field].(string); ok {
				out[v] = true
			}
		}
	}
	return out, rows.Err()
}

func (p *Pipeline) notify(libraryID string) {
	if p.OnChange != nil {
		p.OnChange(libraryID)
	}
}

func (p *Pipeline) runSync(ctx context.Context, q *jobs.Queue, db *sql.DB, libraryID, root string) error {
	kind := catalog.KindMixed
	if p.KindOf != nil {
		kind = p.KindOf(ctx, libraryID)
	}
	res, err := catalog.Sync(ctx, db, kind)
	if err != nil {
		return err
	}
	p.Logger.Info("catalog synced", "library_id", libraryID, "movies", res.Movies, "shows", res.Shows,
		"episodes", res.Episodes, "new", res.NewItems, "to_probe", len(res.NeedProbe),
		"to_match", len(res.NeedMetadata))

	// Probes first: they are quick, and runtimes and chapter markers come
	// from them.
	if probe.Available() && len(res.NeedProbe) > 0 {
		pending, err := pendingPayloadKeys(ctx, db, jobs.KindProbe, "file_id")
		if err != nil {
			return err
		}
		var payloads []map[string]any
		for _, f := range res.NeedProbe {
			if !pending[f.ID] {
				payloads = append(payloads, map[string]any{"file_id": f.ID, "rel_path": f.RelPath})
			}
		}
		if _, err := q.EnqueueAll(ctx, jobs.KindProbe, payloads); err != nil {
			p.Logger.Warn("enqueue probes", "error", err)
		}
	}
	settings := p.Settings.Get(ctx)
	if settings.TMDbKey != "" && settings.Auto && len(res.NeedMetadata) > 0 {
		EnqueueMetadata(ctx, q, p.Logger, res.NeedMetadata, false)
	}
	if settings.GenerateThumbs && stream.Available() {
		p.enqueueThumbnails(ctx, q)
	}
	p.notify(libraryID)
	return nil
}

// EnqueueMetadata queues metadata fetches for items, skipping items already
// queued. force refetches matched items too (a refresh).
func EnqueueMetadata(ctx context.Context, q *jobs.Queue, logger *slog.Logger, items []catalog.ItemRef, force bool) int {
	pending, err := pendingPayloadKeys(ctx, q.RawDB(), jobs.KindMetadata, "item_id")
	if err != nil {
		logger.Warn("pending metadata jobs", "error", err)
		return 0
	}
	var payloads []map[string]any
	for _, it := range items {
		if pending[it.ID] {
			continue
		}
		payloads = append(payloads, map[string]any{"item_type": it.Type, "item_id": it.ID})
	}
	n, err := q.EnqueueAll(ctx, jobs.KindMetadata, payloads)
	if err != nil {
		logger.Warn("enqueue metadata", "error", err)
	}
	return n
}

func (p *Pipeline) enqueueThumbnails(ctx context.Context, q *jobs.Queue) {
	if active, err := q.HasActive(ctx, jobs.KindThumbnails); err != nil || active {
		return
	}
	if _, err := q.Enqueue(ctx, jobs.KindThumbnails, map[string]any{}); err != nil {
		p.Logger.Warn("enqueue thumbnails", "error", err)
	}
}

func (p *Pipeline) runProbe(ctx context.Context, q *jobs.Queue, db *sql.DB, libraryID, root string, job *jobs.Job) error {
	fileID, _ := job.Payload["file_id"].(string)
	if fileID == "" {
		return nil
	}
	store := catalog.NewStore(db, libraryID)
	rel, status, size, mtime, err := store.FileOf(ctx, fileID)
	if errors.Is(err, catalog.ErrNotFound) || status != "present" {
		return nil
	}
	if err != nil {
		return err
	}
	res, perr := probe.File(ctx, filepath.Join(root, filepath.FromSlash(rel)))
	if errors.Is(perr, probe.ErrUnavailable) {
		return nil
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if perr != nil {
		p.Logger.Warn("probe failed", "path", rel, "error", perr)
	}
	if err := catalog.SaveProbe(ctx, db, fileID, size, mtime, res, perr); err != nil {
		return err
	}
	p.markChanged(libraryID)
	// When the last probe of a batch finishes, open pages refresh once.
	if n, err := pendingKinds(ctx, db, jobs.KindProbe); err == nil && n == 0 {
		p.flushChanged(libraryID)
		if p.Settings.Get(ctx).GenerateThumbs && stream.Available() {
			p.enqueueThumbnails(ctx, q)
		}
	}
	return nil
}

func (p *Pipeline) markChanged(libraryID string) {
	p.mu.Lock()
	if p.changed == nil {
		p.changed = map[string]bool{}
	}
	p.changed[libraryID] = true
	p.mu.Unlock()
}

func (p *Pipeline) flushChanged(libraryID string) {
	p.mu.Lock()
	was := p.changed[libraryID]
	delete(p.changed, libraryID)
	p.mu.Unlock()
	if was {
		p.notify(libraryID)
	}
}

func (p *Pipeline) runMetadata(ctx context.Context, q *jobs.Queue, db *sql.DB, libraryID, popcornDir string, job *jobs.Job) error {
	itemType, _ := job.Payload["item_type"].(string)
	itemID, _ := job.Payload["item_id"].(string)
	providerID, _ := job.Payload["provider_id"].(string)
	e := p.Settings.Enricher(ctx, p.Logger)
	if e.Provider == nil {
		return nil
	}
	var err error
	switch itemType {
	case "movie":
		err = e.EnrichMovie(ctx, db, popcornDir, itemID, providerID)
	case "show":
		err = e.EnrichShow(ctx, db, popcornDir, itemID, providerID)
	default:
		return nil
	}
	switch {
	case err == nil:
	case errors.Is(err, catalog.ErrNoMatch), errors.Is(err, provider.ErrNotFound), errors.Is(err, sql.ErrNoRows):
		p.Logger.Info("no metadata match", "type", itemType, "id", itemID)
		err = nil
	case errors.Is(err, provider.ErrNotConfigured):
		err = nil
	default:
		return err
	}
	p.markChanged(libraryID)
	if n, qerr := pendingKinds(ctx, db, jobs.KindMetadata); qerr == nil && n == 0 {
		p.flushChanged(libraryID)
		if p.Settings.Get(ctx).GenerateThumbs && stream.Available() {
			p.enqueueThumbnails(ctx, q)
		}
	} else if qerr == nil && n%10 == 0 {
		p.flushChanged(libraryID)
	}
	return err
}

func (p *Pipeline) runThumbnails(ctx context.Context, q *jobs.Queue, db *sql.DB, libraryID, root, popcornDir string) error {
	store := catalog.NewStore(db, libraryID)
	const batch = 40
	items, err := store.MissingThumbnails(ctx, batch)
	if err != nil {
		return err
	}
	done := 0
	for _, it := range items {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		kind := "thumb"
		if it.Type == "movie" {
			kind = "backdrop"
		}
		if err := store.GenerateArtwork(ctx, root, popcornDir, it.Type, it.ID, kind, 0, false); err != nil {
			p.Logger.Debug("thumbnail", "type", it.Type, "id", it.ID, "error", err)
			// Record a failure as a generated placeholder-free marker so the
			// item is not retried forever: an empty "generated" row would
			// break image serving, so skip instead.
			continue
		}
		done++
	}
	if done > 0 {
		p.notify(libraryID)
	}
	if len(items) == batch && done > 0 {
		if _, err := q.Enqueue(ctx, jobs.KindThumbnails, map[string]any{"continue": true}); err != nil {
			p.Logger.Warn("enqueue thumbnails", "error", err)
		}
	}
	return nil
}
