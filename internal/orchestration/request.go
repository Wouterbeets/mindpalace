package orchestration

import (
	"fmt"
	"mindpalace/internal/llmprocessor"
	"mindpalace/internal/plugins"
	"mindpalace/pkg/aggregate"
	"mindpalace/pkg/eventsourcing"
	"mindpalace/pkg/llmmodels"
	"time"
)

type RequestOrchestrator struct {
	llmClient     *llmprocessor.LLMClient     // Simplified LLM processor
	pluginManager *plugins.PluginManager      // For tool/command access
	agg           *aggregate.AggregateManager // State management
	eventBus      eventsourcing.EventBus      // For publishing events
}

func NewRequestOrchestrator(llmClient *llmprocessor.LLMClient, pm *plugins.PluginManager, agg *aggregate.AggregateManager, eb eventsourcing.EventBus) *RequestOrchestrator {
	return &RequestOrchestrator{
		llmClient:     llmClient,
		pluginManager: pm,
		agg:           agg,
		eventBus:      eb,
	}
}

// ProcessRequest handles the entire request lifecycle
func (ro *RequestOrchestrator) ProcessRequest(requestText string, requestID string) error {
	if requestID == "" {
		requestID = fmt.Sprintf("req-%d", time.Now().UnixNano())
	}

	// Build chat history
	messages := ro.buildChatHistory(10)
	messages = append(messages, llmmodels.Message{Role: "user", Content: requestText})
	// Publish initial event
	ro.eventBus.Publish(&eventsourcing.UserRequestReceivedEvent{
		RequestID:   requestID,
		RequestText: requestText,
		Timestamp:   eventsourcing.ISOTimestamp(),
	})

	// Gather tools from plugins
	tools := ro.gatherTools()
	messages = append(messages, llmmodels.Message{Role: "user", Content: "make the neccesary tool calls"})
	resp, err := ro.llmClient.CallLLM(messages, tools, requestID)
	if err != nil {
		return fmt.Errorf("LLM call failed: %v", err)
	}

	// Handle response
	var finalContent string
	if len(resp.Message.ToolCalls) > 0 {
		toolResults, err := ro.executeToolCalls(requestID, resp.Message.ToolCalls)
		if err != nil {
			return fmt.Errorf("tool execution failed: %v", err)
		}
		// Call LLM again with tool results
		messages = append(messages, llmmodels.Message{Role: "assistant", Content: resp.Message.Content})
		for _, result := range toolResults {
			messages = append(messages, llmmodels.Message{Role: "tool", Name: result["toolName"].(string), Content: result["content"].(string)})
		}
		finalResp, err := ro.llmClient.CallLLM(messages, nil, requestID)
		if err != nil {
			return fmt.Errorf("final LLM call failed: %v", err)
		}
		finalContent = finalResp.Message.Content
	} else {
		finalContent = resp.Message.Content
	}

	// Publish completion event
	ro.eventBus.Publish(&eventsourcing.GenericEvent{
		EventType: "RequestCompleted",
		Data: map[string]interface{}{
			"RequestID":    requestID,
			"ResponseText": finalContent,
			"Timestamp":    eventsourcing.ISOTimestamp(),
		},
	})

	return nil
}

var systemPrompt string = `
You are MindPalace, a friendly AI assistant here to help with various queries and tasks. Provide helpful, accurate, and concise responses, using tools only when they enhance your ability to assist.

### Core Principles:
1. **Assist effectively**: Prioritize the user's needs, answer directly when possible, and use tools wisely to enhance assistance.
2. **Communicate clearly**: Provide concise, relevant responses, using context to avoid redundancy.
3. **Adapt to uncertainty**: Ask clarifying questions or make reasonable assumptions to keep the interaction smooth.
`

// buildChatHistory constructs the conversation history
func (ro *RequestOrchestrator) buildChatHistory(maxMessages int) []llmmodels.Message {
	messages := []llmmodels.Message{{Role: "system", Content: systemPrompt}}
	for _, msg := range ro.agg.ChatHistory {
		if msg.OllamaRole != "none" {
			messages = append(messages, llmmodels.Message{Role: msg.OllamaRole, Content: msg.Content})
		}
	}
	if len(messages) > maxMessages+1 { // +1 for system prompt
		return messages[len(messages)-maxMessages-1:]
	}
	return messages
}

// gatherTools collects available tools from plugins
func (ro *RequestOrchestrator) gatherTools() []llmmodels.Tool {
	var tools []llmmodels.Tool
	tools = append(tools, llmmodels.Tool{
		Type: "function",
		Function: map[string]interface{}{
			"name":        "InitiatePluginCreation",
			"description": "Start creating a new plugin based on user input",
			"parameters": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"PluginName":  map[string]interface{}{"type": "string", "description": "Name of the plugin"},
					"Description": map[string]interface{}{"type": "string", "description": "What the plugin does"},
					"Goal":        map[string]interface{}{"type": "string", "description": "Purpose of the plugin"},
					"Result":      map[string]interface{}{"type": "string", "description": "Expected output"},
				},
				"required": []string{"PluginName"},
			},
		},
	})
	for _, plugin := range ro.pluginManager.GetLLMPlugins() {
		for name, schema := range plugin.Schemas() {
			tools = append(tools, llmmodels.Tool{
				Type: "function",
				Function: map[string]interface{}{
					"name":        name,
					"description": schema["description"],
					"parameters":  schema["parameters"],
				},
			})
		}
	}
	return tools
}

// executeToolCalls runs tool commands and collects results
func (ro *RequestOrchestrator) executeToolCalls(requestID string, toolCalls []llmmodels.OllamaToolCall) ([]map[string]interface{}, error) {
	var results []map[string]interface{}
	for i, call := range toolCalls {
		toolCallID := fmt.Sprintf("%s-%d", requestID, i)
		args := call.Function.Arguments
		args["RequestID"] = requestID
		args["ToolCallID"] = toolCallID

		handler, exists := ro.agg.AllCommands[call.Function.Name]
		if !exists {
			return nil, fmt.Errorf("no handler for tool %s", call.Function.Name)
		}

		events, err := handler(args, ro.agg.GetState())
		if err != nil {
			return nil, fmt.Errorf("tool %s failed: %v", call.Function.Name, err)
		}

		// Process events and extract result
		for _, event := range events {
			ro.eventBus.Publish(event)
			if tce, ok := event.(*eventsourcing.ToolCallCompleted); ok {
				results = append(results, map[string]interface{}{
					"toolName": tce.Function,
					"content":  fmt.Sprintf("%v", tce.Result),
				})
			}
		}
	}
	return results, nil
}
