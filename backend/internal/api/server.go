package api

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime/multipart"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"voice-together/backend/internal/db"
	"voice-together/backend/internal/media"
	"voice-together/backend/internal/storage"
	"voice-together/backend/internal/transcribe"
)

type Server struct {
	store      *db.Store
	paths      storage.Paths
	media      media.Processor
	transcribe transcribe.Service
	staticFS   fs.FS
}

type clipResponse struct {
	Clip     db.Clip      `json:"clip"`
	Segments []db.Segment `json:"segments"`
}

func New(store *db.Store, paths storage.Paths, mediaProcessor media.Processor, transcriber transcribe.Service, staticFS fs.FS) *Server {
	return &Server{
		store:      store,
		paths:      paths,
		media:      mediaProcessor,
		transcribe: transcriber,
		staticFS:   staticFS,
	}
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/health", s.handleHealth)
	mux.HandleFunc("/api/clips", s.handleClips)
	mux.HandleFunc("/api/clips/", s.handleClip)
	mux.HandleFunc("/api/demo/import", s.handleDemoImport)
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
		clips, err := s.store.ListClips(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"clips": clips})
	case http.MethodPost:
		s.handleUpload(w, r)
	default:
		methodNotAllowed(w)
	}
}

func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 500<<20)
	if err := r.ParseMultipartForm(64 << 20); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, errors.New("file field is required"))
		return
	}
	defer file.Close()

	title := strings.TrimSpace(r.FormValue("title"))
	if title == "" {
		title = strings.TrimSuffix(header.Filename, filepath.Ext(header.Filename))
	}
	language := strings.TrimSpace(r.FormValue("language"))

	response, err := s.importMultipart(r.Context(), file, header, title, language)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusCreated, response)
}

func (s *Server) handleDemoImport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}

	demoPath := filepath.Join(s.paths.ProjectRoot, "demo.mp4")
	if _, err := os.Stat(demoPath); err != nil {
		writeError(w, http.StatusNotFound, fmt.Errorf("demo.mp4 not found: %w", err))
		return
	}

	response, err := s.importLocalFile(r.Context(), demoPath, "demo", "")
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusCreated, response)
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
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
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
	ext := safeExt(header.Filename)
	sourcePath := filepath.Join(s.paths.UploadsDir, id+ext)
	if err := writeFile(sourcePath, file); err != nil {
		return clipResponse{}, err
	}
	return s.processSource(ctx, id, title, sourcePath, language)
}

func (s *Server) importLocalFile(ctx context.Context, sourcePath string, title string, language string) (clipResponse, error) {
	id := newID()
	ext := safeExt(sourcePath)
	savedPath := filepath.Join(s.paths.UploadsDir, id+ext)
	source, err := os.Open(sourcePath)
	if err != nil {
		return clipResponse{}, err
	}
	defer source.Close()
	if err := writeFile(savedPath, source); err != nil {
		return clipResponse{}, err
	}
	return s.processSource(ctx, id, title, savedPath, language)
}

func (s *Server) processSource(ctx context.Context, id string, title string, sourcePath string, language string) (clipResponse, error) {
	now := time.Now()
	clip := db.Clip{
		ID:         id,
		Title:      title,
		SourcePath: sourcePath,
		Language:   language,
		Status:     "processing",
		CreatedAt:  now,
	}
	if err := s.store.CreateClip(ctx, clip); err != nil {
		return clipResponse{}, err
	}

	clipDir := filepath.Join(s.paths.ClipsDir, id)
	mediaResult, err := s.media.Extract(ctx, sourcePath, clipDir)
	if err != nil {
		_ = s.store.UpdateClipProcessed(ctx, id, "", 0, language, "error", err.Error())
		return clipResponse{}, err
	}

	transcribeResult, err := s.transcribe.Transcribe(ctx, mediaResult.TranscriptionWAVPath, language)
	if err != nil {
		_ = s.store.UpdateClipProcessed(ctx, id, mediaResult.BrowserAudioPath, 0, language, "error", err.Error())
		return clipResponse{}, err
	}

	for index := range transcribeResult.Segments {
		transcribeResult.Segments[index].ClipID = id
	}
	if err := s.store.ReplaceSegments(ctx, id, transcribeResult.Segments); err != nil {
		return clipResponse{}, err
	}

	finalLanguage := language
	if finalLanguage == "" {
		finalLanguage = transcribeResult.Language
	}
	if err := s.store.UpdateClipProcessed(ctx, id, mediaResult.BrowserAudioPath, transcribeResult.Duration, finalLanguage, "ready", transcribeResult.Warning); err != nil {
		return clipResponse{}, err
	}

	readyClip, segments, err := s.store.GetClip(ctx, id)
	if err != nil {
		return clipResponse{}, err
	}
	return clipResponse{Clip: readyClip, Segments: segments}, nil
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

func safeExt(filename string) string {
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".mp4", ".mov", ".m4v", ".webm", ".mkv", ".mp3", ".m4a", ".wav":
		return ext
	default:
		return ".bin"
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
