#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BACKEND_DIR="$ROOT_DIR/backend"
FRONTEND_DIR="$ROOT_DIR/frontend"
DATA_DIR="$BACKEND_DIR/data"
BIN_DIR="$BACKEND_DIR/bin"
BIN="$BIN_DIR/voice-together"
EMBED_WEB_DIR="$BACKEND_DIR/internal/web/dist"
PID_FILE="$DATA_DIR/voice-together.pid"
LOG_FILE="$DATA_DIR/server.log"

ADDR="${VOICE_TOGETHER_ADDR:-0.0.0.0:8788}"
PUBLIC_URL="${VOICE_TOGETHER_PUBLIC_URL:-http://192.168.1.30:${ADDR##*:}}"
NODE_BIN="${NODE_BIN:-$HOME/.nvm/versions/node/v24.14.1/bin}"

export PATH="/usr/local/go/bin:$NODE_BIN:$PATH"
export VOICE_TOGETHER_ROOT="$ROOT_DIR"

usage() {
  cat <<EOF
Usage: ./start.sh [start|stop|restart|status|logs|run|build]

Commands:
  start    Build and start Voice Together in the background. Default.
  stop     Stop the background service.
  restart  Stop, then start.
  status   Show service status and URL.
  logs     Follow backend logs.
  run      Build and run in the foreground.
  build    Build frontend assets and one embedded backend binary only.
EOF
}

need_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "Missing command: $1" >&2
    exit 1
  fi
}

pid_from_file() {
  if [ -f "$PID_FILE" ]; then
    sed -n '1p' "$PID_FILE"
  fi
}

is_pid_running() {
  local pid="${1:-}"
  [ -n "$pid" ] && kill -0 "$pid" >/dev/null 2>&1
}

running_pid() {
  local pid
  pid="$(pid_from_file || true)"
  if is_pid_running "$pid"; then
    echo "$pid"
    return 0
  fi

  pgrep -f "voice-together.*-root $ROOT_DIR" 2>/dev/null | head -n 1
}

ensure_tools() {
  need_cmd go
  need_cmd npm

  if [ ! -x "$ROOT_DIR/backend/bin/ffmpeg" ] || [ ! -x "$ROOT_DIR/.venv/bin/python" ]; then
    echo "Missing local media/transcription tools." >&2
    echo "Run: cd $ROOT_DIR && scripts/setup-tools.sh" >&2
    exit 1
  fi
}

build_frontend() {
  echo "Building frontend..."
  cd "$FRONTEND_DIR"
  if [ ! -d node_modules ]; then
    npm install
  fi
  npm run build
}

sync_embedded_web() {
  echo "Embedding frontend assets..."
  rm -rf "$EMBED_WEB_DIR"
  mkdir -p "$EMBED_WEB_DIR"
  cp -R "$FRONTEND_DIR/dist/." "$EMBED_WEB_DIR/"
}

build_backend() {
  echo "Building backend..."
  mkdir -p "$BIN_DIR" "$DATA_DIR"
  cd "$BACKEND_DIR"
  go build -buildvcs=false -o "$BIN" ./cmd/server
}

build_app() {
  ensure_tools
  build_frontend
  sync_embedded_web
  build_backend
}

start_app() {
  local pid
  pid="$(running_pid || true)"
  if is_pid_running "$pid"; then
    echo "Voice Together is already running: pid $pid"
    echo "URL: $PUBLIC_URL"
    echo "Log: $LOG_FILE"
    echo "$pid" > "$PID_FILE"
    return 0
  fi

  build_app

  echo "Starting Voice Together..."
  cd "$BACKEND_DIR"
  nohup "$BIN" -root "$ROOT_DIR" -addr "$ADDR" >> "$LOG_FILE" 2>&1 &
  pid="$!"
  echo "$pid" > "$PID_FILE"

  sleep 1
  if ! is_pid_running "$pid"; then
    echo "Voice Together failed to start. Recent logs:" >&2
    tail -n 60 "$LOG_FILE" >&2 || true
    exit 1
  fi

  echo "Started Voice Together: pid $pid"
  echo "URL: $PUBLIC_URL"
  echo "Log: $LOG_FILE"
}

stop_app() {
  local pid
  pid="$(running_pid || true)"
  if ! is_pid_running "$pid"; then
    rm -f "$PID_FILE"
    echo "Voice Together is not running."
    return 0
  fi

  echo "Stopping Voice Together: pid $pid"
  kill -INT "$pid" >/dev/null 2>&1 || true

  for _ in $(seq 1 20); do
    if ! is_pid_running "$pid"; then
      rm -f "$PID_FILE"
      echo "Stopped."
      return 0
    fi
    sleep 0.5
  done

  echo "Process did not stop after interrupt; sending terminate signal."
  kill "$pid" >/dev/null 2>&1 || true
  rm -f "$PID_FILE"
}

status_app() {
  local pid
  pid="$(running_pid || true)"
  if is_pid_running "$pid"; then
    echo "Voice Together is running: pid $pid"
    echo "URL: $PUBLIC_URL"
    echo "Log: $LOG_FILE"
  else
    echo "Voice Together is not running."
  fi
}

logs_app() {
  mkdir -p "$DATA_DIR"
  touch "$LOG_FILE"
  tail -f "$LOG_FILE"
}

run_foreground() {
  build_app
  cd "$BACKEND_DIR"
  exec "$BIN" -root "$ROOT_DIR" -addr "$ADDR"
}

command="${1:-start}"
case "$command" in
  start)
    start_app
    ;;
  stop)
    stop_app
    ;;
  restart)
    stop_app
    start_app
    ;;
  status)
    status_app
    ;;
  logs)
    logs_app
    ;;
  run)
    run_foreground
    ;;
  build)
    build_app
    ;;
  -h|--help|help)
    usage
    ;;
  *)
    usage >&2
    exit 1
    ;;
esac
