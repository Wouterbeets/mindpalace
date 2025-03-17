package core

import (
	"fmt"
	"mindpalace/pkg/eventsourcing"
	"mindpalace/pkg/llmmodels"
	"regexp"
	"strings"
)

// AppAggregate is the main aggregate for the application state
type AppAggregate struct {
	State            map[string]interface{}
	ChatHistory      []ChatMessage
	PendingToolCalls map[string][]string                     // requestID -> list of tool call IDs
	ToolCallResults  map[string]map[string]interface{}       // requestID -> toolCallID -> result
	AllCommands      map[string]eventsourcing.CommandHandler // Store all registered commands
}

// GetPendingToolCalls returns the pending tool calls
func (a *AppAggregate) GetPendingToolCalls() map[string][]string {
	return a.PendingToolCalls
}

// GetToolCallResults returns the tool call results
func (a *AppAggregate) GetToolCallResults() map[string]map[string]interface{} {
	return a.ToolCallResults
}

func (a *AppAggregate) ApplyEvent(event eventsourcing.Event) error {
	// Update state based on event type
	eventType := event.Type()
	
	// Handle standard events by storing them in the state
	// Convert the event to a map for state storage
	var eventData map[string]interface{}
	
	// Handle each concrete event type
	switch e := event.(type) {
	case *eventsourcing.UserRequestReceivedEvent:
		// Store in state with appropriate structure
		eventData = map[string]interface{}{
			"RequestID":   e.RequestID,
			"RequestText": e.RequestText,
			"Timestamp":   e.Timestamp,
		}
		
		// Update chat history
		a.ChatHistory = append(a.ChatHistory, ChatMessage{
			Role:              "You",
			Content:           e.RequestText,
			RequestID:         e.RequestID,
			StreamingComplete: true, // User messages are complete on arrival
		})
		
		// Initialize tracking maps if needed
		if a.PendingToolCalls == nil {
			a.PendingToolCalls = make(map[string][]string)
		}
		if a.ToolCallResults == nil {
			a.ToolCallResults = make(map[string]map[string]interface{})
		}
		a.PendingToolCalls[e.RequestID] = []string{} // Initialize for this request
		
	case *eventsourcing.ToolCallsConfiguredEvent:
		// Store typed event data in state
		eventData = map[string]interface{}{
			"RequestID":   e.RequestID,
			"RequestText": e.RequestText,
			"Tools":       e.Tools,
		}
		
	case *eventsourcing.AllToolCallsCompletedEvent:
		// Store typed event data in state
		eventData = map[string]interface{}{
			"RequestID": e.RequestID,
			"Results":   e.Results,
		}
		
	case *eventsourcing.GenericEvent:
		// For backward compatibility with existing events
		eventData = e.Data
		
		// Process specific generic events based on type
		switch e.EventType {
		case "LLMProcessingCompleted":
			respText, _ := e.Data["ResponseText"].(string)
			requestID, _ := e.Data["RequestID"].(string)
			toolCalls, _ := e.Data["ToolCalls"].([]llmmodels.OllamaToolCall)
			thinks, regular := parseResponseText(respText)

			// Check if we already have a streaming message for this request
			messageUpdated := false
			for i, msg := range a.ChatHistory {
				// If we find an existing message for this request, update it
				// Removed !msg.StreamingComplete check to prevent duplicate messages
				if msg.RequestID == requestID && msg.Role == "MindPalace" {
					// Update with final content
					a.ChatHistory[i].Content = regular
					a.ChatHistory[i].StreamingComplete = true
					messageUpdated = true
					break
				}
			}

			// If no streaming message was found or updated, add the completed message
			if !messageUpdated {
				// Add thinks first if any
				for _, think := range thinks {
					a.ChatHistory = append(a.ChatHistory, ChatMessage{
						Role:              "Assistant [think]",
						Content:           think,
						RequestID:         requestID,
						StreamingComplete: true,
					})
				}

				// Then add the regular content
				if regular != "" {
					a.ChatHistory = append(a.ChatHistory, ChatMessage{
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
		case "ToolCallCompleted":
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
			// Remove from pending tool calls
			if pending, ok := a.PendingToolCalls[requestID]; ok {
				for i, id := range pending {
					if id == toolCallID {
						a.PendingToolCalls[requestID] = append(pending[:i], pending[i+1:]...)
						break
					}
				}
			}
		}
	default:
		return fmt.Errorf("unknown event type: %s", eventType)
	}
	
	// Store event data in state (common for all event types)
	if current, exists := a.State[eventType]; exists {
		if list, ok := current.([]interface{}); ok {
			a.State[eventType] = append(list, eventData)
		} else {
			a.State[eventType] = []interface{}{current, eventData}
		}
	} else {
		a.State[eventType] = []interface{}{eventData}
	}
	
	return nil
}
func (a *AppAggregate) ID() string {
	return "global"
}

func (a *AppAggregate) GetState() map[string]interface{} {
	return a.State
}

// Implement CommandProvider interface
func (a *AppAggregate) GetAllCommands() map[string]eventsourcing.CommandHandler {
	return a.AllCommands
}

// parseResponseText extracts think tags and regular text (moved from app.go)
func parseResponseText(responseText string) (thinks []string, regular string) {
	re := regexp.MustCompile(`(?s)<think>(.*?)</think>`)
	matches := re.FindAllStringSubmatch(responseText, -1)
	for _, match := range matches {
		thinks = append(thinks, match[1])
	}
	regular = re.ReplaceAllString(responseText, "")
	return thinks, strings.TrimSpace(regular)
}
