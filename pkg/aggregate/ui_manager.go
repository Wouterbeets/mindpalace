package aggregate

import (
	"encoding/json"
	"sync"

	"mindpalace/pkg/eventsourcing"
	"mindpalace/pkg/ui3d"
)

// ThreeDUIManagerAggregate manages 3D UI objects, subscribing to domain events and emitting 3D UI events
type ThreeDUIManagerAggregate struct {
	Objects   map[string]*ThreeDObject
	Mu        sync.RWMutex
	eventBus  eventsourcing.EventBus
	layoutMgr *ui3d.LayoutManager
}

// ThreeDObject represents a 3D object in the UI
type ThreeDObject struct {
	ID         string
	MeshType   string
	Position   []float64
	Color      []float64
	Zone       string
	Label      string
	Properties map[string]interface{}
}

// NewThreeDUIManagerAggregate creates a new 3D UI manager aggregate
func NewThreeDUIManagerAggregate() *ThreeDUIManagerAggregate {
	return &ThreeDUIManagerAggregate{
		Objects: make(map[string]*ThreeDObject),
		layoutMgr: &ui3d.LayoutManager{
			Type:    "grid",
			Spacing: 5.0,
			Zone:    "default",
			Counter: 0,
			Zones: map[string][]float64{
				"task":     {0, 0, 20},
				"note":     {-20, 0, 0},
				"calendar": {20, 0, 0},
				"default":  {0, 5, 0},
			},
		},
	}
}

// SetEventBus sets the event bus for publishing 3D UI events
func (a *ThreeDUIManagerAggregate) SetEventBus(eb eventsourcing.EventBus) {
	a.eventBus = eb
}

// ID returns the aggregate identifier
func (a *ThreeDUIManagerAggregate) ID() string {
	return "ui_manager"
}

// ApplyEvent applies 3D UI events to update state
func (a *ThreeDUIManagerAggregate) ApplyEvent(event eventsourcing.Event) error {
	a.Mu.Lock()
	defer a.Mu.Unlock()

	switch e := event.(type) {
	case *eventsourcing.Create3DObjectEvent:
		a.Objects[e.ObjectID] = &ThreeDObject{
			ID:         e.ObjectID,
			MeshType:   e.MeshType,
			Position:   e.Position,
			Color:      e.Color,
			Zone:       e.Zone,
			Label:      e.Label,
			Properties: e.Extra,
		}
	case *eventsourcing.Update3DObjectEvent:
		if obj, exists := a.Objects[e.ObjectID]; exists {
			for k, v := range e.Properties {
				switch k {
				case "position":
					if pos, ok := v.([]float64); ok {
						obj.Position = pos
					}
				case "color":
					if col, ok := v.([]float64); ok {
						obj.Color = col
					}
				case "label":
					if lbl, ok := v.(string); ok {
						obj.Label = lbl
					}
				default:
					if obj.Properties == nil {
						obj.Properties = make(map[string]interface{})
					}
					obj.Properties[k] = v
				}
			}
		}
	case *eventsourcing.Delete3DObjectEvent:
		delete(a.Objects, e.ObjectID)
	case *eventsourcing.Position3DObjectEvent:
		if obj, exists := a.Objects[e.ObjectID]; exists {
			obj.Position = e.Position
		}
	default:
		// Handle domain events to emit 3D UI events
		a.handleDomainEvent(event)
	}
	return nil
}

// handleDomainEvent processes domain events and updates 3D UI state
func (a *ThreeDUIManagerAggregate) handleDomainEvent(event eventsourcing.Event) {
	data, err := json.Marshal(event)
	if err != nil {
		return
	}
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return
	}

	switch event.Type() {
	case "taskmanager_TaskCreated":
		taskID, _ := raw["task_id"].(string)
		title, _ := raw["title"].(string)
		pos := a.layoutMgr.NextPosition()
		zone := "task"
		if zoneOffset, ok := a.layoutMgr.Zones[zone]; ok {
			pos[0] += zoneOffset[0]
			pos[1] += zoneOffset[1]
			pos[2] += zoneOffset[2]
		}
		color := []float64{0, 1, 0, 1} // Green for tasks
		a.Objects[taskID] = &ThreeDObject{
			ID:       taskID,
			MeshType: "box",
			Position: pos,
			Color:    color,
			Zone:     zone,
			Label:    title,
		}
	case "taskmanager_TaskUpdated":
		taskID, _ := raw["task_id"].(string)
		title, _ := raw["title"].(string)
		if obj, exists := a.Objects[taskID]; exists {
			obj.Label = title
		}
	case "taskmanager_TaskDeleted":
		taskID, _ := raw["task_id"].(string)
		delete(a.Objects, taskID)
		// Add cases for note, calendar, etc.
	}
}

// EmitDelta emits DeltaEnvelope for 3D UI events and domain events
func (a *ThreeDUIManagerAggregate) EmitDelta(event eventsourcing.Event) *eventsourcing.DeltaEnvelope {
	a.Mu.RLock()
	defer a.Mu.RUnlock()

	switch e := event.(type) {
	case *eventsourcing.Create3DObjectEvent:
		builder := ui3d.NewDeltaBuilder(ui3d.DefaultTheme())
		labelPos := ui3d.CalculateLabelPosition(e.Position, e.MeshType)
		builder.CreateBox(e.ObjectID, e.Position).WithExtra(map[string]interface{}{
			"event_type": "create_3d_object",
			"material_override": map[string]interface{}{
				"albedo_color": e.Color,
			},
		})
		builder.CreateLabel(e.ObjectID+"_label", e.Label, labelPos).WithExtra(map[string]interface{}{
			"event_type": "create_3d_object",
		})
		return &eventsourcing.DeltaEnvelope{
			Type:      "delta",
			Aggregate: "ui_manager",
			EventID:   eventsourcing.ISOTimestamp(),
			Timestamp: eventsourcing.ISOTimestamp(),
			Actions:   builder.Build(),
		}
	case *eventsourcing.Update3DObjectEvent:
		builder := ui3d.NewDeltaBuilder(ui3d.DefaultTheme())
		builder.Update(e.ObjectID, e.Properties)
		return &eventsourcing.DeltaEnvelope{
			Type:      "delta",
			Aggregate: "ui_manager",
			EventID:   eventsourcing.ISOTimestamp(),
			Timestamp: eventsourcing.ISOTimestamp(),
			Actions:   builder.Build(),
		}
	case *eventsourcing.Delete3DObjectEvent:
		builder := ui3d.NewDeltaBuilder(ui3d.DefaultTheme())
		builder.Delete(e.ObjectID).Delete(e.ObjectID + "_label")
		return &eventsourcing.DeltaEnvelope{
			Type:      "delta",
			Aggregate: "ui_manager",
			EventID:   eventsourcing.ISOTimestamp(),
			Timestamp: eventsourcing.ISOTimestamp(),
			Actions:   builder.Build(),
		}
	case *eventsourcing.Position3DObjectEvent:
		builder := ui3d.NewDeltaBuilder(ui3d.DefaultTheme())
		builder.Update(e.ObjectID, map[string]interface{}{
			"position": e.Position,
		})
		return &eventsourcing.DeltaEnvelope{
			Type:      "delta",
			Aggregate: "ui_manager",
			EventID:   eventsourcing.ISOTimestamp(),
			Timestamp: eventsourcing.ISOTimestamp(),
			Actions:   builder.Build(),
		}
	default:
		// Handle domain events
		data, err := json.Marshal(event)
		if err != nil {
			return nil
		}
		var raw map[string]interface{}
		if err := json.Unmarshal(data, &raw); err != nil {
			return nil
		}

		switch event.Type() {
		case "taskmanager_TaskCreated":
			taskID, _ := raw["task_id"].(string)
			if obj, exists := a.Objects[taskID]; exists {
				builder := ui3d.NewDeltaBuilder(ui3d.DefaultTheme())
				labelPos := ui3d.CalculateLabelPosition(obj.Position, obj.MeshType)
				builder.CreateBox(obj.ID, obj.Position).WithExtra(map[string]interface{}{
					"event_type": "create_3d_object",
					"material_override": map[string]interface{}{
						"albedo_color": obj.Color,
					},
				})
				builder.CreateLabel(obj.ID+"_label", obj.Label, labelPos).WithExtra(map[string]interface{}{
					"event_type": "create_3d_object",
				})
				return &eventsourcing.DeltaEnvelope{
					Type:      "delta",
					Aggregate: "ui_manager",
					EventID:   eventsourcing.ISOTimestamp(),
					Timestamp: eventsourcing.ISOTimestamp(),
					Actions:   builder.Build(),
				}
			}

		case "taskmanager_TaskDeleted":
			taskID, _ := raw["task_id"].(string)
			builder := ui3d.NewDeltaBuilder(ui3d.DefaultTheme())
			builder.Delete(taskID).Delete(taskID + "_label")
			return &eventsourcing.DeltaEnvelope{
				Type:      "delta",
				Aggregate: "ui_manager",
				EventID:   eventsourcing.ISOTimestamp(),
				Timestamp: eventsourcing.ISOTimestamp(),
				Actions:   builder.Build(),
			}
		}
	}
	return nil
}

func (a *ThreeDUIManagerAggregate) Clone() eventsourcing.Aggregate {
	a.Mu.RLock()
	defer a.Mu.RUnlock()
	newAgg := NewThreeDUIManagerAggregate()
	for id, obj := range a.Objects {
		newObj := *obj
		newAgg.Objects[id] = &newObj
	}
	return newAgg
}
