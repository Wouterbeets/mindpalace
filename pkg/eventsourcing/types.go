// Package eventsourcing provides the core interfaces and types for event sourcing in MindPalace.
package eventsourcing

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

var eventRegistry = make(map[string]func() Event)

// RegisterEvent adds an event type and its creator function to the registry.
func RegisterEvent(eventType string, creator func() Event) {
	eventRegistry[eventType] = creator
}

// UnmarshalEvent unmarshals JSON data into the correct event type.
func UnmarshalEvent(data []byte) (Event, error) {
	// logging.Debug("Starting UnmarshalEvent with data length: %d", len(data))

	// First, extract the EventType
	var raw struct {
		EventType string `json:"event_type"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		// logging.Debug("Error reading event type: %v", err)
		return nil, fmt.Errorf("failed to read event type: %v", err)
	}
	// logging.Debug("Extracted event type: %s", raw.EventType)

	// Look up the creator function in the registry
	creator, exists := eventRegistry[raw.EventType]
	if !exists {
		return nil, fmt.Errorf("unable to create event, event not resistered %s", raw.EventType)
	}

	// logging.Debug("Found creator for event type %s in registry", raw.EventType)
	// Create the concrete event and unmarshal into it
	event := creator()
	if err := json.Unmarshal(data, event); err != nil {
		// logging.Debug("Failed to unmarshal into %s: %v", raw.EventType, err)
		return nil, fmt.Errorf("failed to unmarshal into %s: %v", raw.EventType, err)
	}
	// logging.Debug("Successfully unmarshaled data into event type: %s", raw.EventType)

	return event, nil
}

// Global event bus instance
var globalEventBus EventBus

// SetGlobalEventBus sets the global event bus instance
func SetGlobalEventBus(eb EventBus) {
	globalEventBus = eb
}

// GetGlobalEventBus returns the process-wide event bus if one has been registered.
func GetGlobalEventBus() EventBus {
	return globalEventBus
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

type CommandData struct {
	Name string
	Data interface{}
}

// Plugin defines the interface for plugins in the system
type Plugin interface {
	Commands() map[string]CommandHandler
	Schemas() map[string]CommandInput
	Type() PluginType
	Name() string
	Aggregate() Aggregate
	SystemPrompt() string
	AgentModel() string
	Description() string
}

// Aggregate defines the interface for aggregates that process events
type Aggregate interface {
	ID() string
	ApplyEvent(event Event) error
	EmitDelta(event Event) *DeltaEnvelope
}

// ResettableAggregate optionally allows aggregates to clear their in-memory state
// prior to a replay. Aggregates that maintain derived caches should implement Reset.
type ResettableAggregate interface {
	Aggregate
	Reset()
}

// Counter for generating unique IDs
var idCounter uint64 = 0
var idMutex sync.Mutex

// Global sequence counter for DeltaEnvelope sequenceIDs
var sequenceCounter int = 0
var sequenceMutex sync.Mutex

// GenerateUniqueID generates a unique ID for entities like tasks
func GenerateUniqueID() uint64 {
	idMutex.Lock()
	defer idMutex.Unlock()
	idCounter++
	return idCounter
}

// NextSequenceID generates the next sequence ID for DeltaEnvelope
func NextSequenceID() int {
	sequenceMutex.Lock()
	defer sequenceMutex.Unlock()
	sequenceCounter++
	return sequenceCounter
}

// ISOTimestamp returns the current time as an ISO 8601 formatted string
func ISOTimestamp() string {
	return time.Now().UTC().Format(time.RFC3339)
}

type BaseEvent struct {
}

func (e *BaseEvent) Marshal() ([]byte, error) {
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

// DeltaAction represents a single 3D mutation (declarative, idempotent).
type DeltaAction struct {
	Type       string                 `json:"type"`                 // "create", "update", "animate", "delete"
	NodeID     string                 `json:"node_id,omitempty"`    // Unique ID (e.g., "task_123")
	NodeType   string                 `json:"node_type,omitempty"`  // Godot node (e.g., "MeshInstance3D")
	Properties map[string]interface{} `json:"properties,omitempty"` // Key-value props (e.g., {"position": [0,1,0]})
	Animation  *AnimationSpec         `json:"animation,omitempty"`  // For "animate" type
	Metadata   map[string]interface{} `json:"metadata,omitempty"`   // Aggregate-specific (e.g., {"task_id": "123"})
	ModelPath  string                 `json:"model_path,omitempty"` // Path to GLTF model for 3D objects
}

// AnimationSpec for tween-like effects.
type AnimationSpec struct {
	Property string      `json:"property"`       // e.g., "position"
	To       interface{} `json:"to"`             // Target value (e.g., [5,0,0])
	Duration float64     `json:"duration"`       // Seconds
	Ease     string      `json:"ease,omitempty"` // Optional: "linear", "ease_in", etc.
}

// DeltaEnvelope wraps actions for broadcast (includes context for Godot parsing).
type DeltaEnvelope struct {
	Type         string                 `json:"type"`                    // "delta" or "signal"
	Aggregate    string                 `json:"aggregate"`               // e.g., "taskmanager"
	EventID      string                 `json:"event_id"`                // For ordering/resync
	Timestamp    string                 `json:"timestamp"`               // ISO for sorting
	IsFullState  bool                   `json:"is_full_state,omitempty"` // True for full snapshots
	StateSummary map[string]interface{} `json:"state_summary,omitempty"` // Current state summary
	SequenceID   int                    `json:"sequence_id"`             // For ACK-based flow control
	Actions      []DeltaAction          `json:"actions"`
	Components   []interface{}          `json:"components,omitempty"` // Stateful interactive components (serialized)
}

// EventProcessorInterface interface
type EventProcessorInterface interface {
	RegisterCommand(name string, handler CommandHandler)
	ExecuteCommand(name string, data interface{}) error
}

// ThreeDUIBroadcaster allows aggregates to emit 3D deltas on events.
// Implement if the aggregate wants 3D UI (e.g., tasks as cubes).
type ThreeDUIBroadcaster interface {
	EmitDelta(event Event) *DeltaEnvelope // Returns delta envelope for this event, or nil to skip.
}

// EventScore captures a named/labelled score for a specific event.
type EventScore struct {
	Name  string  `json:"name"`
	Label string  `json:"label,omitempty"`
	Value float64 `json:"value"`
}

// EventScorable can be implemented by aggregates that want to accept
// scoring results associated with historical events.
type EventScorable interface {
	RecordEventScore(eventID string, score EventScore)
}
