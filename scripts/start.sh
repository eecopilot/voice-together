#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
export VOICE_TOGETHER_ROOT="$ROOT_DIR"
export PATH="/usr/local/go/bin:$HOME/.nvm/versions/node/v24.14.1/bin:$PATH"

if [ ! -x "$ROOT_DIR/backend/bin/ffmpeg" ] || [ ! -x "$ROOT_DIR/.venv/bin/python" ]; then
  echo "Missing local tools. Run scripts/setup-tools.sh first." >&2
  exit 1
fi

cd "$ROOT_DIR/frontend"
npm install
npm run build

cd "$ROOT_DIR/backend"
go run -buildvcs=false ./cmd/server -root "$ROOT_DIR"
