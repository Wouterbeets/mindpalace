package plugingenerator

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"
	"time"

	"mindpalace/pkg/logging"
)

// PluginRequirements holds the gathered requirements for generating a plugin
type PluginRequirements struct {
	Name        string
	Description string
	Entities    []EntitySpec
	Commands    []CommandSpec
}

// EntitySpec defines an entity (e.g., Drink)
type EntitySpec struct {
	Name   string
	Fields []FieldSpec
}

// FieldSpec defines a field in an entity
type FieldSpec struct {
	Name string
	Type string // e.g., string, int, time.Time
	JSON string // json tag
}

// CommandSpec defines a command (e.g., CreateDrink)
type CommandSpec struct {
	Name   string
	Input  []FieldSpec
	Action string // create, update, delete, list
}

// PluginGenerator handles the generation of plugins
type PluginGenerator struct {
	templatePath string
}

// NewPluginGenerator creates a new generator
func NewPluginGenerator() *PluginGenerator {
	return &PluginGenerator{
		templatePath: "internal/plugingenerator/plugin_template.go.tmpl",
	}
}

// GeneratePlugin generates the plugin code and writes it to the plugins directory
func (pg *PluginGenerator) GeneratePlugin(req *PluginRequirements) error {
	logging.Info("Generating plugin: %s", req.Name)

	// Prepare template data
	data := struct {
		Requirements *PluginRequirements
		Timestamp    string
	}{
		Requirements: req,
		Timestamp:    time.Now().Format(time.RFC3339),
	}

	// Load template
	tmpl, err := template.New("plugin").Funcs(template.FuncMap{
		"lower": strings.ToLower,
	}).ParseFiles(pg.templatePath)
	if err != nil {
		return fmt.Errorf("failed to parse template: %v", err)
	}

	// Create plugin directory
	pluginDir := filepath.Join("plugins", strings.ToLower(req.Name))
	if err := os.MkdirAll(pluginDir, 0755); err != nil {
		return fmt.Errorf("failed to create plugin directory: %v", err)
	}

	// Generate plugin.go
	pluginFile := filepath.Join(pluginDir, "plugin.go")
	f, err := os.Create(pluginFile)
	if err != nil {
		return fmt.Errorf("failed to create plugin file: %v", err)
	}
	defer f.Close()

	if err := tmpl.Execute(f, data); err != nil {
		return fmt.Errorf("failed to execute template: %v", err)
	}

	logging.Info("Plugin generated at: %s", pluginFile)
	return nil
}

// InterviewStep represents a step in the multi-turn interview
type InterviewStep struct {
	Question string
	Field    string
	Validate func(string) bool
}

// ConductInterview gathers requirements via multi-turn conversation
func (pg *PluginGenerator) ConductInterview() (*PluginRequirements, error) {
	req := &PluginRequirements{}

	// For now, simulate or use simple prompts
	// In real implementation, this would interact with user via chat
	req.Name = "drinkingtracker"
	req.Description = "Track daily drinking habits"
	req.Entities = []EntitySpec{
		{
			Name: "Drink",
			Fields: []FieldSpec{
				{"DrinkID", "string", "drink_id"},
				{"Date", "time.Time", "date"},
				{"Amount", "int", "amount"},
				{"Type", "string", "type"},
			},
		},
	}
	req.Commands = []CommandSpec{
		{"LogDrink", []FieldSpec{{"Date", "string", "date"}, {"Amount", "int", "amount"}, {"Type", "string", "type"}}, "create"},
		{"ListDrinks", []FieldSpec{}, "list"},
	}

	return req, nil
}
