#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
VENV_DIR="$ROOT_DIR/.venv"

python3 -m venv "$VENV_DIR"
"$VENV_DIR/bin/python" -m pip install --upgrade pip wheel
"$VENV_DIR/bin/python" -m pip install imageio-ffmpeg faster-whisper

mkdir -p "$ROOT_DIR/backend/bin"
FFMPEG_PATH="$("$VENV_DIR/bin/python" - <<'PY'
import imageio_ffmpeg
print(imageio_ffmpeg.get_ffmpeg_exe())
PY
)"
ln -sf "$FFMPEG_PATH" "$ROOT_DIR/backend/bin/ffmpeg"

echo "Tools installed."
echo "Python: $VENV_DIR/bin/python"
echo "ffmpeg: $ROOT_DIR/backend/bin/ffmpeg"
