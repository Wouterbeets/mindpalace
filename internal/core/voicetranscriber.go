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

	vt.transcriptionText = "" // Initialize transcription text
	vt.audioBuffer = make([]float32, 0, 64000)
	vt.transcriptionCallback = transcriptionCallback
	vt.sessionID = fmt.Sprintf("session-%d", time.Now().UnixNano())
	vt.startTime = time.Now()
	vt.totalSegments = 0

	vt.cmd = exec.Command("/home/mindpalace/mindpalace_venv/bin/python3", "transcribe.py")
	stdin, err := vt.cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := vt.cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := vt.cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := vt.cmd.Start(); err != nil {
		return err
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

	vt.audioFile, err = os.Create("test_audio.wav")
	if err != nil {
		vt.cmd.Process.Kill()
		return err
	}
	vt.writeWAVHeader()

	err = portaudio.Initialize()
	if err != nil {
		vt.audioFile.Close()
		vt.cmd.Process.Kill()
		return err
	}

	// Device selection logic (simplified for brevity, adjust as needed)
	vt.stream, err = portaudio.OpenDefaultStream(1, 0, 16000, 2048, vt.processAudio)
	if err != nil {
		vt.audioFile.Close()
		vt.cmd.Process.Kill()
		portaudio.Terminate()
		return err
	}

	vt.running = true
	if err := vt.stream.Start(); err != nil {
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
			"DeviceInfo": "default", // Replace with actual device info if available
		})
	}
	log.Println("Audio stream started")
	go vt.readTranscription()
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
		vt.sessionCallback("stop", map[string]interface{}{
			"SessionID":    vt.sessionID,
			"DurationSecs": duration,
			"SampleCount":  vt.sampleCount,
		})
		fmt.Println("done")
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

	vt.audioBuffer = append(vt.audioBuffer, in...)

	for len(vt.audioBuffer) >= 32000 {
		chunk := vt.audioBuffer[:32000]
		vt.audioBuffer = vt.audioBuffer[32000:]

		for _, sample := range chunk {
			sampleInt16 := int16(sample * 32767)
			if err := binary.Write(vt.audioFile, binary.LittleEndian, sampleInt16); err != nil {
				log.Printf("WAV write error: %v", err)
			}
			vt.sampleCount++
		}

		b := float32ToByte(chunk)
		if _, err := vt.writer.Write(b); err != nil {
			log.Printf("Failed to send audio to Whisper: %v", err)
			return
		}
		vt.writer.Flush()
	}
}

func (vt *VoiceTranscriber) readTranscription() {
	for vt.running {
		if vt.reader == nil {
			continue
		}
		text, err := vt.reader.ReadString('\n')
		if err != nil {
			if vt.running {
				log.Printf("Error reading transcription: %v", err)
			}
			break
		}
		vt.mu.Lock()
		if vt.running {
			cleanedText := vt.cleanTranscription(text)
			if cleanedText != "" {
				// Accumulate transcription text
				if vt.transcriptionText != "" {
					vt.transcriptionText += " " // Use space as separator
				}
				vt.transcriptionText += cleanedText

				// Update UI in real-time
				if vt.transcriptionCallback != nil {
					vt.transcriptionCallback(cleanedText)
				}
				vt.totalSegments++ // Optional: keep for stats if desired
			}
		}
		vt.mu.Unlock()
	}
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
