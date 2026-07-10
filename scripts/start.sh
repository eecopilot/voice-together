#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ROOT_REAL="$(readlink -f "$ROOT_DIR")"
BACKEND_DIR="$ROOT_DIR/backend"
FRONTEND_DIR="$ROOT_DIR/frontend"
DATA_DIR="$BACKEND_DIR/data"
BIN_DIR="$BACKEND_DIR/bin"
BIN="$BIN_DIR/voice-together"
BIN_REAL="$ROOT_REAL/backend/bin/voice-together"
EMBED_WEB_DIR="$BACKEND_DIR/internal/web/dist"
PID_FILE="$DATA_DIR/voice-together.pid"
LOG_FILE="$DATA_DIR/server.log"
DEV_BACKEND_PID_FILE="$DATA_DIR/dev-backend.pid"
DEV_FRONTEND_PID_FILE="$DATA_DIR/dev-frontend.pid"
DEV_BACKEND_LOG_FILE="$DATA_DIR/dev-backend.log"
DEV_FRONTEND_LOG_FILE="$DATA_DIR/dev-frontend.log"
AIR_CONFIG="$BACKEND_DIR/.air.toml"

ADDR="${VOICE_TOGETHER_ADDR:-0.0.0.0:8788}"
PUBLIC_URL="${VOICE_TOGETHER_PUBLIC_URL:-http://192.168.1.30:${ADDR##*:}}"
DEV_PORT="${VOICE_TOGETHER_DEV_PORT:-5173}"
DEV_URL="${VOICE_TOGETHER_DEV_URL:-http://192.168.1.30:$DEV_PORT}"
NODE_BIN="${NODE_BIN:-$HOME/.nvm/versions/node/v24.14.1/bin}"
AIR_BIN="${AIR_BIN:-air}"

export PATH="/usr/local/go/bin:$NODE_BIN:$PATH"
export VOICE_TOGETHER_ROOT="$ROOT_DIR"
export VOICE_TOGETHER_ADDR="$ADDR"

usage() {
  cat <<EOF
Usage: ./start.sh [start|stop|restart|status|logs|run|build|dev|dev-stop|dev-restart|dev-status|dev-logs]

Commands:
  start        Build and start Voice Together in the background. Default.
  stop         Stop the built background service.
  restart      Stop, then start the built service.
  status       Show built service status and URL.
  logs         Follow built backend logs.
  run          Build and run in the foreground.
  build        Build frontend assets and one embedded backend binary only.
  dev          Start Air backend reload and Vite frontend hot reload.
  dev-stop     Stop both development processes.
  dev-restart  Restart both development processes.
  dev-status   Show Air and Vite process status and URLs.
  dev-logs     Follow both development logs.
EOF
}

need_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "Missing command: $1" >&2
    exit 1
  fi
}

read_pid_file() {
  local file="$1"
  if [ -f "$file" ]; then
    sed -n '1p' "$file"
  fi
}

pid_from_file() {
  read_pid_file "$PID_FILE"
}

is_pid_running() {
  local pid="${1:-}"
  [[ "$pid" =~ ^[0-9]+$ ]] && kill -0 "$pid" >/dev/null 2>&1
}

is_pid_in_dir() {
  local pid="${1:-}"
  local expected_dir="$2"
  local process_dir
  if ! is_pid_running "$pid"; then
    return 1
  fi
  process_dir="$(readlink -f "/proc/$pid/cwd" 2>/dev/null || true)"
  [ "$process_dir" = "$(readlink -f "$expected_dir")" ]
}

is_production_pid() {
  local pid="${1:-}"
  local process_exe
  if ! is_pid_running "$pid"; then
    return 1
  fi
  process_exe="$(readlink -f "/proc/$pid/exe" 2>/dev/null || true)"
  [ "$process_exe" = "$BIN_REAL" ]
}

is_dev_backend_pid() {
  local pid="${1:-}"
  local process_exe air_exe
  if ! is_pid_in_dir "$pid" "$BACKEND_DIR"; then
    return 1
  fi
  process_exe="$(readlink -f "/proc/$pid/exe" 2>/dev/null || true)"
  air_exe="$(command -v "$AIR_BIN" 2>/dev/null || true)"
  [ -n "$air_exe" ] && [ "$process_exe" = "$(readlink -f "$air_exe")" ]
}

is_dev_frontend_pid() {
  local pid="${1:-}"
  local command_line
  if ! is_pid_in_dir "$pid" "$FRONTEND_DIR"; then
    return 1
  fi
  command_line="$(tr '\0' ' ' < "/proc/$pid/cmdline" 2>/dev/null || true)"
  [[ "$command_line" == *"npm run dev"* ]]
}

running_pid() {
  local pid
  pid="$(pid_from_file || true)"
  if is_production_pid "$pid"; then
    echo "$pid"
    return 0
  fi

  pid="$(pgrep -f -x "$BIN -root $ROOT_DIR -addr $ADDR" 2>/dev/null | head -n 1 || true)"
  if is_production_pid "$pid"; then
    echo "$pid"
    return 0
  fi

  pid="$(pgrep -f -x "$BIN_REAL -root $ROOT_REAL -addr $ADDR" 2>/dev/null | head -n 1 || true)"
  if is_production_pid "$pid"; then
    echo "$pid"
  fi
}

dev_backend_pid() {
  read_pid_file "$DEV_BACKEND_PID_FILE"
}

dev_frontend_pid() {
  read_pid_file "$DEV_FRONTEND_PID_FILE"
}

any_dev_process_running() {
  local backend_pid frontend_pid
  backend_pid="$(dev_backend_pid || true)"
  frontend_pid="$(dev_frontend_pid || true)"
  is_dev_backend_pid "$backend_pid" || is_dev_frontend_pid "$frontend_pid"
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
  if any_dev_process_running; then
    echo "Development mode is running. Run: ./start.sh dev-stop" >&2
    return 1
  fi

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
  if is_production_pid "$pid"; then
    echo "Voice Together is running: pid $pid"
    echo "URL: $PUBLIC_URL"
    echo "Log: $LOG_FILE"
  elif any_dev_process_running; then
    echo "The built service is not running; development mode is active."
    dev_status_app
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
  if any_dev_process_running; then
    echo "Development mode is running. Run: ./start.sh dev-stop" >&2
    return 1
  fi

  build_app
  cd "$BACKEND_DIR"
  exec "$BIN" -root "$ROOT_DIR" -addr "$ADDR"
}

stop_managed_process() {
  local label="$1"
  local pid_file="$2"
  local validator="$3"
  local pid pgid target
  pid="$(read_pid_file "$pid_file" || true)"

  if ! "$validator" "$pid"; then
    rm -f "$pid_file"
    echo "$label is not running."
    return 0
  fi

  pgid="$(ps -o pgid= -p "$pid" 2>/dev/null | tr -d '[:space:]')"
  target="$pid"
  if [ -n "$pgid" ] && [ "$pgid" = "$pid" ]; then
    target="-$pgid"
  fi

  echo "Stopping $label: pid $pid"
  kill -INT -- "$target" >/dev/null 2>&1 || true

  for _ in $(seq 1 20); do
    if ! "$validator" "$pid"; then
      rm -f "$pid_file"
      echo "Stopped $label."
      return 0
    fi
    sleep 0.25
  done

  echo "$label did not stop after interrupt; sending terminate signal."
  kill -TERM -- "$target" >/dev/null 2>&1 || true
  for _ in $(seq 1 10); do
    if ! "$validator" "$pid"; then
      rm -f "$pid_file"
      echo "Stopped $label."
      return 0
    fi
    sleep 0.25
  done

  echo "$label did not stop after terminate; sending kill signal."
  kill -KILL -- "$target" >/dev/null 2>&1 || true
  for _ in $(seq 1 10); do
    if ! "$validator" "$pid"; then
      rm -f "$pid_file"
      echo "Stopped $label."
      return 0
    fi
    sleep 0.1
  done

  echo "Failed to stop $label; keeping PID file $pid_file." >&2
  return 1
}

wait_for_url() {
  local url="$1"
  for _ in $(seq 1 60); do
    if curl -fsS "$url" >/dev/null 2>&1; then
      return 0
    fi
    sleep 0.5
  done
  return 1
}

port_is_listening() {
  local port="$1"
  ss -H -ltn | awk -v suffix=":$port" '$4 ~ suffix "$" { found = 1 } END { exit !found }'
}

show_port_listener() {
  local port="$1"
  ss -ltnp | awk -v suffix=":$port" 'NR == 1 || $4 ~ suffix "$"'
}

dev_start_app() {
  local pid backend_pid frontend_pid
  backend_pid="$(dev_backend_pid || true)"
  frontend_pid="$(dev_frontend_pid || true)"

  if is_dev_backend_pid "$backend_pid" && is_dev_frontend_pid "$frontend_pid"; then
    if curl -fsS "http://127.0.0.1:${ADDR##*:}/api/health" >/dev/null 2>&1 \
      && curl -fsS "http://127.0.0.1:$DEV_PORT/api/health" >/dev/null 2>&1; then
      echo "Voice Together development mode is already running."
      dev_status_app
      return 0
    fi
    echo "Development processes are unhealthy; restarting them..."
    dev_stop_app
  fi

  if is_dev_backend_pid "$backend_pid" || is_dev_frontend_pid "$frontend_pid"; then
    echo "Cleaning up a partial development session..."
    dev_stop_app
  fi

  pid="$(running_pid || true)"
  if is_pid_running "$pid"; then
    echo "Stopping the built service before starting development mode..."
    stop_app
  fi

  ensure_tools
  need_cmd "$AIR_BIN"
  need_cmd setsid
  need_cmd curl
  need_cmd ss
  if [ ! -f "$AIR_CONFIG" ]; then
    echo "Missing Air config: $AIR_CONFIG" >&2
    return 1
  fi

  if port_is_listening "${ADDR##*:}"; then
    echo "Backend port ${ADDR##*:} is already in use:" >&2
    show_port_listener "${ADDR##*:}" >&2
    return 1
  fi
  if port_is_listening "$DEV_PORT"; then
    echo "Frontend port $DEV_PORT is already in use:" >&2
    show_port_listener "$DEV_PORT" >&2
    return 1
  fi

  mkdir -p "$DATA_DIR"
  : > "$DEV_BACKEND_LOG_FILE"
  : > "$DEV_FRONTEND_LOG_FILE"

  echo "Starting Air backend..."
  cd "$BACKEND_DIR"
  nohup setsid "$AIR_BIN" -c "$AIR_CONFIG" \
    >> "$DEV_BACKEND_LOG_FILE" 2>&1 < /dev/null &
  echo "$!" > "$DEV_BACKEND_PID_FILE"

  echo "Starting Vite frontend..."
  cd "$FRONTEND_DIR"
  nohup setsid npm run dev -- --host 0.0.0.0 --port "$DEV_PORT" --strictPort \
    >> "$DEV_FRONTEND_LOG_FILE" 2>&1 < /dev/null &
  echo "$!" > "$DEV_FRONTEND_PID_FILE"

  if ! wait_for_url "http://127.0.0.1:${ADDR##*:}/api/health"; then
    echo "Air backend failed to become ready. Recent logs:" >&2
    tail -n 80 "$DEV_BACKEND_LOG_FILE" >&2 || true
    dev_stop_app
    return 1
  fi

  if ! wait_for_url "http://127.0.0.1:$DEV_PORT/api/health"; then
    echo "Vite frontend or API proxy failed to become ready. Recent logs:" >&2
    tail -n 80 "$DEV_FRONTEND_LOG_FILE" >&2 || true
    dev_stop_app
    return 1
  fi

  echo "Voice Together development mode started."
  dev_status_app
}

dev_stop_app() {
  stop_managed_process "Vite frontend" "$DEV_FRONTEND_PID_FILE" is_dev_frontend_pid
  stop_managed_process "Air backend" "$DEV_BACKEND_PID_FILE" is_dev_backend_pid
}

dev_status_app() {
  local backend_pid frontend_pid
  backend_pid="$(dev_backend_pid || true)"
  frontend_pid="$(dev_frontend_pid || true)"

  if is_dev_backend_pid "$backend_pid"; then
    echo "Air backend is running: pid $backend_pid"
    echo "Backend URL: $PUBLIC_URL"
    echo "Backend log: $DEV_BACKEND_LOG_FILE"
  else
    echo "Air backend is not running."
  fi

  if is_dev_frontend_pid "$frontend_pid"; then
    echo "Vite frontend is running: pid $frontend_pid"
    echo "Development URL: $DEV_URL"
    echo "Frontend log: $DEV_FRONTEND_LOG_FILE"
  else
    echo "Vite frontend is not running."
  fi
}

dev_logs_app() {
  mkdir -p "$DATA_DIR"
  touch "$DEV_BACKEND_LOG_FILE" "$DEV_FRONTEND_LOG_FILE"
  tail -n 80 -F "$DEV_BACKEND_LOG_FILE" "$DEV_FRONTEND_LOG_FILE"
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
  dev|dev-start)
    dev_start_app
    ;;
  dev-stop)
    dev_stop_app
    ;;
  dev-restart)
    dev_stop_app
    dev_start_app
    ;;
  dev-status)
    dev_status_app
    ;;
  dev-logs)
    dev_logs_app
    ;;
  -h|--help|help)
    usage
    ;;
  *)
    usage >&2
    exit 1
    ;;
esac
