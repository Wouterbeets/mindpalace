package orchestration

import (
	"encoding/json"
	"fmt"
	"mindpalace/pkg/aggregate"
	"mindpalace/pkg/eventsourcing"
	"mindpalace/pkg/llmmodels"
	"time"
)

// NewToolCallConfigFunc creates an event handler that configures tool calls for LLM
func NewToolCallConfigFunc(commands map[string]eventsourcing.CommandHandler, agg eventsourcing.Aggregate, plugins []eventsourcing.Plugin) eventsourcing.EventHandler {
	return func(event eventsourcing.Event, state map[string]interface{}, commands map[string]eventsourcing.CommandHandler) ([]eventsourcing.Event, error) {
		var requestText string
		var requestID string

		// Handle different event types with a type switch
		switch e := event.(type) {
		case *eventsourcing.UserRequestReceivedEvent:
			// Extract data from the concrete event type
			requestText = e.RequestText
			requestID = e.RequestID

		default:
			return nil, fmt.Errorf("unexpected event type: %s", event.Type())
		}

		// Generate a request ID if none exists
		if requestID == "" {
			requestID = fmt.Sprintf("req-%d", time.Now().UnixNano())
		}

		// Collect available tools from plugins
		var availableTools []llmmodels.Tool
		for _, plugin := range plugins {
			for name, schema := range plugin.Schemas() {
				availableTools = append(availableTools, llmmodels.Tool{
					Type: "function",
					Function: map[string]interface{}{
						"name":        name,
						"description": schema["description"],
						"parameters":  schema["parameters"],
					},
				})
			}
		}

		fmt.Println("configuring tools and returning event")
		// Return strongly typed event
		return []eventsourcing.Event{
			&eventsourcing.ToolCallsConfiguredEvent{
				RequestID:   requestID,
				RequestText: requestText,
				Tools:       availableTools,
			},
		}, nil
	}
}

// NewToolCallFunc creates an event handler that processes LLM tool call requests
func NewToolCallFunc(ep *eventsourcing.EventProcessor, agg eventsourcing.Aggregate, plugins []eventsourcing.Plugin) eventsourcing.EventHandler {
	return func(event eventsourcing.Event, state map[string]interface{}, commands map[string]eventsourcing.CommandHandler) ([]eventsourcing.Event, error) {
		var toolCalls []llmmodels.OllamaToolCall
		var requestID string

		// Extract data from different event types
		switch e := event.(type) {
		case *eventsourcing.GenericEvent:
			// Backward compatibility with GenericEvent
			toolCalls, _ = e.Data["ToolCalls"].([]llmmodels.OllamaToolCall)
			requestID, _ = e.Data["RequestID"].(string)
		default:
			// Try to extract from the event data directly
			type EventData struct {
				RequestID string                     `json:"request_id"`
				ToolCalls []llmmodels.OllamaToolCall `json:"tool_calls"`
			}

			var data EventData
			marshaledData, err := event.Marshal()
			if err == nil {
				if err := json.Unmarshal(marshaledData, &data); err == nil {
					requestID = data.RequestID
					toolCalls = data.ToolCalls
				}
			}
		}

		if len(toolCalls) == 0 {
			return nil, nil
		}

		// Create events for each tool call
		var events []eventsourcing.Event
		for i, call := range toolCalls {
			toolCallID := fmt.Sprintf("%s-%d", requestID, i)
			events = append(events, &eventsourcing.ToolCallInitiatedEvent{
				RequestID:  requestID,
				ToolCallID: toolCallID,
				Function:   call.Function.Name,
				Arguments:  call.Function.Arguments,
			})
		}
		return events, nil
	}
}

// NewToolCallExecutor creates an event handler that executes tool calls
func NewToolCallExecutor(ep *eventsourcing.EventProcessor) eventsourcing.EventHandler {
	return func(event eventsourcing.Event, state map[string]interface{}, commands map[string]eventsourcing.CommandHandler) ([]eventsourcing.Event, error) {
		var toolCallID, function, requestID string
		var args map[string]interface{}

		// Extract data from different event types
		switch e := event.(type) {
		case *eventsourcing.ToolCallInitiatedEvent:
			// Get data directly from the strongly typed event
			toolCallID = e.ToolCallID
			function = e.Function
			requestID = e.RequestID
			args = e.Arguments

		case *eventsourcing.GenericEvent:
			// Backward compatibility with GenericEvent
			toolCallID, _ = e.Data["ToolCallID"].(string)
			function, _ = e.Data["Function"].(string)
			requestID, _ = e.Data["RequestID"].(string)
			args, _ = e.Data["Arguments"].(map[string]interface{})

		default:
			return nil, fmt.Errorf("unsupported event type for tool execution: %s", event.Type())
		}

		// Ensure we have the required data
		if toolCallID == "" || function == "" || requestID == "" {
			return nil, fmt.Errorf("missing required data for tool execution")
		}

		// Augment args with RequestID and ToolCallID
		execArgs := make(map[string]interface{})
		if args != nil {
			for k, v := range args {
				execArgs[k] = v
			}
		}
		execArgs["RequestID"] = requestID
		execArgs["ToolCallID"] = toolCallID

		err := ep.ExecuteCommand(function, execArgs)
		if err != nil {
			return nil, fmt.Errorf("failed to execute tool call %s: %v", toolCallID, err)
		}
		return nil, nil
	}
}

func NewToolCallCompletionHandler(ep *eventsourcing.EventProcessor, agg eventsourcing.Aggregate) eventsourcing.EventHandler {
	return func(event eventsourcing.Event, state map[string]interface{}, commands map[string]eventsourcing.CommandHandler) ([]eventsourcing.Event, error) {
		var requestID string

		// Extract requestID from different event types
		switch e := event.(type) {
		case *eventsourcing.ToolCallCompleted:
			requestID = e.RequestID
		default:
			// Try to extract RequestID directly from the event using the DecodeData method
			type EventData struct {
				RequestID string `json:"RequestID"`
			}
			var data EventData
			marshaledData, err := event.Marshal()
			if err == nil {
				if err := json.Unmarshal(marshaledData, &data); err == nil && data.RequestID != "" {
					requestID = data.RequestID
				}
			}

			// If we still couldn't get a requestID, return an error
			if requestID == "" {
				return nil, fmt.Errorf("could not extract RequestID from event: %s", event.Type())
			}
		}

		coreAgg, ok := agg.(*aggregate.AppAggregate)
		if !ok {
			return nil, fmt.Errorf("aggregate is not a core.AppAggregate")
		}

		if len(coreAgg.PendingToolCalls[requestID]) == 0 {
			results := coreAgg.ToolCallResults[requestID]
			toolCallResults := make([]map[string]interface{}, 0, len(results))
			for toolCallID, res := range results {
				resMap, ok := res.(map[string]interface{})
				if !ok {
					continue // Skip malformed entries
				}
				function, _ := resMap["function"].(string)
				result, _ := resMap["result"].(map[string]interface{})
				toolCallResults = append(toolCallResults, map[string]interface{}{
					"tool_call_id": toolCallID,
					"toolName":     function, // Include tool name
					"result":       result,
				})
			}

			// Return strongly typed event
			return []eventsourcing.Event{
				&eventsourcing.AllToolCallsCompletedEvent{
					RequestID: requestID,
					Results:   toolCallResults,
				},
			}, nil
		}
		return nil, nil
	}
}
