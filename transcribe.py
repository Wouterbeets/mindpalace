import sys
import numpy as np
from faster_whisper import WhisperModel
import traceback
import time

# Initialize the Whisper model on GPU
model = WhisperModel(
    "large",
    device="cuda",
    compute_type="float16",
    device_index=0
)
print("Model loaded on GPU", file=sys.stderr)

# Constant threshold for voice activity detection
MIN_AUDIO_LEVEL = 0.001

while True:
    try:
        # First read the PCM data length as text
        length_str = sys.stdin.readline().strip()
        if not length_str:
            print("End of audio stream detected", file=sys.stderr)
            break
            
        try:
            data_length = int(length_str)
            print(f"Expecting {data_length} bytes of PCM16 data", file=sys.stderr)
        except ValueError:
            print(f"Invalid length string: '{length_str}'", file=sys.stderr)
            continue
            
        # Now read the actual PCM data (16-bit)
        pcm_bytes = sys.stdin.buffer.read(data_length)
        if not pcm_bytes:
            print("Empty PCM data received", file=sys.stderr)
            sys.stdin.readline()  # Skip the trailing newline
            continue
            
        if len(pcm_bytes) != data_length:
            print(f"Wrong data length: got {len(pcm_bytes)}, expected {data_length}", file=sys.stderr)
            sys.stdin.readline()  # Skip the trailing newline
            continue
            
        # Skip the trailing newline
        sys.stdin.readline()
        
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
        
        # Skip transcription for very quiet audio
        if max_amp < MIN_AUDIO_LEVEL:
            print(f"Audio too quiet (max amplitude: {max_amp:.6f}), skipping transcription", file=sys.stderr)
            print("")  # Print empty line to indicate no transcription
            sys.stdout.flush()
            continue
        
        # Transcribe the audio
        print(f"Running transcription on audio with max amplitude: {max_amp:.6f}", file=sys.stderr)
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
            print("")  # Print empty line to indicate no transcription
        else:
            # Combine all segments into one line of text
            transcribed_text = " ".join([segment.text.strip() for segment in segment_list])
            print(f"Detected speech: '{transcribed_text}'", file=sys.stderr)
            # Send transcription to Go process
            print(transcribed_text)
            
        # Make sure output is sent immediately
        sys.stdout.flush()
        
    except KeyboardInterrupt:
        print("Received keyboard interrupt, exiting", file=sys.stderr)
        break
    except Exception as e:
        print(f"Transcription error: {e}", file=sys.stderr)
        traceback.print_exc(file=sys.stderr)
        time.sleep(0.1)  # Short delay to avoid tight error loop
        
    # Make sure all output is sent
    sys.stderr.flush()
    sys.stdout.flush()

print("Transcription process shutting down", file=sys.stderr)