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
	State            map[string]interface{}                  // General state storage
	ChatHistory      []chat.ChatMessage                      // Conversation history
	PendingToolCalls map[string][]string                     // requestID -> list of tool call IDs
	ToolCallResults  map[string]map[string]interface{}       // requestID -> toolCallID -> result
	AllCommands      map[string]eventsourcing.CommandHandler // Registered command handlers
	ActiveSessions   map[string]bool                         // sessionID -> active status (optional)
}

// ### Public Methods

// GetPendingToolCalls returns the map of pending tool calls.
func (a *AppAggregate) GetPendingToolCalls() map[string][]string {
	return a.PendingToolCalls
}

// GetToolCallResults returns the map of tool call results.
func (a *AppAggregate) GetToolCallResults() map[string]map[string]interface{} {
	return a.ToolCallResults
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

	// Store event data in state (optional, depending on requirements)
	eventData := extractEventData(event)
	if eventData != nil {
		if current, exists := a.State[eventType]; exists {
			if list, ok := current.([]interface{}); ok {
				a.State[eventType] = append(list, eventData)
			} else {
				a.State[eventType] = []interface{}{current, eventData}
			}
		} else {
			a.State[eventType] = []interface{}{eventData}
		}
	}

	return nil
}

// ### Task Event Handling
// ### Event Handlers

type eventHandler func(*AppAggregate, eventsourcing.Event)

var eventHandlers = map[string]eventHandler{
	"TaskCreated":            handleTaskCreated,
	"TaskUpdated":            handleTaskUpdated,
	"TaskCompleted":          handleTaskCompleted,
	"TaskDeleted":            handleTaskDeleted,
	"UserRequestReceived":    handleUserRequestReceived,
	"ToolCallsConfigured":    handleToolCallsConfigured,
	"AllToolCallsCompleted":  handleAllToolCallsCompleted,
	"ToolCallCompleted":      handleToolCallCompleted,
	"TranscriptionStarted":   handleTranscriptionStarted,
	"TranscriptionStopped":   handleTranscriptionStopped,
	"ToolCallInitiated":      handleToolCallInitiated,
	"LLMProcessingCompleted": handleLLMProcessingCompleted,
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
	if a.PendingToolCalls == nil {
		a.PendingToolCalls = make(map[string][]string)
	}
	if a.ToolCallResults == nil {
		a.ToolCallResults = make(map[string]map[string]interface{})
	}
	a.PendingToolCalls[e.RequestID] = []string{}
}

func handleToolCallsConfigured(a *AppAggregate, event eventsourcing.Event) {
	// No additional state updates required beyond storing event data
}

func handleAllToolCallsCompleted(a *AppAggregate, event eventsourcing.Event) {
	// No additional state updates required beyond storing event data
}

func handleToolCallCompleted(a *AppAggregate, event eventsourcing.Event) {
	switch e := event.(type) {
	case *eventsourcing.ToolCallCompleted:
		if a.ToolCallResults[e.RequestID] == nil {
			a.ToolCallResults[e.RequestID] = make(map[string]interface{})
		}
		a.ToolCallResults[e.RequestID][e.ToolCallID] = map[string]interface{}{
			"function": e.Function,
			"result":   e.Result,
		}
		removePendingToolCall(a, e.RequestID, e.ToolCallID)
	case *eventsourcing.GenericEvent:
		if e.EventType == "ToolCallCompleted" {
			requestID, _ := e.Data["RequestID"].(string)
			toolCallID, _ := e.Data["ToolCallID"].(string)
			function, _ := e.Data["Function"].(string)
			result, _ := e.Data["Result"].(map[string]interface{})
			if a.ToolCallResults[requestID] == nil {
				a.ToolCallResults[requestID] = make(map[string]interface{})
			}
			a.ToolCallResults[requestID][toolCallID] = map[string]interface{}{
				"function": function,
				"result":   result,
			}
			removePendingToolCall(a, requestID, toolCallID)
		}
	}
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
		a.PendingToolCalls[requestID] = callIDs
		a.ToolCallResults[requestID] = make(map[string]interface{})
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
		task["Status"] = "Completed"
		if completedAt, ok := e.Data["CompletedAt"]; ok {
			task["CompletedAt"] = completedAt
		}
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

// removePendingToolCall removes a tool call ID from the pending list.
func removePendingToolCall(a *AppAggregate, requestID, toolCallID string) {
	if pending, ok := a.PendingToolCalls[requestID]; ok {
		for i, id := range pending {
			if id == toolCallID {
				a.PendingToolCalls[requestID] = append(pending[:i], pending[i+1:]...)
				break
			}
		}
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
