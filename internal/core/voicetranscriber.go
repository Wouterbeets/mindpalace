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
	vt.mu.Lock()
	defer vt.mu.Unlock()

	if !vt.running {
		return
	}

	if vt.stream != nil {
		vt.stream.Stop()
		vt.stream.Close()
	}
	if vt.audioFile != nil {
		vt.updateWAVHeader()
		vt.audioFile.Close()
	}
	if vt.cmd != nil {
		vt.cmd.Process.Kill()
	}
	portaudio.Terminate()
	vt.running = false

	// Calculate duration
	duration := time.Since(vt.startTime).Seconds()

	// Trigger "stop" event
	if vt.sessionCallback != nil {
		vt.sessionCallback("stop", map[string]interface{}{
			"SessionID":    vt.sessionID,
			"DurationSecs": duration,
			"SampleCount":  vt.sampleCount,
		})
	}
	vt.wordHistory = vt.wordHistory[:0]
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
func (vt *VoiceTranscriber) cleanTranscription(text string) string {
	words := strings.Fields(strings.TrimSpace(text))
	if len(words) == 0 {
		return ""
	}

	newWords := make([]string, 0, len(words))
	for _, word := range words {
		if !vt.isSimilarInHistory(word) {
			newWords = append(newWords, word)
		}
	}

	for _, word := range newWords {
		vt.wordHistory = append(vt.wordHistory, word)
		if len(vt.wordHistory) > vt.historySize {
			vt.wordHistory = vt.wordHistory[1:]
		}
	}

	return strings.Join(newWords, " ")
}

func (vt *VoiceTranscriber) isSimilarInHistory(word string) bool {
	for _, histWord := range vt.wordHistory {
		if levenshteinDistance(word, histWord) <= 1 {
			return true
		}
	}
	return false
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

func min(a, b, c int) int {
	if a < b {
		if a < c {
			return a
		}
		return c
	}
	if b < c {
		return b
	}
	return c
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
