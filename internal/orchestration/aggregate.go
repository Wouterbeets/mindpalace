package orchestration

import (
	"encoding/json"
	"fmt"
	"mindpalace/internal/chat"
	"mindpalace/pkg/eventsourcing"
	"regexp"
	"strings"
)

// OrchestrationAggregate manages orchestration-specific state and events.
type OrchestrationAggregate struct {
	ChatHistory []chat.ChatMessage
	commands    map[string]eventsourcing.CommandHandler
}

// NewOrchestrationAggregate initializes a new OrchestrationAggregate.
func NewOrchestrationAggregate() *OrchestrationAggregate {
	return &OrchestrationAggregate{
		commands: make(map[string]eventsourcing.CommandHandler),
	}
}

// ID returns the aggregate's identifier.
func (a *OrchestrationAggregate) ID() string {
	return "orchestration"
}

// GetState returns the current state of the orchestration aggregate.
func (a *OrchestrationAggregate) GetState() map[string]interface{} {
	return map[string]interface{}{
		"ChatHistory": a.ChatHistory,
	}
}

// GetAllCommands returns the registered command handlers.
func (a *OrchestrationAggregate) GetAllCommands() map[string]eventsourcing.CommandHandler {
	return a.commands
}

// ApplyEvent applies an event to update the aggregate's state.
func (a *OrchestrationAggregate) ApplyEvent(event eventsourcing.Event) error {
	switch event.Type() {
	case "UserRequestReceived":
		return a.handleUserRequestReceived(event) // take concrete types
	case "ToolCallCompleted":
		return a.handleToolCallCompleted(event)
	case "RequestCompleted":
		return a.handleRequestCompleted(event)
	default:
		return fmt.Errorf("unknown event type for orchestration: %s", event.Type())
	}
}

// handleUserRequestReceived adds a user request to the chat history.
func (a *OrchestrationAggregate) handleUserRequestReceived(event eventsourcing.Event) error {
	e, ok := event.(*UserRequestReceivedEvent)
	if !ok {
		return fmt.Errorf("expected *eventsourcing.UserRequestReceivedEvent")
	}
	a.ChatHistory = append(a.ChatHistory, chat.ChatMessage{
		Role:              "You",
		OllamaRole:        "user",
		Content:           e.RequestText,
		RequestID:         e.RequestID,
		StreamingComplete: true,
	})
	return nil
}

// handleToolCallCompleted adds a tool call result to the chat history.
func (a *OrchestrationAggregate) handleToolCallCompleted(event eventsourcing.Event) error {
	e, ok := event.(*ToolCallCompleted)
	if !ok {
		return fmt.Errorf("expected ToolCallCompleted event type")
	}
	a.ChatHistory = append(a.ChatHistory, chat.ChatMessage{
		Role:              "MindPalace",
		OllamaRole:        "none",
		Content:           fmt.Sprintf("%+v", e.Result),
		RequestID:         e.RequestID,
		StreamingComplete: true,
	})
	return nil
}

type RequestCompletedEvent struct {
	eventsourcing.BaseEvent
	RequestID    string
	ResponseText string
}

func (e *RequestCompletedEvent) Type() string { return "RequestCompleted" }

// handleRequestCompleted processes the LLM response and updates the chat history.
func (a *OrchestrationAggregate) handleRequestCompleted(event eventsourcing.Event) error {
	e, ok := event.(*RequestCompletedEvent)
	if !ok {
		return fmt.Errorf("expected RequestCompleted event type")
	}
	thinks, regular := parseResponseText(e.ResponseText)
	for _, think := range thinks {
		a.ChatHistory = append(a.ChatHistory, chat.ChatMessage{
			Role:              "Assistant [think]",
			OllamaRole:        "none",
			Content:           think,
			RequestID:         e.RequestID,
			StreamingComplete: true,
		})
	}
	if regular != "" {
		a.ChatHistory = append(a.ChatHistory, chat.ChatMessage{
			Role:              "MindPalace",
			OllamaRole:        "assistant",
			Content:           regular,
			RequestID:         e.RequestID,
			StreamingComplete: true,
		})
	}
	return nil
}

// parseResponseText extracts <think> tags and regular content from the response.
func parseResponseText(responseText string) (thinks []string, regular string) {
	re := regexp.MustCompile(`(?s)<think>(.*?)</think>`)
	matches := re.FindAllStringSubmatch(responseText, -1)
	for _, match := range matches {
		thinks = append(thinks, match[1])
	}
	regular = re.ReplaceAllString(responseText, "")
	return thinks, strings.TrimSpace(regular)
}

// UserRequestReceivedEvent is a strongly typed event for when a user request is received
type UserRequestReceivedEvent struct {
	EventType   string `json:"event_type"`
	RequestID   string `json:"request_id"`
	RequestText string `json:"request_text"`
	Timestamp   string `json:"timestamp"`
}

func (e *UserRequestReceivedEvent) Type() string {
	return "orchestation_UserRequestReceived"
}

func (e *UserRequestReceivedEvent) Marshal() ([]byte, error) {
	e.EventType = e.Type()
	return json.Marshal(e)
}

func (e *UserRequestReceivedEvent) Unmarshal(data []byte) error {
	return json.Unmarshal(data, e)
}

type ToolCallCompleted struct {
	RequestID  string                 `json:"request_id"`
	ToolCallID string                 `json:"tool_call_id"`
	Function   string                 `json:"function"`
	Result     map[string]interface{} `json:"result"`
	EventType  string                 `json:"event_type"`
}

func (e *ToolCallCompleted) Type() string {
	return "orchestation_ToolCallCompleted"
}

func (e *ToolCallCompleted) Marshal() ([]byte, error) {
	e.EventType = e.Type()
	return json.Marshal(e)
}

func (e *ToolCallCompleted) Unmarshal(data []byte) error {
	return json.Unmarshal(data, e)
}

type InitiatePluginCreationEvent struct {
	EventType   string `json:"event_type"`
	RequestID   string `json:"request_id"`
	PluginName  string `json:"plugin_name"`
	Description string `json:"description"`
	Goal        string `json:"goal"`
	Result      string `json:"result"`
}

func (e *InitiatePluginCreationEvent) Type() string { return "orchestation_InitiatePluginCreation" }
func (e *InitiatePluginCreationEvent) Marshal() ([]byte, error) {
	e.EventType = e.Type()
	return json.Marshal(e)
}
func (e *InitiatePluginCreationEvent) Unmarshal(data []byte) error { return json.Unmarshal(data, e) }

func init() {
	eventsourcing.RegisterEvent("UserRequestReceived", func() eventsourcing.Event { return &UserRequestReceivedEvent{} })
	eventsourcing.RegisterEvent("ToolCallCompleted", func() eventsourcing.Event { return &ToolCallCompleted{} })
	eventsourcing.RegisterEvent("InitiatePluginCreation", func() eventsourcing.Event { return &InitiatePluginCreationEvent{} })
	eventsourcing.RegisterEvent("RequestCompleted", func() eventsourcing.Event { return &RequestCompletedEvent{} })
}
