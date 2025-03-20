import sys
import numpy as np
from faster_whisper import WhisperModel
import traceback
import time

# Initialize the Whisper model on GPU
model = WhisperModel(
    "small",
    device="cuda",
    compute_type="float16",
    device_index=0
)
print("Model loaded on GPU", file=sys.stderr)
sys.stderr.flush()

# Constant threshold for voice activity detection
MIN_AUDIO_LEVEL = 0.1

while True:
    try:
        # Read the length line from the binary stream
        length_line = sys.stdin.buffer.readline()
        if not length_line:
            print("End of audio stream detected", file=sys.stderr)
            sys.stderr.flush()
            break

        # Decode and parse the length
        length_str = length_line.strip().decode('utf-8')
        try:
            data_length = int(length_str)
            print(f"Expecting {data_length} bytes of PCM16 data", file=sys.stderr)
        except ValueError:
            print(f"Invalid length string: '{length_str}'", file=sys.stderr)
            sys.stderr.flush()
            continue

        # Read the PCM data from the binary stream
        pcm_bytes = sys.stdin.buffer.read(data_length)
        if not pcm_bytes or len(pcm_bytes) != data_length:
            print(f"Failed to read PCM data: got {len(pcm_bytes)} bytes, expected {data_length}", file=sys.stderr)
            # Read and discard the trailing newline to stay in sync
            sys.stdin.buffer.read(1)
            sys.stderr.flush()
            continue

        # Read the trailing newline from the binary stream
        trailing_newline = sys.stdin.buffer.read(1)
        if trailing_newline != b'\n':
            print(f"Expected trailing newline, got {trailing_newline}", file=sys.stderr)
            sys.stderr.flush()
            continue

        # Convert PCM16 bytes to float32 numpy array (each sample is 2 bytes)
        num_samples = len(pcm_bytes) // 2
        audio = np.zeros(num_samples, dtype=np.float32)

        # Process each 16-bit PCM sample (little endian)
        for i in range(num_samples):
            # Extract 16-bit sample
            sample = (pcm_bytes[i*2] | (pcm_bytes[i*2+1] << 8))
            # Convert to signed int16
            if sample > 32767:
                sample -= 65536
            # Normalize to float32 [-1.0, 1.0]
            audio[i] = float(sample) / 32767.0

        # Get max amplitude for logging
        max_amp = np.max(np.abs(audio))
        print(f"Received audio chunk with {num_samples} samples, max amplitude: {max_amp:.6f}", file=sys.stderr)
        sys.stderr.flush()

        # Skip transcription for very quiet audio
        if max_amp < MIN_AUDIO_LEVEL:
            print(f"Audio too quiet (max amplitude: {max_amp:.6f}), skipping transcription", file=sys.stderr)
            sys.stdout.flush()
            sys.stderr.flush()
            continue

        # Transcribe the audio
        print(f"Running transcription on audio with max amplitude: {max_amp:.6f}", file=sys.stderr)
        sys.stderr.flush()
        segments, info = model.transcribe(
            audio,
            vad_filter=False,
            language="en",
            beam_size=5,
            temperature=0.0,
        )

        # Process segments
        segment_list = list(segments)
        if not segment_list:
            print("No speech detected in audio", file=sys.stderr)
        else:
            # Combine all segments into one line of text
            transcribed_text = " ".join([segment.text.strip() for segment in segment_list])
            print(f"Detected speech: '{transcribed_text}'", file=sys.stderr)
            # Send transcription to Go process
            print(transcribed_text)

        # Ensure output is sent immediately
        sys.stdout.flush()
        sys.stderr.flush()

    except KeyboardInterrupt:
        print("Received keyboard interrupt, exiting", file=sys.stderr)
        sys.stderr.flush()
        break
    except Exception as e:
        print(f"Transcription error: {e}", file=sys.stderr)
        traceback.print_exc(file=sys.stderr)
        sys.stderr.flush()
        time.sleep(0.1)  # Short delay to avoid tight error loop

print("Transcription process shutting down", file=sys.stderr)
sys.stderr.flush()
