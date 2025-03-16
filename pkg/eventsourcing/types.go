package eventsourcing

import (
	"encoding/json"
	"sync/atomic"
	"time"
)

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

// Aggregate defines the interface for aggregates that process events
type Aggregate interface {
	ID() string
	ApplyEvent(event Event) error
	GetState() map[string]interface{}
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
