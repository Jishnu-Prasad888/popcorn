package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Jishnu-Prasad888/popcorn/internal/audit"
	"github.com/Jishnu-Prasad888/popcorn/internal/auth"
	"github.com/Jishnu-Prasad888/popcorn/internal/crypto"
	"github.com/Jishnu-Prasad888/popcorn/internal/db"
	"github.com/Jishnu-Prasad888/popcorn/internal/library"
	"github.com/Jishnu-Prasad888/popcorn/internal/librarydb"
	"github.com/Jishnu-Prasad888/popcorn/internal/userdata"
)

// --- shared test infrastructure ---

// newSearchTestServer creates a fully-wired server with auth and a test library.
// It returns the handler, an authenticated admin client, the library ID, and the
// library root (so tests can seed the per-library database).
func newSearchTestServer(t *testing.T) (http.Handler, *testClient, string, string) {
	t.Helper()
	return newSearchTestServerWithKeys(t, nil)
}

// newSearchTestServerWithKeys is newSearchTestServer with an optional at-rest
// encryption key injected into the HTTP server dependencies.
func newSearchTestServerWithKeys(t *testing.T, keys *crypto.Keys) (http.Handler, *testClient, string, string) {
	t.Helper()
	tmpDir := t.TempDir()
	pool, err := db.Open(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(func() { _ = pool.Close() })
	if err := db.Migrate(pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	auditSvc := audit.New(pool, logger)
	authSvc := auth.NewService(pool, logger, auditSvc)
	libManager := library.NewManager(pool, logger, auditSvc, crypto.NewKeys(""))

	// Register a test library.
	libRoot := filepath.Join(tmpDir, "library")
	if err := os.MkdirAll(libRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	lib, _, err := libManager.Register(context.Background(), libRoot, "Test Library")
	if err != nil {
		t.Fatalf("register library: %v", err)
	}

	handler := New(Dependencies{
		Logger:    logger,
		DB:        pool,
		Auth:      authSvc,
		Libraries: libManager,
		Keys:      keys,
		UserData:  userdata.New(pool),
		DataDir:   tmpDir,
	}).Handler()

	client := &testClient{handler: handler}
	// Bootstrap admin.
	client.roundTrip(t, http.MethodPost, "/api/v1/auth/bootstrap",
		`{"username":"admin","password":"correct-horse-battery"}`)
	// Login.
	client.roundTrip(t, http.MethodPost, "/api/v1/auth/login",
		`{"username":"admin","password":"correct-horse-battery"}`)

	return handler, client, lib.ID, libRoot
}

// seedLibraryFile inserts an indexed file directly into the per-library DB.
func seedLibraryFile(t *testing.T, libRoot, id, relPath string, size int64) error {
	t.Helper()
	ldb, err := librarydb.Open(filepath.Join(libRoot, ".popcorn"))
	if err != nil {
		return err
	}
	defer func() { _ = ldb.Close() }()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = ldb.Exec(`INSERT INTO indexed_files
		(id, rel_path, size_bytes, mod_time, content_hash, status, first_seen_at, last_seen_at, indexed_at)
		VALUES (?, ?, ?, ?, NULL, 'present', ?, ?, ?)`,
		id, relPath, size, now, now, now, now)
	return err
}

// seedLibraryFile inserts a file directly into the per-library database.
// doJSON2 is a helper that sends an authenticated request.
func (c *testClient) do(t *testing.T, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf *bytes.Buffer
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		buf = bytes.NewBuffer(b)
	} else {
		buf = bytes.NewBuffer(nil)
	}
	req := httptest.NewRequest(method, path, buf)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for name, value := range c.cookies {
		req.AddCookie(&http.Cookie{Name: name, Value: value})
	}
	rec := httptest.NewRecorder()
	c.handler.ServeHTTP(rec, req)
	c.applySetCookies(rec.Result().Cookies())
	return rec
}

