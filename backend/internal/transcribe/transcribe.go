package transcribe

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"voice-together/backend/internal/db"
)

type Service struct {
	PythonPath string
	ScriptPath string
	ModelSize  string
}

type Result struct {
	Language string       `json:"language"`
	Duration float64      `json:"duration"`
	Segments []db.Segment `json:"segments"`
	Warning  string       `json:"warning"`
}

func New(projectRoot string) Service {
	pythonPath := os.Getenv("VOICE_TOGETHER_PYTHON")
	if pythonPath == "" {
		candidate := filepath.Join(projectRoot, ".venv", "bin", "python")
		if _, err := os.Stat(candidate); err == nil {
			pythonPath = candidate
		}
	}
	if pythonPath == "" {
		pythonPath = "python3"
	}

	modelSize := os.Getenv("VOICE_TOGETHER_WHISPER_MODEL")
	if modelSize == "" {
		modelSize = "base"
	}

	return Service{
		PythonPath: pythonPath,
		ScriptPath: filepath.Join(projectRoot, "backend", "tools", "transcribe.py"),
		ModelSize:  modelSize,
	}
}

func (s Service) Transcribe(ctx context.Context, audioPath string, language string) (Result, error) {
	runCtx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()

	args := []string{s.ScriptPath, "--audio", audioPath, "--model", s.ModelSize}
	if language != "" && language != "auto" {
		args = append(args, "--language", language)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd := exec.CommandContext(runCtx, s.PythonPath, args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if stderr.Len() > 0 {
			return Result{}, fmt.Errorf("%v: %s", err, stderr.String())
		}
		return Result{}, err
	}

	var result Result
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		return Result{}, fmt.Errorf("parse transcriber output: %w", err)
	}
	return result, nil
}
