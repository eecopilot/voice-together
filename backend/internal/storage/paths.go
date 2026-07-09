package storage

import (
	"os"
	"path/filepath"
)

type Paths struct {
	ProjectRoot string
	DataDir     string
	UploadsDir  string
	ClipsDir    string
	DBPath      string
	WebDir      string
}

func New(projectRoot string) Paths {
	dataDir := envOrDefault("VOICE_TOGETHER_DATA_DIR", filepath.Join(projectRoot, "backend", "data"))
	webDir := envOrDefault("VOICE_TOGETHER_WEB_DIR", filepath.Join(projectRoot, "frontend", "dist"))
	return Paths{
		ProjectRoot: projectRoot,
		DataDir:     dataDir,
		UploadsDir:  filepath.Join(dataDir, "uploads"),
		ClipsDir:    filepath.Join(dataDir, "clips"),
		DBPath:      filepath.Join(dataDir, "voice_together.db"),
		WebDir:      webDir,
	}
}

func (p Paths) Ensure() error {
	for _, dir := range []string{p.DataDir, p.UploadsDir, p.ClipsDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return nil
}

func envOrDefault(key string, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}
