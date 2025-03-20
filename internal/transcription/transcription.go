package transcription

import (
	"fmt"
	"mindpalace/pkg/eventsourcing"
	"time"
)

// TranscriptionManager manages transcription-related functionality
type TranscriptionManager struct {
}

// NewTranscriptionManager creates a new TranscriptionManager instance
func NewTranscriptionManager() *TranscriptionManager {
	return &TranscriptionManager{}
}

// RegisterHandlers registers transcription commands with the event processor
func (tm *TranscriptionManager) RegisterHandlers(ep *eventsourcing.EventProcessor) {
	ep.RegisterCommand("StartTranscription", tm.StartTranscriptionHandler)
	ep.RegisterCommand("StopTranscription", tm.StopTranscriptionHandler)
}

// StartTranscriptionHandler handles the start of a transcription session
func (tm *TranscriptionManager) StartTranscriptionHandler(data map[string]interface{}, state map[string]interface{}) ([]eventsourcing.Event, error) {
	sessionID, ok := data["SessionID"].(string)
	if !ok {
		sessionID = fmt.Sprintf("session-%d", time.Now().UnixNano())
	}

	event := &eventsourcing.GenericEvent{
		EventType: "TranscriptionStarted",
		Data: map[string]interface{}{
			"SessionID":  sessionID,
			"Timestamp":  time.Now().Format(time.RFC3339),
			"DeviceInfo": data["DeviceInfo"],
		},
	}
	return []eventsourcing.Event{event}, nil
}

// StopTranscriptionHandler handles the end of a transcription session
func (tm *TranscriptionManager) StopTranscriptionHandler(data map[string]interface{}, state map[string]interface{}) ([]eventsourcing.Event, error) {
	sessionID, ok := data["SessionID"].(string)
	if !ok {
		return nil, fmt.Errorf("missing SessionID")
	}
	transcriptionText, ok := data["TranscriptionText"].(string)
	if !ok {
		return nil, fmt.Errorf("missing TranscriptionText")
	}

	event := &eventsourcing.GenericEvent{
		EventType: "TranscriptionStopped",
		Data: map[string]interface{}{
			"SessionID":         sessionID,
			"Timestamp":         time.Now().Format(time.RFC3339),
			"DurationSecs":      data["DurationSecs"],
			"SampleCount":       data["SampleCount"],
			"TranscriptionText": transcriptionText,
		},
	}
	return []eventsourcing.Event{event}, nil
}
