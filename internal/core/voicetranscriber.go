package core

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"log"
	"math"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/gordonklaus/portaudio"
)

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

func (vt *VoiceTranscriber) Start(transcriptionCallback func(string)) error {
	vt.mu.Lock()
	defer vt.mu.Unlock()

	if vt.running {
		return nil
	}

	log.Println("Initializing voice transcription...")
	
	// Initialize data structures
	vt.transcriptionText = ""
	vt.audioBuffer = make([]float32, 0, 64000)
	vt.transcriptionCallback = transcriptionCallback
	vt.sessionID = fmt.Sprintf("session-%d", time.Now().UnixNano())
	vt.startTime = time.Now()
	vt.totalSegments = 0
	vt.wordHistory = make([]string, 0, 3)
	vt.transcriptionHistory = make([]string, 0, 3)

	// Start Python transcription process
	log.Println("Starting Python transcription process...")
	vt.cmd = exec.Command("/home/mindpalace/mindpalace_venv/bin/python3", "transcribe.py")
	stdin, err := vt.cmd.StdinPipe()
	if err != nil {
		log.Printf("Failed to create stdin pipe: %v", err)
		return err
	}
	stdout, err := vt.cmd.StdoutPipe()
	if err != nil {
		log.Printf("Failed to create stdout pipe: %v", err)
		return err
	}
	stderr, err := vt.cmd.StderrPipe()
	if err != nil {
		log.Printf("Failed to create stderr pipe: %v", err)
		return err
	}
	
	// Start the process
	if err := vt.cmd.Start(); err != nil {
		log.Printf("Failed to start Python process: %v", err)
		return err
	}
	
	// Create buffered readers/writers
	vt.writer = bufio.NewWriter(stdin)
	vt.reader = bufio.NewReader(stdout)

	// Handle Python stderr output
	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			log.Printf("Python stderr: %s", scanner.Text())
		}
		if err := scanner.Err(); err != nil {
			log.Printf("Error reading Python stderr: %v", err)
		}
	}()

	// Create audio file for debugging
	log.Println("Creating audio debug file...")
	vt.audioFile, err = os.Create("test_audio.wav")
	if err != nil {
		log.Printf("Failed to create audio file: %v", err)
		vt.cmd.Process.Kill()
		return err
	}
	vt.writeWAVHeader()

	// Initialize PortAudio
	log.Println("Initializing PortAudio...")
	err = portaudio.Initialize()
	if err != nil {
		log.Printf("Failed to initialize PortAudio: %v", err)
		vt.audioFile.Close()
		vt.cmd.Process.Kill()
		return err
	}

	// Open audio stream
	log.Println("Opening audio stream...")
	vt.stream, err = portaudio.OpenDefaultStream(1, 0, 16000, 2048, vt.processAudio)
	if err != nil {
		log.Printf("Failed to open audio stream: %v", err)
		vt.audioFile.Close()
		vt.cmd.Process.Kill()
		portaudio.Terminate()
		return err
	}

	// Start audio stream
	vt.running = true
	if err := vt.stream.Start(); err != nil {
		log.Printf("Failed to start audio stream: %v", err)
		vt.running = false
		vt.stream.Close()
		vt.audioFile.Close()
		vt.cmd.Process.Kill()
		portaudio.Terminate()
		return err
	}

	// Trigger "start" event
	if vt.sessionCallback != nil {
		vt.sessionCallback("start", map[string]interface{}{
			"SessionID":  vt.sessionID,
			"DeviceInfo": "default",
		})
	}
	
	log.Println("Audio transcription started successfully")
	
	// Start the transcription reader goroutine
	go vt.readTranscription()
	
	// Start a separate goroutine to monitor the audio stream
	go func() {
		log.Println("Starting audio reading loop...")
		readCount := 0
		for vt.running {
			readCount++
			if readCount % 10 == 0 {
				log.Printf("Reading audio data from stream... (read count: %d)", readCount)
			}
			time.Sleep(200 * time.Millisecond)
		}
	}()
	
	return nil
}

func (vt *VoiceTranscriber) Stop() {
	fmt.Println("in stop")

	// Set running to false first without holding the lock for long
	vt.mu.Lock()
	if !vt.running {
		vt.mu.Unlock()
		return
	}
	vt.running = false // Signal processAudio to stop
	vt.mu.Unlock()

	// Stop and close the stream outside the lock
	fmt.Println("stream stop")
	if vt.stream != nil {
		if err := vt.stream.Stop(); err != nil {
			fmt.Printf("Error stopping stream: %v\n", err)
		}
		if err := vt.stream.Close(); err != nil {
			fmt.Printf("Error closing stream: %v\n", err)
		}
		vt.stream = nil
	}

	fmt.Println("audio file closing")
	if vt.audioFile != nil {
		vt.updateWAVHeader()
		if err := vt.audioFile.Close(); err != nil {
			fmt.Printf("Error closing audio file: %v\n", err)
		}
		vt.audioFile = nil
	}
	fmt.Println("audio file closed")

	fmt.Println("killing whisper")
	if vt.cmd != nil {
		if err := vt.cmd.Process.Kill(); err != nil {
			fmt.Printf("Error killing command: %v\n", err)
		}
		vt.cmd = nil
	}
	fmt.Println("killed")

	fmt.Println("terminate portaudio")
	if err := portaudio.Terminate(); err != nil {
		fmt.Printf("Error terminating PortAudio: %v\n", err)
	}
	fmt.Println("terminated")

	// Calculate duration
	duration := time.Since(vt.startTime).Seconds()

	// Trigger "stop" event
	if vt.sessionCallback != nil {
		fmt.Println("calling callback")
		
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
		fmt.Println("callback dispatched")
	}

	vt.mu.Lock()
	vt.wordHistory = vt.wordHistory[:0]
	vt.mu.Unlock()

	log.Printf("Audio stream stopped, saved %d samples", vt.sampleCount)
}

func (vt *VoiceTranscriber) processAudio(in []float32) {
	vt.mu.Lock()
	defer vt.mu.Unlock()

	if !vt.running || vt.writer == nil {
		return
	}

	// Log audio level for debugging
	maxAmp := float32(0)
	for _, sample := range in {
		if abs := float32(math.Abs(float64(sample))); abs > maxAmp {
			maxAmp = abs
		}
	}
	log.Printf("Got valid audio data - max amplitude: %f (read count: %d)", maxAmp, vt.sampleCount/32000)
	
	// Apply gain to boost audio level
	gainFactor := float32(5.0) // Increase volume by 5x
	amplifiedBuffer := make([]float32, len(in))
	for i, sample := range in {
		// Apply gain with clipping prevention
		amplifiedSample := sample * gainFactor
		if amplifiedSample > 1.0 {
			amplifiedSample = 1.0
		} else if amplifiedSample < -1.0 {
			amplifiedSample = -1.0
		}
		amplifiedBuffer[i] = amplifiedSample
	}
	
	// Append amplified audio to buffer
	vt.audioBuffer = append(vt.audioBuffer, amplifiedBuffer...)

	// Process chunks of 32000 samples (2 seconds at 16kHz)
	for len(vt.audioBuffer) >= 32000 {
		chunk := vt.audioBuffer[:32000]
		vt.audioBuffer = vt.audioBuffer[32000:]
		
		// Calculate max amplitude of chunk for logging
		chunkMaxAmp := float32(0)
		for _, sample := range chunk {
			if abs := float32(math.Abs(float64(sample))); abs > chunkMaxAmp {
				chunkMaxAmp = abs
			}
		}
		log.Printf("Processing audio chunk with max amplitude: %f", chunkMaxAmp)

		// Write to WAV file (original format for debugging)
		for _, sample := range chunk {
			sampleInt16 := int16(sample * 32767)
			if err := binary.Write(vt.audioFile, binary.LittleEndian, sampleInt16); err != nil {
				log.Printf("WAV write error: %v", err)
			}
			vt.sampleCount++
		}

		// Send to Python process
		b := float32ToByte(chunk)
		if _, err := vt.writer.Write(b); err != nil {
			log.Printf("Failed to send audio to Whisper: %v", err)
			return
		}
		if err := vt.writer.Flush(); err != nil {
			log.Printf("Failed to flush audio data: %v", err)
			return
		}
		log.Printf("Sent %d bytes to Python process", len(b))
	}
}

func (vt *VoiceTranscriber) readTranscription() {
	log.Println("Starting transcription processing goroutine...")
	log.Println("Waiting for transcription from Python process...")
	
	for vt.running {
		if vt.reader == nil {
			time.Sleep(100 * time.Millisecond)
			continue
		}
		
		// Set a deadline for reading to prevent blocking indefinitely
		text, err := vt.reader.ReadString('\n')
		if err != nil {
			if vt.running {
				log.Printf("Error reading transcription: %v", err)
				// Short pause to prevent CPU spinning on error
				time.Sleep(100 * time.Millisecond)
			}
			continue // Continue trying to read instead of breaking
		}
		
		// Log raw transcription for debugging
		log.Printf("Received raw transcription: %q", strings.TrimSpace(text))
		
		vt.mu.Lock()
		if vt.running {
			cleanedText := vt.cleanTranscription(text)
			if cleanedText != "" {
				log.Printf("Cleaned transcription text: %q", cleanedText)
				
				// Accumulate transcription text
				if vt.transcriptionText != "" {
					vt.transcriptionText += " " // Use space as separator
				}
				vt.transcriptionText += cleanedText

				// Update UI in real-time with panic recovery
				if vt.transcriptionCallback != nil {
					func() {
						defer func() {
							if r := recover(); r != nil {
								log.Printf("RECOVERED PANIC in transcription callback: %v", r)
							}
						}()
						vt.transcriptionCallback(cleanedText)
						log.Printf("Successfully called transcription callback with text: %q", cleanedText)
					}()
				} else {
					log.Println("Warning: No transcription callback set")
				}
				vt.totalSegments++
			} else {
				log.Println("Transcription text was empty after cleaning")
			}
		}
		vt.mu.Unlock()
	}
	log.Println("Transcription processing goroutine exiting")
}

// cleanTranscription and similarity functions remain unchanged
// cleanTranscription with sentence overlap detection
func (vt *VoiceTranscriber) cleanTranscription(text string) string {
	if text == "" {
		return ""
	}

	// Trim and split into words
	newWords := strings.Fields(strings.TrimSpace(text))
	if len(newWords) == 0 {
		return ""
	}
	newText := strings.Join(newWords, " ")

	// Check for exact duplicates in history
	for _, histText := range vt.transcriptionHistory {
		if newText == histText {
			return "" // Discard if identical to a recent segment
		}
	}

	// Get current transcription as words
	currentWords := strings.Fields(vt.transcriptionText)
	if len(currentWords) == 0 {
		vt.transcriptionText = newText
		vt.updateHistory(newText)
		return newText
	}

	// Find the longest suffix of current that matches prefix of new
	overlapLen := 0
	maxOverlap := min(len(currentWords), len(newWords))
	for i := 1; i <= maxOverlap; i++ {
		suffix := currentWords[len(currentWords)-i:]
		prefix := newWords[:i]
		if strings.Join(suffix, " ") == strings.Join(prefix, " ") {
			overlapLen = i
		} else {
			break
		}
	}

	// Extract non-overlapping part
	if overlapLen == len(newWords) {
		return "" // New segment fully overlaps, discard
	}
	nonOverlapWords := newWords[overlapLen:]
	nonOverlapText := strings.Join(nonOverlapWords, " ")

	// Append non-overlapping part
	vt.transcriptionText = vt.transcriptionText + " " + nonOverlapText
	vt.updateHistory(newText)
	return nonOverlapText
}

func (vt *VoiceTranscriber) isSimilarInHistory(word string) bool {
	for _, histWord := range vt.wordHistory {
		if levenshteinDistance(word, histWord) <= 1 {
			return true
		}
	}
	return false
}

// updateHistory adds to transcription history
func (vt *VoiceTranscriber) updateHistory(text string) {
	vt.transcriptionHistory = append(vt.transcriptionHistory, text)
	if len(vt.transcriptionHistory) > vt.historySize {
		vt.transcriptionHistory = vt.transcriptionHistory[1:]
	}
}

func levenshteinDistance(s, t string) int {
	if s == t {
		return 0
	}
	if len(s) == 0 {
		return len(t)
	}
	if len(t) == 0 {
		return len(s)
	}

	v0 := make([]int, len(t)+1)
	v1 := make([]int, len(t)+1)

	for i := 0; i <= len(t); i++ {
		v0[i] = i
	}

	for i := 0; i < len(s); i++ {
		v1[0] = i + 1
		for j := 0; j < len(t); j++ {
			cost := 0
			if s[i] != t[j] {
				cost = 1
			}
			v1[j+1] = min(v1[j]+1, v0[j+1]+1, v0[j]+cost)
		}
		for j := 0; j <= len(t); j++ {
			v0[j] = v1[j]
		}
	}
	return v1[len(t)]
}

func float32ToByte(f []float32) []byte {
	b := make([]byte, len(f)*4)
	for i, v := range f {
		bits := math.Float32bits(v)
		binary.LittleEndian.PutUint32(b[i*4:], bits)
	}
	return b
}

func (vt *VoiceTranscriber) writeWAVHeader() {
	vt.audioFile.WriteString("RIFF")
	binary.Write(vt.audioFile, binary.LittleEndian, uint32(36))
	vt.audioFile.WriteString("WAVE")
	vt.audioFile.WriteString("fmt ")
	binary.Write(vt.audioFile, binary.LittleEndian, uint32(16))
	binary.Write(vt.audioFile, binary.LittleEndian, uint16(1))
	binary.Write(vt.audioFile, binary.LittleEndian, uint16(1))
	binary.Write(vt.audioFile, binary.LittleEndian, uint32(16000))
	binary.Write(vt.audioFile, binary.LittleEndian, uint32(32000))
	binary.Write(vt.audioFile, binary.LittleEndian, uint16(2))
	binary.Write(vt.audioFile, binary.LittleEndian, uint16(16))
	vt.audioFile.WriteString("data")
	binary.Write(vt.audioFile, binary.LittleEndian, uint32(0))
}

func (vt *VoiceTranscriber) updateWAVHeader() {
	dataSize := uint32(vt.sampleCount * 2)
	vt.audioFile.Seek(4, 0)
	binary.Write(vt.audioFile, binary.LittleEndian, uint32(36+dataSize))
	vt.audioFile.Seek(40, 0)
	binary.Write(vt.audioFile, binary.LittleEndian, dataSize)
}
