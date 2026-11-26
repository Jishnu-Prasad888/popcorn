package httpapi

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
)

// pathFirstUpload sends the "path" field before the file, as the web client
// does, so the handler streams the file straight into the library.
func pathFirstUpload(t *testing.T, c *testClient, libID, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if err := mw.WriteField("path", path); err != nil {
		t.Fatal(err)
	}
	fw, err := mw.CreateFormFile("file", "ignored-name.bin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write([]byte(body)); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/libraries/"+libID+"/files/upload", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	for name, value := range c.cookies {
		req.AddCookie(&http.Cookie{Name: name, Value: value})
	}
	rec := httptest.NewRecorder()
	c.handler.ServeHTTP(rec, req)
	return rec
}

func TestUploadStreamsIntoNestedFolders(t *testing.T) {
	_, admin, _, libID := newAuthzTestServer(t)

	rec := pathFirstUpload(t, admin, libID, "Shows/The Long Coast/Season 1/e01.mkv", "episode one")
	if rec.Code != http.StatusCreated {
		t.Fatalf("upload = %d, body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		File struct {
			RelPath   string `json:"rel_path"`
			SizeBytes int64  `json:"size_bytes"`
		} `json:"file"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.File.RelPath != "Shows/The Long Coast/Season 1/e01.mkv" || resp.File.SizeBytes != int64(len("episode one")) {
		t.Fatalf("file = %+v", resp.File)
	}

	// The same destination again is a conflict the tray can explain.
	rec = pathFirstUpload(t, admin, libID, "Shows/The Long Coast/Season 1/e01.mkv", "again")
	if rec.Code != http.StatusConflict {
		t.Fatalf("duplicate upload = %d, want 409", rec.Code)
	}

	// Escaping the library is refused.
	rec = pathFirstUpload(t, admin, libID, "../outside.mkv", "x")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("traversal upload = %d, want 400", rec.Code)
	}
}

func TestUploadViewerWithoutCreateIsForbidden(t *testing.T) {
	_, _, viewer, libID := newAuthzTestServer(t)
	rec := pathFirstUpload(t, viewer, libID, "Movies/x.mkv", "x")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("viewer upload = %d, want 403", rec.Code)
	}
}
