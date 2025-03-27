package orchestration

import (
	"encoding/json"
	"fmt"
	"mindpalace/internal/chat"
	"mindpalace/internal/llmprocessor"
	"mindpalace/internal/plugins"
	"mindpalace/pkg/eventsourcing"
	"mindpalace/pkg/llmmodels"
	"mindpalace/pkg/logging"
	"time"
)

type RequestOrchestrator struct {
	llmClient      *llmprocessor.LLMClient // Simplified LLM processor
	pluginManager  *plugins.PluginManager  // For tool/command access
	agg            *OrchestrationAggregate // State management
	eventProcessor *eventsourcing.EventProcessor
	eventBus       eventsourcing.EventBus
}

func NewRequestOrchestrator(llmClient *llmprocessor.LLMClient, pm *plugins.PluginManager, agg *OrchestrationAggregate, ep *eventsourcing.EventProcessor, eb eventsourcing.EventBus) *RequestOrchestrator {
	ro := &RequestOrchestrator{
		llmClient:      llmClient,
		eventProcessor: ep,
		agg:            agg,
		eventBus:       eb,
		pluginManager:  pm,
	}
	ro.Initialize()
	return ro
}

func (ro *RequestOrchestrator) ProcessUserRequestCommand(data map[string]interface{}) ([]eventsourcing.Event, error) {
	requestText, ok := data["requestText"].(string)
	if !ok {
		return nil, fmt.Errorf("requestText must be a string")
	}
	requestID, _ := data["requestID"].(string)
	if requestID == "" {
		requestID = fmt.Sprintf("req-%d", time.Now().UnixNano())
	}

	var events []eventsourcing.Event
	events = append(events, &UserRequestReceivedEvent{
		EventType:   "orchestration_UserRequestReceived",
		RequestID:   requestID,
		RequestText: requestText,
		Timestamp:   eventsourcing.ISOTimestamp(),
	})

	// Start the agent decision process
	return events, nil
}

func (ro *RequestOrchestrator) DecideAgentCallCommand(event *UserRequestReceivedEvent) ([]eventsourcing.Event, error) {
	messages := ro.buildChatHistory(ro.agg.ChatHistory, 10)
	agentTools := ro.gatherAgentTools()
	resp, err := ro.llmClient.CallLLM(messages, agentTools, event.RequestID, "")
	if err != nil {
		return nil, fmt.Errorf("LLM call failed: %v", err)
	}

	var events []eventsourcing.Event
	if len(resp.Message.ToolCalls) > 0 {
		for _, call := range resp.Message.ToolCalls {
			plug, err := ro.pluginManager.GetPlugin(call.Function.Name)
			if err != nil {
				return nil, fmt.Errorf("requested plugin does not exist: %w", err)
			}
			events = append(events, &AgentCallDecidedEvent{
				RequestID: event.RequestID,
				AgentName: plug.Name(),
				Timestamp: eventsourcing.ISOTimestamp(),
				Model:     plug.AgentModel(),
				Query:     event.RequestText,
			})
		}
		return events, nil
	}

	events = append(events, &RequestCompletedEvent{
		RequestID:    event.RequestID,
		ResponseText: resp.Message.Content,
		CompletedAt:  eventsourcing.ISOTimestamp(),
	})
	return events, nil
}

func (ro *RequestOrchestrator) gatherAgentTools() []llmmodels.Tool {
	var tools []llmmodels.Tool
	for _, plugin := range ro.pluginManager.GetLLMPlugins() {
		tools = append(tools, llmmodels.Tool{
			Type: "function",
			Function: map[string]interface{}{
				"name":        plugin.Name(),
				"description": "Delegate to the " + plugin.Name() + " agent",
				"parameters": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"query": map[string]interface{}{
							"type":        "string",
							"description": "User query for the agent",
						},
					},
					"required": []string{"query"},
				},
			},
		})
	}
	return tools
}
func (ro *RequestOrchestrator) ExecuteToolCallCommand(event *ToolCallRequestPlaced) ([]eventsourcing.Event, error) {
	var events []eventsourcing.Event
	events = append(events, &ToolCallStarted{
		RequestID:  event.RequestID,
		ToolCallID: event.ToolCallID,
		Function:   event.Function,
		Timestamp:  eventsourcing.ISOTimestamp(),
	})

	allCommands := make(map[string]eventsourcing.CommandHandler)
	for _, p := range ro.pluginManager.GetLLMPlugins() {
		for name, command := range p.Commands() {
			allCommands[name] = command
		}
	}
	logging.Debug("calling tool %s, %+v", event.Function, event.Arguments)
	handler, exists := allCommands[event.Function]
	if !exists {
		return nil, fmt.Errorf("no handler for tool %s", event.Function)
	}

	toolEvents, err := handler.Execute(event.Arguments) // Note: This still uses map[string]interface{}
	if err != nil {
		return nil, fmt.Errorf("tool %s failed: %v", event.Function, err)
	}
	logging.Debug("toolcall finished %s", event.Function)

	for _, tevent := range toolEvents {
		logging.Debug("returned events from handler: %+v", event)
		events = append(events, tevent)
		events = append(events, &ToolCallCompleted{
			RequestID:  event.RequestID,
			ToolCallID: event.ToolCallID,
			Function:   event.Function,
			Results:    map[string]interface{}{"success": true, "result": toolEvents},
			Timestamp:  eventsourcing.ISOTimestamp(),
		})
	}
	return events, nil
}

var systemPrompt string = `
You are MindPalace, a friendly AI assistant here to help with various queries and tasks. Provide helpful, accurate, and concise responses, using tools only when they enhance your ability to assist.

Based on the user's request, decide if a specialized agent is needed to handle their query efficiently. You can call specialized agents for specific domains by using the CallAgent tool.

Available agents:
- taskmanager: Specialized agent for managing tasks (create, update, complete, list, delete)

When to call agents:
- When a request clearly maps to a specific agent's domain
- When specialized context or tools would benefit the user
- When the request mentions a specific plugin by name

When NOT to call agents:
- For general knowledge questions
- For simple requests that don't need specialized tools
- When you can handle the request directly

Respond directly if no agent is needed. Your goal is to provide the most helpful and efficient experience.`

// buildChatHistory constructs the conversation history
func (ro *RequestOrchestrator) buildChatHistory(chat []chat.ChatMessage, maxMessages int) []llmmodels.Message {
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

// gatherPluginTools gathers tools specific to a given plugin
func (ro *RequestOrchestrator) gatherPluginTools(plugin eventsourcing.Plugin) []llmmodels.Tool {
	var tools []llmmodels.Tool
	for name, schema := range plugin.Schemas() {
		tools = append(tools, llmmodels.Tool{
			Type: "function",
			Function: map[string]interface{}{
				"name":        name,
				"description": schema.Schema()["description"],
				"parameters":  schema.Schema()["parameters"],
			},
		})
	}
	return tools
}

func (ro *RequestOrchestrator) ExecuteAgentCall(event *AgentCallDecidedEvent) ([]eventsourcing.Event, error) {
	plugin, err := ro.pluginManager.GetPlugin(event.AgentName)
	if err != nil {
		return nil, fmt.Errorf("agent call failed: %w", err)
	}

	resp, err := ro.CallPluginAgent(plugin, event.Query, event.RequestID)
	if err != nil {
		return nil, fmt.Errorf("plugin call failed: %w", err)
	}

	var events []eventsourcing.Event
	for i, toolCall := range resp.Message.ToolCalls {
		events = append(events, &ToolCallRequestPlaced{
			RequestID:  event.RequestID,
			Function:   toolCall.Function.Name,
			Arguments:  toolCall.Function.Arguments,
			Timestamp:  eventsourcing.ISOTimestamp(),
			ToolCallID: fmt.Sprintf("toolrequest-%d", i),
		})
	}
	if len(events) == 0 {
		// TODO: Handle case where agent calls no tools
	}
	return events, nil
}

// CallPluginAgent calls a plugin-specific agent with appropriate context and prompt
func (ro *RequestOrchestrator) CallPluginAgent(plugin eventsourcing.Plugin, requestText string, requestID string) (*llmmodels.OllamaResponse, error) {
	// Get plugin state from its aggregate
	agg := plugin.Aggregate()
	stateJSON, err := json.Marshal(agg)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal plugin state: %v", err)
	}

	// Build dynamic prompt with plugin state
	prompt := fmt.Sprintf("%s\n\nCurrent State:\n%s", plugin.SystemPrompt(), string(stateJSON))

	messages := []llmmodels.Message{
		{Role: "system", Content: prompt},
		{Role: "user", Content: requestText},
	}

	// Use plugin-specific model and tools
	tools := ro.gatherPluginTools(plugin)
	return ro.llmClient.CallLLM(messages, tools, requestID, plugin.AgentModel())
}

// CompleteRequestCommand checks if all tool calls are done and finalizes the request
func (ro *RequestOrchestrator) CompleteRequestCommand(data map[string]interface{}) ([]eventsourcing.Event, error) {
	requestID, ok := data["RequestID"].(string)
	if !ok || requestID == "" {
		return nil, fmt.Errorf("RequestID is required and must be a string")
	}

	// Check if all tool calls for this RequestID are complete
	if pending, exists := ro.agg.PendingToolCalls[requestID]; exists && len(pending) > 0 {
		// Not all tool calls are done yet; no events to emit
		return nil, nil
	}

	chatHist := ro.agg.ChatHistory
	model := "qwq"
	if agentState, exists := ro.agg.AgentStates[requestID]; exists {
		chatHist = agentState.ChatHistory
		model = agentState.Model
	}

	resp, err := ro.llmClient.CallLLM(ro.buildChatHistory(chatHist, 10), nil, requestID, model)
	if err != nil {
		return nil, fmt.Errorf("error calling llm client: %w", err)
	}

	// Emit RequestCompletedEvent
	completedEvent := &RequestCompletedEvent{
		EventType:    "orchestration_RequestCompleted",
		RequestID:    requestID,
		ResponseText: resp.Message.Content,
		CompletedAt:  eventsourcing.ISOTimestamp(),
	}
	return []eventsourcing.Event{completedEvent}, nil
}

func (ro *RequestOrchestrator) Initialize() {
	ro.eventProcessor.RegisterCommand("ProcessUserRequest", eventsourcing.NewCommand(ro.ProcessUserRequestCommand))
	ro.eventProcessor.RegisterCommand("DecideAgentCall", eventsourcing.NewCommand(ro.DecideAgentCallCommand))
	ro.eventBus.Subscribe("orchestration_UserRequestReceived", func(event eventsourcing.Event) error {
		return ro.eventProcessor.ExecuteCommand("DecideAgentCall", event)
	})
	ro.eventProcessor.RegisterCommand("ExecuteAgentCall", eventsourcing.NewCommand(ro.ExecuteAgentCall))
	ro.eventBus.Subscribe("orchestration_AgentCallDecided", func(event eventsourcing.Event) error {
		if e, ok := event.(*AgentCallDecidedEvent); ok {
			data := e // Directly pass the event
			return ro.eventProcessor.ExecuteCommand("ExecuteAgentCall", data)
		}
		return nil
	})
	ro.eventProcessor.RegisterCommand("ExecuteToolCall", eventsourcing.NewCommand(ro.ExecuteToolCallCommand))
	ro.eventBus.Subscribe("orchestration_ToolCallRequestPlaced", func(event eventsourcing.Event) error {
		if e, ok := event.(*ToolCallRequestPlaced); ok {
			return ro.eventProcessor.ExecuteCommand("ExecuteToolCall", e)
		}
		return nil
	})
	ro.eventProcessor.RegisterCommand("CompleteRequest", eventsourcing.NewCommand(ro.CompleteRequestCommand))
	ro.eventBus.Subscribe("orchestration_ToolCallCompleted", func(event eventsourcing.Event) error {
		if e, ok := event.(*ToolCallCompleted); ok {
			return ro.eventProcessor.ExecuteCommand("CompleteRequest", e)
		}
		return nil
	})
}
