package plugingenerator

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"text/template"
	"time"
	"unicode"

	"mindpalace/pkg/logging"
)

// RequirementsError captures validation failures with a user-facing hint.
type RequirementsError struct {
	Reason      string
	UserMessage string
}

func (e *RequirementsError) Error() string {
	return e.Reason
}

func newRequirementsError(reason, userMessage string) *RequirementsError {
	return &RequirementsError{
		Reason:      reason,
		UserMessage: userMessage,
	}
}

// PluginRequirements holds the gathered requirements for generating a plugin
type PluginRequirements struct {
	Name        string        `json:"name"`
	Description string        `json:"description"`
	Entities    []EntitySpec  `json:"entities"`
	Commands    []CommandSpec `json:"commands"`
}

// EntitySpec defines an entity (e.g., Drink)
type EntitySpec struct {
	Name   string      `json:"name"`
	Fields []FieldSpec `json:"fields"`
}

// FieldSpec defines a field in an entity
type FieldSpec struct {
	Name string `json:"name"`
	Type string `json:"type"` // e.g., string, int, time.Time
	JSON string `json:"json"` // json tag
}

// CommandSpec defines a command (e.g., CreateDrink)
type CommandSpec struct {
	Name   string      `json:"name"`
	Input  []FieldSpec `json:"input"`
	Action string      `json:"action"` // create, update, delete, list
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
	if err := ValidateRequirements(req); err != nil {
		return fmt.Errorf("invalid requirements: %w", err)
	}
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
		"schemaType": func(goType string) string {
			return jsonSchemaType(goType)
		},
		"randomPos": func() string {
			return fmt.Sprintf("[]float64{%0.2f, %0.2f, %0.2f}",
				rand.Float64()*10-5,
				rand.Float64()*10-5,
				rand.Float64()*10-5,
			)
		},
	}).ParseFiles(pg.templatePath)
	if err != nil {
		return fmt.Errorf("failed to parse template: %v", err)
	}

	// Create plugin directory
	pluginDir := filepath.Join("plugins", pluginFolderName(req.Name))
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

// ValidateRequirements ensures the provided blueprint can be turned into compilable Go code.
func ValidateRequirements(req *PluginRequirements) error {
	if req == nil {
		return newRequirementsError(
			"requirements cannot be nil",
			"Provide a plugin description or run the Forge Plugin interview so we can gather the blueprint.")
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		return newRequirementsError(
			"plugin name is required",
			"Give this plugin a short codename (letters/numbers/underscores) or use the Forge Plugin UI to fill it in.")
	}
	if !isValidIdentifier(req.Name) {
		return newRequirementsError(
			fmt.Sprintf("plugin name '%s' must be a valid Go identifier", req.Name),
			"Use letters, numbers, or underscores only (e.g., diesel_tracker).")
	}
	if len(req.Entities) == 0 {
		return newRequirementsError(
			"at least one entity specification is required",
			"Specify what kind of record this plugin tracks (for example, FuelLog or LessonPlan).")
	}
	entity := &req.Entities[0]
	entity.Name = strings.TrimSpace(entity.Name)
	if entity.Name == "" {
		return newRequirementsError(
			"primary entity name is required",
			"Name the primary record (e.g., Drink, FuelLog, JournalEntry).")
	}
	if !isValidIdentifier(entity.Name) {
		return newRequirementsError(
			fmt.Sprintf("entity name '%s' must be a valid Go identifier", entity.Name),
			"Use CamelCase letters for entity names (for example, FuelLog).")
	}
	if len(entity.Fields) == 0 {
		return newRequirementsError(
			fmt.Sprintf("entity '%s' must define at least one field", entity.Name),
			"Add at least one meaningful field (amount, date, notes, etc.).")
	}
	expectedID := fmt.Sprintf("%sID", entity.Name)
	for i := range entity.Fields {
		field := &entity.Fields[i]
		field.Name = strings.TrimSpace(field.Name)
		field.Type = strings.TrimSpace(field.Type)
		if field.Name == "" || field.Type == "" {
			return newRequirementsError(
				fmt.Sprintf("entity '%s' has a field with missing name or type", entity.Name),
				"Each field needs both a label and a data type (string, int, bool, etc.).")
		}
		if !isValidIdentifier(field.Name) {
			return newRequirementsError(
				fmt.Sprintf("field name '%s' must be a valid Go identifier", field.Name),
				"Stick to CamelCase letters/numbers for field names (e.g., Amount, Notes).")
		}
		if field.JSON == "" {
			field.JSON = jsonTagFromName(field.Name)
		}
		if strings.EqualFold(field.Type, "datetime") {
			field.Type = "time.Time"
		}
	}
	if entity.Fields[0].Name != expectedID {
		return newRequirementsError(
			fmt.Sprintf("the first field of entity '%s' must be named '%s'", entity.Name, expectedID),
			fmt.Sprintf("Include an ID field first (e.g., %s) so we can reference each record.", expectedID))
	}
	if len(req.Commands) == 0 {
		return newRequirementsError(
			"at least one command must be specified",
			"Choose at least one action (create, list, update, delete) for the plugin.")
	}
	allowedActions := map[string]struct{}{
		"create": {},
		"update": {},
		"delete": {},
		"list":   {},
	}
	for i := range req.Commands {
		cmd := &req.Commands[i]
		cmd.Name = strings.TrimSpace(cmd.Name)
		if cmd.Name == "" {
			return newRequirementsError(
				fmt.Sprintf("command #%d is missing a name", i+1),
				"Give every command a descriptive name (e.g., CreateFuelLog).")
		}
		if !isValidIdentifier(cmd.Name) {
			return newRequirementsError(
				fmt.Sprintf("command name '%s' must be a valid Go identifier", cmd.Name),
				"Command names should be CamelCase (CreateFuelLog, ListEntries, etc.).")
		}
		action := strings.ToLower(strings.TrimSpace(cmd.Action))
		if action == "" {
			return newRequirementsError(
				fmt.Sprintf("command '%s' must specify an action", cmd.Name),
				"Set action to create, update, delete, or list.")
		}
		if _, ok := allowedActions[action]; !ok {
			return newRequirementsError(
				fmt.Sprintf("command '%s' uses unsupported action '%s'", cmd.Name, cmd.Action),
				"Only create, update, delete, and list are supported right now.")
		}
		cmd.Action = action
		for j := range cmd.Input {
			field := &cmd.Input[j]
			field.Name = strings.TrimSpace(field.Name)
			field.Type = strings.TrimSpace(field.Type)
			if field.Name == "" || field.Type == "" {
				return newRequirementsError(
					fmt.Sprintf("command '%s' input field #%d is incomplete", cmd.Name, j+1),
					"Spell out each input field name and type for the command payload.")
			}
			if !isValidIdentifier(field.Name) {
				return newRequirementsError(
					fmt.Sprintf("command '%s' input field '%s' must be a valid Go identifier", cmd.Name, field.Name),
					"Use CamelCase identifiers for command inputs (e.g., Amount, LoggedAt).")
			}
			if field.JSON == "" {
				field.JSON = jsonTagFromName(field.Name)
			}
		}

		switch action {
		case "create":
			hasPayload := false
			for _, field := range cmd.Input {
				if field.Name != expectedID {
					hasPayload = true
					break
				}
			}
			if !hasPayload {
				return newRequirementsError(
					fmt.Sprintf("create command '%s' must include at least one field besides %s", cmd.Name, expectedID),
					"When creating a record, include the attributes to capture (amount, date, etc.).")
			}
		case "update":
			if len(cmd.Input) != len(entity.Fields) {
				return newRequirementsError(
					fmt.Sprintf("update command '%s' must provide all fields for entity '%s'", cmd.Name, entity.Name),
					"Updates must include the ID plus every field so we can overwrite the record.")
			}
			if cmd.Input[0].Name != expectedID {
				return newRequirementsError(
					fmt.Sprintf("update command '%s' must take %s as the first input", cmd.Name, expectedID),
					fmt.Sprintf("Include the %s first so we know which record to update.", expectedID))
			}
		case "delete":
			if len(cmd.Input) != 1 || cmd.Input[0].Name != expectedID {
				return newRequirementsError(
					fmt.Sprintf("delete command '%s' must only take %s", cmd.Name, expectedID),
					fmt.Sprintf("Deletes only need the record ID (e.g., %s).", expectedID))
			}
		case "list":
			if len(cmd.Input) != 0 {
				return newRequirementsError(
					fmt.Sprintf("list command '%s' cannot accept any inputs", cmd.Name),
					"The list action does not take parameters.")
			}
		}
	}
	return nil
}

func jsonSchemaType(goType string) string {
	switch goType {
	case "int", "int64", "uint", "uint64":
		return "integer"
	case "float32", "float64":
		return "number"
	case "bool":
		return "boolean"
	default:
		return "string"
	}
}

func pluginFolderName(name string) string {
	if name == "" {
		return "plugin"
	}
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			if r == '-' {
				b.WriteRune('_')
				continue
			}
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return "plugin"
	}
	return b.String()
}

func jsonTagFromName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "field"
	}
	var builder strings.Builder
	for i, r := range name {
		if unicode.IsUpper(r) {
			if i > 0 {
				builder.WriteRune('_')
			}
			builder.WriteRune(unicode.ToLower(r))
		} else if r == ' ' || r == '-' {
			builder.WriteRune('_')
		} else {
			builder.WriteRune(unicode.ToLower(r))
		}
	}
	tag := builder.String()
	tag = strings.ReplaceAll(tag, "__", "_")
	tag = strings.Trim(tag, "_")
	if tag == "" {
		return "field"
	}
	return tag
}

func isValidIdentifier(name string) bool {
	if name == "" {
		return false
	}
	for i, r := range name {
		if i == 0 && unicode.IsDigit(r) {
			return false
		}
		if !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_') {
			return false
		}
	}
	return true
}
