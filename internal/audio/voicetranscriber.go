package audio

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"log"
	"mindpalace/pkg/logging"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/gordonklaus/portaudio"
)

// VoiceTranscriber manages audio recording and real-time transcription
type VoiceTranscriber struct {
	wg                    sync.WaitGroup
	stream                *portaudio.Stream
	audioFile             *os.File
	sampleCount           int
	mu                    sync.Mutex
	running               bool
	transcriptionCallback func(string)
	sessionCallback       func(eventType string, data map[string]interface{})
	cmd                   *exec.Cmd
	writer                *bufio.Writer
	reader                *bufio.Reader
	audioBuffer           []float32
	wordHistory           []string
	historySize           int
	transcriptionText     string
	transcriptionHistory  []string
	sessionID             string
	startTime             time.Time
	totalSegments         int
}

// NewVoiceTranscriber initializes a new VoiceTranscriber instance
func NewVoiceTranscriber() *VoiceTranscriber {
	return &VoiceTranscriber{
		historySize: 3,
		wordHistory: make([]string, 0, 3),
	}
}

// SetSessionEventCallback sets the callback for session events
func (vt *VoiceTranscriber) SetSessionEventCallback(callback func(eventType string, data map[string]interface{})) {
	vt.mu.Lock()
	defer vt.mu.Unlock()
	vt.sessionCallback = callback
}

// Start initiates audio recording and transcription
func (vt *VoiceTranscriber) Start(transcriptionCallback func(string)) error {
	vt.mu.Lock()
	defer vt.mu.Unlock()

	if vt.running {
		return nil
	}

	vt.transcriptionText = ""
	vt.audioBuffer = make([]float32, 0, 64000)
	vt.transcriptionCallback = transcriptionCallback
	vt.sessionID = fmt.Sprintf("session-%d", time.Now().UnixNano())
	vt.startTime = time.Now()
	vt.totalSegments = 0

	// Initialize Python transcription process
	logging.Trace("Starting transcription process...")
	vt.cmd = exec.Command("/home/mindpalace/mindpalace_venv/bin/python3", "transcribe.py")
	stdin, err := vt.cmd.StdinPipe()
	if err != nil {
		log.Printf("Error getting stdin pipe: %v", err)
		return fmt.Errorf("failed to create stdin pipe: %w", err)
	}
	stdout, err := vt.cmd.StdoutPipe()
	if err != nil {
		log.Printf("Error getting stdout pipe: %v", err)
		return fmt.Errorf("failed to create stdout pipe: %w", err)
	}
	stderr, err := vt.cmd.StderrPipe()
	if err != nil {
		log.Printf("Error getting stderr pipe: %v", err)
		return fmt.Errorf("failed to create stderr pipe: %w", err)
	}
	if err := vt.cmd.Start(); err != nil {
		log.Printf("Error starting Python process: %v", err)
		return fmt.Errorf("failed to start transcription process: %w", err)
	}
	vt.writer = bufio.NewWriter(stdin)
	vt.reader = bufio.NewReader(stdout)

	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			log.Printf("Python stderr: %s", scanner.Text())
		}
		if err := scanner.Err(); err != nil {
			log.Printf("Error reading Python stderr: %v", err)
		}
	}()

	// Setup audio file
	logging.Trace("Creating audio file...")
	vt.audioFile, err = os.Create("test_audio.wav")
	if err != nil {
		vt.cmd.Process.Kill()
		log.Printf("Error creating audio file: %v", err)
		return fmt.Errorf("failed to create audio file: %w", err)
	}
	vt.writeWAVHeader()

	// Initialize PortAudio
	logging.Trace("Initializing PortAudio...")
	var paInitError error
	func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("RECOVERED PANIC in portaudio initialization: %v", r)
				paInitError = fmt.Errorf("panic in portaudio initialization: %v", r)
			}
		}()
		paInitError = portaudio.Initialize()
	}()
	if paInitError != nil {
		log.Printf("PortAudio initialization error: %v", paInitError)
		vt.audioFile.Close()
		vt.cmd.Process.Kill()
		return fmt.Errorf("failed to initialize audio system: %w", paInitError)
	}

	// Open audio stream
	logging.Trace("Opening audio stream...")
	bufferSize := 1024
	buffer := make([]float32, bufferSize)
	var streamError error
	func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("RECOVERED PANIC in stream opening: %v", r)
				streamError = fmt.Errorf("panic in stream opening: %v", r)
			}
		}()
		vt.stream, streamError = portaudio.OpenDefaultStream(1, 0, 16000, bufferSize, &buffer)
	}()
	if streamError != nil {
		log.Printf("Error opening audio stream: %v", streamError)
		portaudio.Terminate()
		vt.audioFile.Close()
		vt.cmd.Process.Kill()
		return fmt.Errorf("failed to open audio stream: %w", streamError)
	}

	// Start audio stream
	var startError error
	func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("RECOVERED PANIC in stream.Start: %v", r)
				startError = fmt.Errorf("panic in stream.Start: %v", r)
			}
		}()
		startError = vt.stream.Start()
	}()
	if startError != nil {
		log.Printf("Error starting audio stream: %v", startError)
		vt.stream.Close()
		portaudio.Terminate()
		vt.audioFile.Close()
		vt.cmd.Process.Kill()
		return fmt.Errorf("failed to start audio stream: %w", startError)
	}

	vt.running = true
	logging.Trace("Audio transcription started successfully")

	// Start goroutines
	go vt.processTranscriptions()
	vt.wg.Add(1)
	go func() {
		defer vt.wg.Done()
		defer func() {
			if r := recover(); r != nil {
				log.Printf("RECOVERED PANIC in audio processing loop: %v", r)
			}
		}()

		logging.Trace("Starting audio reading loop...")
		for {
			vt.mu.Lock()
			if !vt.running || vt.stream == nil {
				vt.mu.Unlock()
				logging.Trace("Audio reading loop stopping")
				return
			}
			vt.mu.Unlock()

			err := vt.stream.Read()
			if err != nil {
				log.Printf("Error reading from audio stream: %v", err)
				time.Sleep(100 * time.Millisecond)
				continue
			}

			vt.mu.Lock()
			if vt.running {
				vt.processAudio(buffer)
			}
			vt.mu.Unlock()

			time.Sleep(10 * time.Millisecond)
		}
	}()

	return nil
}

// Stop terminates audio recording and transcription
func (vt *VoiceTranscriber) Stop() {
	vt.mu.Lock()
	vt.running = false
	vt.mu.Unlock()

	if vt.stream != nil {
		func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("Recovered panic in stream.Abort: %v", r)
				}
			}()
			if err := vt.stream.Abort(); err != nil {
				log.Printf("Error aborting stream: %v", err)
			}
		}()

		vt.wg.Wait()

		func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("Recovered panic in stream.Close: %v", r)
				}
			}()
			if err := vt.stream.Close(); err != nil {
				log.Printf("Error closing stream: %v", err)
			}
		}()
		vt.stream = nil
	}
	logging.Trace("Audio stream closed")

	if vt.audioFile != nil {
		logging.Trace("Updating WAV header...")
		vt.updateWAVHeader()
		logging.Trace("Closing audio file...")
		if err := vt.audioFile.Close(); err != nil {
			log.Printf("Error closing file: %v", err)
		}
		vt.audioFile = nil
	}
	logging.Trace("Audio file closed")

	logging.Trace("Terminating transcription process...")
	if vt.cmd != nil {
		if err := vt.cmd.Process.Kill(); err != nil {
			log.Printf("Error killing command: %v", err)
		}
		vt.cmd = nil
	}
	logging.Trace("Transcription process terminated")

	logging.Trace("Terminating PortAudio...")
	func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("RECOVERED PANIC in portaudio.Terminate: %v", r)
			}
		}()
		if err := portaudio.Terminate(); err != nil {
			log.Printf("Error terminating PortAudio: %v", err)
		}
	}()
	logging.Trace("PortAudio terminated")

	duration := time.Since(vt.startTime).Seconds()
	if vt.sessionCallback != nil {
		logging.Trace("Dispatching session end callback...")
		sessionCallback := vt.sessionCallback
		sessionID := vt.sessionID
		go func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("RECOVERED PANIC in transcriber session callback: %v", r)
				}
			}()
			sessionCallback("stop", map[string]interface{}{
				"SessionID":    sessionID,
				"DurationSecs": duration,
				"SampleCount":  vt.sampleCount,
			})
		}()
		logging.Trace("Session end callback dispatched")
	}

	vt.mu.Lock()
	vt.wordHistory = vt.wordHistory[:0]
	vt.mu.Unlock()

	log.Printf("Audio transcription stopped, processed %d samples", vt.sampleCount)
}

// processAudio handles audio data processing
func (vt *VoiceTranscriber) processAudio(in []float32) {
	vt.mu.Lock()
	defer vt.mu.Unlock()

	if !vt.running || vt.writer == nil {
		return
	}

	vt.audioBuffer = append(vt.audioBuffer, in...)
	for len(vt.audioBuffer) >= 32000 {
		chunk := vt.audioBuffer[:32000]
		vt.audioBuffer = vt.audioBuffer[32000:]

		var pcmData []byte
		for _, sample := range chunk {
			sample16 := int16(sample * 32767)
			pcmData = append(pcmData, byte(sample16&0xFF), byte(sample16>>8))
		}

		fmt.Fprintf(vt.writer, "%d\n", len(pcmData))
		if _, err := vt.writer.Write(pcmData); err != nil {
			log.Printf("Error writing PCM data: %v", err)
		}
		vt.writer.WriteByte('\n')
		if err := vt.writer.Flush(); err != nil {
			log.Printf("Error flushing writer: %v", err)
		}
	}
}

// processTranscriptions processes transcription output from Python
func (vt *VoiceTranscriber) processTranscriptions() {
	for {
		vt.mu.Lock()
		if !vt.running || vt.reader == nil {
			vt.mu.Unlock()
			return
		}
		reader := vt.reader
		callback := vt.transcriptionCallback
		vt.mu.Unlock()

		line, err := reader.ReadString('\n')
		if err != nil {
			log.Printf("Error reading from Python: %v", err)
			time.Sleep(100 * time.Millisecond)
			continue
		}

		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		vt.mu.Lock()
		if !vt.running {
			vt.mu.Unlock()
			return
		}
		vt.totalSegments++
		vt.mu.Unlock()

		if callback != nil {
			callback(line)
		}
	}
}

// writeWAVHeader writes the initial WAV file header
func (vt *VoiceTranscriber) writeWAVHeader() {
	vt.audioFile.WriteString("RIFF")
	binary.Write(vt.audioFile, binary.LittleEndian, uint32(36))
	vt.audioFile.WriteString("WAVE")
	vt.audioFile.WriteString("fmt ")
	binary.Write(vt.audioFile, binary.LittleEndian, uint32(16))
	binary.Write(vt.audioFile, binary.LittleEndian, uint16(1))
	binary.Write(vt.audioFile, binary.LittleEndian, uint16(1))
	binary.Write(vt.audioFile, binary.LittleEndian, uint32(16000))
	binary.Write(vt.audioFile, binary.LittleEndian, uint32(16000*2))
	binary.Write(vt.audioFile, binary.LittleEndian, uint16(2))
	binary.Write(vt.audioFile, binary.LittleEndian, uint16(16))
	vt.audioFile.WriteString("data")
	binary.Write(vt.audioFile, binary.LittleEndian, uint32(0))
}

// updateWAVHeader updates the WAV header with final sizes
func (vt *VoiceTranscriber) updateWAVHeader() {
	currentPos, err := vt.audioFile.Seek(0, 1)
	if err != nil {
		log.Printf("Error getting current file position: %v", err)
		return
	}

	vt.audioFile.Seek(4, 0)
	binary.Write(vt.audioFile, binary.LittleEndian, uint32(currentPos-8))
	vt.audioFile.Seek(40, 0)
	binary.Write(vt.audioFile, binary.LittleEndian, uint32(currentPos-44))
}
