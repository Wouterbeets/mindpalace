package audio

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/gordonklaus/portaudio"
)

// Helper function to get absolute value of float32
func absVal(x float32) float32 {
	if x < 0 {
		return -x
	}
	return x
}

// VoiceTranscriber handles audio recording and real-time transcription
type VoiceTranscriber struct {
	stream                *portaudio.Stream
	audioFile             *os.File
	sampleCount           int
	mu                    sync.Mutex
	running               bool
	transcriptionCallback func(string)                                        // For raw transcription text
	sessionCallback       func(eventType string, data map[string]interface{}) // For session events
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

// NewVoiceTranscriber creates a new instance of the voice transcriber
func NewVoiceTranscriber() *VoiceTranscriber {
	return &VoiceTranscriber{
		historySize: 3,
		wordHistory: make([]string, 0, 3),
	}
}

// SetSessionEventCallback sets the callback for session-related events
func (vt *VoiceTranscriber) SetSessionEventCallback(callback func(eventType string, data map[string]interface{})) {
	vt.mu.Lock()
	defer vt.mu.Unlock()
	vt.sessionCallback = callback
}

// Start begins audio recording and transcription
func (vt *VoiceTranscriber) Start(transcriptionCallback func(string)) error {
	vt.mu.Lock()
	defer vt.mu.Unlock()

	if vt.running {
		return nil
	}

	// Set up everything for a new recording session
	vt.transcriptionText = "" // Initialize transcription text
	vt.audioBuffer = make([]float32, 0, 64000)
	vt.transcriptionCallback = transcriptionCallback
	vt.sessionID = fmt.Sprintf("session-%d", time.Now().UnixNano())
	vt.startTime = time.Now()
	vt.totalSegments = 0

	// Start the Python transcription process
	log.Println("Starting transcription process...")
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

	// Process stderr in a separate goroutine
	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			log.Printf("Python stderr: %s", scanner.Text())
		}
		if err := scanner.Err(); err != nil {
			log.Printf("Error reading Python stderr: %v", err)
		}
	}()

	// Create the WAV file for recording
	log.Println("Creating audio file...")
	vt.audioFile, err = os.Create("test_audio.wav")
	if err != nil {
		vt.cmd.Process.Kill()
		log.Printf("Error creating audio file: %v", err)
		return fmt.Errorf("failed to create audio file: %w", err)
	}
	vt.writeWAVHeader()

	// Initialize PortAudio with panic protection
	log.Println("Initializing PortAudio...")
	var paInitError error
	func() {
		// Use a defer/recover to catch panics in portaudio initialization
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

	// Open audio stream with simplified approach
	log.Println("Opening audio stream...")
	
	// Create buffer for audio capture
	bufferSize := 1024
	buffer := make([]float32, bufferSize)
	
	// Try to open a default stream for input
	var streamError error
	func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("RECOVERED PANIC in stream opening: %v", r)
				streamError = fmt.Errorf("panic in stream opening: %v", r)
			}
		}()
		
		// Simple default input stream
		vt.stream, streamError = portaudio.OpenDefaultStream(1, 0, 16000, bufferSize, &buffer)
	}()

	if streamError != nil {
		log.Printf("Error opening audio stream: %v", streamError)
		portaudio.Terminate()
		vt.audioFile.Close()
		vt.cmd.Process.Kill()
		return fmt.Errorf("failed to open audio stream: %w", streamError)
	}

	// Start the audio stream
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
	log.Println("Audio transcription started successfully")

	// Start transcription goroutine to process text from Python
	go vt.processTranscriptions()
	
	// Start a separate goroutine to read audio in a blocking fashion
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("RECOVERED PANIC in audio processing loop: %v", r)
			}
		}()
		
		log.Println("Starting audio reading loop...")
		readCount := 0
		
		for {
			// Check if we should continue running
			vt.mu.Lock()
			if !vt.running || vt.stream == nil {
				vt.mu.Unlock()
				log.Println("Audio reading loop stopping - not running or stream closed")
				return
			}
			vt.mu.Unlock()
			
			// Read from the stream (blocking call)
			log.Printf("Reading audio data from stream... (read count: %d)", readCount)
			err := vt.stream.Read()
			readCount++
			
			if err != nil {
				log.Printf("Error reading from audio stream: %v", err)
				time.Sleep(100 * time.Millisecond) // Avoid tight loop on error
				continue
			}
			
			// Check if we have valid audio data
			hasAudio := false
			maxVal := float32(0.0)
			for _, sample := range buffer {
				if sample != 0 {
					hasAudio = true
					if absVal(sample) > maxVal {
						maxVal = absVal(sample)
					}
				}
			}
			
			if hasAudio {
				log.Printf("Got valid audio data - max amplitude: %.6f (read count: %d)", maxVal, readCount)
			} else {
				log.Printf("WARNING: Read from stream returned success but buffer contains only zeros (read count: %d)", readCount)
			}
			
			// Process the audio data we just read
			vt.mu.Lock()
			if vt.running {
				log.Println("Processing audio data...")
				vt.processAudio(buffer)
			}
			vt.mu.Unlock()
			
			// Small delay to avoid excessive CPU usage
			time.Sleep(10 * time.Millisecond)
		}
	}()

	return nil
}

// Stop ends the audio recording and transcription
func (vt *VoiceTranscriber) Stop() {
	log.Println("Stop function called")
	if vt.stream == nil {
		log.Println("No active stream to stop")
		return
	}

	log.Println("Stopping stream...")
	vt.mu.Lock()
	vt.running = false
	vt.mu.Unlock()

	// Stop the stream with panic protection
	log.Println("Closing audio stream...")
	if vt.stream != nil {
		func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("RECOVERED PANIC in stream.Stop: %v", r)
				}
			}()
			
			if err := vt.stream.Stop(); err != nil {
				log.Printf("Error stopping stream: %v", err)
			}
		}()
		
		func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("RECOVERED PANIC in stream.Close: %v", r)
				}
			}()
			
			if err := vt.stream.Close(); err != nil {
				log.Printf("Error closing stream: %v", err)
			}
		}()
		
		vt.stream = nil
	}
	log.Println("Audio stream closed")

	// Close the audio file
	if vt.audioFile != nil {
		log.Println("Updating WAV header...")
		vt.updateWAVHeader()
		log.Println("Closing audio file...")
		if err := vt.audioFile.Close(); err != nil {
			log.Printf("Error closing file: %v", err)
		}
		vt.audioFile = nil
	}
	log.Println("Audio file closed")

	// Kill the Python transcription process
	log.Println("Terminating transcription process...")
	if vt.cmd != nil {
		if err := vt.cmd.Process.Kill(); err != nil {
			log.Printf("Error killing command: %v", err)
		}
		vt.cmd = nil
	}
	log.Println("Transcription process terminated")

	// Clean up PortAudio
	log.Println("Terminating PortAudio...")
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
	log.Println("PortAudio terminated")

	// Calculate duration
	duration := time.Since(vt.startTime).Seconds()

	// Trigger "stop" event
	if vt.sessionCallback != nil {
		log.Println("Dispatching session end callback...")
		
		// Use eventsourcing's recovery pattern directly
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
		log.Println("Session end callback dispatched")
	}

	vt.mu.Lock()
	vt.wordHistory = vt.wordHistory[:0]
	vt.mu.Unlock()

	log.Printf("Audio transcription stopped, processed %d samples", vt.sampleCount)
}

// processAudio processes incoming audio data
func (vt *VoiceTranscriber) processAudio(in []float32) {
	vt.mu.Lock()
	defer vt.mu.Unlock()

	if !vt.running || vt.writer == nil {
		log.Println("processAudio: Not running or writer is nil - skipping processing")
		return
	}

	// Check if we have any non-zero samples
	hasAudio := false
	maxVal := float32(0.0)
	for _, sample := range in {
		if sample != 0 {
			hasAudio = true
			if absVal(sample) > maxVal {
				maxVal = absVal(sample)
			}
		}
	}
	
	if !hasAudio {
		log.Println("processAudio: Buffer contains only zeros - skipping")
		return
	}
	
	log.Printf("processAudio: Adding %d samples to buffer (max amplitude: %.6f)", len(in), maxVal)
	vt.audioBuffer = append(vt.audioBuffer, in...)

	// Process chunks of 32000 samples (2 seconds at 16kHz)
	chunkCount := 0
	for len(vt.audioBuffer) >= 32000 {
		chunk := vt.audioBuffer[:32000]
		vt.audioBuffer = vt.audioBuffer[32000:]
		chunkCount++

		// Check if this chunk has non-zero data
		chunkHasAudio := false
		chunkMaxVal := float32(0.0)
		for _, sample := range chunk {
			if sample != 0 {
				chunkHasAudio = true
				if absVal(sample) > chunkMaxVal {
					chunkMaxVal = absVal(sample)
				}
			}
		}
		
		if !chunkHasAudio {
			log.Println("Chunk contains only zeros - skipping")
			continue
		}
		
		log.Printf("Processing chunk %d of 32000 samples (max amplitude: %.6f)", chunkCount, chunkMaxVal)

		// Write to WAV file
		for _, sample := range chunk {
			sampleInt16 := int16(sample * 32767)
			if err := binary.Write(vt.audioFile, binary.LittleEndian, sampleInt16); err != nil {
				log.Printf("WAV write error: %v", err)
			}
		}
		vt.sampleCount += len(chunk)

		// Convert chunk to binary PCM16 for transmission to Python
		var pcmData []byte
		for _, sample := range chunk {
			sample16 := int16(sample * 32767)
			pcmData = append(pcmData, byte(sample16&0xFF), byte(sample16>>8))
		}

		// Send the chunk to Python
		log.Printf("Sending %d bytes of PCM data to Python process", len(pcmData))
		fmt.Fprintf(vt.writer, "%d\n", len(pcmData))
		bytesWritten, err := vt.writer.Write(pcmData)
		if err != nil {
			log.Printf("Error writing PCM data: %v", err)
		} else {
			log.Printf("Wrote %d bytes of PCM data", bytesWritten)
		}
		
		vt.writer.WriteByte('\n')
		err = vt.writer.Flush()
		if err != nil {
			log.Printf("Error flushing writer: %v", err)
		} else {
			log.Println("Flushed PCM data to Python process")
		}
	}
}

// processTranscriptions handles incoming transcriptions from the Python process
func (vt *VoiceTranscriber) processTranscriptions() {
	log.Println("Starting transcription processing goroutine...")
	transcriptionCount := 0
	
	for {
		vt.mu.Lock()
		if !vt.running || vt.reader == nil {
			vt.mu.Unlock()
			log.Println("Stopping transcription processor - not running or reader closed")
			return
		}
		reader := vt.reader
		callback := vt.transcriptionCallback
		vt.mu.Unlock()

		log.Println("Waiting for transcription from Python process...")
		line, err := reader.ReadString('\n')
		if err != nil {
			log.Printf("Error reading from Python: %v", err)
			time.Sleep(100 * time.Millisecond) // Avoid tight loop on error
			continue
		}

		line = strings.TrimSpace(line)
		if line == "" {
			log.Println("Received empty line from Python process - skipping")
			continue
		}

		transcriptionCount++
		log.Printf("Received transcription #%d: %s", transcriptionCount, line)

		vt.mu.Lock()
		if !vt.running {
			vt.mu.Unlock()
			log.Println("Stopping transcription processor - running flag cleared")
			return
		}

		vt.totalSegments++
		vt.mu.Unlock()

		// Call the callback with the transcription
		if callback != nil {
			log.Printf("Calling transcription callback with: %s", line)
			callback(line)
		} else {
			log.Println("WARNING: No transcription callback set")
		}
	}
}

// writeWAVHeader writes the WAV header to the output file
func (vt *VoiceTranscriber) writeWAVHeader() {
	vt.audioFile.WriteString("RIFF") // ChunkID
	binary.Write(vt.audioFile, binary.LittleEndian, uint32(36))
	vt.audioFile.WriteString("WAVE") // Format
	vt.audioFile.WriteString("fmt ") // Subchunk1ID
	binary.Write(vt.audioFile, binary.LittleEndian, uint32(16))
	binary.Write(vt.audioFile, binary.LittleEndian, uint16(1))                 // PCM
	binary.Write(vt.audioFile, binary.LittleEndian, uint16(1))                 // Mono
	binary.Write(vt.audioFile, binary.LittleEndian, uint32(16000))             // Sample rate
	binary.Write(vt.audioFile, binary.LittleEndian, uint32(16000*2))           // Byte rate
	binary.Write(vt.audioFile, binary.LittleEndian, uint16(2))                 // Block align
	binary.Write(vt.audioFile, binary.LittleEndian, uint16(16))                // Bits per sample
	vt.audioFile.WriteString("data") // Subchunk2ID
	binary.Write(vt.audioFile, binary.LittleEndian, uint32(0))
}

// updateWAVHeader updates the WAV header with the final file size
func (vt *VoiceTranscriber) updateWAVHeader() {
	currentPos, err := vt.audioFile.Seek(0, 1)
	if err != nil {
		log.Printf("Error getting current file position: %v", err)
		return
	}

	// Update Chunk Size
	vt.audioFile.Seek(4, 0)
	chunkSize := uint32(currentPos - 8)
	binary.Write(vt.audioFile, binary.LittleEndian, chunkSize)

	// Update Subchunk2Size
	vt.audioFile.Seek(40, 0)
	subchunk2Size := uint32(currentPos - 44)
	binary.Write(vt.audioFile, binary.LittleEndian, subchunk2Size)
}