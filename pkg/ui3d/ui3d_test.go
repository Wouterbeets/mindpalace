package ui3d

import (
	"reflect"
	"testing"

	"mindpalace/pkg/eventsourcing"
)

func TestDefaultTheme(t *testing.T) {
	theme := DefaultTheme()
	expected := Theme{
		Primary:    []float64{0.2, 0.5, 0.8, 1.0},
		Secondary:  []float64{0.5, 0.5, 0.5, 1.0},
		Accent:     []float64{1.0, 0.8, 0.0, 1.0},
		Background: []float64{0.1, 0.1, 0.1, 1.0},
		Text:       []float64{1.0, 1.0, 1.0, 1.0},
	}
	if !reflect.DeepEqual(theme, expected) {
		t.Errorf("DefaultTheme() = %v, want %v", theme, expected)
	}
}

func TestLightTheme(t *testing.T) {
	theme := LightTheme()
	expected := Theme{
		Primary:    []float64{0.0, 0.4, 0.8, 1.0},
		Secondary:  []float64{0.7, 0.7, 0.7, 1.0},
		Accent:     []float64{1.0, 0.6, 0.0, 1.0},
		Background: []float64{0.9, 0.9, 0.9, 1.0},
		Text:       []float64{0.0, 0.0, 0.0, 1.0},
	}
	if !reflect.DeepEqual(theme, expected) {
		t.Errorf("LightTheme() = %v, want %v", theme, expected)
	}
}

func TestCreateBox(t *testing.T) {
	theme := DefaultTheme()
	position := []float64{1.0, 2.0, 3.0}
	action := CreateBox("test_box", position, theme)

	expected := eventsourcing.DeltaAction{
		Type:     "create",
		NodeID:   "test_box",
		NodeType: "MeshInstance3D",
		Properties: map[string]interface{}{
			"mesh":     "box",
			"position": position,
			"material_override": map[string]interface{}{
				"albedo_color":     theme.Primary,
				"emissive_color":   theme.Accent,
				"emission_enabled": true,
			},
		},
	}

	if action.Type != expected.Type || action.NodeID != expected.NodeID || action.NodeType != expected.NodeType {
		t.Errorf("CreateBox() basic fields mismatch")
	}
	if !reflect.DeepEqual(action.Properties, expected.Properties) {
		t.Errorf("CreateBox() properties = %v, want %v", action.Properties, expected.Properties)
	}
}

func TestCreateSphere(t *testing.T) {
	theme := DefaultTheme()
	position := []float64{0.0, 0.0, 0.0}
	action := CreateSphere("test_sphere", position, theme)

	expected := eventsourcing.DeltaAction{
		Type:     "create",
		NodeID:   "test_sphere",
		NodeType: "MeshInstance3D",
		Properties: map[string]interface{}{
			"mesh":     "sphere",
			"position": position,
			"material_override": map[string]interface{}{
				"albedo_color":     theme.Primary,
				"emissive_color":   theme.Accent,
				"emission_enabled": true,
			},
		},
	}

	if !reflect.DeepEqual(action, expected) {
		t.Errorf("CreateSphere() = %v, want %v", action, expected)
	}
}

func TestCreateLabel(t *testing.T) {
	theme := DefaultTheme()
	position := []float64{1.0, 1.0, 1.0}
	action := CreateLabel("test_label", "Hello", position, theme)

	expected := eventsourcing.DeltaAction{
		Type:     "create",
		NodeID:   "test_label",
		NodeType: "Label3D",
		Properties: map[string]interface{}{
			"text":             "Hello",
			"position":         position,
			"modulate":         theme.Text,
			"outline_modulate": theme.Accent,
		},
	}

	if !reflect.DeepEqual(action, expected) {
		t.Errorf("CreateLabel() = %v, want %v", action, expected)
	}
}

func TestCreateCard(t *testing.T) {
	theme := DefaultTheme()
	position := []float64{0.0, 0.0, 0.0}
	actions := CreateCard("test_card", "Title", position, theme)

	if len(actions) != 2 {
		t.Errorf("CreateCard() should return 2 actions, got %d", len(actions))
	}

	// Check box
	box := actions[0]
	if box.NodeID != "test_card" || box.NodeType != "MeshInstance3D" {
		t.Errorf("CreateCard() box action incorrect")
	}

	// Check label
	label := actions[1]
	if label.NodeID != "test_card_label" || label.NodeType != "Label3D" {
		t.Errorf("CreateCard() label action incorrect")
	}
	expectedLabelPos := []float64{0.0, 1.2, 0.0}
	if !reflect.DeepEqual(label.Properties["position"], expectedLabelPos) {
		t.Errorf("CreateCard() label position = %v, want %v", label.Properties["position"], expectedLabelPos)
	}
}

func TestPositionInGrid(t *testing.T) {
	pos := PositionInGrid(1, 2, 5.0)
	expected := []float64{10.0, 0, 5.0}
	if !reflect.DeepEqual(pos, expected) {
		t.Errorf("PositionInGrid() = %v, want %v", pos, expected)
	}
}

func TestPositionInCircle(t *testing.T) {
	pos := PositionInCircle(0, 10.0, 2.0)
	// For index 0, angle 0, x=10*cos(0)=10, z=10*sin(0)=0
	expected := []float64{10.0, 2.0, 0.0}
	if !reflect.DeepEqual(pos, expected) {
		t.Errorf("PositionInCircle() = %v, want %v", pos, expected)
	}
}

func TestCreateImageHolder(t *testing.T) {
	theme := DefaultTheme()
	position := []float64{1.0, 1.0, 1.0}
	action := CreateImageHolder("test_image", position, theme)

	expected := eventsourcing.DeltaAction{
		Type:     "create",
		NodeID:   "test_image",
		NodeType: "MeshInstance3D",
		Properties: map[string]interface{}{
			"mesh":     "box",
			"position": position,
			"material_override": map[string]interface{}{
				"albedo_color": theme.Background,
			},
		},
	}

	if !reflect.DeepEqual(action, expected) {
		t.Errorf("CreateImageHolder() = %v, want %v", action, expected)
	}
}

func TestCreateCylinder(t *testing.T) {
	theme := DefaultTheme()
	position := []float64{1.0, 2.0, 3.0}
	action := CreateCylinder("test_cylinder", position, theme)

	expected := eventsourcing.DeltaAction{
		Type:     "create",
		NodeID:   "test_cylinder",
		NodeType: "MeshInstance3D",
		Properties: map[string]interface{}{
			"mesh":     "cylinder",
			"position": position,
			"material_override": map[string]interface{}{
				"albedo_color":     theme.Primary,
				"emissive_color":   theme.Accent,
				"emission_enabled": true,
			},
		},
	}

	if !reflect.DeepEqual(action, expected) {
		t.Errorf("CreateCylinder() = %v, want %v", action, expected)
	}
}

func TestCreatePlane(t *testing.T) {
	theme := DefaultTheme()
	position := []float64{0.0, 0.0, 0.0}
	action := CreatePlane("test_plane", position, theme)

	expected := eventsourcing.DeltaAction{
		Type:     "create",
		NodeID:   "test_plane",
		NodeType: "MeshInstance3D",
		Properties: map[string]interface{}{
			"mesh":     "plane",
			"position": position,
			"material_override": map[string]interface{}{
				"albedo_color": theme.Background,
			},
		},
	}

	if !reflect.DeepEqual(action, expected) {
		t.Errorf("CreatePlane() = %v, want %v", action, expected)
	}
}

func TestCreateCapsule(t *testing.T) {
	theme := DefaultTheme()
	position := []float64{1.0, 1.0, 1.0}
	action := CreateCapsule("test_capsule", position, theme)

	expected := eventsourcing.DeltaAction{
		Type:     "create",
		NodeID:   "test_capsule",
		NodeType: "MeshInstance3D",
		Properties: map[string]interface{}{
			"mesh":     "capsule",
			"position": position,
			"material_override": map[string]interface{}{
				"albedo_color": theme.Accent,
			},
		},
	}

	if !reflect.DeepEqual(action, expected) {
		t.Errorf("CreateCapsule() = %v, want %v", action, expected)
	}
}

func TestCreateTree(t *testing.T) {
	theme := DefaultTheme()
	position := []float64{0.0, 0.0, 0.0}
	actions := CreateTree("test_tree", position, theme)

	if len(actions) != 2 {
		t.Errorf("CreateTree() should return 2 actions, got %d", len(actions))
	}

	// Check trunk
	trunk := actions[0]
	if trunk.NodeID != "test_tree_trunk" || trunk.NodeType != "MeshInstance3D" {
		t.Errorf("CreateTree() trunk action incorrect")
	}

	// Check crown
	crown := actions[1]
	if crown.NodeID != "test_tree_crown" || crown.NodeType != "MeshInstance3D" {
		t.Errorf("CreateTree() crown action incorrect")
	}
	expectedCrownPos := []float64{0.0, 2.0, 0.0}
	if !reflect.DeepEqual(crown.Properties["position"], expectedCrownPos) {
		t.Errorf("CreateTree() crown position = %v, want %v", crown.Properties["position"], expectedCrownPos)
	}
}

func TestCreateWater(t *testing.T) {
	theme := DefaultTheme()
	position := []float64{0.0, 0.0, 0.0}
	action := CreateWater("test_water", position, theme)

	expected := eventsourcing.DeltaAction{
		Type:     "create",
		NodeID:   "test_water",
		NodeType: "MeshInstance3D",
		Properties: map[string]interface{}{
			"mesh":     "plane",
			"position": position,
			"material_override": map[string]interface{}{
				"albedo_color": []float64{0.0, 0.5, 1.0, 0.8},
			},
		},
	}

	if !reflect.DeepEqual(action, expected) {
		t.Errorf("CreateWater() = %v, want %v", action, expected)
	}
}

func TestCreateRock(t *testing.T) {
	theme := DefaultTheme()
	position := []float64{1.0, 1.0, 1.0}
	action := CreateRock("test_rock", position, theme)

	expected := eventsourcing.DeltaAction{
		Type:     "create",
		NodeID:   "test_rock",
		NodeType: "MeshInstance3D",
		Properties: map[string]interface{}{
			"mesh":     "box",
			"position": position,
			"scale":    []float64{1.5, 0.8, 1.2},
			"rotation": []float64{0.3, 0.0, 0.2},
			"material_override": map[string]interface{}{
				"albedo_color": []float64{0.4, 0.3, 0.2, 1.0},
			},
		},
	}

	if !reflect.DeepEqual(action, expected) {
		t.Errorf("CreateRock() = %v, want %v", action, expected)
	}
}

func TestPositionInSpiral(t *testing.T) {
	pos := PositionInSpiral(0, 1.0, 0.0)
	expected := []float64{0.0, 0.0, 0.0}
	if !reflect.DeepEqual(pos, expected) {
		t.Errorf("PositionInSpiral(0) = %v, want %v", pos, expected)
	}

	pos = PositionInSpiral(1, 1.0, 0.0)
	// angle = 0.5, radius = 1.0, x = 1*cos(0.5) ≈ 0.877, z = 1*sin(0.5) ≈ 0.479
	expected = []float64{0.8775825618903728, 0.0, 0.479425538604203}
	if !reflect.DeepEqual(pos, expected) {
		t.Errorf("PositionInSpiral(1) = %v, want %v", pos, expected)
	}
}

func TestPositionRandom(t *testing.T) {
	pos := PositionRandom(0, 10.0, 1.0)
	// For seed 0, x = (0-50)/50 *10 = -10, z = same = -10
	expected := []float64{-10.0, 1.0, -10.0}
	if !reflect.DeepEqual(pos, expected) {
		t.Errorf("PositionRandom(0) = %v, want %v", pos, expected)
	}
}

func TestCreateInteractiveText(t *testing.T) {
	theme := DefaultTheme()
	position := []float64{0.0, 0.0, 0.0}
	action := CreateInteractiveText("test_text", "Click me", position, theme)

	// Should be same as CreateLabel
	expected := CreateLabel("test_text", "Click me", position, theme)

	if !reflect.DeepEqual(action, expected) {
		t.Errorf("CreateInteractiveText() = %v, want %v", action, expected)
	}
}
