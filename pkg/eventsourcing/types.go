package eventsourcing

import (
	"encoding/json"
	"fmt"
)

type PluginType string

// SubmitEvent is a function that plugins can use to submit events asynchronously.
var SubmitEvent func(Event)

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

// GlobalAggregate is a single shared aggregate for all plugins
type GlobalAggregate struct {
	State map[string]interface{}
}

func (a *GlobalAggregate) ID() string {
	return "global"
}

func (a *GlobalAggregate) ApplyEvent(event Event) error {
	genericEvent, ok := event.(*GenericEvent)
	if !ok {
		return fmt.Errorf("event is not a GenericEvent")
	}
	key := genericEvent.EventType
	if current, exists := a.State[key]; exists {
		if list, ok := current.([]interface{}); ok {
			a.State[key] = append(list, genericEvent.Data)
		} else {
			a.State[key] = []interface{}{current, genericEvent.Data}
		}
	} else {
		a.State[key] = []interface{}{genericEvent.Data}
	}
	return nil
}

func (a *GlobalAggregate) GetState() map[string]interface{} {
	return a.State
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
