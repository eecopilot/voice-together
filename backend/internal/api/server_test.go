package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"voice-together/backend/internal/db"
	"voice-together/backend/internal/media"
	"voice-together/backend/internal/storage"
	"voice-together/backend/internal/transcribe"
)

type clipListPayload struct {
	Clips  []db.Clip     `json:"clips"`
	Total  int           `json:"total"`
	Limit  int           `json:"limit"`
	Offset int           `json:"offset"`
	Counts db.ClipCounts `json:"counts"`
}

type fakeMediaProcessor struct {
	extract func(context.Context, string, string) (media.Result, error)
}

func (f fakeMediaProcessor) Extract(ctx context.Context, sourcePath string, clipDir string) (media.Result, error) {
	if f.extract == nil {
		return media.Result{}, errors.New("unexpected media extraction")
	}
	return f.extract(ctx, sourcePath, clipDir)
}

type fakeTranscriber struct {
	transcribe func(context.Context, string, string) (transcribe.Result, error)
}

func (f fakeTranscriber) Transcribe(ctx context.Context, audioPath string, language string) (transcribe.Result, error) {
	if f.transcribe == nil {
		return transcribe.Result{}, errors.New("unexpected transcription")
	}
	return f.transcribe(ctx, audioPath, language)
}

func TestListClipsSearchStatusPaginationAndCounts(t *testing.T) {
	server, store, _ := newTestServer(t, fakeMediaProcessor{}, fakeTranscriber{})
	seedClipList(t, store)

	t.Run("title search and status", func(t *testing.T) {
		payload := requestClipList(t, server, "/api/clips?q=ALPHA&status=ready&limit=5&offset=0")
		if payload.Total != 1 || payload.Limit != 5 || payload.Offset != 0 {
			t.Fatalf("unexpected page metadata: %#v", payload)
		}
		if len(payload.Clips) != 1 || payload.Clips[0].ID != "alpha-ready" {
			t.Fatalf("unexpected clips: %#v", payload.Clips)
		}
		wantCounts := db.ClipCounts{All: 2, Ready: 1, Processing: 1, Error: 0}
		if payload.Counts != wantCounts {
			t.Fatalf("counts = %#v, want %#v", payload.Counts, wantCounts)
		}
	})

	t.Run("language search and pagination", func(t *testing.T) {
		payload := requestClipList(t, server, "/api/clips?q=eN&limit=1&offset=1")
		if payload.Total != 2 || payload.Limit != 1 || payload.Offset != 1 {
			t.Fatalf("unexpected page metadata: %#v", payload)
		}
		if len(payload.Clips) != 1 || payload.Clips[0].ID != "alpha-ready" {
			t.Fatalf("unexpected clips: %#v", payload.Clips)
		}
		wantCounts := db.ClipCounts{All: 2, Ready: 1, Processing: 0, Error: 1}
		if payload.Counts != wantCounts {
			t.Fatalf("counts = %#v, want %#v", payload.Counts, wantCounts)
		}
	})

	t.Run("status ignores filter for counts", func(t *testing.T) {
		payload := requestClipList(t, server, "/api/clips?status=ready")
		if payload.Total != 2 || payload.Limit != 10 || payload.Offset != 0 || len(payload.Clips) != 2 {
			t.Fatalf("unexpected ready page: %#v", payload)
		}
		wantCounts := db.ClipCounts{All: 4, Ready: 2, Processing: 1, Error: 1}
		if payload.Counts != wantCounts {
			t.Fatalf("counts = %#v, want %#v", payload.Counts, wantCounts)
		}
	})
}

func TestListClipsRejectsInvalidParameters(t *testing.T) {
	server, _, _ := newTestServer(t, fakeMediaProcessor{}, fakeTranscriber{})
	tests := []string{
		"/api/clips?status=unknown",
		"/api/clips?limit=0",
		"/api/clips?limit=101",
		"/api/clips?limit=abc",
		"/api/clips?offset=-1",
		"/api/clips?offset=abc",
	}
	for _, target := range tests {
		t.Run(target, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, target, nil)
			response := httptest.NewRecorder()
			server.Routes().ServeHTTP(response, request)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusBadRequest, response.Body.String())
			}
		})
	}
}

func TestUploadRejectsUnsupportedExtension(t *testing.T) {
	server, _, _ := newTestServer(t, fakeMediaProcessor{}, fakeTranscriber{})
	body, contentType := multipartUpload(t, "sample.exe", []byte("not media"))
	request := httptest.NewRequest(http.MethodPost, "/api/clips", body)
	request.Header.Set("Content-Type", contentType)
	response := httptest.NewRecorder()

	server.Routes().ServeHTTP(response, request)

	if response.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusUnsupportedMediaType, response.Body.String())
	}
}

func TestDeleteMissingClipReturnsNotFound(t *testing.T) {
	server, _, _ := newTestServer(t, fakeMediaProcessor{}, fakeTranscriber{})
	request := httptest.NewRequest(http.MethodDelete, "/api/clips/missing", nil)
	response := httptest.NewRecorder()

	server.Routes().ServeHTTP(response, request)

	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusNotFound, response.Body.String())
	}
}

func TestClipSourceEndpoint(t *testing.T) {
	server, store, paths := newTestServer(t, fakeMediaProcessor{}, fakeTranscriber{})
	sourcePath := filepath.Join(paths.UploadsDir, "stream.mp3")
	if err := os.WriteFile(sourcePath, []byte("0123456789"), 0o600); err != nil {
		t.Fatal(err)
	}
	createSourceClip(t, store, "stream", sourcePath)

	t.Run("success", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodGet, "/api/clips/stream/source", nil)
		response := httptest.NewRecorder()
		server.Routes().ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusOK, response.Body.String())
		}
		if response.Body.String() != "0123456789" {
			t.Fatalf("body = %q, want source content", response.Body.String())
		}
		if response.Header().Get("Content-Type") == "" {
			t.Fatal("Content-Type header is missing")
		}
	})

	t.Run("range", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodGet, "/api/clips/stream/source", nil)
		request.Header.Set("Range", "bytes=2-5")
		response := httptest.NewRecorder()
		server.Routes().ServeHTTP(response, request)
		if response.Code != http.StatusPartialContent {
			t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusPartialContent, response.Body.String())
		}
		if response.Body.String() != "2345" {
			t.Fatalf("body = %q, want %q", response.Body.String(), "2345")
		}
		if got := response.Header().Get("Content-Range"); got != "bytes 2-5/10" {
			t.Fatalf("Content-Range = %q, want %q", got, "bytes 2-5/10")
		}
	})

	t.Run("missing clip", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodGet, "/api/clips/missing/source", nil)
		response := httptest.NewRecorder()
		server.Routes().ServeHTTP(response, request)
		if response.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusNotFound, response.Body.String())
		}
	})

	t.Run("missing source path", func(t *testing.T) {
		createSourceClip(t, store, "empty-source", "")
		request := httptest.NewRequest(http.MethodGet, "/api/clips/empty-source/source", nil)
		response := httptest.NewRecorder()
		server.Routes().ServeHTTP(response, request)
		if response.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusNotFound, response.Body.String())
		}
	})

	t.Run("missing source file", func(t *testing.T) {
		createSourceClip(t, store, "missing-file", filepath.Join(paths.UploadsDir, "missing.mp3"))
		request := httptest.NewRequest(http.MethodGet, "/api/clips/missing-file/source", nil)
		response := httptest.NewRecorder()
		server.Routes().ServeHTTP(response, request)
		if response.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusNotFound, response.Body.String())
		}
	})

	t.Run("wrong method", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodPost, "/api/clips/stream/source", nil)
		response := httptest.NewRecorder()
		server.Routes().ServeHTTP(response, request)
		if response.Code != http.StatusMethodNotAllowed {
			t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusMethodNotAllowed, response.Body.String())
		}
	})

	t.Run("unknown child path", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodGet, "/api/clips/stream/source/extra", nil)
		response := httptest.NewRecorder()
		server.Routes().ServeHTTP(response, request)
		if response.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusNotFound, response.Body.String())
		}
	})
}

func TestDemoImportEndpointIsNotExposed(t *testing.T) {
	server, _, _ := newTestServer(t, fakeMediaProcessor{}, fakeTranscriber{})
	request := httptest.NewRequest(http.MethodPost, "/api/demo/import", nil)
	response := httptest.NewRecorder()

	server.Routes().ServeHTTP(response, request)

	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusNotFound, response.Body.String())
	}
}

func TestFailedReprocessPreservesExistingClip(t *testing.T) {
	mediaProcessor := fakeMediaProcessor{extract: func(_ context.Context, _ string, runDir string) (media.Result, error) {
		if err := os.MkdirAll(runDir, 0o755); err != nil {
			return media.Result{}, err
		}
		audioPath := filepath.Join(runDir, "audio.mp3")
		wavPath := filepath.Join(runDir, "transcribe.wav")
		if err := os.WriteFile(audioPath, []byte("new audio"), 0o600); err != nil {
			return media.Result{}, err
		}
		if err := os.WriteFile(wavPath, []byte("new wav"), 0o600); err != nil {
			return media.Result{}, err
		}
		return media.Result{BrowserAudioPath: audioPath, TranscriptionWAVPath: wavPath}, nil
	}}
	transcriber := fakeTranscriber{transcribe: func(context.Context, string, string) (transcribe.Result, error) {
		return transcribe.Result{}, errors.New("transcriber failed")
	}}
	server, store, paths := newTestServer(t, mediaProcessor, transcriber)

	clipID := "existing"
	sourcePath := filepath.Join(paths.UploadsDir, clipID+".mp3")
	oldAudioPath := filepath.Join(paths.ClipsDir, clipID, "audio.mp3")
	if err := os.MkdirAll(filepath.Dir(oldAudioPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sourcePath, []byte("source"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(oldAudioPath, []byte("old audio"), 0o600); err != nil {
		t.Fatal(err)
	}
	original := db.Clip{
		ID:         clipID,
		Title:      "Existing",
		SourcePath: sourcePath,
		SourceHash: "hash",
		AudioPath:  oldAudioPath,
		Duration:   12.5,
		Language:   "en",
		Status:     "ready",
		Error:      "old warning",
		CreatedAt:  time.Now(),
	}
	if err := store.CreateClip(context.Background(), original); err != nil {
		t.Fatal(err)
	}
	if err := store.ReplaceSegments(context.Background(), clipID, []db.Segment{{ClipID: clipID, Start: 1, End: 2, Text: "old segment"}}); err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodPost, "/api/clips/"+clipID+"/reprocess", nil)
	response := httptest.NewRecorder()
	server.Routes().ServeHTTP(response, request)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusInternalServerError, response.Body.String())
	}

	clip, segments, err := store.GetClip(context.Background(), clipID)
	if err != nil {
		t.Fatal(err)
	}
	if clip.AudioPath != original.AudioPath || clip.Duration != original.Duration || clip.Language != original.Language || clip.Status != original.Status || clip.Error != original.Error {
		t.Fatalf("clip changed after failed reprocess: %#v", clip)
	}
	if len(segments) != 1 || segments[0].Text != "old segment" || segments[0].Start != 1 || segments[0].End != 2 {
		t.Fatalf("segments changed after failed reprocess: %#v", segments)
	}
	if data, err := os.ReadFile(oldAudioPath); err != nil || string(data) != "old audio" {
		t.Fatalf("old audio was not preserved: data=%q err=%v", data, err)
	}
	runs, err := filepath.Glob(filepath.Join(paths.ClipsDir, clipID, "runs", "*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 0 {
		t.Fatalf("failed run directories remain: %v", runs)
	}
}

func TestSuccessfulReprocessSwitchesMediaAndSegments(t *testing.T) {
	mediaProcessor := fakeMediaProcessor{extract: func(_ context.Context, _ string, runDir string) (media.Result, error) {
		if err := os.MkdirAll(runDir, 0o755); err != nil {
			return media.Result{}, err
		}
		audioPath := filepath.Join(runDir, "audio.mp3")
		wavPath := filepath.Join(runDir, "transcribe.wav")
		if err := os.WriteFile(audioPath, []byte("new audio"), 0o600); err != nil {
			return media.Result{}, err
		}
		if err := os.WriteFile(wavPath, []byte("new wav"), 0o600); err != nil {
			return media.Result{}, err
		}
		return media.Result{BrowserAudioPath: audioPath, TranscriptionWAVPath: wavPath}, nil
	}}
	transcriber := fakeTranscriber{transcribe: func(context.Context, string, string) (transcribe.Result, error) {
		return transcribe.Result{
			Language: "fr",
			Duration: 21,
			Segments: []db.Segment{{Start: 3, End: 4, Text: "new segment"}},
		}, nil
	}}
	server, store, paths := newTestServer(t, mediaProcessor, transcriber)
	clipID := "success"
	sourcePath := filepath.Join(paths.UploadsDir, clipID+".mp3")
	oldAudioPath := filepath.Join(paths.ClipsDir, clipID, "audio.mp3")
	oldWAVPath := filepath.Join(paths.ClipsDir, clipID, "transcribe.wav")
	if err := os.MkdirAll(filepath.Dir(oldAudioPath), 0o755); err != nil {
		t.Fatal(err)
	}
	for path, data := range map[string]string{sourcePath: "source", oldAudioPath: "old audio", oldWAVPath: "old wav"} {
		if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.CreateClip(context.Background(), db.Clip{
		ID: clipID, Title: "Success", SourcePath: sourcePath, AudioPath: oldAudioPath,
		Duration: 12.5, Status: "ready", CreatedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.ReplaceSegments(context.Background(), clipID, []db.Segment{{Start: 1, End: 2, Text: "old segment"}}); err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodPost, "/api/clips/"+clipID+"/reprocess", nil)
	response := httptest.NewRecorder()
	server.Routes().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusOK, response.Body.String())
	}

	clip, segments, err := store.GetClip(context.Background(), clipID)
	if err != nil {
		t.Fatal(err)
	}
	if clip.AudioPath == oldAudioPath || clip.Duration != 21 || clip.Language != "fr" || clip.Status != "ready" {
		t.Fatalf("clip was not switched to completed run: %#v", clip)
	}
	if len(segments) != 1 || segments[0].Text != "new segment" {
		t.Fatalf("segments were not replaced: %#v", segments)
	}
	if _, err := os.Stat(clip.AudioPath); err != nil {
		t.Fatalf("new audio missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(clip.AudioPath), "transcribe.wav")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("transcription wav was not cleaned: %v", err)
	}
	for _, oldPath := range []string{oldAudioPath, oldWAVPath} {
		if _, err := os.Stat(oldPath); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("old media was not cleaned: %s err=%v", oldPath, err)
		}
	}
}

func TestDeleteReturnsConflictWhenClipLocked(t *testing.T) {
	server, store, paths := newTestServer(t, fakeMediaProcessor{}, fakeTranscriber{})
	clipID := "locked"
	sourcePath := filepath.Join(paths.UploadsDir, clipID+".mp3")
	if err := os.WriteFile(sourcePath, []byte("source"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateClip(context.Background(), db.Clip{
		ID: clipID, Title: "Locked", SourcePath: sourcePath, Status: "ready", CreatedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	unlock, ok := server.clipLocks.tryLock(clipID)
	if !ok {
		t.Fatal("failed to lock clip")
	}
	defer unlock()

	request := httptest.NewRequest(http.MethodDelete, "/api/clips/"+clipID, nil)
	response := httptest.NewRecorder()
	server.Routes().ServeHTTP(response, request)
	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusConflict, response.Body.String())
	}
	if _, _, err := store.GetClip(context.Background(), clipID); err != nil {
		t.Fatalf("locked clip was deleted: %v", err)
	}
}

func TestUploadReturnsConflictWhenProcessingBusy(t *testing.T) {
	server, _, _ := newTestServer(t, fakeMediaProcessor{}, fakeTranscriber{})
	finish, ok := server.tryStartProcessing()
	if !ok {
		t.Fatal("failed to occupy processing slot")
	}
	defer finish()

	body, contentType := multipartUpload(t, "sample.mp3", []byte("audio"))
	request := httptest.NewRequest(http.MethodPost, "/api/clips", body)
	request.Header.Set("Content-Type", contentType)
	response := httptest.NewRecorder()
	server.Routes().ServeHTTP(response, request)

	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusConflict, response.Body.String())
	}
}

func newTestServer(t *testing.T, mediaProcessor MediaProcessor, transcriber Transcriber) (*Server, *db.Store, storage.Paths) {
	t.Helper()
	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	paths := storage.Paths{
		ProjectRoot: root,
		DataDir:     dataDir,
		UploadsDir:  filepath.Join(dataDir, "uploads"),
		ClipsDir:    filepath.Join(dataDir, "clips"),
		DBPath:      filepath.Join(dataDir, "test.db"),
	}
	if err := paths.Ensure(); err != nil {
		t.Fatal(err)
	}
	store, err := db.Open(context.Background(), paths.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return New(store, paths, mediaProcessor, transcriber, nil), store, paths
}

func seedClipList(t *testing.T, store *db.Store) {
	t.Helper()
	base := time.Date(2026, 7, 10, 8, 0, 0, 0, time.UTC)
	clips := []db.Clip{
		{ID: "alpha-ready", Title: "Alpha Lesson", SourcePath: "/tmp/alpha-ready.mp3", Language: "en", Status: "ready", CreatedAt: base.Add(time.Second)},
		{ID: "alpha-processing", Title: "alpha practice", SourcePath: "/tmp/alpha-processing.mp3", Language: "fr", Status: "processing", CreatedAt: base.Add(2 * time.Second)},
		{ID: "beta-error", Title: "Beta", SourcePath: "/tmp/beta-error.mp3", Language: "EN", Status: "error", CreatedAt: base.Add(3 * time.Second)},
		{ID: "gamma-ready", Title: "Gamma", SourcePath: "/tmp/gamma-ready.mp3", Language: "de", Status: "ready", CreatedAt: base.Add(4 * time.Second)},
	}
	for _, clip := range clips {
		if err := store.CreateClip(context.Background(), clip); err != nil {
			t.Fatal(err)
		}
	}
}

func createSourceClip(t *testing.T, store *db.Store, id string, sourcePath string) {
	t.Helper()
	if err := store.CreateClip(context.Background(), db.Clip{
		ID:         id,
		Title:      id,
		SourcePath: sourcePath,
		Status:     "ready",
		CreatedAt:  time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
}

func requestClipList(t *testing.T, server *Server, target string) clipListPayload {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, target, nil)
	response := httptest.NewRecorder()
	server.Routes().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusOK, response.Body.String())
	}
	var payload clipListPayload
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	return payload
}

func multipartUpload(t *testing.T, filename string, data []byte) (*bytes.Buffer, string) {
	t.Helper()
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return body, writer.FormDataContentType()
}
