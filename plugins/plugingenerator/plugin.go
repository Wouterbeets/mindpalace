package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"fyne.io/fyne/v2"
	"mindpalace/internal/plugingenerator"
	"mindpalace/pkg/eventsourcing"
	"mindpalace/pkg/logging"
)

// PluginGeneratorPlugin implements the plugin interface for generating other plugins
type PluginGeneratorPlugin struct {
	aggregate *PluginGeneratorAggregate
}

func NewPlugin() eventsourcing.Plugin {
	agg := NewPluginGeneratorAggregate()
	p := &PluginGeneratorPlugin{aggregate: agg}
	agg.commands = map[string]eventsourcing.CommandHandler{
		"GeneratePlugin": eventsourcing.NewCommand(func(input *GeneratePluginInput) ([]eventsourcing.Event, error) {
			return p.generatePluginHandler(input)
		}),
	}
	eventsourcing.RegisterEvent("plugingenerator_PluginGenerated", func() eventsourcing.Event { return &PluginGeneratedEvent{} })
	return p
}

// PluginGeneratorAggregate manages the state
type PluginGeneratorAggregate struct {
	commands map[string]eventsourcing.CommandHandler
	Mu       sync.RWMutex
}

func NewPluginGeneratorAggregate() *PluginGeneratorAggregate {
	return &PluginGeneratorAggregate{
		commands: make(map[string]eventsourcing.CommandHandler),
	}
}

func (a *PluginGeneratorAggregate) ID() string {
	return "plugingenerator"
}

func (a *PluginGeneratorAggregate) ApplyEvent(event eventsourcing.Event) error {
	// No state to manage for now
	return nil
}

func (a *PluginGeneratorAggregate) GetCustomUI() fyne.CanvasObject {
	return nil // No UI for this plugin
}

// Commands returns the command handlers
func (p *PluginGeneratorPlugin) Commands() map[string]eventsourcing.CommandHandler {
	return p.aggregate.commands
}

// Name returns the plugin name
func (p *PluginGeneratorPlugin) Name() string {
	return "plugingenerator"
}

// Schemas defines the command schemas
func (p *PluginGeneratorPlugin) Schemas() map[string]eventsourcing.CommandInput {
	return map[string]eventsourcing.CommandInput{
		"GeneratePlugin": &GeneratePluginInput{},
	}
}

// Command Input Structs

func (i *GeneratePluginInput) New() any {
	return &GeneratePluginInput{}
}

// GeneratePluginInput defines the input for generating a plugin
type GeneratePluginInput struct {
	Description string `json:"Description"`
}

func (i *GeneratePluginInput) Schema() map[string]interface{} {
	return map[string]interface{}{
		"description": "Generate a new plugin based on description",
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"Description": map[string]interface{}{
					"type":        "string",
					"description": "Description of the plugin to generate",
				},
			},
			"required": []string{"Description"},
		},
	}
}

// Event Types
type PluginGeneratedEvent struct {
	EventType   string `json:"event_type"`
	PluginName  string `json:"plugin_name"`
	Description string `json:"description"`
}

func (e *PluginGeneratedEvent) Type() string { return "plugingenerator_PluginGenerated" }
func (e *PluginGeneratedEvent) Marshal() ([]byte, error) {
	e.EventType = e.Type()
	return json.Marshal(e)
}
func (e *PluginGeneratedEvent) Unmarshal(data []byte) error { return json.Unmarshal(data, e) }

// Command Handlers
func (p *PluginGeneratorPlugin) generatePluginHandler(input *GeneratePluginInput) ([]eventsourcing.Event, error) {
	logging.Info("Generating plugin with description: %s", input.Description)

	// For now, hardcode to drinking tracker if description contains "drink"
	if strings.Contains(strings.ToLower(input.Description), "drink") {
		pg := plugingenerator.NewPluginGenerator()
		req, err := pg.ConductInterview()
		if err != nil {
			return nil, fmt.Errorf("failed to conduct interview: %v", err)
		}
		if err := pg.GeneratePlugin(req); err != nil {
			return nil, fmt.Errorf("failed to generate plugin: %v", err)
		}
		event := &PluginGeneratedEvent{
			EventType:   "plugingenerator_PluginGenerated",
			PluginName:  req.Name,
			Description: input.Description,
		}
		return []eventsourcing.Event{event}, nil
	}

	return nil, fmt.Errorf("plugin generation not supported for this description")
}

// Additional Plugin Methods
func (p *PluginGeneratorPlugin) Aggregate() eventsourcing.Aggregate {
	return p.aggregate
}

func (p *PluginGeneratorPlugin) Type() eventsourcing.PluginType {
	return eventsourcing.LLMPlugin
}

func (p *PluginGeneratorPlugin) SystemPrompt() string {
	return `You are PluginGenerator, an AI that helps create new plugins for MindPalace.

The user input will be a JSON object containing the arguments for the command. For example, {"Description": "track daily drinking habits"}.

Parse the JSON and use the GeneratePlugin command with the parsed description.`
}

func (p *PluginGeneratorPlugin) AgentModel() string {
	return "gpt-oss:20b"
}

func (p *PluginGeneratorPlugin) EventHandlers() map[string]eventsourcing.EventHandler {
	return nil
}
