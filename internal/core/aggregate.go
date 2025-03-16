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
	genericEvent, ok := event.(*eventsourcing.GenericEvent)
	if !ok {
		return fmt.Errorf("event is not a GenericEvent")
	}
	// Existing state and ChatHistory logic...
	key := genericEvent.EventType
	if current, exists := a.State[key]; exists {
		if list, ok := current.([]interface{}); ok {
			a.State[key] = append(list, genericEvent.Data)
		} else {
			a.State[key] = []interface{}{current, genericEvent.Data}
		}
	} else {
		a.State[key] = []interface{}{genericEvent.Data}
	}

	switch genericEvent.EventType {
	case "UserRequestReceived":
		reqText, _ := genericEvent.Data["RequestText"].(string)
		requestID, _ := genericEvent.Data["RequestID"].(string)
		a.ChatHistory = append(a.ChatHistory, ChatMessage{
			Role:              "You",
			Content:           reqText,
			RequestID:         requestID,
			StreamingComplete: true, // User messages are complete on arrival
		})
		if a.PendingToolCalls == nil {
			a.PendingToolCalls = make(map[string][]string)
		}
		if a.ToolCallResults == nil {
			a.ToolCallResults = make(map[string]map[string]interface{})
		}
		a.PendingToolCalls[requestID] = []string{} // Initialize for this request
	case "LLMProcessingCompleted":
		respText, _ := genericEvent.Data["ResponseText"].(string)
		requestID, _ := genericEvent.Data["RequestID"].(string)
		toolCalls, _ := genericEvent.Data["ToolCalls"].([]llmmodels.OllamaToolCall)
		thinks, regular := parseResponseText(respText)

		// Check if we already have a streaming message for this request
		messageUpdated := false
		for i, msg := range a.ChatHistory {
			// If we find an existing streaming message for this request, update it
			if msg.RequestID == requestID && msg.Role == "MindPalace" && !msg.StreamingComplete {
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
		genericEvent, ok := event.(*eventsourcing.GenericEvent)
		if !ok {
			return fmt.Errorf("event is not a GenericEvent")
		}
		requestID, _ := genericEvent.Data["RequestID"].(string)
		toolCallID, _ := genericEvent.Data["ToolCallID"].(string)
		function, _ := genericEvent.Data["Function"].(string)
		result, _ := genericEvent.Data["Result"].(map[string]interface{})
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
