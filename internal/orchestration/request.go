package orchestration

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"text/template"
	"time"

	"mindpalace/internal/llmprocessor"
	"mindpalace/pkg/aggregate"
	"mindpalace/pkg/eventsourcing"
	"mindpalace/pkg/llmmodels"
	"mindpalace/pkg/logging"
)

// Interfaces for testability
type LLMClientInterface interface {
	ChatCompletion(ctx context.Context, messages []llmprocessor.Message, tools []llmprocessor.Tool, stream bool) (*llmprocessor.ChatResponse, error)
	Close() error
}

type PluginManagerInterface interface {
	GetLLMPlugins() []eventsourcing.Plugin
	GetPlugin(name string) (eventsourcing.Plugin, error)
	GetPluginByCommand(cmd string) (eventsourcing.Plugin, error)
}

type EventProcessorInterface interface {
	RegisterCommand(name string, handler eventsourcing.CommandHandler)
	ExecuteCommand(name string, data interface{}) error
}

type EventBusInterface interface {
	Subscribe(eventType string, handler eventsourcing.EventHandler)
	Publish(event eventsourcing.Event)
	SubscribeAll(handler eventsourcing.EventHandler)
}

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
	llmClient        LLMClientInterface
	pluginManager    PluginManagerInterface
	agg              *OrchestrationAggregate
	eventProcessor   EventProcessorInterface
	eventBus         EventBusInterface
	systemPromptTmpl *template.Template // Base template, no plugin specifics here
	commandChan      <-chan eventsourcing.CommandData
	controlChan      <-chan string
	aggStore         eventsourcing.AggregateStore
	events           []eventsourcing.Event
}

func NewRequestOrchestrator(llmClient LLMClientInterface, pm PluginManagerInterface, agg *OrchestrationAggregate, ep EventProcessorInterface, eb EventBusInterface, commandChan <-chan eventsourcing.CommandData, controlChan <-chan string, aggStore eventsourcing.AggregateStore, events []eventsourcing.Event) *RequestOrchestrator {
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
		commandChan:      commandChan,
		controlChan:      controlChan,
		aggStore:         aggStore,
		events:           events,
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
	ro.agg.chatState.GetChatManager().ResetPluginPrompts() // Add this method to ChatManager
	for _, plugin := range plugins {
		ro.agg.chatState.GetChatManager().SetPluginPrompt(plugin.Name(), plugin.SystemPrompt())
	}

	// Get LLM context with fresh plugin data
	llmMessages := ro.agg.chatState.GetChatManager().GetLLMContext(pluginNames)
	// Convert messages
	messages := make([]llmprocessor.Message, len(llmMessages))
	for i, m := range llmMessages {
		messages[i] = llmprocessor.Message{
			Role:    m.Role,
			Content: m.Content,
		}
	}
	// Convert tools
	llmTools := ro.gatherAgentTools()
	tools := make([]llmprocessor.Tool, len(llmTools))
	for i, t := range llmTools {
		funcMap := t.Function
		params, _ := json.Marshal(funcMap["parameters"])
		tools[i] = llmprocessor.Tool{
			Type: t.Type,
			Function: llmprocessor.FunctionDef{
				Name:        funcMap["name"].(string),
				Description: funcMap["description"].(string),
				Parameters:  params,
			},
		}
	}
	ctx := context.WithValue(context.Background(), "request_id", event.RequestID)
	resp, err := ro.llmClient.ChatCompletion(ctx, messages, tools, false)
	if err != nil {
		return nil, fmt.Errorf("LLM call failed: %v", err)
	}

	choice := resp.Choices[0]
	var events []eventsourcing.Event

	if len(choice.ToolCalls) == 0 {
		now := eventsourcing.ISOTimestamp()
		events = append(events, &AgentCallDecidedEvent{
			RequestID:    event.RequestID,
			AgentName:    "",
			CallAgent:    false,
			Timestamp:    now,
			ResponseText: choice.Message.Content,
			RawResponse:  choice.Message.Content,
		})
		events = append(events, &RequestCompletedEvent{
			RequestID:    event.RequestID,
			ResponseText: choice.Message.Content,
			CompletedAt:  now,
		})
		return events, nil
	}

	for _, call := range choice.ToolCalls {
		plug, err := ro.pluginManager.GetPlugin(call.Function.Name)
		if err != nil {
			return nil, fmt.Errorf("requested plugin does not exist: %w", err)
		}
		var args map[string]interface{}
		if err := json.Unmarshal(call.Function.Arguments, &args); err != nil {
			return nil, fmt.Errorf("failed to unmarshal arguments for plugin %s: %w", plug.Name(), err)
		}
		queryBytes, _ := json.Marshal(args)
		query := string(queryBytes)
		agentCallEvent := &AgentCallDecidedEvent{
			RequestID:   event.RequestID,
			AgentName:   plug.Name(),
			CallAgent:   true,
			Timestamp:   eventsourcing.ISOTimestamp(),
			Model:       plug.AgentModel(),
			Query:       query,
			RawResponse: choice.Message.Content,
		}
		logging.Debug("agent call event: %v", agentCallEvent)
		events = append(events, agentCallEvent)
	}
	logging.Info("DecideAgentCallCommand generated %d events", len(events))
	return events, nil
}

// gatherAgentTools gathers tools for available LLM plugins using their descriptions
func (ro *RequestOrchestrator) gatherAgentTools() []llmmodels.Tool {
	var tools []llmmodels.Tool
	for _, plugin := range ro.pluginManager.GetLLMPlugins() {
		tools = append(tools, llmmodels.Tool{
			Type: "function",
			Function: map[string]interface{}{
				"name":        plugin.Name(),
				"description": plugin.Description(),
				"parameters": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"query": map[string]interface{}{
							"type":        "string",
							"description": "query for the agent",
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

func (ro *RequestOrchestrator) Start() {
	go func() {
		for cmd := range ro.commandChan {
			err := ro.eventProcessor.ExecuteCommand(cmd.Name, cmd.Data)
			if err != nil {
				logging.Error("Failed to execute command %s: %v", cmd.Name, err)
			}
		}
	}()

	go func() {
		for msg := range ro.controlChan {
			switch msg {
			case "rebuild_state":
				logging.Info("Rebuilding aggregate state after ready signal")
				if aggManager, ok := ro.aggStore.(*aggregate.AggregateManager); ok {
					logging.Info("ORCHESTRATOR: Starting rebuild with %d events", len(ro.events))
					err := aggManager.RebuildState(ro.events)
					if err != nil {
						logging.Error("Failed to rebuild state: %v", err)
					} else {
						logging.Info("ORCHESTRATOR: Rebuild completed successfully")
					}
				} else {
					logging.Error("AggregateStore is not *AggregateManager")
				}
			default:
				logging.Info("Unknown control message: %s", msg)
			}
		}
	}()
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
					if !e.CallAgent {
						logging.Debug("Agent decision for request %s resolved without downstream agent call", e.RequestID)
						return nil
					}
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
		AgentName:  event.AgentName,
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
			AgentName:  event.AgentName,
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
			AgentName:  event.AgentName,
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
			AgentName:  event.AgentName,
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
		logging.Debug("tool call returned event: %v", toolEvent)
	}
	// Step 6: Append results and complete the tool call
	events = append(events, toolEvents...)
	events = append(events, &ToolCallCompleted{
		RequestID:  event.RequestID,
		ToolCallID: event.ToolCallID,
		Function:   event.Function,
		AgentName:  event.AgentName,
		Results:    map[string]interface{}{"success": true, "result": toolEvents},
		Timestamp:  eventsourcing.ISOTimestamp(),
	})
	logging.Info("added tool call completed event")

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
	if !event.CallAgent {
		logging.Debug("ExecuteAgentCall skipped for request %s: decision resolved without agent", event.RequestID)
		return nil, nil
	}

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

	responseText := resp.Choices[0].Message.Content
	stage := "pre_tool"
	if len(resp.Choices[0].ToolCalls) == 0 {
		stage = "final"
	}
	if strings.TrimSpace(responseText) != "" {
		events = append(events, &AgentResponseEvent{
			RequestID:    event.RequestID,
			AgentName:    event.AgentName,
			ResponseText: responseText,
			RawResponse:  responseText,
			Stage:        stage,
			Timestamp:    eventsourcing.ISOTimestamp(),
		})
	}

	toolCalls := resp.Choices[0].ToolCalls
	for i, toolCall := range toolCalls {
		var args map[string]interface{}
		if err := json.Unmarshal(toolCall.Function.Arguments, &args); err != nil {
			return nil, fmt.Errorf("failed to decode tool call arguments for %s: %w", toolCall.Function.Name, err)
		}
		events = append(events, &ToolCallRequestPlaced{
			RequestID:  event.RequestID,
			Function:   toolCall.Function.Name,
			Arguments:  args,
			Timestamp:  eventsourcing.ISOTimestamp(),
			ToolCallID: fmt.Sprintf("%s-toolcall-%d", event.RequestID, i),
			AgentName:  event.AgentName,
		})
	}
	if len(toolCalls) == 0 {
		events = append(events, &RequestCompletedEvent{
			EventType:    "orchestration_RequestCompleted",
			RequestID:    event.RequestID,
			ResponseText: resp.Choices[0].Message.Content,
			CompletedAt:  eventsourcing.ISOTimestamp(),
		})
	}

	return events, nil
}

// CallPluginAgent calls a plugin-specific agent with appropriate context and prompt
func (ro *RequestOrchestrator) CallPluginAgent(plugin eventsourcing.Plugin, requestText string, requestID string) (*llmprocessor.ChatResponse, error) {
	// Get plugin state from its aggregate
	agg := plugin.Aggregate()
	stateJSON, err := json.Marshal(agg)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal plugin state: %v", err)
	}

	logging.Debug("current state in agent call %s", stateJSON)
	// Build dynamic prompt with plugin state
	prompt := fmt.Sprintf("%s\n\nCurrent State:\n%s", plugin.SystemPrompt(), string(stateJSON))

	messages := []llmprocessor.Message{
		{Role: "system", Content: prompt},
		{Role: "user", Content: requestText},
	}

	// Use plugin-specific model and tools
	llmTools := ro.gatherPluginTools(plugin)
	tools := make([]llmprocessor.Tool, len(llmTools))
	for i, t := range llmTools {
		funcMap := t.Function
		params, _ := json.Marshal(funcMap["parameters"])
		tools[i] = llmprocessor.Tool{
			Type: t.Type,
			Function: llmprocessor.FunctionDef{
				Name:        funcMap["name"].(string),
				Description: funcMap["description"].(string),
				Parameters:  params,
			},
		}
	}
	ctx := context.WithValue(context.Background(), "request_id", requestID)
	return ro.llmClient.ChatCompletion(ctx, messages, tools, false)
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

	// Use tag-based context selection for better relevance
	relevantTags := []string{"task", "completion", "response"} // Basic tags for completion context
	var activeAgents []string
	if agentState, exists := ro.agg.AgentStates[requestID]; exists && agentState.AgentName != "" {
		activeAgents = append(activeAgents, agentState.AgentName)
	}
	llmMessages := ro.agg.chatState.GetChatManager().GetLLMContextWithTags(activeAgents, relevantTags)
	messages := make([]llmprocessor.Message, len(llmMessages))
	for i, m := range llmMessages {
		messages[i] = llmprocessor.Message{
			Role:    m.Role,
			Content: m.Content,
		}
	}
	resp, err := ro.llmClient.ChatCompletion(context.Background(), messages, nil, false)
	if err != nil {
		return nil, fmt.Errorf("error calling llm client: %w", err)
	}

	// Emit RequestCompletedEvent
	completedEvent := &RequestCompletedEvent{
		EventType:    "orchestration_RequestCompleted",
		RequestID:    requestID,
		ResponseText: resp.Choices[0].Message.Content,
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
