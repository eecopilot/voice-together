package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"voice-together/backend/internal/api"
	"voice-together/backend/internal/db"
	"voice-together/backend/internal/media"
	"voice-together/backend/internal/storage"
	"voice-together/backend/internal/transcribe"
	"voice-together/backend/internal/web"
)

func main() {
	addr := flag.String("addr", envOrDefault("VOICE_TOGETHER_ADDR", "0.0.0.0:8788"), "HTTP listen address")
	root := flag.String("root", envOrDefault("VOICE_TOGETHER_ROOT", guessProjectRoot()), "project root")
	flag.Parse()

	paths := storage.New(*root)
	if err := paths.Ensure(); err != nil {
		log.Fatal(err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	store, err := db.Open(ctx, paths.DBPath)
	if err != nil {
		log.Fatal(err)
	}
	defer store.Close()

	apiServer := api.New(store, paths, media.New(paths.ProjectRoot), transcribe.New(paths.ProjectRoot), web.Dist())
	server := &http.Server{
		Addr:              *addr,
		Handler:           apiServer.Routes(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Printf("Voice Together listening on http://%s", *addr)
		log.Printf("project root: %s", paths.ProjectRoot)
		errCh <- server.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			log.Printf("HTTP shutdown: %v", err)
			_ = server.Close()
		}
	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			log.Printf("HTTP server stopped: %v", err)
		}
	}

	jobShutdownCtx, cancelJobs := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelJobs()
	if err := apiServer.Shutdown(jobShutdownCtx); err != nil {
		log.Printf("background processing shutdown: %v", err)
	}
}

func guessProjectRoot() string {
	if cwd, err := os.Getwd(); err == nil {
		if filepath.Base(cwd) == "backend" {
			return filepath.Dir(cwd)
		}
		return cwd
	}
	return "."
}

func envOrDefault(key string, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}
