package audio

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/mutablelogic/go-media/pkg/ffmpeg"
	// "github.com/mutablelogic/go-whisper"
	// "github.com/mutablelogic/go-whisper/pkg/schema"
	// "github.com/mutablelogic/go-whisper/pkg/task"
	"mindpalace/pkg/logging"
)

// VoiceTranscriber manages audio recording and real-time transcription using go-whisper
type VoiceTranscriber struct {
	// whisper               *whisper.Whisper
	// model                 *schema.Model
	mu                    sync.Mutex
	transcriptionCallback func(string)
	sessionCallback       func(eventType string, data map[string]interface{})
	audioBuffer           []float32
	sampleRate            int
	bufferThreshold       int // samples to buffer before transcription
	sessionID             string
	startTime             time.Time
	totalSegments         int
	running               bool
	captureCtx            context.Context
	captureCancel         context.CancelFunc
}

// NewVoiceTranscriber initializes a new VoiceTranscriber instance with go-whisper
func NewVoiceTranscriber(modelPath string) (*VoiceTranscriber, error) {
	// Whisper disabled for now
	logging.Info("AUDIO: Whisper transcription disabled")

	vt := &VoiceTranscriber{
		sampleRate:      16000,
		bufferThreshold: 16000 * 1,                    // 1 second of audio for faster testing
		audioBuffer:     make([]float32, 0, 16000*10), // Pre-allocate for 10 seconds
	}

	return vt, nil
}

// SetSessionEventCallback sets the callback for session events
func (vt *VoiceTranscriber) SetSessionEventCallback(callback func(eventType string, data map[string]interface{})) {
	vt.mu.Lock()
	defer vt.mu.Unlock()
	vt.sessionCallback = callback
}

// Start initializes the transcriber for receiving audio chunks
func (vt *VoiceTranscriber) Start(transcriptionCallback func(string)) error {
	logging.Debug("AUDIO: Starting voice transcriber")
	vt.mu.Lock()
	defer vt.mu.Unlock()

	if vt.running {
		logging.Debug("AUDIO: Transcriber already running")
		return nil
	}

	vt.transcriptionCallback = transcriptionCallback
	vt.sessionID = fmt.Sprintf("session-%d", time.Now().UnixNano())
	vt.startTime = time.Now()
	vt.totalSegments = 0
	vt.audioBuffer = vt.audioBuffer[:0] // Clear buffer
	vt.running = true

	logging.Info("AUDIO: Voice transcriber started with session %s", vt.sessionID)
	return nil
}

// Stop terminates the transcription session
func (vt *VoiceTranscriber) Stop() {
	vt.mu.Lock()
	defer vt.mu.Unlock()

	if !vt.running {
		return
	}

	vt.running = false

	duration := time.Since(vt.startTime).Seconds()
	if vt.sessionCallback != nil {
		vt.sessionCallback("stop", map[string]interface{}{
			"SessionID":    vt.sessionID,
			"DurationSecs": duration,
			"Segments":     vt.totalSegments,
		})
	}

	logging.Info("AUDIO: Voice transcriber stopped, session %s, duration %.2fs, %d segments",
		vt.sessionID, duration, vt.totalSegments)
}

// ProcessAudioChunk processes incoming audio data from WebSocket
func (vt *VoiceTranscriber) ProcessAudioChunk(pcmData []byte) error {
	logging.Debug("AUDIO: Received audio chunk: %d bytes", len(pcmData))
	vt.mu.Lock()
	if !vt.running {
		vt.mu.Unlock()
		logging.Debug("AUDIO: Transcriber not running, ignoring audio chunk")
		return nil
	}
	vt.mu.Unlock()

	// Convert PCM16 to float32
	logging.Debug("AUDIO: Converting PCM data to float32")
	samples, err := convertPCM16ToFloat32(pcmData)
	if err != nil {
		logging.Error("AUDIO: Failed to convert PCM data: %v", err)
		return fmt.Errorf("failed to convert PCM data: %w", err)
	}
	logging.Debug("AUDIO: Converted to %d float32 samples", len(samples))

	vt.mu.Lock()
	vt.audioBuffer = append(vt.audioBuffer, samples...)
	logging.Debug("AUDIO: Buffer now has %d samples (threshold: %d)", len(vt.audioBuffer), vt.bufferThreshold)

	// Process when we have enough audio (1 second for faster testing)
	if len(vt.audioBuffer) >= vt.bufferThreshold {
		audioToProcess := make([]float32, len(vt.audioBuffer))
		copy(audioToProcess, vt.audioBuffer)
		vt.audioBuffer = vt.audioBuffer[:0] // Clear buffer
		vt.mu.Unlock()

		logging.Info("AUDIO: Buffer threshold reached (%d samples), starting transcription", len(audioToProcess))
		// Process in background
		go vt.transcribeAudio(audioToProcess)
	} else {
		vt.mu.Unlock()
		logging.Debug("AUDIO: Buffer not full yet, continuing to accumulate")
	}

	return nil
}

// transcribeAudio performs the actual transcription using go-whisper
func (vt *VoiceTranscriber) transcribeAudio(audio []float32) {
	logging.Info("AUDIO: Transcription disabled - skipping %d audio samples (%.2fs)", len(audio), float64(len(audio))/float64(vt.sampleRate))

	// Save audio to file for debugging
	if err := saveAudioToWav(audio, fmt.Sprintf("debug_audio_%d.wav", time.Now().UnixNano())); err != nil {
		logging.Info("AUDIO: Failed to save debug audio: %v", err)
	} else {
		logging.Debug("AUDIO: Saved debug audio to file")
	}

	// Whisper is disabled, so just increment segment count and call callback with empty text
	vt.mu.Lock()
	vt.totalSegments++
	vt.mu.Unlock()

	logging.Info("AUDIO: Transcription skipped (whisper disabled), segments: %d", vt.totalSegments)
	if vt.transcriptionCallback != nil {
		logging.Debug("AUDIO: Calling transcription callback with empty text")
		vt.transcriptionCallback("")
	} else {
		logging.Info("AUDIO: No transcription callback set")
	}
}

// convertPCM16ToFloat32 converts 16-bit PCM bytes to float32 samples
func convertPCM16ToFloat32(pcmData []byte) ([]float32, error) {
	if len(pcmData)%2 != 0 {
		return nil, fmt.Errorf("PCM data length must be even")
	}

	samples := make([]float32, len(pcmData)/2)
	for i := 0; i < len(samples); i++ {
		// Read 16-bit little-endian sample
		sample := int16(binary.LittleEndian.Uint16(pcmData[i*2:]))
		// Convert to float32 (-1.0 to 1.0)
		samples[i] = float32(sample) / 32768.0
	}

	return samples, nil
}

// saveAudioToWav saves float32 audio samples to a WAV file for debugging
func saveAudioToWav(samples []float32, filename string) error {
	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	// WAV file header
	sampleRate := uint32(16000)
	bitsPerSample := uint16(16)
	numChannels := uint16(1)
	byteRate := sampleRate * uint32(numChannels) * uint32(bitsPerSample/8)
	blockAlign := numChannels * bitsPerSample / 8
	dataSize := uint32(len(samples) * 2) // 2 bytes per sample
	fileSize := 36 + dataSize

	// Write WAV header
	header := []byte("RIFF")
	binary.Write(file, binary.LittleEndian, header)
	binary.Write(file, binary.LittleEndian, fileSize)
	header = []byte("WAVE")
	binary.Write(file, binary.LittleEndian, header)
	header = []byte("fmt ")
	binary.Write(file, binary.LittleEndian, header)
	binary.Write(file, binary.LittleEndian, uint32(16)) // fmt chunk size
	binary.Write(file, binary.LittleEndian, uint16(1))  // PCM format
	binary.Write(file, binary.LittleEndian, numChannels)
	binary.Write(file, binary.LittleEndian, sampleRate)
	binary.Write(file, binary.LittleEndian, byteRate)
	binary.Write(file, binary.LittleEndian, blockAlign)
	binary.Write(file, binary.LittleEndian, bitsPerSample)
	header = []byte("data")
	binary.Write(file, binary.LittleEndian, header)
	binary.Write(file, binary.LittleEndian, dataSize)

	// Write audio data
	for _, sample := range samples {
		// Convert float32 to int16
		intSample := int16(sample * 32767.0)
		binary.Write(file, binary.LittleEndian, intSample)
	}

	return nil
}

// Close cleans up the Whisper instance
func (vt *VoiceTranscriber) StartCapture(ctx context.Context) error {
	logging.Info("AUDIO: Starting microphone capture")
	vt.mu.Lock()
	if vt.running && vt.captureCancel != nil {
		vt.mu.Unlock()
		logging.Info("AUDIO: Capture already running")
		return nil
	}
	vt.mu.Unlock()

	// Open microphone input - using specific Pulse source for USB mic from pactl
	logging.Info("AUDIO: Opening Pulse USB mic source")
	input, err := ffmpeg.Open("pulse:alsa_input.usb-K-MIC_NATRIUM_K-MIC_NATRIUM_20190805V001-00.iec958-stereo",
		ffmpeg.OptInputOpt("sample_rate", "16000"),
		ffmpeg.OptInputOpt("channels", "1"),
		ffmpeg.OptInputOpt("format", "s16"),
		ffmpeg.OptInputOpt("channel_layout", "mono"),
	)
	if err != nil {
		logging.Error("AUDIO: Failed to open USB Pulse source, trying built-in analog: %v", err)
		// Fallback to built-in analog mic
		input, err = ffmpeg.Open("pulse:alsa_input.pci-0000_10_00.6.analog-stereo",
			ffmpeg.OptInputOpt("sample_rate", "16000"),
			ffmpeg.OptInputOpt("channels", "1"),
			ffmpeg.OptInputOpt("format", "s16"),
			ffmpeg.OptInputOpt("channel_layout", "mono"),
		)
		if err != nil {
			logging.Error("AUDIO: Failed to open analog Pulse source, trying ALSA USB: %v", err)
			// Fallback to ALSA USB mic
			input, err = ffmpeg.Open("hw:1,0",
				ffmpeg.OptInputOpt("sample_rate", "16000"),
				ffmpeg.OptInputOpt("channels", "1"),
				ffmpeg.OptInputOpt("format", "s16"),
			)
			if err != nil {
				logging.Error("AUDIO: Failed to open hw:1,0, trying ALSA default: %v", err)
				// Final fallback to ALSA default
				input, err = ffmpeg.Open("alsa:default",
					ffmpeg.OptInputOpt("sample_rate", "16000"),
					ffmpeg.OptInputOpt("channels", "1"),
					ffmpeg.OptInputOpt("format", "s16"),
				)
				if err != nil {
					return fmt.Errorf("failed to open microphone (tried USB Pulse, analog Pulse, hw:1,0, default): %w", err)
				}
				logging.Info("AUDIO: Successfully opened alsa:default as final fallback")
			} else {
				logging.Info("AUDIO: Successfully opened USB microphone (hw:1,0)")
			}
		} else {
			logging.Info("AUDIO: Successfully opened built-in analog microphone (Pulse)")
		}
	} else {
		logging.Info("AUDIO: Successfully opened USB microphone (Pulse source)")
	}

	// Map function to use input parameters
	mapfn := func(stream int, par *ffmpeg.Par) (*ffmpeg.Par, error) {
		return par, nil
	}

	// Create capture context
	captureCtx, cancel := context.WithCancel(ctx)
	vt.mu.Lock()
	vt.captureCtx = captureCtx
	vt.captureCancel = cancel
	vt.mu.Unlock()

	// Start capture goroutine
	go func() {
		defer input.Close()
		defer func() {
			vt.mu.Lock()
			vt.captureCtx = nil
			vt.captureCancel = nil
			vt.mu.Unlock()
		}()
		logging.Info("AUDIO: Started continuous microphone capture goroutine")

		chunkCount := 0
		for {
			err := input.Decode(captureCtx, mapfn, func(stream int, frame *ffmpeg.Frame) error {
				if frame == nil {
					return nil
				}

				data := frame.Bytes(0)
				if len(data) == 0 {
					return nil
				}

				logging.Debug("AUDIO: Captured frame with %d bytes", len(data))
				if err := vt.ProcessAudioChunk(data); err != nil {
					logging.Error("AUDIO: Failed to process captured chunk: %v", err)
				}
				chunkCount++

				return nil
			})
			if err != nil {
				if errors.Is(err, context.Canceled) || errors.Is(err, io.EOF) {
					logging.Info("AUDIO: Capture stopped (context canceled or EOF)")
					break
				}
				logging.Error("AUDIO: Capture decode error: %v", err)
				select {
				case <-captureCtx.Done():
					return
				default:
					time.Sleep(100 * time.Millisecond)
				}
			}
		}
		logging.Info("AUDIO: Microphone capture goroutine ended. Processed %d chunks", chunkCount)
	}()

	return nil
}

func (vt *VoiceTranscriber) StopCapture() {
	vt.mu.Lock()
	defer vt.mu.Unlock()
	if vt.captureCancel != nil {
		vt.captureCancel()
		vt.captureCancel = nil
		vt.captureCtx = nil
		logging.Info("AUDIO: Stopped microphone capture")
	}
}

func (vt *VoiceTranscriber) Close() error {
	vt.StopCapture()
	// if vt.whisper != nil {
	// 	return vt.whisper.Close()
	// }
	return nil
}
