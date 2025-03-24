package aggregate

import (
	"fmt"
	"mindpalace/internal/chat"
	"mindpalace/pkg/eventsourcing"
	"mindpalace/pkg/logging"
	"regexp"
	"strings"
)

// AggregateManager acts as a facade to manage multiple plugin aggregates.
type AggregateManager struct {
	PluginAggregates map[string]eventsourcing.Aggregate // Map of plugin name to its aggregate
	ChatHistory      []chat.ChatMessage                 // Core still manages chat history
	AllCommands      map[string]eventsourcing.CommandHandler
}

// NewAggregateManager creates a new AggregateManager.
func NewAggregateManager() *AggregateManager {
	return &AggregateManager{
		PluginAggregates: make(map[string]eventsourcing.Aggregate),
		AllCommands:      make(map[string]eventsourcing.CommandHandler),
	}
}

// RegisterPluginAggregate adds a plugin's aggregate to the manager.
func (m *AggregateManager) RegisterPluginAggregate(pluginName string, agg eventsourcing.Aggregate) {
	m.PluginAggregates[pluginName] = agg
	// Merge plugin commands into the global command set
	for cmdName, cmdHandler := range agg.GetAllCommands() {
		m.AllCommands[cmdName] = cmdHandler
	}
	logging.Info("Registered aggregate for plugin: %s", pluginName)
}

// ID returns a generic identifier for the manager (not tied to a single aggregate).
func (m *AggregateManager) ID() string {
	return "system"
}

// GetState aggregates state from all plugin aggregates.
func (m *AggregateManager) GetState() map[string]interface{} {
	state := make(map[string]interface{})
	for pluginName, agg := range m.PluginAggregates {
		state[pluginName] = agg.GetState()
	}
	state["ChatHistory"] = m.ChatHistory
	return state
}

// GetAllCommands returns the combined command handlers from all plugins.
func (m *AggregateManager) GetAllCommands() map[string]eventsourcing.CommandHandler {
	return m.AllCommands
}

// ApplyEvent routes the event to the appropriate plugin aggregate or handles core events.
func (m *AggregateManager) ApplyEvent(event eventsourcing.Event) error {
	eventType := event.Type()

	// Core events (e.g., chat-related) handled directly
	switch eventType {
	case "UserRequestReceived":
		return m.handleUserRequestReceived(event)
	case "ToolCallCompleted":
		return m.handleToolCallCompleted(event)
	case "RequestCompleted":
		return m.handleRequestCompleted(event)
	}

	// Route to plugin aggregate based on event type prefix or metadata
	pluginName := determinePluginName(eventType)
	if pluginName == "" {
		logging.Debug("No plugin identified for event type: %s", eventType)
		return nil // Skip unhandled events
	}

	agg, exists := m.PluginAggregates[pluginName]
	if !exists {
		logging.Debug("No aggregate registered for plugin: %s", pluginName)
		return nil
	}

	err := agg.ApplyEvent(event)
	if err != nil {
		logging.Error("Failed to apply event %s to plugin %s: %v", eventType, pluginName, err)
	}
	return err
}

// Core event handlers
func (m *AggregateManager) handleUserRequestReceived(event eventsourcing.Event) error {
	e, ok := event.(*eventsourcing.UserRequestReceivedEvent)
	if !ok {
		return fmt.Errorf("expected *eventsourcing.UserRequestReceivedEvent for UserRequestReceived")
	}
	m.ChatHistory = append(m.ChatHistory, chat.ChatMessage{
		Role:              "You",
		OllamaRole:        "user",
		Content:           e.RequestText,
		RequestID:         e.RequestID,
		StreamingComplete: true,
	})
	return nil
}

func (m *AggregateManager) handleToolCallCompleted(event eventsourcing.Event) error {
	e, ok := event.(*eventsourcing.ToolCallCompleted)
	if !ok {
		return fmt.Errorf("expected *eventsourcing.UserRequestReceivedEvent for UserRequestReceived")
	}
	m.ChatHistory = append(m.ChatHistory, chat.ChatMessage{
		Role:              "MindPalace",
		OllamaRole:        "none",
		Content:           fmt.Sprintf("%+v", e.Result),
		RequestID:         e.RequestID,
		StreamingComplete: true,
	})
	return nil
}

func (m *AggregateManager) handleRequestCompleted(event eventsourcing.Event) error {
	e, ok := event.(*eventsourcing.GenericEvent)
	if !ok || e.EventType != "RequestCompleted" {
		return fmt.Errorf("expected *eventsourcing.GenericEvent for RequestCompleted")
	}
	respText, _ := e.Data["ResponseText"].(string)
	requestID, _ := e.Data["RequestID"].(string)
	thinks, regular := parseResponseText(respText)
	for _, think := range thinks {
		m.ChatHistory = append(m.ChatHistory, chat.ChatMessage{
			Role:              "Assistant [think]",
			OllamaRole:        "none",
			Content:           think,
			RequestID:         requestID,
			StreamingComplete: true,
		})
	}
	if regular != "" {
		m.ChatHistory = append(m.ChatHistory, chat.ChatMessage{
			Role:              "MindPalace",
			OllamaRole:        "assistant",
			Content:           regular,
			RequestID:         requestID,
			StreamingComplete: true,
		})
	}
	return nil
}

// Helper to determine plugin name from event type (e.g., "taskmanager_TaskCreated" -> "taskmanager")
func determinePluginName(eventType string) string {
	parts := strings.SplitN(eventType, "_", 2)
	if len(parts) > 1 {
		return parts[0] // Assumes plugin prefixes its events
	}
	// Fallback: could use event metadata if available
	return ""
}

// Helper to parse think tags (unchanged)
func parseResponseText(responseText string) (thinks []string, regular string) {
	re := regexp.MustCompile(`(?s)<think>(.*?)</think>`)
	matches := re.FindAllStringSubmatch(responseText, -1)
	for _, match := range matches {
		thinks = append(thinks, match[1])
	}
	regular = re.ReplaceAllString(responseText, "")
	return thinks, strings.TrimSpace(regular)
}
