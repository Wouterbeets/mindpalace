package aggregate

import (
	"encoding/json"
	"fmt"
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/widget"
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

// Broadcast3DDelta emits DeltaActions for 3D UI events and domain events
func (a *ThreeDUIManagerAggregate) Broadcast3DDelta(event eventsourcing.Event) []eventsourcing.DeltaAction {
	a.Mu.RLock()
	defer a.Mu.RUnlock()

	switch e := event.(type) {
	case *eventsourcing.Create3DObjectEvent:
		obj := ui3d.StandardObject{
			ID:       e.ObjectID,
			MeshType: e.MeshType,
			Position: e.Position,
			Label:    &ui3d.LabelConfig{Text: e.Label},
			Theme:    ui3d.DefaultTheme(),
			Extra: map[string]interface{}{
				"event_type": "create_3d_object",
				"material_override": map[string]interface{}{
					"albedo_color": e.Color,
				},
			},
		}
		return ui3d.CreateStandardObject(obj)
	case *eventsourcing.Update3DObjectEvent:
		actions := []eventsourcing.DeltaAction{}
		for k, v := range e.Properties {
			action := eventsourcing.DeltaAction{
				Type:     "update",
				NodeID:   e.ObjectID,
				NodeType: "MeshInstance3D",
				Properties: map[string]interface{}{
					k: v,
				},
			}
			actions = append(actions, action)
		}
		return actions
	case *eventsourcing.Delete3DObjectEvent:
		return []eventsourcing.DeltaAction{
			{Type: "delete", NodeID: e.ObjectID},
			{Type: "delete", NodeID: e.ObjectID + "_label"},
		}
	case *eventsourcing.Position3DObjectEvent:
		return []eventsourcing.DeltaAction{
			{
				Type:     "update",
				NodeID:   e.ObjectID,
				NodeType: "MeshInstance3D",
				Properties: map[string]interface{}{
					"position": e.Position,
				},
			},
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
				obj2 := ui3d.StandardObject{
					ID:       obj.ID,
					MeshType: obj.MeshType,
					Position: obj.Position,
					Label:    &ui3d.LabelConfig{Text: obj.Label},
					Theme:    ui3d.DefaultTheme(),
					Extra: map[string]interface{}{
						"event_type": "create_3d_object",
						"material_override": map[string]interface{}{
							"albedo_color": obj.Color,
						},
					},
				}
				return ui3d.CreateStandardObject(obj2)
			}

		case "taskmanager_TaskDeleted":
			taskID, _ := raw["task_id"].(string)
			return []eventsourcing.DeltaAction{
				{Type: "delete", NodeID: taskID},
				{Type: "delete", NodeID: taskID + "_label"},
			}
		}
	}
	return nil
}

// GetCustomUI returns a UI for the 3D manager (placeholder)
func (a *ThreeDUIManagerAggregate) GetCustomUI() fyne.CanvasObject {
	// Placeholder: return a label with object count
	a.Mu.RLock()
	count := len(a.Objects)
	a.Mu.RUnlock()
	return widget.NewLabel(fmt.Sprintf("3D Objects: %d", count))
}

// Clone returns a copy for replaying events
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
