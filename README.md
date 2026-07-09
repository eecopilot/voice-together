# Voice Together

Voice Together is a local language-learning player.

Current flow:

```text
Upload video/audio
  -> Go backend saves it
  -> ffmpeg extracts browser audio and transcription WAV
  -> Whisper-compatible tool generates timed transcript segments
  -> React player syncs audio with subtitles
  -> learner loops any dialogue segment
```

## Requirements

- Go
- Node.js + npm
- Python 3

The project installs `ffmpeg` and `faster-whisper` into a local `.venv` with `scripts/setup-tools.sh`, so it does not require system-wide ffmpeg.

## Run

```bash
cd ~/myproject/voice-together
scripts/setup-tools.sh
./start.sh
```

Then open:

```text
http://192.168.1.30:8788
```

For the existing sample file, click `导入 demo.mp4`.

Useful commands:

```bash
./start.sh status
./start.sh logs
./start.sh restart
./start.sh stop
```

The React frontend is embedded into the Go binary during `./start.sh build`, so the built server does not need `frontend/dist` at runtime:

```bash
backend/bin/voice-together -root "$PWD"
```

## Development Notes

- Backend: `backend/cmd/server`
- Frontend: `frontend/src`
- Embedded frontend source: `backend/internal/web`
- Runtime data: `backend/data`
- Extracted browser audio: `backend/data/clips/<clip-id>/audio.mp3`
- Transcription WAV: `backend/data/clips/<clip-id>/transcribe.wav`

If `faster-whisper` is not installed, the app still starts but returns an explicit placeholder subtitle telling you to run `scripts/setup-tools.sh`.
