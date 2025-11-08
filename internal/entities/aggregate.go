package entities

import (
    "encoding/json"
    "fmt"
    "sync"

    "mindpalace/pkg/eventsourcing"
    "mindpalace/pkg/logging"
    "mindpalace/pkg/modellib"
    "mindpalace/pkg/ui3d"
)

// EntityPlacedEvent records the placement of a model entity into the world.
type EntityPlacedEvent struct {
    EventType string    `json:"event_type"`
    EntityID  string    `json:"entity_id"`
    ModelID   string    `json:"model_id"`
    Position  []float64 `json:"position"`
    Rotation  []float64 `json:"rotation,omitempty"`
    Scale     []float64 `json:"scale,omitempty"`
    Label     string    `json:"label,omitempty"`
}

func (e *EntityPlacedEvent) Type() string { return "entities_EntityPlaced" }
func (e *EntityPlacedEvent) Marshal() ([]byte, error) {
    e.EventType = e.Type()
    return json.Marshal(e)
}
func (e *EntityPlacedEvent) Unmarshal(data []byte) error { return json.Unmarshal(data, e) }

// entityState holds runtime data for one placed entity.
type entityState struct {
	ModelID  string
	ModelRes string
	Position []float64
	Rotation []float64
	Scale    []float64
	Label    string
}

// Aggregate manages placed entities and emits deltas for the UI.
type Aggregate struct {
	mu           sync.RWMutex
	entities     map[string]*entityState
	catalog      *modellib.Catalog
}

func init() {
	eventsourcing.RegisterEvent("entities_EntityPlaced", func() eventsourcing.Event { return &EntityPlacedEvent{} })
}

func NewAggregate() *Aggregate {
	return &Aggregate{entities: make(map[string]*entityState)}
}

func (a *Aggregate) SetModelLibrary(c *modellib.Catalog) { a.catalog = c }

func (a *Aggregate) ID() string { return "entities" }

func (a *Aggregate) Reset() { // implements ResettableAggregate
	a.mu.Lock()
	defer a.mu.Unlock()
	a.entities = make(map[string]*entityState)
}

func (a *Aggregate) ApplyEvent(event eventsourcing.Event) error {
	switch e := event.(type) {
	case *EntityPlacedEvent:
		if e.EntityID == "" || e.ModelID == "" {
			return fmt.Errorf("invalid EntityPlacedEvent: missing id or model")
		}
		var resPath string
		if a.catalog != nil {
			if p, err := a.catalog.EnsureModel(e.ModelID); err == nil {
				resPath = p
			} else {
				logging.Info("ENTITIES: unknown model id '%s': %v", e.ModelID, err)
			}
		}
		a.mu.Lock()
		a.entities[e.EntityID] = &entityState{
			ModelID:  e.ModelID,
			ModelRes: resPath,
			Position: append([]float64{}, e.Position...),
			Rotation: append([]float64{}, e.Rotation...),
			Scale:    append([]float64{}, e.Scale...),
			Label:    e.Label,
		}
		a.mu.Unlock()
		return nil
	default:
		return nil
	}
}

func (a *Aggregate) EmitDelta(event eventsourcing.Event) *eventsourcing.DeltaEnvelope {
	theme := ui3d.DefaultTheme()
	switch e := event.(type) {
	case *EntityPlacedEvent:
		// Build create action for the new entity
		pos := e.Position
		if len(pos) != 3 {
			pos = []float64{0, 0, 0}
		}
    scale := e.Scale
    var modelPath string
    if a.catalog != nil {
        if p, err := a.catalog.EnsureModel(e.ModelID); err == nil {
            modelPath = p
            if len(scale) != 3 {
                if entry, ok := a.catalog.Entry(e.ModelID); ok {
                    sf := entry.RecommendedScale
                    if sf <= 0 {
                        sf = 1
                    }
                    scale = []float64{sf, sf, sf}
                }
            }
        }
    }
    if len(scale) != 3 {
        scale = []float64{1, 1, 1}
    }
		builder := ui3d.NewDeltaBuilder(theme)
		if modelPath != "" {
			builder.CreateModel(e.EntityID, modelPath, pos, scale)
		} else {
			// Fallback: create a box if no model catalog
			builder.CreateBox(e.EntityID, pos)
		}
		return &eventsourcing.DeltaEnvelope{
			Type:       "delta",
			Aggregate:  a.ID(),
			EventID:    eventsourcing.ISOTimestamp(),
			Timestamp:  eventsourcing.ISOTimestamp(),
			SequenceID: eventsourcing.NextSequenceID(),
			Actions:    builder.Build(),
		}
	}
	return nil
}
