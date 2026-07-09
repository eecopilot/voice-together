#!/usr/bin/env python3
import argparse
import json
import math
import sys
import wave


def audio_duration(path: str) -> float:
    try:
        with wave.open(path, "rb") as wav:
            frames = wav.getnframes()
            rate = wav.getframerate()
            if rate <= 0:
                return 0.0
            return frames / float(rate)
    except Exception:
        return 0.0


def fallback_result(path: str, warning: str) -> dict:
    duration = audio_duration(path)
    end = max(1.0, min(duration or 8.0, 8.0))
    return {
        "language": "",
        "duration": duration,
        "warning": warning,
        "segments": [
            {
                "start": 0.0,
                "end": round(end, 2),
                "text": "Transcription tool is not installed. Run scripts/setup-tools.sh to enable Whisper."
            }
        ]
    }


def transcribe_with_faster_whisper(path: str, model: str, language: str | None) -> dict:
    from faster_whisper import WhisperModel

    whisper = WhisperModel(model, device="cpu", compute_type="int8")
    segments_iter, info = whisper.transcribe(
        path,
        language=language,
        vad_filter=True,
        beam_size=5,
    )

    segments = []
    for segment in segments_iter:
        text = segment.text.strip()
        if not text:
            continue
        start = max(0.0, float(segment.start))
        end = max(start + 0.1, float(segment.end))
        segments.append({
            "start": round(start, 3),
            "end": round(end, 3),
            "text": text,
        })

    duration = audio_duration(path)
    if not segments:
        segments.append({
            "start": 0.0,
            "end": round(max(1.0, duration), 3),
            "text": "No speech detected.",
        })

    return {
        "language": getattr(info, "language", "") or "",
        "duration": duration or max((s["end"] for s in segments), default=0.0),
        "warning": "",
        "segments": segments,
    }


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--audio", required=True)
    parser.add_argument("--model", default="base")
    parser.add_argument("--language", default="")
    args = parser.parse_args()

    try:
        result = transcribe_with_faster_whisper(
            args.audio,
            args.model,
            args.language or None,
        )
    except ModuleNotFoundError as exc:
        result = fallback_result(args.audio, f"missing Python package: {exc.name}")
    except Exception as exc:
        print(f"transcription failed: {exc}", file=sys.stderr)
        return 1

    print(json.dumps(result, ensure_ascii=False))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
