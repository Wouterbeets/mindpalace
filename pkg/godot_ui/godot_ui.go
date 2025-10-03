package godot_ui

import (
	"encoding/json"
	"fmt"
	"os"
)

// Color represents an RGBA color with values 0.0 to 1.0
type Color struct {
	R, G, B, A float32
}

// Theme holds color schemes and other visual settings
type Theme struct {
	Colors map[string]Color `json:"colors"`
}

// UIAction represents a UI component action for Godot
type UIAction struct {
	ComponentType string                 `json:"component_type"`
	Properties    map[string]interface{} `json:"properties"`
	Children      []UIAction             `json:"children,omitempty"`
}

// WithChild adds a child component to the UIAction
func (u UIAction) WithChild(child UIAction) UIAction {
	u.Children = append(u.Children, child)
	return u
}

// LoadTheme loads a theme from a JSON file
func LoadTheme(path string) (Theme, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Theme{}, fmt.Errorf("failed to read theme file: %w", err)
	}
	var theme Theme
	if err := json.Unmarshal(data, &theme); err != nil {
		return Theme{}, fmt.Errorf("failed to unmarshal theme: %w", err)
	}
	return theme, nil
}

// NewCard creates a generic card component (box mesh with theme material)
func NewCard(size [3]float32, theme string) UIAction {
	return UIAction{
		ComponentType: "Card",
		Properties: map[string]interface{}{
			"size":  size,
			"theme": theme,
			"mesh":  "box",
		},
	}
}

// NewIcon creates an icon component (sprite with texture reference)
func NewIcon(textureRef string) UIAction {
	return UIAction{
		ComponentType: "Icon",
		Properties: map[string]interface{}{
			"texture_ref": textureRef,
			"mesh":        "quad",
		},
	}
}

// NewText creates a text component (Label3D with content and style)
func NewText(content, style string) UIAction {
	return UIAction{
		ComponentType: "Text",
		Properties: map[string]interface{}{
			"content": content,
			"style":   style,
		},
	}
}

// NewParticles creates a particle effect component
func NewParticles(effect string) UIAction {
	return UIAction{
		ComponentType: "Particles",
		Properties: map[string]interface{}{
			"effect": effect,
		},
	}
}

// NewPanel creates a panel component with layout and children
func NewPanel(layout string, children []UIAction) UIAction {
	return UIAction{
		ComponentType: "Panel",
		Properties: map[string]interface{}{
			"layout": layout,
		},
		Children: children,
	}
}
