package ui3d

import (
	"math"

	"mindpalace/pkg/eventsourcing"
)

// LabelConfig defines optional label settings for StandardObject
type LabelConfig struct {
	Text  string
	Color []float64 // Optional override for theme.Text
}

// DisplayInfo holds information for HUD display
type DisplayInfo struct {
	Title       string                 `json:"title"`
	Description string                 `json:"description"`
	Details     map[string]interface{} `json:"details"`
}

// StandardObject represents a standard 3D object with optional label
type StandardObject struct {
	ID          string
	MeshType    string // "box", "sphere", "cylinder", "plane", "capsule"
	Position    []float64
	Label       *LabelConfig // nil if no label
	Theme       Theme
	Extra       map[string]interface{} // scale, rotation, etc.
	DisplayInfo *DisplayInfo           // nil if no display info
}

// LayoutManager handles positioning for groups of objects
type LayoutManager struct {
	Type    string // "grid", "circle", "spiral", "random"
	Spacing float64
	Zone    string // Ties to PLUGIN_ZONES in Godot
	Counter int
	Seed    int64 // For random positioning
}

// Theme defines a color scheme for UI elements
type Theme struct {
	Primary    []float64 // RGBA
	Secondary  []float64
	Accent     []float64
	Background []float64
	Text       []float64
}

// DefaultTheme returns a standard dark theme
func DefaultTheme() Theme {
	return Theme{
		Primary:    []float64{0.2, 0.5, 0.8, 1.0}, // Blue
		Secondary:  []float64{0.5, 0.5, 0.5, 1.0}, // Gray
		Accent:     []float64{1.0, 0.8, 0.0, 1.0}, // Orange
		Background: []float64{0.1, 0.1, 0.1, 1.0}, // Dark
		Text:       []float64{1.0, 1.0, 1.0, 1.0}, // White
	}
}

// LightTheme returns a light theme
func LightTheme() Theme {
	return Theme{
		Primary:    []float64{0.0, 0.4, 0.8, 1.0}, // Blue
		Secondary:  []float64{0.7, 0.7, 0.7, 1.0}, // Light Gray
		Accent:     []float64{1.0, 0.6, 0.0, 1.0}, // Orange
		Background: []float64{0.9, 0.9, 0.9, 1.0}, // Light
		Text:       []float64{0.0, 0.0, 0.0, 1.0}, // Black
	}
}

// CreateStandardObject creates a standard object with optional label
func CreateStandardObject(obj StandardObject) []eventsourcing.DeltaAction {
	actions := []eventsourcing.DeltaAction{}
	// Create mesh action
	meshAction := createMeshAction(obj.ID, obj.MeshType, obj.Position, obj.Theme, obj.Extra)
	if obj.DisplayInfo != nil {
		if meshAction.Properties == nil {
			meshAction.Properties = make(map[string]interface{})
		}
		meshAction.Properties["display_info"] = map[string]interface{}{
			"title":       obj.DisplayInfo.Title,
			"description": obj.DisplayInfo.Description,
			"details":     obj.DisplayInfo.Details,
		}
	}
	actions = append(actions, meshAction)

	// Auto-tie label if present
	if obj.Label != nil {
		labelPos := calculateLabelPosition(obj.Position, obj.MeshType)
		labelAction := CreateLabel(obj.ID+"_label", obj.Label.Text, labelPos, obj.Theme)
		if obj.Label.Color != nil {
			labelAction.Properties["modulate"] = obj.Label.Color
		}
		// Set parent_id for proper parenting in Godot
		labelAction.Properties["parent_id"] = obj.ID
		labelAction.Properties["mesh_type"] = obj.MeshType
		if obj.DisplayInfo != nil {
			labelAction.Properties["display_info"] = map[string]interface{}{
				"title":       obj.DisplayInfo.Title,
				"description": obj.DisplayInfo.Description,
				"details":     obj.DisplayInfo.Details,
			}
		}
		actions = append(actions, labelAction)
	}
	return actions
}

// calculateLabelPosition computes label position relative to mesh
func calculateLabelPosition(basePos []float64, meshType string) []float64 {
	offsetY := 1.2 // Default for box
	switch meshType {
	case "sphere":
		offsetY = 0.8
	case "cylinder":
		offsetY = 1.5
	case "plane":
		offsetY = 0.1
	case "capsule":
		offsetY = 1.0
	}
	return []float64{basePos[0], basePos[1] + offsetY, basePos[2]}
}

// createMeshAction internal helper for mesh creation
func createMeshAction(nodeID, meshType string, position []float64, theme Theme, extra map[string]interface{}) eventsourcing.DeltaAction {
	props := map[string]interface{}{
		"mesh":     meshType,
		"position": position,
		"material_override": map[string]interface{}{
			"albedo_color":     theme.Primary,
			"emissive_color":   theme.Accent,
			"emission_enabled": true,
		},
	}
	// Apply extra properties
	for k, v := range extra {
		props[k] = v
	}
	return eventsourcing.DeltaAction{
		Type:       "create",
		NodeID:     nodeID,
		NodeType:   "MeshInstance3D",
		Properties: props,
	}
}

// CreateBox creates a DeltaAction for a themed box mesh (deprecated: use CreateStandardObject)
func CreateBox(nodeID string, position []float64, theme Theme) eventsourcing.DeltaAction {
	return createMeshAction(nodeID, "box", position, theme, nil)
}

// CreateSphere creates a DeltaAction for a themed sphere mesh (deprecated: use CreateStandardObject)
func CreateSphere(nodeID string, position []float64, theme Theme) eventsourcing.DeltaAction {
	return createMeshAction(nodeID, "sphere", position, theme, nil)
}

// CreateLabel creates a DeltaAction for a 3D text label
func CreateLabel(nodeID string, text string, position []float64, theme Theme) eventsourcing.DeltaAction {
	return eventsourcing.DeltaAction{
		Type:     "create",
		NodeID:   nodeID,
		NodeType: "Label3D",
		Properties: map[string]interface{}{
			"text":             text,
			"position":         position,
			"modulate":         theme.Text,   // Assuming modulate affects color
			"outline_modulate": theme.Accent, // Make outline emissive
		},
	}
}

// CreateCard creates a simple card as a box with a label on top (deprecated: use CreateStandardObject)
func CreateCard(nodeID string, title string, position []float64, theme Theme) []eventsourcing.DeltaAction {
	return CreateStandardObject(StandardObject{
		ID:       nodeID,
		MeshType: "box",
		Position: position,
		Label:    &LabelConfig{Text: title},
		Theme:    theme,
	})
}

// NextPosition computes the next position based on layout type
func (lm *LayoutManager) NextPosition() []float64 {
	lm.Counter++
	switch lm.Type {
	case "grid":
		cols := 4.0 // Default columns
		row := math.Floor(float64(lm.Counter-1) / cols)
		col := math.Mod(float64(lm.Counter-1), cols)
		return []float64{col * lm.Spacing, 0, row * lm.Spacing}
	case "circle":
		angle := 2 * math.Pi * float64(lm.Counter-1) / 8 // Assuming 8 items
		x := lm.Spacing * math.Cos(angle)
		z := lm.Spacing * math.Sin(angle)
		return []float64{x, 0, z}
	case "spiral":
		angle := float64(lm.Counter-1) * 0.5
		radius := float64(lm.Counter-1) * lm.Spacing
		x := radius * math.Cos(angle)
		z := radius * math.Sin(angle)
		return []float64{x, 0, z}
	case "random":
		x := (float64(lm.Seed%100) - 50.0) / 50.0 * lm.Spacing
		z := (float64((lm.Seed/100)%100) - 50.0) / 50.0 * lm.Spacing
		lm.Seed++ // Increment seed for next
		return []float64{x, 0, z}
	default:
		return []float64{0, 0, 0}
	}
}

// PositionInGrid positions items in a grid layout (deprecated: use LayoutManager)
func PositionInGrid(row, col, spacing float64) []float64 {
	return []float64{col * spacing, 0, row * spacing}
}

// PositionInCircle positions items in a circle
func PositionInCircle(index int, radius, height float64) []float64 {
	angle := 2 * math.Pi * float64(index) / 8 // Assuming 8 items for simplicity
	x := radius * math.Cos(angle)
	z := radius * math.Sin(angle)
	return []float64{x, height, z}
}

// CreateImageHolder creates a placeholder for an image (using a box for now, as Godot handles images separately)
func CreateImageHolder(nodeID string, position []float64, theme Theme) eventsourcing.DeltaAction {
	return eventsourcing.DeltaAction{
		Type:     "create",
		NodeID:   nodeID,
		NodeType: "MeshInstance3D",
		Properties: map[string]interface{}{
			"mesh":     "box",
			"position": position,
			"material_override": map[string]interface{}{
				"albedo_color": theme.Background,
			},
		},
	}
}

// CreateCylinder creates a DeltaAction for a themed cylinder mesh (deprecated: use CreateStandardObject)
func CreateCylinder(nodeID string, position []float64, theme Theme) eventsourcing.DeltaAction {
	return createMeshAction(nodeID, "cylinder", position, theme, nil)
}

// CreatePlane creates a DeltaAction for a themed plane mesh
func CreatePlane(nodeID string, position []float64, theme Theme) eventsourcing.DeltaAction {
	return eventsourcing.DeltaAction{
		Type:     "create",
		NodeID:   nodeID,
		NodeType: "MeshInstance3D",
		Properties: map[string]interface{}{
			"mesh":     "plane",
			"position": position,
			"material_override": map[string]interface{}{
				"albedo_color": theme.Background,
			},
		},
	}
}

// CreateCapsule creates a DeltaAction for a themed capsule mesh
func CreateCapsule(nodeID string, position []float64, theme Theme) eventsourcing.DeltaAction {
	return eventsourcing.DeltaAction{
		Type:     "create",
		NodeID:   nodeID,
		NodeType: "MeshInstance3D",
		Properties: map[string]interface{}{
			"mesh":     "capsule",
			"position": position,
			"material_override": map[string]interface{}{
				"albedo_color": theme.Accent,
			},
		},
	}
}

// CreateTree creates a simple tree as a cylinder trunk with a sphere crown
func CreateTree(nodeID string, position []float64, theme Theme) []eventsourcing.DeltaAction {
	trunk := CreateCylinder(nodeID+"_trunk", position, theme)
	crownPos := []float64{position[0], position[1] + 2.0, position[2]}
	crown := CreateSphere(nodeID+"_crown", crownPos, theme)
	return []eventsourcing.DeltaAction{trunk, crown}
}

// CreateWater creates a water plane
func CreateWater(nodeID string, position []float64, theme Theme) eventsourcing.DeltaAction {
	return eventsourcing.DeltaAction{
		Type:     "create",
		NodeID:   nodeID,
		NodeType: "MeshInstance3D",
		Properties: map[string]interface{}{
			"mesh":     "plane",
			"position": position,
			"material_override": map[string]interface{}{
				"albedo_color": []float64{0.0, 0.5, 1.0, 0.8}, // Blue semi-transparent
			},
		},
	}
}

// CreateRock creates a rock as a scaled and rotated box
func CreateRock(nodeID string, position []float64, theme Theme) eventsourcing.DeltaAction {
	return eventsourcing.DeltaAction{
		Type:     "create",
		NodeID:   nodeID,
		NodeType: "MeshInstance3D",
		Properties: map[string]interface{}{
			"mesh":     "box",
			"position": position,
			"scale":    []float64{1.5, 0.8, 1.2}, // Irregular shape
			"rotation": []float64{0.3, 0.0, 0.2}, // Slight rotation
			"material_override": map[string]interface{}{
				"albedo_color": []float64{0.4, 0.3, 0.2, 1.0}, // Brown
			},
		},
	}
}

// PositionInSpiral positions items in a spiral pattern
func PositionInSpiral(index int, spacing, height float64) []float64 {
	angle := float64(index) * 0.5
	radius := float64(index) * spacing
	x := radius * math.Cos(angle)
	z := radius * math.Sin(angle)
	return []float64{x, height, z}
}

// PositionRandom positions items randomly within a radius
func PositionRandom(seed int64, radius, height float64) []float64 {
	// Simple pseudo-random based on seed
	x := (float64(seed%100) - 50.0) / 50.0 * radius
	z := (float64((seed/100)%100) - 50.0) / 50.0 * radius
	return []float64{x, height, z}
}

// CreateInteractiveText creates a label that could be made interactive (Godot-side logic needed)
func CreateInteractiveText(nodeID string, text string, position []float64, theme Theme) eventsourcing.DeltaAction {
	return CreateLabel(nodeID, text, position, theme)
}
