package orchestration

import (
	"fmt"
	"mindpalace/internal/core"
	"mindpalace/pkg/eventsourcing"
	"mindpalace/pkg/llmmodels"
	"time"
)

// NewToolCallConfigFunc creates an event handler that configures tool calls for LLM
func NewToolCallConfigFunc(commands map[string]eventsourcing.CommandHandler, agg eventsourcing.Aggregate, plugins []eventsourcing.Plugin) eventsourcing.EventHandler {
	return func(event eventsourcing.Event, state map[string]interface{}, commands map[string]eventsourcing.CommandHandler) ([]eventsourcing.Event, error) {
		genericEvent, ok := event.(*eventsourcing.GenericEvent)
		if !ok {
			return nil, fmt.Errorf("event is not a GenericEvent")
		}
		requestText, _ := genericEvent.Data["RequestText"].(string)
		requestID, _ := genericEvent.Data["RequestID"].(string)
		if requestID == "" {
			requestID = fmt.Sprintf("req-%d", time.Now().UnixNano())
			genericEvent.Data["RequestID"] = requestID
		}
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
		// Return new event instead of using SubmitEvent directly
		return []eventsourcing.Event{
			&eventsourcing.GenericEvent{
				EventType: "ToolCallsConfigured",
				Data: map[string]interface{}{
					"Tools":       availableTools,
					"RequestText": requestText,
					"RequestID":   requestID,
				},
			},
		}, nil
	}
}

// NewToolCallFunc creates an event handler that processes LLM tool call requests
func NewToolCallFunc(ep *eventsourcing.EventProcessor, agg eventsourcing.Aggregate, plugins []eventsourcing.Plugin) eventsourcing.EventHandler {
	return func(event eventsourcing.Event, state map[string]interface{}, commands map[string]eventsourcing.CommandHandler) ([]eventsourcing.Event, error) {
		genericEvent, ok := event.(*eventsourcing.GenericEvent)
		if !ok {
			return nil, fmt.Errorf("event is not a GenericEvent")
		}
		toolCalls, _ := genericEvent.Data["ToolCalls"].([]llmmodels.OllamaToolCall)
		requestID, _ := genericEvent.Data["RequestID"].(string)
		if len(toolCalls) == 0 {
			return nil, nil
		}
		
		// Create events for each tool call
		var events []eventsourcing.Event
		for i, call := range toolCalls {
			toolCallID := fmt.Sprintf("%s-%d", requestID, i)
			events = append(events, &eventsourcing.GenericEvent{
				EventType: "ToolCallInitiated",
				Data: map[string]interface{}{
					"RequestID":  requestID,
					"ToolCallID": toolCallID,
					"Function":   call.Function.Name,
					"Arguments":  call.Function.Arguments,
				},
			})
		}
		return events, nil
	}
}

// NewToolCallExecutor creates an event handler that executes tool calls
func NewToolCallExecutor(ep *eventsourcing.EventProcessor) eventsourcing.EventHandler {
	return func(event eventsourcing.Event, state map[string]interface{}, commands map[string]eventsourcing.CommandHandler) ([]eventsourcing.Event, error) {
		genericEvent, ok := event.(*eventsourcing.GenericEvent)
		if !ok {
			return nil, fmt.Errorf("event is not a GenericEvent")
		}
		toolCallID, _ := genericEvent.Data["ToolCallID"].(string)
		function, _ := genericEvent.Data["Function"].(string)
		requestID, _ := genericEvent.Data["RequestID"].(string)
		args, _ := genericEvent.Data["Arguments"].(map[string]interface{})

		// Augment args with RequestID and ToolCallID
		execArgs := make(map[string]interface{})
		for k, v := range args {
			execArgs[k] = v
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

// NewToolCallCompletionHandler creates an event handler that processes completed tool calls
func NewToolCallCompletionHandler(ep *eventsourcing.EventProcessor, agg eventsourcing.Aggregate) eventsourcing.EventHandler {
	return func(event eventsourcing.Event, state map[string]interface{}, commands map[string]eventsourcing.CommandHandler) ([]eventsourcing.Event, error) {
		genericEvent, ok := event.(*eventsourcing.GenericEvent)
		if !ok {
			return nil, fmt.Errorf("event is not a GenericEvent")
		}
		requestID, _ := genericEvent.Data["RequestID"].(string)

		// Get pending tool calls from the aggregate
		// This cast is needed since we need to access specific fields
		coreAgg, ok := agg.(*core.AppAggregate)
		if !ok {
			return nil, fmt.Errorf("aggregate is not a core.AppAggregate")
		}

		if len(coreAgg.PendingToolCalls[requestID]) == 0 {
			// All tool calls completed
			results := coreAgg.ToolCallResults[requestID]
			toolCallResults := make([]map[string]interface{}, 0, len(results))
			for toolCallID, result := range results {
				toolCallResults = append(toolCallResults, map[string]interface{}{
					"tool_call_id": toolCallID,
					"result":       result,
				})
			}
			return []eventsourcing.Event{
				&eventsourcing.GenericEvent{
					EventType: "AllToolCallsCompleted",
					Data: map[string]interface{}{
						"RequestID": requestID,
						"Results":   toolCallResults,
					},
				},
			}, nil
		}
		return nil, nil
	}
}

// AppAggregate is a temporary interface to maintain compatibility with existing code
// In a future refactoring, we should define a proper interface in this package
type AppAggregate struct {
	PendingToolCalls map[string][]string               // requestID -> list of tool call IDs
	ToolCallResults  map[string]map[string]interface{} // requestID -> toolCallID -> result
}