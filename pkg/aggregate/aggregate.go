package aggregate

import (
	"fmt"
	"mindpalace/internal/chat"
	"mindpalace/pkg/eventsourcing"
	"mindpalace/pkg/llmmodels"
	"mindpalace/pkg/logging"
	"regexp"
	"strings"
)

// AppAggregate is the main aggregate for managing the application state.
type AppAggregate struct {
	State          map[string]interface{}                  // General state storage
	ChatHistory    []chat.ChatMessage                      // Conversation history
	AllCommands    map[string]eventsourcing.CommandHandler // Registered command handlers
	ActiveSessions map[string]bool                         // sessionID -> active status (optional)
}

// ID returns the aggregate's identifier.
func (a *AppAggregate) ID() string {
	return "global"
}

// GetState returns the current state of the aggregate.
func (a *AppAggregate) GetState() map[string]interface{} {
	return a.State
}

// GetAllCommands returns all registered command handlers.
func (a *AppAggregate) GetAllCommands() map[string]eventsourcing.CommandHandler {
	return a.AllCommands
}

// ### Event Application

// ApplyEvent updates the aggregate state based on the given event.
func (a *AppAggregate) ApplyEvent(event eventsourcing.Event) error {
	// Initialize state if nil
	if a.State == nil {
		a.State = make(map[string]interface{})
	}

	// Ensure tasks map exists
	if _, exists := a.State["tasks"]; !exists {
		a.State["tasks"] = make(map[string]map[string]interface{})
	}

	// Look up and call the handler
	eventType := event.Type()
	handler, exists := eventHandlers[eventType]
	if !exists {
		return fmt.Errorf("unknown event type: %s", eventType)
	}
	handler(a, event)
	return nil
}

// ### Task Event Handling
// ### Event Handlers

type eventHandler func(*AppAggregate, eventsourcing.Event)

var eventHandlers = map[string]eventHandler{
	"UserRequestReceived":  handleUserRequestReceived,
	"RequestCompleted":     handleRequestCompleted,
	"TaskCreated":          handleTaskCreated,
	"TaskUpdated":          handleTaskUpdated,
	"TaskCompleted":        handleTaskCompleted,
	"TaskDeleted":          handleTaskDeleted,
	"TranscriptionStarted": handleTranscriptionStarted,
	"TranscriptionStopped": handleTranscriptionStopped,
}

func handleRequestCompleted(a *AppAggregate, event eventsourcing.Event) {
	e, ok := event.(*eventsourcing.GenericEvent)
	if !ok || e.EventType != "RequestCompleted" {
		return
	}
	respText, _ := e.Data["ResponseText"].(string)
	requestID, _ := e.Data["RequestID"].(string)
	thinks, regular := parseResponseText(respText)
	for _, think := range thinks {
		a.ChatHistory = append(a.ChatHistory, chat.ChatMessage{Role: "Assistant [think]", Content: think, RequestID: requestID, StreamingComplete: true})
	}
	if regular != "" {
		a.ChatHistory = append(a.ChatHistory, chat.ChatMessage{Role: "MindPalace", Content: regular, RequestID: requestID, StreamingComplete: true})
	}
}

func handleUserRequestReceived(a *AppAggregate, event eventsourcing.Event) {
	e, ok := event.(*eventsourcing.UserRequestReceivedEvent)
	if !ok {
		logging.Error("Expected *eventsourcing.UserRequestReceivedEvent for UserRequestReceived")
		return
	}
	a.ChatHistory = append(a.ChatHistory, chat.ChatMessage{
		Role:              "You",
		Content:           e.RequestText,
		RequestID:         e.RequestID,
		StreamingComplete: true,
	})
}

func handleToolCallsConfigured(a *AppAggregate, event eventsourcing.Event) {
	// No additional state updates required beyond storing event data
}

func handleAllToolCallsCompleted(a *AppAggregate, event eventsourcing.Event) {
	// No additional state updates required beyond storing event data
}

func handleTranscriptionStarted(a *AppAggregate, event eventsourcing.Event) {
	e, ok := event.(*eventsourcing.GenericEvent)
	if !ok || e.EventType != "TranscriptionStarted" {
		logging.Error("Expected *eventsourcing.GenericEvent with EventType TranscriptionStarted")
		return
	}
	if a.ActiveSessions == nil {
		a.ActiveSessions = make(map[string]bool)
	}
	sessionID, _ := e.Data["SessionID"].(string)
	a.ActiveSessions[sessionID] = true
}

func handleTranscriptionStopped(a *AppAggregate, event eventsourcing.Event) {
	e, ok := event.(*eventsourcing.GenericEvent)
	if !ok || e.EventType != "TranscriptionStopped" {
		logging.Error("Expected *eventsourcing.GenericEvent with EventType TranscriptionStopped")
		return
	}
	sessionID, _ := e.Data["SessionID"].(string)
	if a.ActiveSessions != nil {
		delete(a.ActiveSessions, sessionID)
	}
}

func handleToolCallInitiated(a *AppAggregate, event eventsourcing.Event) {
	// No additional state updates required beyond storing event data
}

func handleLLMProcessingCompleted(a *AppAggregate, event eventsourcing.Event) {
	e, ok := event.(*eventsourcing.GenericEvent)
	if !ok || e.EventType != "LLMProcessingCompleted" {
		logging.Error("Expected *eventsourcing.GenericEvent with EventType LLMProcessingCompleted")
		return
	}
	respText, _ := e.Data["ResponseText"].(string)
	requestID, _ := e.Data["RequestID"].(string)
	toolCalls, _ := e.Data["ToolCalls"].([]llmmodels.OllamaToolCall)
	thinks, regular := parseResponseText(respText)

	// Update or append chat history
	messageUpdated := false
	for i, msg := range a.ChatHistory {
		if msg.RequestID == requestID && msg.Role == "MindPalace" {
			a.ChatHistory[i].Content = regular
			a.ChatHistory[i].StreamingComplete = true
			messageUpdated = true
			break
		}
	}
	if !messageUpdated {
		for _, think := range thinks {
			a.ChatHistory = append(a.ChatHistory, chat.ChatMessage{
				Role:              "Assistant [think]",
				Content:           think,
				RequestID:         requestID,
				StreamingComplete: true,
			})
		}
		if regular != "" {
			a.ChatHistory = append(a.ChatHistory, chat.ChatMessage{
				Role:              "MindPalace",
				Content:           regular,
				RequestID:         requestID,
				StreamingComplete: true,
			})
		}
	}

	// Handle tool calls
	if len(toolCalls) > 0 {
		callIDs := make([]string, len(toolCalls))
		for i := range toolCalls {
			callIDs[i] = fmt.Sprintf("%s-%d", requestID, i)
		}
	}
}

func handleTaskCreated(a *AppAggregate, event eventsourcing.Event) {
	e, ok := event.(*eventsourcing.GenericEvent)
	if !ok || e.EventType != "TaskCreated" {
		logging.Error("Expected *eventsourcing.GenericEvent with EventType TaskCreated")
		return
	}
	tasks := a.State["tasks"].(map[string]map[string]interface{})
	taskID, _ := e.Data["TaskID"].(string)
	tasks[taskID] = make(map[string]interface{})
	for k, v := range e.Data {
		tasks[taskID][k] = v
	}
}

func handleTaskUpdated(a *AppAggregate, event eventsourcing.Event) {
	e, ok := event.(*eventsourcing.GenericEvent)
	if !ok || e.EventType != "TaskUpdated" {
		logging.Error("Expected *eventsourcing.GenericEvent with EventType TaskUpdated")
		return
	}
	tasks := a.State["tasks"].(map[string]map[string]interface{})
	taskID, _ := e.Data["TaskID"].(string)
	if task, exists := tasks[taskID]; exists {
		for k, v := range e.Data {
			if k != "TaskID" {
				task[k] = v
			}
		}
	}
}

func handleTaskCompleted(a *AppAggregate, event eventsourcing.Event) {
	e, ok := event.(*eventsourcing.GenericEvent)
	if !ok || e.EventType != "TaskCompleted" {
		logging.Error("Expected *eventsourcing.GenericEvent with EventType TaskCompleted")
		return
	}
	tasks := a.State["tasks"].(map[string]map[string]interface{})
	taskID, _ := e.Data["TaskID"].(string)
	if task, exists := tasks[taskID]; exists {
		// Update task with all event data
		for k, v := range e.Data {
			if k != "TaskID" {
				task[k] = v
			}
		}
		// Ensure Status is set to "Completed"
		task["Status"] = "Completed"
	}
}

func handleTaskDeleted(a *AppAggregate, event eventsourcing.Event) {
	e, ok := event.(*eventsourcing.GenericEvent)
	if !ok || e.EventType != "TaskDeleted" {
		logging.Error("Expected *eventsourcing.GenericEvent with EventType TaskDeleted")
		return
	}
	tasks := a.State["tasks"].(map[string]map[string]interface{})
	taskID, _ := e.Data["TaskID"].(string)
	delete(tasks, taskID)
}

// ### Helper Functions

// extractEventData converts an event into a map for state storage.
func extractEventData(event eventsourcing.Event) map[string]interface{} {
	switch e := event.(type) {
	case *eventsourcing.UserRequestReceivedEvent:
		return map[string]interface{}{
			"RequestID":   e.RequestID,
			"RequestText": e.RequestText,
			"Timestamp":   e.Timestamp,
		}
	case *eventsourcing.ToolCallsConfiguredEvent:
		return map[string]interface{}{
			"RequestID":   e.RequestID,
			"RequestText": e.RequestText,
			"Tools":       e.Tools,
		}
	case *eventsourcing.AllToolCallsCompletedEvent:
		return map[string]interface{}{
			"RequestID": e.RequestID,
			"Results":   e.Results,
		}
	case *eventsourcing.ToolCallCompleted:
		return map[string]interface{}{
			"RequestID":  e.RequestID,
			"ToolCallID": e.ToolCallID,
			"Function":   e.Function,
			"Result":     e.Result,
		}
	case *eventsourcing.GenericEvent:
		return e.Data
	default:
		return nil
	}
}

// parseResponseText extracts think tags and regular text from a response.
func parseResponseText(responseText string) (thinks []string, regular string) {
	re := regexp.MustCompile(`(?s)<think>(.*?)</think>`)
	matches := re.FindAllStringSubmatch(responseText, -1)
	for _, match := range matches {
		thinks = append(thinks, match[1])
	}
	regular = re.ReplaceAllString(responseText, "")
	return thinks, strings.TrimSpace(regular)
}
