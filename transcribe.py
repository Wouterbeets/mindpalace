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
chunk_count = 0
vad_threshold = 0.001  # Lower threshold for voice activity detection

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
        max_amplitude = np.max(np.abs(audio))
        chunk_count += 1
        
        print(f"Audio chunk #{chunk_count}: {len(audio)} samples, max amplitude: {max_amplitude}", file=sys.stderr)
        
        # Skip processing if audio is too quiet (optional)
        if max_amplitude < vad_threshold:
            print(f"Audio too quiet (below {vad_threshold}), skipping transcription", file=sys.stderr)
            continue
            
        print("Processing audio chunk with speech...", file=sys.stderr)
            
        # Transcribe with modified settings
        segments, info = model.transcribe(
            audio,
            vad_filter=False,  # Disable VAD filter to catch quieter speech
            language="en",
            beam_size=5,
            temperature=0.0,
        )
        
        # Process segments
        segment_list = list(segments)
        if not segment_list:
            print("No speech detected in chunk", file=sys.stderr)
            continue
            
        # Combine segment texts
        transcribed_text = " ".join([segment.text.strip() for segment in segment_list])
        print(f"Detected speech: '{transcribed_text}'", file=sys.stderr)
        print(transcribed_text)

    except Exception as e:
        print(f"Transcription error: {e}", file=sys.stderr)
    sys.stdout.flush()
