import sys
import numpy as np
from faster_whisper import WhisperModel

# Initialize the Whisper model on GPU
model = WhisperModel(
    "large",
    device="cuda",
    compute_type="float16",
    device_index=0
)
print("Model loaded on GPU", file=sys.stderr)

# Set chunk size: 32,000 samples * 4 bytes = 128,000 bytes
chunk_size = 32000 * 4

while True:
    try:
        # Read exactly 128,000 bytes from stdin
        audio_bytes = sys.stdin.buffer.read(chunk_size)
        if not audio_bytes:
            print("End of audio stream", file=sys.stderr)
            break
        if len(audio_bytes) != chunk_size:
            print(f"Error: Received {len(audio_bytes)} bytes, expected {chunk_size}", file=sys.stderr)
            continue

        # Convert bytes to float32 numpy array
        audio = np.frombuffer(audio_bytes, dtype=np.float32)
        print(f"Audio chunk: {len(audio)} samples, max: {np.max(np.abs(audio))}", file=sys.stderr)

        # Transcribe without prefix
        segments, _ = model.transcribe(
            audio,
            vad_filter=True,  # Try False if issues persist
            language="en",
            beam_size=5,
            temperature=0.0,
        )
        # Combine segment texts
        transcribed_text = " ".join([segment.text.strip() for segment in segments])
        print(transcribed_text)

    except Exception as e:
        print(f"Transcription error: {e}", file=sys.stderr)
    sys.stdout.flush()
