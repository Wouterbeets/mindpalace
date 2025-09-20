package orchestration

import (
	"encoding/json"
	"fmt"
	"text/template"
	"time"

	"mindpalace/internal/llmprocessor"
	"mindpalace/internal/plugins"
	"mindpalace/pkg/eventsourcing"
	"mindpalace/pkg/llmmodels"
	"mindpalace/pkg/logging"
)

const systemPromptTemplate = `You are MindPalace, a friendly AI assistant here to help with various queries and tasks. Provide helpful, accurate, and concise responses, using tools only when they enhance your ability to assist.

{{if .Agents}}

Based on the user's request, decide if a specialized agent is needed to handle their query efficiently. You can call specialized agents for specific domains by using the CallAgent tool.

Available agents:
{{range .Agents}}- {{.}}
{{end}}
When to call agents:
- When a request clearly maps to a specific agent's domain
- When specialized context or tools would benefit the user
- When the request mentions a specific plugin by name

When NOT to call agents:
- For general knowledge questions
- For simple requests that don't need specialized tools
- When you can handle the request directly
{{else}}
Since there are no specialized agents available, respond directly to the user's query.
{{end}}

Your goal is to provide the most helpful and efficient experience.`

type RequestOrchestrator struct {
	llmClient        *llmprocessor.LLMClient
	pluginManager    *plugins.PluginManager
	agg              *OrchestrationAggregate
	eventProcessor   *eventsourcing.EventProcessor
	eventBus         eventsourcing.EventBus
	systemPromptTmpl *template.Template // Base template, no plugin specifics here
}

func NewRequestOrchestrator(llmClient *llmprocessor.LLMClient, pm *plugins.PluginManager, agg *OrchestrationAggregate, ep *eventsourcing.EventProcessor, eb eventsourcing.EventBus) *RequestOrchestrator {
	tmpl, err := template.New("systemPrompt").Parse(systemPromptTemplate)
	if err != nil {
		logging.Error("Failed to parse system prompt template: %v", err)
		panic(err.Error())
	}
	ro := &RequestOrchestrator{
		llmClient:        llmClient,
		pluginManager:    pm,
		agg:              agg,
		eventProcessor:   ep,
		eventBus:         eb,
		systemPromptTmpl: tmpl,
	}
	ro.initializeCommandsAndSubscriptions()
	return ro
}

// DecideAgentCallCommand now dynamically fetches plugin prompts per call
func (ro *RequestOrchestrator) DecideAgentCallCommand(event *UserRequestReceivedEvent) ([]eventsourcing.Event, error) {
	// Get all LLM plugins at this moment
	plugins := ro.pluginManager.GetLLMPlugins()
	pluginNames := make([]string, len(plugins))
	for i, p := range plugins {
		pluginNames[i] = p.Name()
	}

	// Reset and populate plugin prompts in ChatManager for this call
	ro.agg.chatManager.ResetPluginPrompts() // Add this method to ChatManager
	for _, plugin := range plugins {
		ro.agg.chatManager.SetPluginPrompt(plugin.Name(), plugin.SystemPrompt())
	}

	// Get LLM context with fresh plugin data
	messages := ro.agg.chatManager.GetLLMContext(pluginNames)
	resp, err := ro.llmClient.CallLLM(messages, ro.gatherAgentTools(), event.RequestID, "")
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
			query := ""
			for argName, argVal := range call.Function.Arguments {
				query += argName + ":" + argVal.(string)
			}
			agentCallEvent := &AgentCallDecidedEvent{
				RequestID: event.RequestID,
				AgentName: plug.Name(),
				Timestamp: eventsourcing.ISOTimestamp(),
				Model:     plug.AgentModel(),
				Query:     query,
			}
			fmt.Println(agentCallEvent)
			events = append(events, agentCallEvent)
		}
		fmt.Println("In DecideAgentCallCommand", len(events), "were generated")
		return events, nil
	}

	events = append(events, &RequestCompletedEvent{
		RequestID:    event.RequestID,
		ResponseText: resp.Message.Content,
		CompletedAt:  eventsourcing.ISOTimestamp(),
	})
	return events, nil
}

// gatherAgentTools remains unchanged but included for context
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

// commandHandler defines the structure for command registration
type commandHandler struct {
	name    string
	handler eventsourcing.CommandHandler
}

// eventSubscription defines the structure for event subscriptions
type eventSubscription struct {
	eventType string
	handler   func(eventsourcing.Event) error
}

func (ro *RequestOrchestrator) initializeCommandsAndSubscriptions() {
	// Define all command handlers
	commands := []commandHandler{
		{
			name:    "ProcessUserRequest",
			handler: eventsourcing.NewCommand(ro.ProcessUserRequestCommand),
		},
		{
			name:    "DecideAgentCall",
			handler: eventsourcing.NewCommand(ro.DecideAgentCallCommand),
		},
		{
			name:    "ExecuteAgentCall",
			handler: eventsourcing.NewCommand(ro.ExecuteAgentCall),
		},
		{
			name:    "ExecuteToolCall",
			handler: eventsourcing.NewCommand(ro.ExecuteToolCallCommand),
		},
		{
			name:    "CompleteRequest",
			handler: eventsourcing.NewCommand(ro.CompleteRequestCommand),
		},
		{
			name:    "CompleteRequestWithError",
			handler: eventsourcing.NewCommand(ro.CompleteRequestWithErrorCommand),
		},
	}

	// Define all event subscriptions
	subscriptions := []eventSubscription{
		{
			eventType: "orchestration_UserRequestReceived",
			handler: func(event eventsourcing.Event) error {
				return ro.eventProcessor.ExecuteCommand("DecideAgentCall", event)
			},
		},
		{
			eventType: "orchestration_AgentCallDecided",
			handler: func(event eventsourcing.Event) error {
				if e, ok := event.(*AgentCallDecidedEvent); ok {
					return ro.eventProcessor.ExecuteCommand("ExecuteAgentCall", e)
				}
				return nil
			},
		},
		{
			eventType: "orchestration_ToolCallRequestPlaced",
			handler: func(event eventsourcing.Event) error {
				if e, ok := event.(*ToolCallRequestPlaced); ok {
					return ro.eventProcessor.ExecuteCommand("ExecuteToolCall", e)
				}
				return nil
			},
		},
		{
			eventType: "orchestration_ToolCallCompleted",
			handler: func(event eventsourcing.Event) error {
				if e, ok := event.(*ToolCallCompleted); ok {
					return ro.eventProcessor.ExecuteCommand("CompleteRequest", e)
				}
				return nil
			},
		},
		{
			eventType: "orchestration_AgentExecutionFailed",
			handler: func(event eventsourcing.Event) error {
				if e, ok := event.(*AgentExecutionFailedEvent); ok {
					return ro.eventProcessor.ExecuteCommand("CompleteRequestWithError", e)
				}
				return nil
			},
		},
		{
			eventType: "orchestration_ToolCallFailed",
			handler: func(event eventsourcing.Event) error {
				if e, ok := event.(*ToolCallFailedEvent); ok {
					return ro.eventProcessor.ExecuteCommand("CompleteRequestWithError", e)
				}
				return nil
			},
		},
	}

	// Register all commands
	for _, cmd := range commands {
		ro.eventProcessor.RegisterCommand(cmd.name, cmd.handler)
	}

	// Register all subscriptions
	for _, sub := range subscriptions {
		ro.eventBus.Subscribe(sub.eventType, sub.handler)
		logging.Debug("Subscribed to event: %s", sub.eventType)
	}
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

	logging.Info("Processing user request. Request ID: %s", requestID)

	return []eventsourcing.Event{
		&UserRequestReceivedEvent{
			EventType:   "orchestration_UserRequestReceived",
			RequestID:   requestID,
			RequestText: requestText,
			Timestamp:   eventsourcing.ISOTimestamp(),
		},
	}, nil
}

func (ro *RequestOrchestrator) ExecuteToolCallCommand(event *ToolCallRequestPlaced) ([]eventsourcing.Event, error) {
	var events []eventsourcing.Event

	// Record the start of the tool call
	events = append(events, &ToolCallStarted{
		RequestID:  event.RequestID,
		ToolCallID: event.ToolCallID,
		Function:   event.Function,
		Timestamp:  eventsourcing.ISOTimestamp(),
	})

	// Step 1: Identify the plugin responsible for the command
	plugin, err := ro.pluginManager.GetPluginByCommand(event.Function)
	if err != nil {
		errorMsg := fmt.Sprintf("no plugin found for command %s", event.Function)
		logging.Error(errorMsg)
		events = append(events, &ToolCallFailedEvent{
			EventType:  "orchestration_ToolCallFailed",
			RequestID:  event.RequestID,
			ToolCallID: event.ToolCallID,
			Function:   event.Function,
			ErrorMsg:   errorMsg,
			Timestamp:  eventsourcing.ISOTimestamp(),
		})
		return events, nil
	}

	// Step 2: Retrieve the command's input schema
	schemas := plugin.Schemas()
	inputSchema, exists := schemas[event.Function]
	if !exists {
		errorMsg := fmt.Sprintf("no schema found for command %s", event.Function)
		logging.Error(errorMsg)
		events = append(events, &ToolCallFailedEvent{
			EventType:  "orchestration_ToolCallFailed",
			RequestID:  event.RequestID,
			ToolCallID: event.ToolCallID,
			Function:   event.Function,
			ErrorMsg:   errorMsg,
			Timestamp:  eventsourcing.ISOTimestamp(),
		})
		return events, nil
	}

	// Step 3: Create a new instance of the input struct
	input := inputSchema.New()

	// Step 4: Convert map[string]interface{} to the struct
	inputJSON, err := json.Marshal(event.Arguments)
	if err != nil {
		errorMsg := fmt.Sprintf("failed to marshal arguments: %v", err)
		logging.Error(errorMsg)
		events = append(events, &ToolCallFailedEvent{
			EventType:  "orchestration_ToolCallFailed",
			RequestID:  event.RequestID,
			ToolCallID: event.ToolCallID,
			Function:   event.Function,
			ErrorMsg:   errorMsg,
			Timestamp:  eventsourcing.ISOTimestamp(),
		})
		return events, nil
	}

	if err := json.Unmarshal(inputJSON, input); err != nil {
		errorMsg := fmt.Sprintf("failed to unmarshal arguments into %T: %v", input, err)
		logging.Error(errorMsg)
		events = append(events, &ToolCallFailedEvent{
			EventType:  "orchestration_ToolCallFailed",
			RequestID:  event.RequestID,
			ToolCallID: event.ToolCallID,
			Function:   event.Function,
			ErrorMsg:   errorMsg,
			Timestamp:  eventsourcing.ISOTimestamp(),
		})
		return events, nil
	}

	// Step 5: Execute the command with the correct input type
	handler, exists := plugin.Commands()[event.Function]
	if !exists {
		errorMsg := fmt.Sprintf("no handler for command %s", event.Function)
		logging.Error(errorMsg)
		events = append(events, &ToolCallFailedEvent{
			EventType:  "orchestration_ToolCallFailed",
			RequestID:  event.RequestID,
			ToolCallID: event.ToolCallID,
			Function:   event.Function,
			ErrorMsg:   errorMsg,
			Timestamp:  eventsourcing.ISOTimestamp(),
		})
		return events, nil
	}

	toolEvents, err := handler.Execute(input)
	if err != nil {
		errorMsg := fmt.Sprintf("command %s failed: %v", event.Function, err)
		logging.Error(errorMsg)
		events = append(events, &ToolCallFailedEvent{
			EventType:  "orchestration_ToolCallFailed",
			RequestID:  event.RequestID,
			ToolCallID: event.ToolCallID,
			Function:   event.Function,
			ErrorMsg:   errorMsg,
			Timestamp:  eventsourcing.ISOTimestamp(),
		})
		return events, nil
	}
	for _, toolEvent := range toolEvents {
		fmt.Println("tool call returned event:", toolEvent)
	}
	// Step 6: Append results and complete the tool call
	events = append(events, toolEvents...)
	events = append(events, &ToolCallCompleted{
		RequestID:  event.RequestID,
		ToolCallID: event.ToolCallID,
		Function:   event.Function,
		Results:    map[string]interface{}{"success": true, "result": toolEvents},
		Timestamp:  eventsourcing.ISOTimestamp(),
	})
	fmt.Println("added tool call completed event")

	return events, nil
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
	var events []eventsourcing.Event
	plugin, err := ro.pluginManager.GetPlugin(event.AgentName)
	if err != nil {
		errorMsg := fmt.Sprintf("agent call failed: %v", err)
		return []eventsourcing.Event{&AgentExecutionFailedEvent{
			EventType:   "orchestration_AgentExecutionFailed",
			RequestID:   event.RequestID,
			AgentName:   event.AgentName,
			ErrorMsg:    errorMsg,
			Timestamp:   eventsourcing.ISOTimestamp(),
			Recoverable: false,
		}}, nil
	}

	resp, err := ro.CallPluginAgent(plugin, event.Query, event.RequestID)
	if err != nil {
		errorMsg := fmt.Sprintf("plugin call failed: %v", err)
		return []eventsourcing.Event{&AgentExecutionFailedEvent{
			EventType:   "orchestration_AgentExecutionFailed",
			RequestID:   event.RequestID,
			AgentName:   event.AgentName,
			ErrorMsg:    errorMsg,
			Timestamp:   eventsourcing.ISOTimestamp(),
			Recoverable: false,
		}}, nil
	}

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
		events = append(events, &RequestCompletedEvent{
			EventType:    "orchestration_RequestCompleted",
			RequestID:    event.RequestID,
			ResponseText: resp.Message.Content,
			CompletedAt:  eventsourcing.ISOTimestamp(),
		})
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

	logging.Debug("current state in agent call %s", stateJSON)
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
func (ro *RequestOrchestrator) CompleteRequestCommand(event *ToolCallCompleted) ([]eventsourcing.Event, error) {
	requestID := event.RequestID
	// Check if all tool calls for this RequestID are complete
	if pending, exists := ro.agg.PendingToolCalls[requestID]; exists && len(pending) > 0 {
		logging.Debug("pending toolcalls: %d", len(pending))
		// Not all tool calls are done yet; no events to emit
		return nil, nil
	}

	model := "gpt-oss:20b"
	if agentState, exists := ro.agg.AgentStates[requestID]; exists {
		model = agentState.Model
	}
	// Use tag-based context selection for better relevance
	relevantTags := []string{"task", "completion", "response"} // Basic tags for completion context
	messages := ro.agg.chatManager.GetLLMContextWithTags(nil, relevantTags)
	resp, err := ro.llmClient.CallLLM(messages, nil, requestID, model)
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
	marsh, _ := completedEvent.Marshal()
	logging.Debug("calling marshall in complete request %s", marsh)
	return []eventsourcing.Event{completedEvent}, nil
}

// CompleteRequestWithErrorCommand handles completing a request that had an error
func (ro *RequestOrchestrator) CompleteRequestWithErrorCommand(event eventsourcing.Event) ([]eventsourcing.Event, error) {
	requestID := ""
	errorMsg := ""

	// Extract the requestID and errorMsg from different error event types
	switch e := event.(type) {
	case *AgentExecutionFailedEvent:
		requestID = e.RequestID
		errorMsg = e.ErrorMsg
	case *ToolCallFailedEvent:
		requestID = e.RequestID
		errorMsg = e.ErrorMsg
	default:
		return nil, fmt.Errorf("unsupported error event type: %T", event)
	}

	// Check if we need to finalize the request
	if pending, exists := ro.agg.PendingToolCalls[requestID]; exists && len(pending) > 0 {
		// Not all tool calls are done yet; no events to emit
		// We'll let the CompleteRequest command handle it when all calls finish
		return nil, nil
	}

	// Create a completion response with error information
	completedEvent := &RequestCompletedEvent{
		EventType:    "orchestration_RequestCompleted",
		RequestID:    requestID,
		ResponseText: fmt.Sprintf("I encountered an error while processing your request: %s", errorMsg),
		CompletedAt:  eventsourcing.ISOTimestamp(),
	}

	return []eventsourcing.Event{completedEvent}, nil
}
