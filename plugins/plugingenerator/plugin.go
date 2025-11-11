package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

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
		"functions.GeneratePlugin": eventsourcing.NewCommand(func(input *GeneratePluginInput) ([]eventsourcing.Event, error) {
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

func (a *PluginGeneratorAggregate) EmitDelta(event eventsourcing.Event) *eventsourcing.DeltaEnvelope {
	return nil
}

// Reset satisfies the ResettableAggregate interface; no state to clear yet.
func (a *PluginGeneratorAggregate) Reset() {
	// Intentionally left blank; aggregate maintains no in-memory state.
}

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
	Description  string                              `json:"Description"`
	Requirements *plugingenerator.PluginRequirements `json:"Requirements,omitempty"`
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
				"Requirements": map[string]interface{}{
					"type":        "object",
					"description": "Explicit plugin blueprint collected via the front-end interview.",
					"properties": map[string]interface{}{
						"name": map[string]interface{}{
							"type":        "string",
							"description": "The plugin identifier (letters, numbers, underscore).",
						},
						"description": map[string]interface{}{
							"type":        "string",
							"description": "Human-readable summary of the plugin.",
						},
						"entities": map[string]interface{}{
							"type":        "array",
							"description": "List of entity schemas the plugin manages.",
						},
						"commands": map[string]interface{}{
							"type":        "array",
							"description": "Command specifications (create/update/delete/list).",
						},
					},
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

	pg := plugingenerator.NewPluginGenerator()
	var (
		req *plugingenerator.PluginRequirements
		err error
	)
	if input.Requirements != nil {
		req = input.Requirements
	} else {
		req, err = pg.ConductInterview()
		if err != nil {
			return nil, fmt.Errorf("failed to conduct interview: %v", err)
		}
	}

	if err := plugingenerator.ValidateRequirements(req); err != nil {
		var reqErr *plugingenerator.RequirementsError
		if errors.As(err, &reqErr) {
			return nil, fmt.Errorf("plugin blueprint incomplete: %s", reqErr.UserMessage)
		}
		return nil, fmt.Errorf("invalid plugin requirements: %w", err)
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

// Additional Plugin Methods
func (p *PluginGeneratorPlugin) Aggregate() eventsourcing.Aggregate {
	return p.aggregate
}

func (p *PluginGeneratorPlugin) Type() eventsourcing.PluginType {
	return eventsourcing.LLMPlugin
}

func (p *PluginGeneratorPlugin) SystemPrompt() string {
	return `You are PluginGenerator, MindPalace's architect for new plugins.

Before calling GeneratePlugin you must run a short interview (or confirm the answers already exist):
  • Name: a short codename (letters / underscores).
  • Description: one or two sentences about the workflow.
  • Entity: the record this plugin manages (e.g., FuelLog) plus its fields (Amount, LoggedAt, Notes, etc.).
  • Commands: which actions (create, list, update, delete) the user needs.

Ask follow-up questions, propose sensible defaults, and confirm the user is happy. If they prefer a guided flow, suggest the “Forge Plugin” button inside the Control Matrix UI.

Only after the blueprint is complete should you call GeneratePlugin with a JSON payload like:
{"Description":"track diesel refills","Requirements":{"name":"dieseltracker","description":"Track diesel refills","entities":[...],"commands":[...]}}

If validation fails, explain what is missing and gather it from the user before trying again.`
}

func (p *PluginGeneratorPlugin) AgentModel() string {
	return "gpt-oss:20b"
}

// Description returns a short description of how the orchestrator AI can use this agent
func (p *PluginGeneratorPlugin) Description() string {
	return "use this to generate new plugins, talk to me in natural language about what kind of plugin you want to create."
}

func (p *PluginGeneratorPlugin) Metadata() eventsourcing.PluginMetadata {
	return eventsourcing.PluginMetadata{
		Name:      p.Name(),
		Summary:   "Translates rough requirements into fully scaffolded MindPalace plugins.",
		UsageHint: "Invoke when an unmet workflow needs a dedicated agent or integration.",
		Capabilities: []eventsourcing.PluginCapability{
			{Name: "GeneratePlugin", Description: "Conduct an interview and emit events to scaffold a new plugin."},
		},
		Examples: []string{
			"Create a plugin that tracks diesel refills and calculates cost per kilometre.",
			"Spin up an agent to catalog campsite reviews with photos and tags."},
		Tags:           []string{"meta", "builder", "generator"},
		Maintainer:     "MindPalace Core Team",
		DefaultTimeout: 35 * time.Second,
		Safety:         "needs_review",
		Reliability:    "experimental",
		Lifecycle:      "active",
		ModelAsset:     "591245",
	}
}

func (p *PluginGeneratorPlugin) EventHandlers() map[string]eventsourcing.EventHandler {
	return nil
}
