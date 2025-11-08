package ui3d

import (
	"math"
	"strconv"

	"mindpalace/pkg/eventsourcing"
)

// DeltaBuilder provides a fluent interface for building 3D UI deltas
type DeltaBuilder struct {
	actions []eventsourcing.DeltaAction
	theme   Theme
}

// NewDeltaBuilder creates a new DeltaBuilder with the given theme
func NewDeltaBuilder(theme Theme) *DeltaBuilder {
	return &DeltaBuilder{
		actions: []eventsourcing.DeltaAction{},
		theme:   theme,
	}
}

// CreateBox adds a box mesh to the builder
func (db *DeltaBuilder) CreateBox(nodeID string, position []float64) *DeltaBuilder {
	action := createMeshAction(nodeID, "box", position, db.theme, nil)
	db.actions = append(db.actions, action)
	return db
}

// CreateSphere adds a sphere mesh to the builder
func (db *DeltaBuilder) CreateSphere(nodeID string, position []float64) *DeltaBuilder {
	action := createMeshAction(nodeID, "sphere", position, db.theme, nil)
	db.actions = append(db.actions, action)
	return db
}

// CreateCylinder adds a cylinder mesh to the builder
func (db *DeltaBuilder) CreateCylinder(nodeID string, position []float64) *DeltaBuilder {
	action := createMeshAction(nodeID, "cylinder", position, db.theme, nil)
	db.actions = append(db.actions, action)
	return db
}

// CreatePlane adds a plane mesh to the builder
func (db *DeltaBuilder) CreatePlane(nodeID string, position []float64) *DeltaBuilder {
	action := eventsourcing.DeltaAction{
		Type:     "create",
		NodeID:   nodeID,
		NodeType: "MeshInstance3D",
		Properties: map[string]interface{}{
			"mesh":     "plane",
			"position": position,
			"material_override": map[string]interface{}{
				"albedo_color": db.theme.Background,
			},
		},
	}
	db.actions = append(db.actions, action)
	return db
}

// CreateCapsule adds a capsule mesh to the builder
func (db *DeltaBuilder) CreateCapsule(nodeID string, position []float64) *DeltaBuilder {
	action := eventsourcing.DeltaAction{
		Type:     "create",
		NodeID:   nodeID,
		NodeType: "MeshInstance3D",
		Properties: map[string]interface{}{
			"mesh":     "capsule",
			"position": position,
			"material_override": map[string]interface{}{
				"albedo_color": db.theme.Accent,
			},
		},
	}
	db.actions = append(db.actions, action)
	return db
}

// CreateModel spawns a MeshInstance3D backed by a GLB resource.
func (db *DeltaBuilder) CreateModel(nodeID string, modelPath string, position []float64, scale []float64) *DeltaBuilder {
	props := map[string]interface{}{
		"position":   position,
		"model_path": modelPath,
	}
	if len(scale) == 3 {
		props["scale"] = scale
	}
	action := eventsourcing.DeltaAction{
		Type:       "create",
		NodeID:     nodeID,
		NodeType:   "MeshInstance3D",
		Properties: props,
	}
	db.actions = append(db.actions, action)
	return db
}

// CreateLabel adds a 3D text label to the builder
func (db *DeltaBuilder) CreateLabel(nodeID string, text string, position []float64) *DeltaBuilder {
	action := eventsourcing.DeltaAction{
		Type:     "create",
		NodeID:   nodeID,
		NodeType: "Label3D",
		Properties: map[string]interface{}{
			"text":             text,
			"position":         position,
			"modulate":         db.theme.Text,
			"outline_modulate": db.theme.Accent,
		},
	}
	db.actions = append(db.actions, action)
	return db
}

// AddAction appends a raw delta action, useful for bespoke geometry outside the predefined helpers.
func (db *DeltaBuilder) AddAction(action eventsourcing.DeltaAction) *DeltaBuilder {
	db.actions = append(db.actions, action)
	return db
}

// WithExtra adds extra properties to the last added action
func (db *DeltaBuilder) WithExtra(extra map[string]interface{}) *DeltaBuilder {
	if len(db.actions) > 0 {
		last := &db.actions[len(db.actions)-1]
		if last.Properties == nil {
			last.Properties = make(map[string]interface{})
		}
		for k, v := range extra {
			last.Properties[k] = v
		}
	}
	return db
}

// WithModel sets the GLTF model path for the last added action
func (db *DeltaBuilder) WithModel(modelPath string) *DeltaBuilder {
	if len(db.actions) > 0 {
		last := &db.actions[len(db.actions)-1]
		if last.Properties == nil {
			last.Properties = make(map[string]interface{})
		}
		last.Properties["model_path"] = modelPath
	}
	return db
}

// WithDisplayInfo adds display info to the last added action
func (db *DeltaBuilder) WithDisplayInfo(info *DisplayInfo) *DeltaBuilder {
	if len(db.actions) > 0 && info != nil {
		last := &db.actions[len(db.actions)-1]
		if last.Properties == nil {
			last.Properties = make(map[string]interface{})
		}
		last.Properties["display_info"] = map[string]interface{}{
			"title":       info.Title,
			"description": info.Description,
			"details":     info.Details,
		}
	}
	return db
}

// AnimateMoveTo adds a move animation to the specified node
func (db *DeltaBuilder) AnimateMoveTo(nodeID string, targetPos []float64, duration float64, ease string) *DeltaBuilder {
	action := eventsourcing.DeltaAction{
		Type:   "animate",
		NodeID: nodeID,
		Animation: &eventsourcing.AnimationSpec{
			Property: "position",
			To:       targetPos,
			Duration: duration,
			Ease:     ease,
		},
	}
	db.actions = append(db.actions, action)
	return db
}

// AnimateMoveToTouch adds a move-to-touch animation
func (db *DeltaBuilder) AnimateMoveToTouch(nodeID, targetNodeID string, speed float64, onTouchCallback string) *DeltaBuilder {
	action := eventsourcing.DeltaAction{
		Type:   "animate",
		NodeID: nodeID,
		Animation: &eventsourcing.AnimationSpec{
			Property: "position",
			To:       []interface{}{"move_to_touch", targetNodeID, speed, onTouchCallback},
		},
	}
	db.actions = append(db.actions, action)
	return db
}

// UpdateDisplayInfo emits an update action that refreshes the display info metadata for a node.
func (db *DeltaBuilder) UpdateDisplayInfo(nodeID string, info *DisplayInfo) *DeltaBuilder {
	if info == nil {
		return db
	}
	action := eventsourcing.DeltaAction{
		Type:   "update",
		NodeID: nodeID,
		Properties: map[string]interface{}{
			"display_info": map[string]interface{}{
				"title":       info.Title,
				"description": info.Description,
				"details":     info.Details,
			},
		},
	}
	db.actions = append(db.actions, action)
	return db
}

// AnimateFade adds a fade animation
func (db *DeltaBuilder) AnimateFade(nodeID string, targetOpacity float64, duration float64) *DeltaBuilder {
	action := eventsourcing.DeltaAction{
		Type:   "animate",
		NodeID: nodeID,
		Animation: &eventsourcing.AnimationSpec{
			Property: "opacity",
			To:       targetOpacity,
			Duration: duration,
		},
	}
	db.actions = append(db.actions, action)
	return db
}

// Update adds an update action for the specified node with given properties
func (db *DeltaBuilder) Update(nodeID string, properties map[string]interface{}) *DeltaBuilder {
	action := eventsourcing.DeltaAction{
		Type:       "update",
		NodeID:     nodeID,
		NodeType:   "MeshInstance3D",
		Properties: properties,
	}
	db.actions = append(db.actions, action)
	return db
}

// Delete adds a delete action for the specified node
func (db *DeltaBuilder) Delete(nodeID string) *DeltaBuilder {
	action := eventsourcing.DeltaAction{
		Type:   "delete",
		NodeID: nodeID,
	}
	db.actions = append(db.actions, action)
	return db
}

// Build returns the accumulated DeltaActions
func (db *DeltaBuilder) Build() []eventsourcing.DeltaAction {
	return db.actions
}

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
	Type    string // "grid", "circle", "spiral", "random", "linear"
	Spacing float64
	Zone    string // Ties to PLUGIN_ZONES in Godot
	Counter int
	Seed    int64                // For random positioning
	Zones   map[string][]float64 // Zone offsets
	Columns int                  // Number of columns for grid layouts (default 4)
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
		labelPos := CalculateLabelPosition(obj.Position, obj.MeshType)
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

// CalculateLabelPosition computes label position relative to mesh
func CalculateLabelPosition(basePos []float64, meshType string) []float64 {
	offsetY := 1.0 // Default for box
	switch meshType {
	case "sphere":
		offsetY = 0.8
	case "cylinder":
		offsetY = 1.2
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

// CreateZoneLines creates thin cylinders to draw zone boundaries on the floor
func (db *DeltaBuilder) CreateZoneLines(zones map[string][]float64) *DeltaBuilder {
	pluginCount := len(zones)
	if pluginCount == 0 {
		return db
	}
	for i := 0; i < pluginCount; i++ {
		// Calculate boundary angle between zones
		boundaryAngle := 2 * math.Pi * (float64(i) + 0.5) / float64(pluginCount)
		// Draw a line from center outward along the boundary using a thin cylinder
		center := []float64{0, 0.1, 0} // Slightly above floor
		end := []float64{100 * math.Cos(boundaryAngle), 0.1, 100 * math.Sin(boundaryAngle)}
		// Calculate midpoint and rotation
		midX := (center[0] + end[0]) / 2
		midZ := (center[2] + end[2]) / 2
		dx := end[0] - center[0]
		dz := end[2] - center[2]
		length := math.Sqrt(dx*dx + dz*dz)
		angle := math.Atan2(dz, dx)
		action := eventsourcing.DeltaAction{
			Type:     "create",
			NodeID:   "zone_boundary_" + strconv.Itoa(i),
			NodeType: "MeshInstance3D",
			Properties: map[string]interface{}{
				"mesh":     "cylinder",
				"position": []float64{midX, 0.05, midZ},       // Half height above floor
				"scale":    []float64{0.05, length / 2, 0.05}, // Thin and long
				"rotation": []float64{0, angle, math.Pi / 2},  // Rotate to lie flat
				"material_override": map[string]interface{}{
					"albedo_color": db.theme.Accent,
				},
			},
		}
		db.actions = append(db.actions, action)
	}
	return db
}

// CreateZoneLabels creates high aerial labels for each zone
func (db *DeltaBuilder) CreateZoneLabels(zones map[string][]float64) *DeltaBuilder {
	for name, offset := range zones {
		labelPos := []float64{offset[0], 10, offset[2]} // High up
		action := eventsourcing.DeltaAction{
			Type:     "create",
			NodeID:   "zone_label_" + name,
			NodeType: "Label3D",
			Properties: map[string]interface{}{
				"text":             name,
				"position":         labelPos,
				"modulate":         db.theme.Text,
				"outline_modulate": db.theme.Accent,
				"font_size":        128,
			},
		}
		db.actions = append(db.actions, action)
	}
	return db
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

// CalculateDynamicZones computes zone offsets for plugins around the orchestrator
func (lm *LayoutManager) CalculateDynamicZones(pluginNames []string) map[string]Zone {
	pluginCount := len(pluginNames)
	zones := make(map[string]Zone)
	if pluginCount == 0 {
		return zones
	}
	baseDistance := 100.0
	spacing := 5.0
	distance := baseDistance + float64(pluginCount-1)*spacing
	for i, name := range pluginNames {
		angleRad := 2 * math.Pi * float64(i) / float64(pluginCount)
		angleDeg := angleRad * 180 / math.Pi

		// Set grid dimensions based on plugin
		var rows, cols, depth int
		switch name {
		case "taskmanager":
			rows, cols, depth = 1, 3, 10
		case "calendar":
			rows, cols, depth = 1, 5, 5
		case "plugingenerator":
			rows, cols, depth = 1, 2, 2
		default:
			rows, cols, depth = 1, 1, 1
		}

		zones[name] = Zone{
			Angle:     angleDeg,
			Radius:    distance,
			GridRows:  rows,
			GridCols:  cols,
			GridDepth: depth,
		}
	}
	return zones
}

// NextPosition computes the next position based on layout type
func (lm *LayoutManager) NextPosition() []float64 {
	lm.Counter++
	var pos []float64
	switch lm.Type {
	case "grid":
		cols := 4.0 // Default columns
		if lm.Columns > 0 {
			cols = float64(lm.Columns)
		}
		row := math.Floor(float64(lm.Counter-1) / cols)
		col := math.Mod(float64(lm.Counter-1), cols)
		pos = []float64{col * lm.Spacing, 0, row * lm.Spacing}
	case "circle":
		angle := 2 * math.Pi * float64(lm.Counter-1) / 8 // Assuming 8 items
		x := lm.Spacing * math.Cos(angle)
		z := lm.Spacing * math.Sin(angle)
		pos = []float64{x, 0, z}
	case "spiral":
		angle := float64(lm.Counter-1) * 0.5
		radius := float64(lm.Counter-1) * lm.Spacing
		x := radius * math.Cos(angle)
		z := radius * math.Sin(angle)
		pos = []float64{x, 0, z}
	case "random":
		x := (float64(lm.Seed%100) - 50.0) / 50.0 * lm.Spacing
		z := (float64((lm.Seed/100)%100) - 50.0) / 50.0 * lm.Spacing
		lm.Seed++ // Increment seed for next
		pos = []float64{x, 0, z}
	case "linear":
		pos = []float64{float64(lm.Counter-1) * lm.Spacing, 0, 0}
	default:
		pos = []float64{0, 0, 0}
	}
	// Apply zone offset if defined
	if lm.Zone != "" && lm.Zones != nil {
		if offset, exists := lm.Zones[lm.Zone]; exists && len(offset) >= 3 {
			pos[0] += offset[0]
			pos[1] += offset[1]
			pos[2] += offset[2]
		}
	}
	return pos
}

// UIComponent defines an interactive UI element sent from backend to front-end.
type UIComponent interface {
	Type() string
	Properties() map[string]interface{}
	Actions() map[string]Action
	Serialize() map[string]interface{}
}

// Action represents an interaction that triggers a backend command.
type Action struct {
	Type              string                 `json:"type"`
	Data              map[string]interface{} `json:"data,omitempty"`
	Trigger           string                 `json:"trigger,omitempty"`
	CommandDescriptor *CommandDescriptor     `json:"command_descriptor,omitempty"`
	Label             string                 `json:"label,omitempty"`
	Icon              string                 `json:"icon,omitempty"`
	Hints             map[string]interface{} `json:"hints,omitempty"`
}

// CommandDescriptor describes how to build a backend command invocation from a UI interaction.
type CommandDescriptor struct {
	Command      string                  `json:"command"`
	Arguments    map[string]ValueBinding `json:"arguments,omitempty"`
	Description  string                  `json:"description,omitempty"`
	Confirmation string                  `json:"confirmation,omitempty"`
	Metadata     map[string]interface{}  `json:"metadata,omitempty"`
}

// ValueBinding identifies how an argument should be resolved.
type ValueBinding struct {
	Source BindingSource `json:"source"`
	Path   string        `json:"path,omitempty"`
	Value  interface{}   `json:"value,omitempty"`
}

// BindingSource enumerates valid argument binding sources.
type BindingSource string

const (
	BindingSourceStatic    BindingSource = "static"
	BindingSourceContext   BindingSource = "context"
	BindingSourceComponent BindingSource = "component"
	BindingSourceUserInput BindingSource = "user_input"
)

// StaticValue returns a binding with a fixed value.
func StaticValue(val interface{}) ValueBinding {
	return ValueBinding{
		Source: BindingSourceStatic,
		Value:  val,
	}
}

// ContextValue returns a binding that pulls from the interaction context (e.g., drop target).
func ContextValue(path string) ValueBinding {
	return ValueBinding{
		Source: BindingSourceContext,
		Path:   path,
	}
}

// ComponentValue returns a binding that references component state.
func ComponentValue(path string) ValueBinding {
	return ValueBinding{
		Source: BindingSourceComponent,
		Path:   path,
	}
}

// UserInputValue returns a binding sourced from user input collected during the interaction.
func UserInputValue(path string) ValueBinding {
	return ValueBinding{
		Source: BindingSourceUserInput,
		Path:   path,
	}
}

// NewCommandAction constructs a command-producing action descriptor.
func NewCommandAction(trigger string, descriptor CommandDescriptor) Action {
	return Action{
		Type:              "command",
		Trigger:           trigger,
		CommandDescriptor: &descriptor,
	}
}
