# Voice Together Implementation Plan

## Goal

Build a language-learning player that takes a short video clip, extracts audio and transcript segments, then lets the learner replay any dialogue line repeatedly.

## Current Understanding

The core product flow is:

```text
Upload video clip
  -> Go backend saves and processes it
  -> backend tools extract playable audio
  -> backend tools generate timed transcript text
  -> React player displays text + audio together
  -> learner loops any dialogue segment indefinitely
```

The backend should be written in Go where practical. Go does not need to implement speech recognition itself; it should orchestrate proven command-line tools such as `ffmpeg` and a Whisper-compatible transcriber, then persist the generated audio and transcript metadata.

## MVP Scope

- Upload a local video/audio clip.
- Extract a normalized audio file with `ffmpeg`.
- Transcribe speech into timed text segments with Whisper.
- Show a sentence/segment list beside a player.
- Click a segment to loop only that segment.
- Support play/pause, previous/next segment, repeat current segment, and playback speed.
- Persist imported clips and their transcript metadata locally.

## Non-Goals For First Version

- YouTube URL import.
- User login or cloud sync.
- AI explanation, vocabulary, or grammar notes.
- Speaker diarization/role separation.
- Mobile app packaging.

These can be added after the local-clip workflow is reliable.

## Proposed Stack

- Backend: Go
- Media processing: ffmpeg
- Transcription: whisper.cpp CLI first, faster-whisper as fallback if accuracy/speed is better
- Storage: local filesystem plus SQLite
- Frontend: React + Vite + TypeScript
- UI: Tailwind CSS + shadcn/ui
- Audio UI: native audio first; add WaveSurfer later only if needed
- Deployment: Docker Compose

## User Flow

1. User opens the app.
2. User uploads a short video/audio clip.
3. Backend saves the source file.
4. Backend extracts audio to a standard format.
5. Backend transcribes audio and stores timed segments.
6. Frontend shows the clip in the library.
7. User opens the clip.
8. User clicks a transcript segment.
9. Player loops from `segment.start` to `segment.end`.
10. User can move to previous/next segment and change speed.

## Data Shape

### Clip

```json
{
  "id": "clip_001",
  "title": "Example dialogue",
  "source_file": "uploads/clip_001/source.mp4",
  "audio_file": "clips/clip_001/audio.mp3",
  "duration": 42.5,
  "language": "en",
  "created_at": "2026-07-09T16:20:00+08:00"
}
```

### Segment

```json
{
  "id": "seg_001",
  "clip_id": "clip_001",
  "start": 10.2,
  "end": 13.8,
  "text": "How are you doing today?"
}
```

## API Draft

- `GET /api/health`
- `GET /api/clips`
- `POST /api/clips`
  - Upload video/audio file.
  - Returns clip record and processing status.
- `GET /api/clips/{clip_id}`
  - Returns clip metadata and transcript segments.
- `GET /api/clips/{clip_id}/audio`
  - Streams extracted audio.
- `DELETE /api/clips/{clip_id}`

Later:

- `POST /api/import/youtube`
- `PATCH /api/clips/{clip_id}/segments/{segment_id}`
- `POST /api/clips/{clip_id}/notes`

## Directory Plan

```text
voice-together/
  backend/
    cmd/
      server/
        main.go
    internal/
      api/
      db/
      media/
      transcribe/
      storage/
    data/
      uploads/
      clips/
      voice_together.db
    go.mod
  frontend/
    src/
      components/
      pages/
      lib/
    package.json
  docker-compose.yml
  IMPLEMENTATION_PLAN.md
```

## Current Run Status

- App URL: `http://192.168.1.30:8788`
- Backend process: compiled Go binary `backend/bin/voice-together`
- Runtime log: `backend/data/server.log`
- Demo clip imported successfully from root `demo.mp4`
- Demo transcription result: 13 timed English segments
- Browser audio generated: `backend/data/clips/<clip-id>/audio.mp3`
- Upload endpoint verified with `demo.mp4`, then the temporary upload clip was deleted

## Implementation Checklist

### 1. Project Setup

- [x] Initialize Git repository.
- [x] Add `.gitignore`.
- [x] Create backend Go HTTP API skeleton.
- [x] Create frontend Vite React TypeScript app.
- [x] Add Tailwind CSS.
- [x] Add shadcn-style local base components.
- [ ] Add Docker Compose for backend and frontend/dev workflow.

### 2. Backend Media Pipeline

- [x] Add upload endpoint.
- [x] Save uploaded file under `backend/data/uploads/`.
- [x] Validate file extension and size.
- [x] Use `ffmpeg` to extract mono 16 kHz WAV for transcription.
- [x] Create MP3 audio file for browser playback.
- [x] Store clip metadata in SQLite.

### 3. Transcription

- [x] Choose first transcriber: faster-whisper.
- [x] Add model download/setup notes.
- [x] Implement Go transcription service that calls the selected tool.
- [x] Parse transcriber output into `{start, end, text}` segments.
- [x] Store segments with start/end/text.
- [x] Return processing errors clearly.
- [x] Add a small test clip for manual verification.

### 4. Frontend Player

- [x] Build clip upload screen.
- [x] Build clip library/list.
- [x] Build player page.
- [x] Render transcript segment list.
- [x] Click segment to seek and play.
- [x] Loop current segment using `timeupdate`.
- [x] Add previous/next segment controls.
- [x] Add playback speed control.
- [ ] Add keyboard shortcuts only after base UI is stable.

### 5. UX Polish

- [x] Show processing/loading state after upload.
- [x] Show transcription failures in the UI.
- [x] Highlight active segment while audio plays.
- [x] Keep player controls stable on mobile and desktop.
- [x] Add delete clip flow.

### 6. Verification

- [x] Upload MP4 and confirm audio extraction.
- [x] Confirm transcript timestamps are returned with audio.
- [x] Confirm segment loop logic is implemented with `timeupdate`.
- [x] Confirm previous/next buttons are implemented.
- [x] Confirm page reload preserves imported clips through SQLite.
- [ ] Confirm Docker startup works from a clean checkout.

## Open Decisions

- [x] Transcriber choice: faster-whisper.
- [ ] Whisper model size: `base`, `small`, or `medium`.
- [ ] Default language: force English or auto-detect.
- [x] Audio playback format: MP3.
- [ ] Max upload size for MVP.
- [x] Processing mode: synchronous first.

## Recommended First Pass

Use synchronous processing first:

```text
POST /api/clips
  -> save file
  -> Go calls ffmpeg to extract audio
  -> Go calls Whisper-compatible tool to transcribe
  -> Go stores audio + transcript segments
  -> return completed clip
```

This is simpler to build and debug. If transcription becomes slow, move it to a background job later.

## Later Enhancements

- YouTube segment import with `yt-dlp`.
- Subtitle export as SRT/VTT.
- Manual transcript editing.
- Speaker labels.
- Record-yourself comparison.
- AI explanation and vocabulary extraction.
- Per-segment learning progress.
