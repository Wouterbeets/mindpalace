// Package eventsourcing provides the core interfaces and types for event sourcing in MindPalace.
package eventsourcing

import (
	"encoding/json"
	"fmt"
	"mindpalace/pkg/logging"
	"sync/atomic"
	"time"

	"fyne.io/fyne/v2"
)

var eventRegistry = make(map[string]func() Event)

// RegisterEvent adds an event type and its creator function to the registry.
func RegisterEvent(eventType string, creator func() Event) {
	eventRegistry[eventType] = creator
}

// UnmarshalEvent unmarshals JSON data into the correct event type.
func UnmarshalEvent(data []byte) (Event, error) {
	logging.Debug("Starting UnmarshalEvent with data length: %d", len(data))

	// First, extract the EventType
	var raw struct {
		EventType string `json:"event_type"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		logging.Debug("Error reading event type: %v", err)
		return nil, fmt.Errorf("failed to read event type: %v", err)
	}
	logging.Debug("Extracted event type: %s", raw.EventType)

	// Look up the creator function in the registry
	creator, exists := eventRegistry[raw.EventType]
	if !exists {
		return nil, fmt.Errorf("unable to create event, event not resistered %s", raw.EventType)
	}

	logging.Debug("Found creator for event type %s in registry", raw.EventType)
	// Create the concrete event and unmarshal into it
	event := creator()
	if err := json.Unmarshal(data, event); err != nil {
		logging.Debug("Failed to unmarshal into %s: %v", raw.EventType, err)
		return nil, fmt.Errorf("failed to unmarshal into %s: %v", raw.EventType, err)
	}
	logging.Debug("Successfully unmarshaled data into event type: %s", raw.EventType)

	return event, nil
}

// Global event bus instance
var globalEventBus EventBus

// SubmitStreamingEvent is a function for sending streaming events that won't be persisted
var SubmitStreamingEvent func(eventType string, data map[string]interface{})

// SetGlobalEventBus sets the global event bus instance
func SetGlobalEventBus(eb EventBus) {
	globalEventBus = eb
}

type EventStore interface {
	Append(events ...Event) error
	GetEvents() []Event
	Load() error
}

// Event defines the interface for all events in the system
type Event interface {
	Type() string
	Unmarshal(data []byte) error
	Marshal() ([]byte, error)
}

const (
	SystemPlugin PluginType = "system" // Plugins for internal system operations
	LLMPlugin    PluginType = "llm"    // Plugins usable by the LLM
)

type PluginType string

// Plugin defines the interface for plugins in the system
type Plugin interface {
	Commands() map[string]CommandHandler
	Schemas() map[string]CommandInput
	Type() PluginType
	Name() string
	Aggregate() Aggregate
	SystemPrompt() string // New: Dynamic system prompt
	AgentModel() string   // New: Preferred LLM model
}

// Aggregate defines the interface for aggregates that process events
type Aggregate interface {
	ID() string
	ApplyEvent(event Event) error
	GetCustomUI() fyne.CanvasObject
	GetWebUI() string // New: Returns HTMX-enabled HTML for web UI
}

// Counter for generating unique IDs
var idCounter uint64 = 0

// GenerateUniqueID generates a unique ID for entities like tasks
func GenerateUniqueID() uint64 {
	return atomic.AddUint64(&idCounter, 1)
}

// ISOTimestamp returns the current time as an ISO 8601 formatted string
func ISOTimestamp() string {
	return time.Now().UTC().Format(time.RFC3339)
}

type BaseEvent struct {
}

func (e *BaseEvent) Marshal() ([]byte, error) {
	logging.Debug("calling base event marshal")
	return json.Marshal(e)
}
func (e *BaseEvent) Unmarshal(data []byte) error { return json.Unmarshal(data, e) }

type InitiatePluginCreationEvent struct {
	EventType   string `json:"event_type"`
	RequestID   string `json:"request_id"`
	PluginName  string `json:"plugin_name"`
	Description string `json:"description"`
	Goal        string `json:"goal"`
	Result      string `json:"result"`
}

func (e *InitiatePluginCreationEvent) Type() string { return "InitiatePluginCreation" }
func (e *InitiatePluginCreationEvent) Marshal() ([]byte, error) {
	e.EventType = e.Type()
	return json.Marshal(e)
}
func (e *InitiatePluginCreationEvent) Unmarshal(data []byte) error { return json.Unmarshal(data, e) }
