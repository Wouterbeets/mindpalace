package main

import (
	"fmt"
	"mindpalace/pkg/eventsourcing"
	"time"
)

type TranscriptionPlugin struct{}

func (p *TranscriptionPlugin) GetCommands() map[string]eventsourcing.CommandHandler {
	return map[string]eventsourcing.CommandHandler{
		"StartTranscription": StartTranscriptionHandler,
		"StopTranscription":  StopTranscriptionHandler,
	}
}

func (p *TranscriptionPlugin) GetSchemas() map[string]map[string]interface{} {
	return map[string]map[string]interface{}{}
}

func StartTranscriptionHandler(data map[string]interface{}, state map[string]interface{}) ([]eventsourcing.Event, error) {
	// Generate a unique session ID (could be passed from UI or generated here)
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

func StopTranscriptionHandler(data map[string]interface{}, state map[string]interface{}) ([]eventsourcing.Event, error) {
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
			// "TotalSegments": data["TotalSegments"], // Optional; removed here
		},
	}
	return []eventsourcing.Event{event}, nil
}

func NewPlugin() eventsourcing.Plugin {
	return &TranscriptionPlugin{}
}

func (p *TranscriptionPlugin) GetType() eventsourcing.PluginType {
	return eventsourcing.SystemPlugin
}

func (p *TranscriptionPlugin) GetEventHandlers() map[string]eventsourcing.EventHandler {
	return nil // No event handlers needed
}
