package media

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

type Processor struct {
	FFmpegPath string
}

type Result struct {
	BrowserAudioPath     string
	TranscriptionWAVPath string
}

func New(projectRoot string) Processor {
	ffmpegPath := os.Getenv("VOICE_TOGETHER_FFMPEG")
	if ffmpegPath == "" {
		if found, err := exec.LookPath("ffmpeg"); err == nil {
			ffmpegPath = found
		}
	}
	if ffmpegPath == "" {
		candidate := filepath.Join(projectRoot, "backend", "bin", "ffmpeg")
		if _, err := os.Stat(candidate); err == nil {
			ffmpegPath = candidate
		}
	}
	if ffmpegPath == "" {
		ffmpegPath = "ffmpeg"
	}
	return Processor{FFmpegPath: ffmpegPath}
}

func (p Processor) Extract(ctx context.Context, sourcePath string, clipDir string) (Result, error) {
	if err := os.MkdirAll(clipDir, 0o755); err != nil {
		return Result{}, err
	}

	wavPath := filepath.Join(clipDir, "transcribe.wav")
	audioPath := filepath.Join(clipDir, "audio.mp3")

	if err := p.runFFmpeg(ctx, "-y", "-i", sourcePath, "-vn", "-ac", "1", "-ar", "16000", "-c:a", "pcm_s16le", wavPath); err != nil {
		return Result{}, fmt.Errorf("extract wav: %w", err)
	}
	if err := p.runFFmpeg(ctx, "-y", "-i", sourcePath, "-vn", "-c:a", "libmp3lame", "-q:a", "2", audioPath); err != nil {
		return Result{}, fmt.Errorf("extract browser audio: %w", err)
	}

	return Result{
		BrowserAudioPath:     audioPath,
		TranscriptionWAVPath: wavPath,
	}, nil
}

func (p Processor) runFFmpeg(ctx context.Context, args ...string) error {
	runCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	var stderr bytes.Buffer
	cmd := exec.CommandContext(runCtx, p.FFmpegPath, args...)
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if stderr.Len() > 0 {
			return fmt.Errorf("%v: %s", err, stderr.String())
		}
		return err
	}
	return nil
}
