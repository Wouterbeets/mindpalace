package audio

import (
	"mindpalace/pkg/logging"
)

// AudioStreamProcessor handles sliding window audio processing with overlap
type AudioStreamProcessor struct {
	buffer      []float32
	windowSize  int // samples in window (e.g., 2s)
	overlapSize int // samples to overlap (e.g., 0.5s)
	stepSize    int // windowSize - overlapSize
	sampleRate  int
}

// NewAudioStreamProcessor creates a new stream processor with given window and overlap in seconds
// windowSec: size of each processing window (e.g., 4s)
// overlapSec: overlap between windows (e.g., 0.5s)
func NewAudioStreamProcessor(windowSec, overlapSec float64, sampleRate int) *AudioStreamProcessor {
	windowSize := int(windowSec * float64(sampleRate))
	overlapSize := int(overlapSec * float64(sampleRate))
	stepSize := windowSize - overlapSize
	capacity := windowSize + overlapSize // enough for one full window plus overlap

	return &AudioStreamProcessor{
		buffer:      make([]float32, 0, capacity),
		windowSize:  windowSize,
		overlapSize: overlapSize,
		stepSize:    stepSize,
		sampleRate:  sampleRate,
	}
}

// AddSamples appends new audio samples to the buffer
func (asp *AudioStreamProcessor) AddSamples(samples []float32) {
	asp.buffer = append(asp.buffer, samples...)
	logging.Debug("AUDIO: Added %d samples, buffer now has %d samples", len(samples), len(asp.buffer))
}

// GetNextChunk returns the next overlapping chunk if available
func (asp *AudioStreamProcessor) GetNextChunk() ([]float32, bool) {
	if len(asp.buffer) >= asp.windowSize {
		chunk := make([]float32, asp.windowSize)
		copy(chunk, asp.buffer[:asp.windowSize])

		// Slide the buffer by stepSize, keeping the overlap
		asp.buffer = asp.buffer[asp.stepSize:]
		logging.Debug("AUDIO: Processed chunk of %d samples, buffer now has %d samples", asp.windowSize, len(asp.buffer))

		return chunk, true
	}
	return nil, false
}

// GetBufferSize returns current buffer size for monitoring
func (asp *AudioStreamProcessor) GetBufferSize() int {
	return len(asp.buffer)
}
