"""faster-whisper + embedding sidecar (CPU-only).

Two jobs, two endpoints, both called by the Go services over internal HTTP:
  POST /transcribe  raw WAV bytes  -> WebVTT (auto-captions, word-level timing)
  POST /embed       {"texts":[..]} -> {"embeddings":[[..]], "dim":N}  (optional)

faster-whisper (CTranslate2) transcribes with word_timestamps=True, which uses the
model's alignment heads to produce precise word/segment boundaries — no torch or
wav2vec2 needed, so the image stays light and CPU-efficient.

This is the Flask dev server: single process, fine for a single-box bulk lane
where transcode/index jobs run one at a time.
"""
import os
import tempfile

from faster_whisper import WhisperModel
from flask import Flask, Response, jsonify, request

app = Flask(__name__)

WHISPER_MODEL = os.environ.get("WHISPER_MODEL", "base")
EMBED_MODEL = os.environ.get("EMBED_MODEL", "BAAI/bge-m3")

# int8 keeps whisper light enough for a CPU-only box.
#
# Load offline-first. faster-whisper otherwise contacts the HF Hub on EVERY start
# to resolve the model snapshot — even when the model is already in the mounted
# cache. With no HF_TOKEN the Hub rate-limits unauthenticated requests and that
# call can hang indefinitely; the process then never reaches app.run(), port 8000
# never binds, and callers get "connection refused" while `docker ps` still shows
# the container "Up". local_files_only=True skips the network entirely when the
# model is cached. Only a genuine cache miss (first ever start) falls back to a
# one-time networked download.
try:
    model = WhisperModel(
        WHISPER_MODEL, device="cpu", compute_type="int8", local_files_only=True
    )
except Exception as exc:  # cold cache: download the model once, then it's offline
    print(f"[whisper] model '{WHISPER_MODEL}' not in cache ({exc}); downloading once from HF Hub")
    model = WhisperModel(WHISPER_MODEL, device="cpu", compute_type="int8")

# Local embedding model is OPTIONAL: only loaded if sentence-transformers is
# installed (image built with EMBED_ENABLED=1). The default deployment uses a
# remote embedding provider, so this stays absent and /embed returns 501.
embedder = None
try:
    from sentence_transformers import SentenceTransformer

    embedder = SentenceTransformer(EMBED_MODEL, device="cpu")
except Exception as exc:  # pragma: no cover - import/availability guard
    print(f"[whisper] local embedder disabled: {exc}")


def vtt_ts(t: float) -> str:
    h = int(t // 3600)
    m = int((t % 3600) // 60)
    s = t % 60
    return f"{h:02d}:{m:02d}:{s:06.3f}"


@app.post("/transcribe")
def transcribe():
    lang = request.args.get("lang") or None  # optional forced language
    data = request.get_data()
    if not data:
        return Response("empty audio", status=400)

    with tempfile.NamedTemporaryFile(suffix=".wav", delete=False) as f:
        f.write(data)
        path = f.name
    try:
        # word_timestamps=True yields precise per-word (and tighter per-segment)
        # boundaries via the model's alignment heads.
        segments, info = model.transcribe(
            path, language=lang, vad_filter=True, word_timestamps=True
        )
        lines = ["WEBVTT", ""]
        for seg in segments:
            text = seg.text.strip()
            if not text:
                continue
            lines.append(f"{vtt_ts(seg.start)} --> {vtt_ts(seg.end)}")
            lines.append(text)
            lines.append("")
        vtt = "\n".join(lines)
    finally:
        os.unlink(path)

    return Response(
        vtt,
        mimetype="text/vtt",
        headers={"X-Detected-Language": info.language or ""},
    )


@app.post("/embed")
def embed():
    if embedder is None:
        return Response("local embedder not installed (use a remote provider)", status=501)
    payload = request.get_json(silent=True) or {}
    texts = payload.get("texts")
    if not isinstance(texts, list) or not texts:
        return Response("texts (non-empty list) required", status=400)
    vecs = embedder.encode(texts, normalize_embeddings=True)
    embeddings = [v.tolist() for v in vecs]
    dim = len(embeddings[0]) if embeddings else 0
    return jsonify({"embeddings": embeddings, "dim": dim})


@app.get("/healthz")
def healthz():
    return {
        "status": "ok",
        "engine": "faster-whisper",
        "model": WHISPER_MODEL,
        "embed_model": EMBED_MODEL if embedder is not None else None,
    }


if __name__ == "__main__":
    app.run(host="0.0.0.0", port=8000)
