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

func init() {
	RegisterEvent("UserRequestReceived", func() Event { return &UserRequestReceivedEvent{} })
	RegisterEvent("ToolCallCompleted", func() Event { return &ToolCallCompleted{} })
	RegisterEvent("InitiatePluginCreation", func() Event { return &InitiatePluginCreationEvent{} })
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
		logging.Debug("Event type %s not found in registry, falling back to GenericEvent", raw.EventType)
		// Fallback to GenericEvent if type is not registered
		event := &GenericEvent{}
		if err := json.Unmarshal(data, event); err != nil {
			logging.Debug("Failed to unmarshal into GenericEvent: %v", err)
			return nil, fmt.Errorf("failed to unmarshal into GenericEvent: %v", err)
		}
		logging.Debug("Successfully unmarshaled into GenericEvent")
		return event, nil
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

type PluginType string

// Global event bus instance
var globalEventBus EventBus

// SubmitEvent is a function that plugins can use to submit events asynchronously.
var SubmitEvent = func(event Event) {
	if globalEventBus != nil {
		globalEventBus.Publish(event)
	}
}

// SubmitStreamingEvent is a function for sending streaming events that won't be persisted
var SubmitStreamingEvent func(eventType string, data map[string]interface{})

// SetGlobalEventBus sets the global event bus instance
func SetGlobalEventBus(eb EventBus) {
	globalEventBus = eb
}

const (
	SystemPlugin PluginType = "system" // Plugins for internal system operations
	LLMPlugin    PluginType = "llm"    // Plugins usable by the LLM
)

type EventStore interface {
	Append(events ...Event) error
	GetEvents() []Event
	Load() error
}

// Event defines the interface for all events in the system
type Event interface {
	Type() string
	Marshal() ([]byte, error)
	Unmarshal(data []byte) error
}

// GenericEvent is a flexible event type with a type string and generic data
type GenericEvent struct {
	EventType string                 `json:"event_type"`
	Data      map[string]interface{} `json:"data"`
}

func (e *GenericEvent) Type() string {
	return e.EventType
}

func (e *GenericEvent) Marshal() ([]byte, error) {
	return json.Marshal(e)
}

func (e *GenericEvent) Unmarshal(data []byte) error {
	return json.Unmarshal(data, e)
}

// DecodeData is a helper to decode GenericEvent data into a struct
func (e *GenericEvent) DecodeData(v interface{}) error {
	// Use the standard library JSON encoding to decode the data
	// This works as a simple alternative to mapstructure
	jsonData, err := json.Marshal(e.Data)
	if err != nil {
		return err
	}
	return json.Unmarshal(jsonData, v)
}

type ToolCallCompleted struct {
	RequestID  string                 `json:"request_id"`
	ToolCallID string                 `json:"tool_call_id"`
	Function   string                 `json:"function"`
	Result     map[string]interface{} `json:"result"`
	EventType  string                 `json:"event_type"`
}

func (e *ToolCallCompleted) Type() string {
	return "ToolCallCompleted"
}

func (e *ToolCallCompleted) Marshal() ([]byte, error) {
	e.EventType = e.Type()
	return json.Marshal(e)
}

func (e *ToolCallCompleted) Unmarshal(data []byte) error {
	return json.Unmarshal(data, e)
}

// UserRequestReceivedEvent is a strongly typed event for when a user request is received
type UserRequestReceivedEvent struct {
	EventType   string `json:"event_type"`
	RequestID   string `json:"request_id"`
	RequestText string `json:"request_text"`
	Timestamp   string `json:"timestamp"`
}

func (e *UserRequestReceivedEvent) Type() string {
	return "UserRequestReceived"
}

func (e *UserRequestReceivedEvent) Marshal() ([]byte, error) {
	e.EventType = e.Type()
	return json.Marshal(e)
}

func (e *UserRequestReceivedEvent) Unmarshal(data []byte) error {
	return json.Unmarshal(data, e)
}

// Aggregate defines the interface for aggregates that process events
type Aggregate interface {
	ID() string
	ApplyEvent(event Event) error
	GetState() map[string]interface{}
	GetAllCommands() map[string]CommandHandler
}

// CommandProvider is an interface for objects that can provide access to all registered commands
type CommandProvider interface {
	GetAllCommands() map[string]CommandHandler
}

// CommandHandler defines the signature for command handling functions, now with access to state
type CommandHandler func(data map[string]interface{}, state map[string]interface{}) ([]Event, error)

// EventHandler defines a function that reacts to an event and optionally produces new events
type EventHandler func(event Event, state map[string]interface{}, commands map[string]CommandHandler) ([]Event, error)

// Plugin defines the interface for plugins in the system
type Plugin interface {
	Commands() map[string]CommandHandler
	Schemas() map[string]map[string]interface{}
	Type() PluginType
	EventHandlers() map[string]EventHandler // Add this method
	Name() string
	GetCustomUI(agg Aggregate) fyne.CanvasObject
	Aggregate() Aggregate
}

// DefaultEventHandler is a no-op handler for plugins that don't handle events
func DefaultEventHandler(event Event, state map[string]interface{}, commands map[string]CommandHandler) ([]Event, error) {
	return nil, nil
}

// GenericEvent ApplyEvent implementation (unchanged)
func (e *GenericEvent) ApplyEvent(state map[string]interface{}) error {
	key := e.EventType
	if current, exists := state[key]; exists {
		if list, ok := current.([]interface{}); ok {
			state[key] = append(list, e.Data)
		} else {
			state[key] = []interface{}{current, e.Data}
		}
	} else {
		state[key] = []interface{}{e.Data}
	}
	return nil
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
