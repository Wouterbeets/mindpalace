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
	db := NewDeltaBuilder(theme)
	db.CreateBox("test_box", position)
	actions := db.Build()
	action := actions[0]

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
	db := NewDeltaBuilder(theme)
	db.CreateSphere("test_sphere", position)
	actions := db.Build()
	action := actions[0]

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

func TestLayoutManagerGrid(t *testing.T) {
	lm := &LayoutManager{
		Type:    "grid",
		Spacing: 5.0,
		Counter: 1, // Will increment to 2, giving col=1
	}
	pos := lm.NextPosition()
	expected := []float64{5.0, 0, 0} // col=1, row=0
	if !reflect.DeepEqual(pos, expected) {
		t.Errorf("LayoutManager grid = %v, want %v", pos, expected)
	}
}

func TestLayoutManagerWithZones(t *testing.T) {
	lm := &LayoutManager{
		Type:    "linear",
		Spacing: 2.0,
		Zone:    "test_zone",
		Zones:   map[string][]float64{"test_zone": {10, 5, 3}},
		Counter: 0, // Will increment to 1, giving index 0
	}
	pos := lm.NextPosition()
	expected := []float64{10, 5, 3} // 0*2 + offset
	if !reflect.DeepEqual(pos, expected) {
		t.Errorf("LayoutManager with zones = %v, want %v", pos, expected)
	}
}
