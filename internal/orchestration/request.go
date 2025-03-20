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
	llmClient     *llmprocessor.LLMClient // Simplified LLM processor
	pluginManager *plugins.PluginManager  // For tool/command access
	agg           *aggregate.AppAggregate // State management
	eventBus      eventsourcing.EventBus  // For publishing events
}

func NewRequestOrchestrator(llmClient *llmprocessor.LLMClient, pm *plugins.PluginManager, agg *aggregate.AppAggregate, eb eventsourcing.EventBus) *RequestOrchestrator {
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

	// Publish initial event
	ro.eventBus.Publish(&eventsourcing.UserRequestReceivedEvent{
		RequestID:   requestID,
		RequestText: requestText,
		Timestamp:   eventsourcing.ISOTimestamp(),
	})

	// Build chat history
	messages := ro.buildChatHistory(10)
	messages = append(messages, llmmodels.Message{Role: "user", Content: requestText})

	// Gather tools from plugins
	tools := ro.gatherTools()

	// Call LLM
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

### Response Structure:
Before answering, always:
1. Think deeply in <think> tags about the user's request and whether a tool is necessary.
   - Consider if the user explicitly requested a tool.
   - Consider if the request implies a tool is needed.
   - Consider if the information is already accessible without a tool.
2. Decide if tools are needed based on the above.
3. Only then respond or call tools.

**Format**:
<think>Reasoning steps...</think>
[Tool calls OR Final Answer]

### Tone and Style:
Be friendly and approachable. Avoid jargon unless necessary. Keep responses concise yet complete.

### Final Notes:
Adapt to diverse user needs. Use tools to enhance, not replace, your intelligence. Strive for a seamless user experience.`

// buildChatHistory constructs the conversation history
func (ro *RequestOrchestrator) buildChatHistory(maxMessages int) []llmmodels.Message {
	messages := []llmmodels.Message{{Role: "system", Content: systemPrompt}}
	for _, msg := range ro.agg.ChatHistory {
		messages = append(messages, llmmodels.Message{Role: msg.Role, Content: msg.Content})
	}
	if len(messages) > maxMessages+1 { // +1 for system prompt
		return messages[len(messages)-maxMessages-1:]
	}
	return messages
}

// gatherTools collects available tools from plugins
func (ro *RequestOrchestrator) gatherTools() []llmmodels.Tool {
	var tools []llmmodels.Tool
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
