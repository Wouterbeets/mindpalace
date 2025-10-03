package godot_ui

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestNewCard(t *testing.T) {
	action := NewCard([3]float32{1.0, 2.0, 0.1}, "primary")
	if action.ComponentType != "Card" {
		t.Errorf("Expected component_type 'Card', got %s", action.ComponentType)
	}
	size, ok := action.Properties["size"].([3]float32)
	if !ok || size != [3]float32{1.0, 2.0, 0.1} {
		t.Errorf("Expected size [1,2,0.1], got %v", action.Properties["size"])
	}
	if action.Properties["theme"] != "primary" {
		t.Errorf("Expected theme 'primary', got %v", action.Properties["theme"])
	}
}

func TestNewIcon(t *testing.T) {
	action := NewIcon("icon_calendar.png")
	if action.ComponentType != "Icon" {
		t.Errorf("Expected component_type 'Icon', got %s", action.ComponentType)
	}
	if action.Properties["texture_ref"] != "icon_calendar.png" {
		t.Errorf("Expected texture_ref 'icon_calendar.png', got %v", action.Properties["texture_ref"])
	}
}

func TestNewText(t *testing.T) {
	action := NewText("Hello World", "bold")
	if action.ComponentType != "Text" {
		t.Errorf("Expected component_type 'Text', got %s", action.ComponentType)
	}
	if action.Properties["content"] != "Hello World" {
		t.Errorf("Expected content 'Hello World', got %v", action.Properties["content"])
	}
	if action.Properties["style"] != "bold" {
		t.Errorf("Expected style 'bold', got %v", action.Properties["style"])
	}
}

func TestNewParticles(t *testing.T) {
	action := NewParticles("glow")
	if action.ComponentType != "Particles" {
		t.Errorf("Expected component_type 'Particles', got %s", action.ComponentType)
	}
	if action.Properties["effect"] != "glow" {
		t.Errorf("Expected effect 'glow', got %v", action.Properties["effect"])
	}
}

func TestNewPanel(t *testing.T) {
	children := []UIAction{NewText("Child", "normal")}
	action := NewPanel("vertical", children)
	if action.ComponentType != "Panel" {
		t.Errorf("Expected component_type 'Panel', got %s", action.ComponentType)
	}
	if action.Properties["layout"] != "vertical" {
		t.Errorf("Expected layout 'vertical', got %v", action.Properties["layout"])
	}
	if len(action.Children) != 1 || action.Children[0].ComponentType != "Text" {
		t.Errorf("Expected 1 child of type 'Text', got %v", action.Children)
	}
}

func TestWithChild(t *testing.T) {
	action := NewCard([3]float32{1, 1, 0.1}, "primary").WithChild(NewIcon("test.png"))
	if len(action.Children) != 1 {
		t.Errorf("Expected 1 child, got %d", len(action.Children))
	}
	if action.Children[0].ComponentType != "Icon" {
		t.Errorf("Expected child component_type 'Icon', got %s", action.Children[0].ComponentType)
	}
}

func TestLoadTheme(t *testing.T) {
	// Create a temporary theme file
	themeData := `{"colors": {"primary": {"R": 0.2, "G": 0.5, "B": 0.8, "A": 1.0}}}`
	tmpFile, err := os.CreateTemp("", "theme_*.json")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	if _, err := tmpFile.WriteString(themeData); err != nil {
		t.Fatalf("Failed to write temp file: %v", err)
	}
	tmpFile.Close()

	theme, err := LoadTheme(tmpFile.Name())
	if err != nil {
		t.Fatalf("Failed to load theme: %v", err)
	}
	if len(theme.Colors) != 1 {
		t.Errorf("Expected 1 color, got %d", len(theme.Colors))
	}
	color, ok := theme.Colors["primary"]
	if !ok || color.R != 0.2 || color.G != 0.5 || color.B != 0.8 || color.A != 1.0 {
		t.Errorf("Expected primary color {0.2,0.5,0.8,1.0}, got %v", color)
	}
}

func TestNoDomainRefs(t *testing.T) {
	// Ensure no domain-specific terms in component types or properties
	action := NewCard([3]float32{1, 1, 0.1}, "primary").WithChild(NewIcon("icon.png")).WithChild(NewText("content", "style"))
	data, _ := json.Marshal(action)
	jsonStr := string(data)
	domainTerms := []string{"task", "calendar", "note", "event", "plugin"}
	for _, term := range domainTerms {
		if contains(jsonStr, term) {
			t.Errorf("Found domain term '%s' in UIAction JSON: %s", term, jsonStr)
		}
	}
}

func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}
