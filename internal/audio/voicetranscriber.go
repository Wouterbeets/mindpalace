package audio

import (
	"context"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/gordonklaus/portaudio"
	"github.com/mutablelogic/go-whisper"
	"github.com/mutablelogic/go-whisper/pkg/schema"
	"github.com/mutablelogic/go-whisper/pkg/task"
	"mindpalace/pkg/logging"
)

// VoiceTranscriber manages audio recording and real-time transcription using go-whisper
type VoiceTranscriber struct {
	whisper               *whisper.Whisper
	model                 *schema.Model
	task                  *task.Context
	mu                    sync.Mutex
	transcriptionCallback func(string)
	sessionCallback       func(eventType string, data map[string]interface{})
	streamProcessor       *AudioStreamProcessor
	sampleRate            int
	sessionID             string
	startTime             time.Time
	totalSegments         int
	running               bool
	captureCtx            context.Context
	captureCancel         context.CancelFunc
	stream                *portaudio.Stream
	fullTranscription     string
}

// NewVoiceTranscriber initializes a new VoiceTranscriber instance with go-whisper
func NewVoiceTranscriber(modelPath string) (*VoiceTranscriber, error) {
	dir := filepath.Dir(modelPath)
	filename := filepath.Base(modelPath)
	var err error
	vt := &VoiceTranscriber{
		sampleRate: 16000,
	}
	vt.streamProcessor = NewAudioStreamProcessor(4.0, 0.5, vt.sampleRate) // 4s window, 0.5s overlap
	vt.whisper, err = whisper.New(dir)
	if err != nil {
		return nil, err
	}
	vt.model = vt.whisper.GetModelById(filename)
	if vt.model == nil {
		logging.Info("AUDIO: Downloading model %s", filename)
		vt.model, err = vt.whisper.DownloadModel(context.Background(), filename, nil)
		if err != nil {
			return nil, err
		}
	}
	vt.task = task.New()
	if err = vt.task.Init(dir, vt.model, 0); err != nil {
		return nil, err
	}
	logging.Info("AUDIO: Whisper transcription enabled with model %s", vt.model.Id)

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
	vt.fullTranscription = ""
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

// ProcessAudioChunk processes incoming audio data from microphone
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
	vt.streamProcessor.AddSamples(samples)

	// Process any available chunks
	for {
		chunk, ok := vt.streamProcessor.GetNextChunk()
		if !ok {
			break
		}
		vt.mu.Unlock()
		logging.Info("AUDIO: Processing overlapped chunk of %d samples (%.1fs)", len(chunk), float64(len(chunk))/float64(vt.sampleRate))
		vt.transcribeAudio(chunk)
		vt.mu.Lock()
	}
	vt.mu.Unlock()

	return nil
}

// transcribeAudio performs the actual transcription using go-whisper
func (vt *VoiceTranscriber) transcribeAudio(audio []float32) {
	logging.Info("AUDIO: Transcribing %d audio samples (%.1fs)", len(audio), float64(len(audio))/float64(vt.sampleRate))

	// Save audio to file for debugging
	if err := saveAudioToWav(audio, fmt.Sprintf("debug_audio_%d.wav", time.Now().UnixNano())); err != nil {
		logging.Error("AUDIO: Failed to save debug audio: %v", err)
	} else {
		logging.Debug("AUDIO: Saved debug audio to file")
	}

	vt.task.CopyParams()
	vt.task.SetLanguage("auto")
	vt.task.SetTranslate(false)
	ts := time.Since(vt.startTime)
	err := vt.task.Transcribe(context.Background(), ts, audio, func(seg *schema.Segment) {
		vt.mu.Lock()
		vt.totalSegments++
		vt.mu.Unlock()
		logging.Debug("AUDIO: New segment: %s", seg.Text)
		if vt.transcriptionCallback != nil {
			vt.transcriptionCallback(seg.Text)
		}
	})
	if err != nil {
		logging.Error("AUDIO: Transcription error: %v", err)
		return
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

// StartCapture starts microphone capture using PortAudio
func (vt *VoiceTranscriber) StartCapture(ctx context.Context) error {
	logging.Info("AUDIO: Starting microphone capture")
	vt.mu.Lock()
	if vt.running && vt.captureCancel != nil {
		vt.mu.Unlock()
		logging.Info("AUDIO: Capture already running")
		return nil
	}
	vt.mu.Unlock()

	// Initialize PortAudio
	if err := portaudio.Initialize(); err != nil {
		return fmt.Errorf("failed to initialize PortAudio: %w", err)
	}

	// Open default input stream
	bufferSize := 1024
	stream, err := portaudio.OpenDefaultStream(1, 0, float64(vt.sampleRate), bufferSize, func(in []int16) {
		if len(in) == 0 {
			return
		}
		// Convert int16 to float32
		samples := make([]float32, len(in))
		for i, sample := range in {
			samples[i] = float32(sample) / 32768.0
		}
		// Convert to PCM bytes for ProcessAudioChunk
		pcmData := make([]byte, len(samples)*2)
		for i, sample := range samples {
			intSample := int16(sample * 32767.0)
			binary.LittleEndian.PutUint16(pcmData[i*2:], uint16(intSample))
		}
		if err := vt.ProcessAudioChunk(pcmData); err != nil {
			logging.Error("AUDIO: Failed to process captured chunk: %v", err)
		}
	})
	if err != nil {
		portaudio.Terminate()
		return fmt.Errorf("failed to open audio stream: %w", err)
	}

	// Create capture context
	captureCtx, cancel := context.WithCancel(ctx)
	vt.mu.Lock()
	vt.captureCtx = captureCtx
	vt.captureCancel = cancel
	vt.stream = stream
	vt.mu.Unlock()

	// Start the stream
	if err := stream.Start(); err != nil {
		stream.Close()
		portaudio.Terminate()
		vt.mu.Lock()
		vt.captureCtx = nil
		vt.captureCancel = nil
		vt.stream = nil
		vt.mu.Unlock()
		return fmt.Errorf("failed to start audio stream: %w", err)
	}

	logging.Info("AUDIO: Successfully started PortAudio microphone capture")

	// Start a goroutine to handle stopping
	go func() {
		<-captureCtx.Done()
		logging.Info("AUDIO: Stopping microphone capture")
		if err := stream.Stop(); err != nil {
			logging.Error("AUDIO: Error stopping stream: %v", err)
		}
		stream.Close()
		portaudio.Terminate()
		vt.mu.Lock()
		vt.captureCtx = nil
		vt.captureCancel = nil
		vt.stream = nil
		vt.mu.Unlock()
		logging.Info("AUDIO: Microphone capture stopped")
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
	if vt.task != nil {
		if err := vt.task.Close(); err != nil {
			return err
		}
	}
	if vt.whisper != nil {
		return vt.whisper.Close()
	}
	return nil
}
