package api

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"voice-together/backend/internal/db"
	"voice-together/backend/internal/media"
	"voice-together/backend/internal/storage"
	"voice-together/backend/internal/transcribe"
)

const maxUploadBytes int64 = 500 << 20

var errUnsupportedMedia = errors.New("unsupported media type")

type MediaProcessor interface {
	Extract(context.Context, string, string) (media.Result, error)
}

type Transcriber interface {
	Transcribe(context.Context, string, string) (transcribe.Result, error)
}

type keyedLocks struct {
	mu   sync.Mutex
	held map[string]struct{}
}

func newKeyedLocks() *keyedLocks {
	return &keyedLocks{held: make(map[string]struct{})}
}

func (l *keyedLocks) tryLock(key string) (func(), bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, exists := l.held[key]; exists {
		return nil, false
	}
	l.held[key] = struct{}{}
	return func() {
		l.mu.Lock()
		delete(l.held, key)
		l.mu.Unlock()
	}, true
}

type Server struct {
	store      *db.Store
	paths      storage.Paths
	media      MediaProcessor
	transcribe Transcriber
	staticFS   fs.FS
	processing chan struct{}
	clipLocks  *keyedLocks
}

type clipResponse struct {
	Clip     db.Clip      `json:"clip"`
	Segments []db.Segment `json:"segments"`
	Reused   bool         `json:"reused,omitempty"`
}

func New(store *db.Store, paths storage.Paths, mediaProcessor MediaProcessor, transcriber Transcriber, staticFS fs.FS) *Server {
	return &Server{
		store:      store,
		paths:      paths,
		media:      mediaProcessor,
		transcribe: transcriber,
		staticFS:   staticFS,
		processing: make(chan struct{}, 1),
		clipLocks:  newKeyedLocks(),
	}
}

func (s *Server) tryStartProcessing() (func(), bool) {
	select {
	case s.processing <- struct{}{}:
		return func() { <-s.processing }, true
	default:
		return nil, false
	}
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/health", s.handleHealth)
	mux.HandleFunc("/api/clips", s.handleClips)
	mux.HandleFunc("/api/clips/", s.handleClip)
	mux.HandleFunc("/api/", http.NotFound)
	mux.HandleFunc("/", s.handleStatic)
	return withCORS(mux)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok",
		"time":   time.Now().Format(time.RFC3339),
	})
}

func (s *Server) handleClips(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		query, err := parseClipListQuery(r)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		result, err := s.store.QueryClips(r.Context(), query)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"clips":  result.Clips,
			"total":  result.Total,
			"limit":  query.Limit,
			"offset": query.Offset,
			"counts": result.Counts,
		})
	case http.MethodPost:
		s.handleUpload(w, r)
	default:
		methodNotAllowed(w)
	}
}

func parseClipListQuery(r *http.Request) (db.ClipListQuery, error) {
	values := r.URL.Query()
	status := strings.TrimSpace(values.Get("status"))
	switch status {
	case "", "all", "ready", "processing", "error":
	default:
		return db.ClipListQuery{}, errors.New("invalid status")
	}
	limit, err := parseQueryInteger(values.Get("limit"), 10, 1, 100, "limit")
	if err != nil {
		return db.ClipListQuery{}, err
	}
	offset, err := parseQueryInteger(values.Get("offset"), 0, 0, -1, "offset")
	if err != nil {
		return db.ClipListQuery{}, err
	}
	return db.ClipListQuery{
		Query:  strings.TrimSpace(values.Get("q")),
		Status: status,
		Limit:  limit,
		Offset: offset,
	}, nil
}

func parseQueryInteger(raw string, defaultValue int, minimum int, maximum int, name string) (int, error) {
	if raw == "" {
		return defaultValue, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < minimum || (maximum >= 0 && value > maximum) {
		return 0, fmt.Errorf("invalid %s", name)
	}
	return value, nil
}

func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	finishProcessing, ok := s.tryStartProcessing()
	if !ok {
		writeError(w, http.StatusConflict, errors.New("media processing is busy"))
		return
	}
	defer finishProcessing()

	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes)
	if err := r.ParseMultipartForm(64 << 20); err != nil {
		var maxBytesError *http.MaxBytesError
		if errors.As(err, &maxBytesError) {
			writeError(w, http.StatusRequestEntityTooLarge, errors.New("upload is too large"))
			return
		}
		writeError(w, http.StatusBadRequest, err)
		return
	}
	defer r.MultipartForm.RemoveAll()

	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, errors.New("file field is required"))
		return
	}
	defer file.Close()
	if _, err := safeExt(header.Filename); err != nil {
		writeError(w, http.StatusUnsupportedMediaType, err)
		return
	}

	title := strings.TrimSpace(r.FormValue("title"))
	if title == "" {
		title = strings.TrimSuffix(header.Filename, filepath.Ext(header.Filename))
	}
	language := strings.TrimSpace(r.FormValue("language"))

	response, err := s.importMultipart(r.Context(), file, header, title, language)
	if err != nil {
		if errors.Is(err, errUnsupportedMedia) {
			writeError(w, http.StatusUnsupportedMediaType, err)
			return
		}
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	status := http.StatusCreated
	if response.Reused {
		status = http.StatusOK
	}
	writeJSON(w, status, response)
}

func (s *Server) handleClip(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/clips/")
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		http.NotFound(w, r)
		return
	}
	clipID := parts[0]

	if len(parts) == 2 && parts[1] == "audio" {
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		s.handleAudio(w, r, clipID)
		return
	}

	if len(parts) == 2 && parts[1] == "source" {
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		s.handleSource(w, r, clipID)
		return
	}

	if len(parts) == 2 && parts[1] == "reprocess" {
		if r.Method != http.MethodPost {
			methodNotAllowed(w)
			return
		}
		s.handleReprocess(w, r, clipID)
		return
	}
	if len(parts) != 1 {
		http.NotFound(w, r)
		return
	}

	switch r.Method {
	case http.MethodGet:
		clip, segments, err := s.store.GetClip(r.Context(), clipID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				writeError(w, http.StatusNotFound, errors.New("clip not found"))
				return
			}
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, clipResponse{Clip: clip, Segments: segments})
	case http.MethodDelete:
		clip, _, err := s.store.GetClip(r.Context(), clipID)
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, errors.New("clip not found"))
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		unlock, ok := s.clipLocks.tryLock(clipID)
		if !ok {
			writeError(w, http.StatusConflict, errors.New("clip is busy"))
			return
		}
		defer unlock()
		clip, _, err = s.store.GetClip(r.Context(), clipID)
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, errors.New("clip not found"))
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		if err := s.store.DeleteClip(r.Context(), clipID); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		_ = os.RemoveAll(filepath.Join(s.paths.ClipsDir, clipID))
		if clip.SourcePath != "" {
			_ = os.Remove(clip.SourcePath)
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
	default:
		methodNotAllowed(w)
	}
}

func (s *Server) handleReprocess(w http.ResponseWriter, r *http.Request, clipID string) {
	finishProcessing, ok := s.tryStartProcessing()
	if !ok {
		writeError(w, http.StatusConflict, errors.New("media processing is busy"))
		return
	}
	defer finishProcessing()

	unlock, ok := s.clipLocks.tryLock(clipID)
	if !ok {
		writeError(w, http.StatusConflict, errors.New("clip is busy"))
		return
	}
	defer unlock()

	clip, _, err := s.store.GetClip(r.Context(), clipID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, errors.New("clip not found"))
			return
		}
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if clip.SourcePath == "" {
		writeError(w, http.StatusBadRequest, errors.New("clip source is missing"))
		return
	}
	if _, err := os.Stat(clip.SourcePath); err != nil {
		writeError(w, http.StatusNotFound, fmt.Errorf("clip source not found: %w", err))
		return
	}

	if clip.SourceHash == "" {
		if sourceHash, err := hashFile(clip.SourcePath); err == nil {
			clip.SourceHash = sourceHash
			_ = s.store.UpdateClipSourceHash(r.Context(), clip.ID, sourceHash)
		}
	}

	response, err := s.processExistingSource(r.Context(), clip, true)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleAudio(w http.ResponseWriter, r *http.Request, clipID string) {
	clip, _, err := s.store.GetClip(r.Context(), clipID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, errors.New("clip not found"))
			return
		}
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if clip.AudioPath == "" {
		writeError(w, http.StatusNotFound, errors.New("clip audio is not ready"))
		return
	}
	http.ServeFile(w, r, clip.AudioPath)
}

func (s *Server) handleSource(w http.ResponseWriter, r *http.Request, clipID string) {
	clip, _, err := s.store.GetClip(r.Context(), clipID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, errors.New("clip not found"))
			return
		}
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if clip.SourcePath == "" {
		writeError(w, http.StatusNotFound, errors.New("clip source is missing"))
		return
	}
	info, err := os.Stat(clip.SourcePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			writeError(w, http.StatusNotFound, errors.New("clip source not found"))
			return
		}
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if info.IsDir() {
		writeError(w, http.StatusNotFound, errors.New("clip source not found"))
		return
	}
	http.ServeFile(w, r, clip.SourcePath)
}

func (s *Server) handleStatic(w http.ResponseWriter, r *http.Request) {
	if s.staticFS != nil && s.paths.WebDir == "" {
		s.handleEmbeddedStatic(w, r)
		return
	}

	if r.URL.Path == "/" {
		setIndexCacheHeader(w)
		http.ServeFile(w, r, filepath.Join(s.paths.WebDir, "index.html"))
		return
	}

	requestedPath := filepath.Clean(strings.TrimPrefix(r.URL.Path, "/"))
	fullPath := filepath.Join(s.paths.WebDir, requestedPath)
	if !strings.HasPrefix(fullPath, s.paths.WebDir) {
		http.NotFound(w, r)
		return
	}
	if info, err := os.Stat(fullPath); err == nil && !info.IsDir() {
		setStaticCacheHeader(w, filepath.ToSlash(requestedPath))
		http.ServeFile(w, r, fullPath)
		return
	}
	if isStaticAssetPath(filepath.ToSlash(requestedPath)) {
		http.NotFound(w, r)
		return
	}
	setIndexCacheHeader(w)
	http.ServeFile(w, r, filepath.Join(s.paths.WebDir, "index.html"))
}

func (s *Server) handleEmbeddedStatic(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/" {
		setIndexCacheHeader(w)
		http.ServeFileFS(w, r, s.staticFS, "index.html")
		return
	}

	requestedPath := path.Clean(strings.TrimPrefix(r.URL.Path, "/"))
	if requestedPath == "." || strings.HasPrefix(requestedPath, "../") || strings.HasPrefix(requestedPath, "/") {
		http.NotFound(w, r)
		return
	}

	if info, err := fs.Stat(s.staticFS, requestedPath); err == nil && !info.IsDir() {
		setStaticCacheHeader(w, requestedPath)
		http.ServeFileFS(w, r, s.staticFS, requestedPath)
		return
	}
	if isStaticAssetPath(requestedPath) {
		http.NotFound(w, r)
		return
	}
	setIndexCacheHeader(w)
	http.ServeFileFS(w, r, s.staticFS, "index.html")
}

func isStaticAssetPath(requestedPath string) bool {
	return strings.HasPrefix(requestedPath, "assets/") || path.Ext(requestedPath) != ""
}

func setIndexCacheHeader(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-cache")
}

func setStaticCacheHeader(w http.ResponseWriter, requestedPath string) {
	if strings.HasPrefix(requestedPath, "assets/") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	}
}

func (s *Server) importMultipart(ctx context.Context, file multipart.File, header *multipart.FileHeader, title string, language string) (clipResponse, error) {
	id := newID()
	ext, err := safeExt(header.Filename)
	if err != nil {
		return clipResponse{}, err
	}
	sourcePath := filepath.Join(s.paths.UploadsDir, id+ext)
	sourceHash, err := writeFileAndHash(sourcePath, file)
	if err != nil {
		return clipResponse{}, err
	}
	if existing, ok, err := s.findExistingByHash(ctx, sourceHash); err != nil {
		_ = os.Remove(sourcePath)
		return clipResponse{}, err
	} else if ok {
		_ = os.Remove(sourcePath)
		existing.Reused = true
		return existing, nil
	}
	return s.processSource(ctx, id, title, sourcePath, sourceHash, language)
}

func (s *Server) processSource(ctx context.Context, id string, title string, sourcePath string, sourceHash string, language string) (clipResponse, error) {
	unlock, ok := s.clipLocks.tryLock(id)
	if !ok {
		return clipResponse{}, errors.New("clip is busy")
	}
	defer unlock()

	now := time.Now()
	clip := db.Clip{
		ID:         id,
		Title:      title,
		SourcePath: sourcePath,
		SourceHash: sourceHash,
		Language:   language,
		Status:     "processing",
		CreatedAt:  now,
	}
	if err := s.store.CreateClip(ctx, clip); err != nil {
		_ = os.Remove(sourcePath)
		return clipResponse{}, err
	}
	return s.processExistingSource(ctx, clip, false)
}

func (s *Server) processExistingSource(ctx context.Context, clip db.Clip, preserveExisting bool) (clipResponse, error) {
	runDir := filepath.Join(s.paths.ClipsDir, clip.ID, "runs", newID())
	removeRun := true
	defer func() {
		if removeRun {
			_ = os.RemoveAll(runDir)
		}
	}()

	mediaResult, err := s.media.Extract(ctx, clip.SourcePath, runDir)
	if err != nil {
		if !preserveExisting {
			_ = s.store.UpdateClipProcessed(ctx, clip.ID, "", 0, clip.Language, "error", err.Error())
		}
		return clipResponse{}, err
	}
	defer os.Remove(mediaResult.TranscriptionWAVPath)

	transcribeResult, err := s.transcribe.Transcribe(ctx, mediaResult.TranscriptionWAVPath, clip.Language)
	if err != nil {
		if !preserveExisting {
			_ = s.store.UpdateClipProcessed(ctx, clip.ID, "", 0, clip.Language, "error", err.Error())
		}
		return clipResponse{}, err
	}

	for index := range transcribeResult.Segments {
		transcribeResult.Segments[index].ClipID = clip.ID
	}

	finalLanguage := clip.Language
	if finalLanguage == "" {
		finalLanguage = transcribeResult.Language
	}
	if err := s.store.CompleteClipProcessing(
		ctx,
		clip.ID,
		mediaResult.BrowserAudioPath,
		transcribeResult.Duration,
		finalLanguage,
		"ready",
		transcribeResult.Warning,
		transcribeResult.Segments,
	); err != nil {
		if !preserveExisting {
			_ = s.store.UpdateClipProcessed(ctx, clip.ID, "", 0, clip.Language, "error", err.Error())
		}
		return clipResponse{}, err
	}
	removeRun = false
	s.cleanupOldMedia(clip, runDir)

	readyClip, segments, err := s.store.GetClip(ctx, clip.ID)
	if err != nil {
		return clipResponse{}, err
	}
	return clipResponse{Clip: readyClip, Segments: segments}, nil
}

func (s *Server) cleanupOldMedia(clip db.Clip, newRunDir string) {
	if clip.AudioPath == "" {
		return
	}
	clipDir := filepath.Join(s.paths.ClipsDir, clip.ID)
	oldDir := filepath.Dir(clip.AudioPath)
	if oldDir == clipDir {
		for _, filename := range []string{"audio.mp3", "transcribe.wav"} {
			if err := os.Remove(filepath.Join(clipDir, filename)); err != nil && !errors.Is(err, os.ErrNotExist) {
				log.Printf("clean old clip media: %v", err)
			}
		}
		return
	}
	runsDir := filepath.Join(clipDir, "runs")
	rel, err := filepath.Rel(runsDir, oldDir)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") || oldDir == newRunDir {
		return
	}
	if err := os.RemoveAll(oldDir); err != nil {
		log.Printf("clean old clip run: %v", err)
	}
}

func (s *Server) findExistingByHash(ctx context.Context, sourceHash string) (clipResponse, bool, error) {
	if sourceHash == "" {
		return clipResponse{}, false, nil
	}

	clip, segments, err := s.store.GetClipBySourceHash(ctx, sourceHash)
	if err == nil {
		return clipResponse{Clip: clip, Segments: segments}, true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return clipResponse{}, false, err
	}

	clips, err := s.store.ListClips(ctx)
	if err != nil {
		return clipResponse{}, false, err
	}
	updated := false
	for _, clip := range clips {
		if clip.SourceHash != "" || clip.SourcePath == "" {
			continue
		}
		existingHash, err := hashFile(clip.SourcePath)
		if err != nil {
			continue
		}
		if err := s.store.UpdateClipSourceHash(ctx, clip.ID, existingHash); err != nil {
			return clipResponse{}, false, err
		}
		if existingHash == sourceHash {
			updated = true
		}
	}
	if !updated {
		return clipResponse{}, false, nil
	}

	clip, segments, err = s.store.GetClipBySourceHash(ctx, sourceHash)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return clipResponse{}, false, nil
		}
		return clipResponse{}, false, err
	}
	return clipResponse{Clip: clip, Segments: segments}, true, nil
}

func writeFile(path string, source io.Reader) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	destination, err := os.Create(path)
	if err != nil {
		return err
	}
	defer destination.Close()
	_, err = io.Copy(destination, source)
	return err
}

func writeFileAndHash(path string, source io.Reader) (hash string, err error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	destination, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", err
	}
	complete := false
	defer func() {
		if !complete {
			_ = os.Remove(path)
		}
	}()

	hasher := sha256.New()
	if _, err := io.Copy(io.MultiWriter(destination, hasher), source); err != nil {
		_ = destination.Close()
		return "", err
	}
	if err := destination.Close(); err != nil {
		return "", err
	}
	complete = true
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

func hashFile(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

func safeExt(filename string) (string, error) {
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".mp4", ".mov", ".m4v", ".webm", ".mkv", ".mp3", ".m4a", ".wav":
		return ext, nil
	default:
		return "", fmt.Errorf("%w: %q", errUnsupportedMedia, ext)
	}
}

func newID() string {
	var bytes [8]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return time.Now().Format("20060102150405") + "-" + hex.EncodeToString(bytes[:])
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

func methodNotAllowed(w http.ResponseWriter) {
	writeError(w, http.StatusMethodNotAllowed, errors.New("method not allowed"))
}

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
