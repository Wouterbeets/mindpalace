package eventsourcing

import (
	"encoding/json"
)

// Create3DObjectEvent represents the creation of a 3D object in the UI
type Create3DObjectEvent struct {
	EventType string                 `json:"event_type"`
	ObjectID  string                 `json:"object_id"`
	MeshType  string                 `json:"mesh_type"`
	Position  []float64              `json:"position"`
	Color     []float64              `json:"color,omitempty"`
	Zone      string                 `json:"zone"`
	Label     string                 `json:"label,omitempty"`
	Extra     map[string]interface{} `json:"extra,omitempty"`
}

func (e *Create3DObjectEvent) Type() string {
	return "ui_Create3DObject"
}

func (e *Create3DObjectEvent) Marshal() ([]byte, error) {
	e.EventType = e.Type()
	return json.Marshal(e)
}

func (e *Create3DObjectEvent) Unmarshal(data []byte) error {
	return json.Unmarshal(data, e)
}

// Update3DObjectEvent represents an update to a 3D object's properties
type Update3DObjectEvent struct {
	EventType  string                 `json:"event_type"`
	ObjectID   string                 `json:"object_id"`
	Properties map[string]interface{} `json:"properties"`
}

func (e *Update3DObjectEvent) Type() string {
	return "ui_Update3DObject"
}

func (e *Update3DObjectEvent) Marshal() ([]byte, error) {
	e.EventType = e.Type()
	return json.Marshal(e)
}

func (e *Update3DObjectEvent) Unmarshal(data []byte) error {
	return json.Unmarshal(data, e)
}

// Delete3DObjectEvent represents the deletion of a 3D object
type Delete3DObjectEvent struct {
	EventType string `json:"event_type"`
	ObjectID  string `json:"object_id"`
}

func (e *Delete3DObjectEvent) Type() string {
	return "ui_Delete3DObject"
}

func (e *Delete3DObjectEvent) Marshal() ([]byte, error) {
	e.EventType = e.Type()
	return json.Marshal(e)
}

func (e *Delete3DObjectEvent) Unmarshal(data []byte) error {
	return json.Unmarshal(data, e)
}

// Position3DObjectEvent represents a position update for a 3D object (e.g., from dragging)
type Position3DObjectEvent struct {
	EventType string    `json:"event_type"`
	ObjectID  string    `json:"object_id"`
	Position  []float64 `json:"position"`
}

func (e *Position3DObjectEvent) Type() string {
	return "ui_Position3DObject"
}

func (e *Position3DObjectEvent) Marshal() ([]byte, error) {
	e.EventType = e.Type()
	return json.Marshal(e)
}

func (e *Position3DObjectEvent) Unmarshal(data []byte) error {
	return json.Unmarshal(data, e)
}
