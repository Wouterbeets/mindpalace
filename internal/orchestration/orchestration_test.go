package orchestration

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"mindpalace/internal/llmprocessor"
	"mindpalace/pkg/eventsourcing"
)

// Mock implementations for testing

type mockLLMClient struct {
	responses map[string]*llmprocessor.ChatResponse
}

func (m *mockLLMClient) ChatCompletion(ctx context.Context, messages []llmprocessor.Message, tools []llmprocessor.Tool, stream bool) (*llmprocessor.ChatResponse, error) {
	if resp, ok := m.responses[ctx.Value("request_id").(string)]; ok {
		return resp, nil
	}
	// Default response
	return &llmprocessor.ChatResponse{
		Choices: []llmprocessor.Choice{
			{
				Message: llmprocessor.Message{
					Role:    "assistant",
					Content: "Mock response",
				},
			},
		},
	}, nil
}

func (m *mockLLMClient) Close() error {
	return nil
}

type mockPluginManager struct {
	plugins   map[string]eventsourcing.Plugin
	telemetry map[string]eventsourcing.PluginTelemetry
}

func (m *mockPluginManager) GetLLMPlugins() []eventsourcing.Plugin {
	var plugs []eventsourcing.Plugin
	for _, p := range m.plugins {
		plugs = append(plugs, p)
	}
	return plugs
}

func (m *mockPluginManager) GetPlugin(name string) (eventsourcing.Plugin, error) {
	if p, ok := m.plugins[name]; ok {
		return p, nil
	}
	return nil, nil // or error
}

func (m *mockPluginManager) GetPluginByCommand(cmd string) (eventsourcing.Plugin, error) {
	for _, p := range m.plugins {
		if cmds := p.Commands(); cmds != nil {
			if _, exists := cmds[cmd]; exists {
				return p, nil
			}
		}
	}
	return nil, fmt.Errorf("plugin not found for command %s", cmd)
}

func (m *mockPluginManager) PluginMetadataSnapshot(name string) (eventsourcing.PluginMetadataSnapshot, bool) {
	if _, ok := m.plugins[name]; !ok {
		return eventsourcing.PluginMetadataSnapshot{}, false
	}
	meta := eventsourcing.DefaultPluginMetadata(name)
	return meta.Snapshot(), true
}

func (m *mockPluginManager) PluginSnapshots() []eventsourcing.PluginSnapshot {
	snapshots := make([]eventsourcing.PluginSnapshot, 0, len(m.plugins))
	for name := range m.plugins {
		meta := eventsourcing.DefaultPluginMetadata(name)
		tele := m.telemetry[name]
		snapshots = append(snapshots, eventsourcing.PluginSnapshot{
			Metadata:  meta.Snapshot(),
			Telemetry: tele,
		})
	}
	return snapshots
}

func (m *mockPluginManager) PluginDefaultTimeout(name string, fallback time.Duration) time.Duration {
	return fallback
}

func (m *mockPluginManager) RecordInvocation(name string, result eventsourcing.PluginInvocationResult) eventsourcing.PluginTelemetry {
	if m.telemetry == nil {
		m.telemetry = make(map[string]eventsourcing.PluginTelemetry)
	}
	tele := m.telemetry[name]
	tele = tele.Merge(result)
	m.telemetry[name] = tele
	return tele
}

type mockAggregateStore struct {
	aggregates []eventsourcing.Aggregate
}

func (m *mockAggregateStore) AllAggregates() []eventsourcing.Aggregate {
	return m.aggregates
}

func (m *mockAggregateStore) ApplyEventToAllAggs(event eventsourcing.Event) error {
	for _, agg := range m.aggregates {
		if err := agg.ApplyEvent(event); err != nil {
			return err
		}
	}
	return nil
}

type mockPlugin struct {
	name         string
	systemPrompt string
	model        string
	commands     map[string]eventsourcing.CommandHandler
	schemas      map[string]eventsourcing.CommandInput
}

func (m *mockPlugin) Name() string                                         { return m.name }
func (m *mockPlugin) Type() eventsourcing.PluginType                       { return eventsourcing.LLMPlugin }
func (m *mockPlugin) EventHandlers() map[string]eventsourcing.EventHandler { return nil }
func (m *mockPlugin) Commands() map[string]eventsourcing.CommandHandler    { return m.commands }
func (m *mockPlugin) Schemas() map[string]eventsourcing.CommandInput       { return m.schemas }
func (m *mockPlugin) Aggregate() eventsourcing.Aggregate                   { return nil }
func (m *mockPlugin) SystemPrompt() string                                 { return m.systemPrompt }
func (m *mockPlugin) AgentModel() string                                   { return m.model }
func (m *mockPlugin) Description() string                                  { return "Mock plugin for testing" }

type stubInput struct {
	Query string `json:"query"`
}

type stubCommandInput struct{}

func (stubCommandInput) New() any { return &stubInput{} }

func (stubCommandInput) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"query": map[string]interface{}{"type": "string"},
		},
		"required": []string{"query"},
	}
}

type mockEventProcessor struct {
	commands         map[string]eventsourcing.CommandHandler
	executedCommands []string
}

func (m *mockEventProcessor) RegisterCommand(name string, handler eventsourcing.CommandHandler) {
	m.commands[name] = handler
}

func (m *mockEventProcessor) ExecuteCommand(name string, data interface{}) error {
	m.executedCommands = append(m.executedCommands, name)
	if handler, ok := m.commands[name]; ok {
		_, err := handler.Execute(data)
		return err
	}
	return nil
}

func (m *mockEventProcessor) GetExecutedCommands() []string {
	return m.executedCommands
}

type mockEventBus struct {
	subscriptions map[string][]eventsourcing.EventHandler
}

func hasActionType(actions []eventsourcing.DeltaAction, actionType string) bool {
	for _, action := range actions {
		if action.Type == actionType {
			return true
		}
	}
	return false
}

func (m *mockEventBus) Subscribe(eventType string, handler eventsourcing.EventHandler) {
	m.subscriptions[eventType] = append(m.subscriptions[eventType], handler)
}

func (m *mockEventBus) Publish(event eventsourcing.Event) {
	// Simulate publishing by calling handlers
	if handlers, ok := m.subscriptions[event.Type()]; ok {
		for _, h := range handlers {
			h(event) // Ignore error for test
		}
	}
}

func (m *mockEventBus) SubscribeAll(handler eventsourcing.EventHandler) {
	// Not implemented
}

func TestNewOrchestrationAggregate(t *testing.T) {
	agg := NewOrchestrationAggregate()
	if agg == nil {
		t.Fatal("NewOrchestrationAggregate returned nil")
	}
	if agg.ID() != "orchestration" {
		t.Errorf("Expected ID 'orchestration', got %s", agg.ID())
	}
	if agg.PendingToolCalls == nil {
		t.Error("PendingToolCalls not initialized")
	}
	if agg.ToolCallStates == nil {
		t.Error("ToolCallStates not initialized")
	}
	if agg.AgentStates == nil {
		t.Error("AgentStates not initialized")
	}
	if agg.RequestIDs == nil {
		t.Error("RequestIDs not initialized")
	}
}

func TestApplyEvent_UserRequestReceived(t *testing.T) {
	agg := NewOrchestrationAggregate()
	deltaChan := make(chan eventsourcing.DeltaEnvelope, 10)
	ackChan := make(chan int, 10)
	agg.SetChannels(deltaChan, ackChan)
	go func() {
		for envelope := range deltaChan {
			ackChan <- envelope.SequenceID
		}
	}()
	event := &UserRequestReceivedEvent{
		RequestID:   "req1",
		RequestText: "Test request",
		Timestamp:   "2023-01-01T00:00:00Z",
	}
	err := agg.ApplyEvent(event)
	if err != nil {
		t.Fatalf("ApplyEvent failed: %v", err)
	}
	if len(agg.RequestIDs) != 1 || agg.RequestIDs[0] != "req1" {
		t.Errorf("RequestIDs not updated correctly: %v", agg.RequestIDs)
	}
	// Check chat messages
	messages := agg.GetChatManager().GetUIMessages()
	if len(messages) != 1 || messages[0].Content != "Test request" {
		t.Errorf("Chat message not added correctly: %v", messages)
	}
}

func TestApplyEvent_ToolCallRequestPlaced(t *testing.T) {
	agg := NewOrchestrationAggregate()
	deltaChan := make(chan eventsourcing.DeltaEnvelope, 10)
	ackChan := make(chan int, 10)
	agg.SetChannels(deltaChan, ackChan)
	go func() {
		for envelope := range deltaChan {
			ackChan <- envelope.SequenceID
		}
	}()
	// First add an agent state
	agg.AgentStates["req1"] = &AgentState{
		RequestID:     "req1",
		AgentName:     "testAgent",
		Status:        "executing",
		ToolCallIDs:   []string{},
		ExecutionData: make(map[string]interface{}),
		LastUpdated:   "2023-01-01T00:00:00Z",
	}
	event := &ToolCallRequestPlaced{
		RequestID:  "req1",
		ToolCallID: "tool1",
		Function:   "testFunc",
		AgentName:  "testAgent",
		Arguments:  map[string]interface{}{"arg": "value"},
		Timestamp:  "2023-01-01T00:00:00Z",
	}
	err := agg.ApplyEvent(event)
	if err != nil {
		t.Fatalf("ApplyEvent failed: %v", err)
	}
	if state, ok := agg.ToolCallStates["tool1"]; !ok || state.Status != "requested" {
		t.Errorf("ToolCallState not set correctly: %v", agg.ToolCallStates)
	}
	if _, exists := agg.PendingToolCalls["req1"]["tool1"]; !exists {
		t.Errorf("PendingToolCalls not updated: %v", agg.PendingToolCalls)
	}
	if len(agg.AgentStates["req1"].ToolCallIDs) != 1 {
		t.Errorf("Agent ToolCallIDs not updated")
	}
}

func TestApplyEvent_ToolCallCompleted(t *testing.T) {
	agg := NewOrchestrationAggregate()
	deltaChan := make(chan eventsourcing.DeltaEnvelope, 10)
	ackChan := make(chan int, 10)
	agg.SetChannels(deltaChan, ackChan)
	go func() {
		for envelope := range deltaChan {
			ackChan <- envelope.SequenceID
		}
	}()
	// Setup tool call state
	agg.ToolCallStates["tool1"] = &ToolCallState{
		RequestID:   "req1",
		ToolCallID:  "tool1",
		Function:    "testFunc",
		Status:      "started",
		AgentName:   "testAgent",
		LastUpdated: "2023-01-01T00:00:00Z",
	}
	agg.AgentStates["req1"] = &AgentState{
		RequestID:     "req1",
		ExecutionData: make(map[string]interface{}),
	}
	agg.PendingToolCalls["req1"] = map[string]struct{}{"tool1": {}}
	event := &ToolCallCompleted{
		RequestID:  "req1",
		ToolCallID: "tool1",
		Function:   "testFunc",
		AgentName:  "testAgent",
		Results:    map[string]interface{}{"result": "success"},
		Timestamp:  "2023-01-01T00:00:00Z",
	}
	err := agg.ApplyEvent(event)
	if err != nil {
		t.Fatalf("ApplyEvent failed: %v", err)
	}
	if agg.ToolCallStates["tool1"].Status != "success" {
		t.Errorf("ToolCallState status not updated to success")
	}
	if len(agg.PendingToolCalls["req1"]) != 0 {
		t.Errorf("PendingToolCalls not cleared")
	}
	if agg.AgentStates["req1"].ExecutionData["tool1"] == nil {
		t.Errorf("ExecutionData not updated")
	}
}

func TestApplyEvent_AgentCallDecided(t *testing.T) {
	agg := NewOrchestrationAggregate()
	deltaChan := make(chan eventsourcing.DeltaEnvelope, 10)
	ackChan := make(chan int, 10)
	agg.SetChannels(deltaChan, ackChan)
	go func() {
		for envelope := range deltaChan {
			ackChan <- envelope.SequenceID
		}
	}()
	event := &AgentCallDecidedEvent{
		RequestID: "req1",
		AgentName: "testAgent",
		Model:     "gpt-4",
		CallAgent: true,
		Timestamp: "2023-01-01T00:00:00Z",
	}
	err := agg.ApplyEvent(event)
	if err != nil {
		t.Fatalf("ApplyEvent failed: %v", err)
	}
	if state, ok := agg.AgentStates["req1"]; !ok || state.AgentName != "testAgent" || state.Status != "executing" {
		t.Errorf("AgentState not set correctly: %v", agg.AgentStates)
	}
}

func TestApplyEvent_RequestCompleted(t *testing.T) {
	agg := NewOrchestrationAggregate()
	deltaChan := make(chan eventsourcing.DeltaEnvelope, 10)
	ackChan := make(chan int, 10)
	agg.SetChannels(deltaChan, ackChan)
	go func() {
		for envelope := range deltaChan {
			ackChan <- envelope.SequenceID
		}
	}()
	agg.AgentStates["req1"] = &AgentState{
		RequestID: "req1",
		Status:    "executing",
	}
	event := &RequestCompletedEvent{
		RequestID:    "req1",
		ResponseText: "Response with <think>hidden</think> visible",
		CompletedAt:  "2023-01-01T00:00:00Z",
	}
	err := agg.ApplyEvent(event)
	if err != nil {
		t.Fatalf("ApplyEvent failed: %v", err)
	}
	if agg.AgentStates["req1"].Status != "completed" {
		t.Errorf("AgentState status not updated to completed")
	}
	messages := agg.GetChatManager().GetUIMessages()
	// Should have the regular message (hidden is not visible)
	if len(messages) != 1 || messages[0].Content != "Response with  visible" {
		t.Errorf("Messages not added correctly: %v", messages)
	}
}

func TestClone(t *testing.T) {
	agg := NewOrchestrationAggregate()
	agg.RequestIDs = []string{"req1", "req2"}
	cloned := agg.Clone()
	if cloned == nil {
		t.Fatal("Clone returned nil")
	}
	if cloned.ID() != agg.ID() {
		t.Errorf("Cloned ID mismatch")
	}
	clonedAgg, ok := cloned.(*OrchestrationAggregate)
	if !ok {
		t.Fatal("Clone not *OrchestrationAggregate")
	}
	if len(clonedAgg.RequestIDs) != 2 {
		t.Errorf("RequestIDs not copied")
	}
}

func TestBroadcast3DDelta(t *testing.T) {
	agg := NewOrchestrationAggregate()
	agg.OrchestratorAICreated = true // Prevent extra action for orchestrator creation
	event := &ToolCallRequestPlaced{
		RequestID:  "req1",
		ToolCallID: "tool1",
		Function:   "test",
		AgentName:  "testAgent",
		Timestamp:  "2023-01-01T00:00:00Z",
	}
	envelope := agg.EmitDelta(event)
	actions := envelope.Actions
	if len(actions) < 1 {
		t.Fatalf("Expected at least one action")
	}
	if actions[0].Type != "create" {
		t.Fatalf("Expected first action create, got %s", actions[0].Type)
	}
	if len(actions) < 2 {
		t.Fatalf("Expected create and label actions")
	}
	if actions[0].NodeType != "MeshInstance3D" {
		t.Errorf("Expected first action to be MeshInstance3D, got %s", actions[0].NodeType)
	}
	if actions[1].NodeType != "Label3D" {
		t.Errorf("Expected second action to be Label3D, got %s", actions[1].NodeType)
	}
}

// RequestOrchestrator tests require full interface implementations, skipped for now

func TestOrchestrationFlow_UserRequestToCompletion(t *testing.T) {
	// Integration test: simulate the flow from user request to completion
	llmClient := &mockLLMClient{} // No tool calls, direct response
	pm := &mockPluginManager{}
	agg := NewOrchestrationAggregate()
	ep := &mockEventProcessor{commands: make(map[string]eventsourcing.CommandHandler)}
	eb := &mockEventBus{subscriptions: make(map[string][]eventsourcing.EventHandler)}
	commandChan := make(chan eventsourcing.CommandData, 10)
	controlChan := make(chan string, 10)
	aggStore := &mockAggregateStore{}
	events := []eventsourcing.Event{}

	// Set up channels for delta communication
	deltaChan := make(chan eventsourcing.DeltaEnvelope, 10)
	ackChan := make(chan int, 10)
	agg.SetChannels(deltaChan, ackChan)

	// Collect received envelopes
	var receivedEnvelopes []eventsourcing.DeltaEnvelope
	go func() {
		for envelope := range deltaChan {
			receivedEnvelopes = append(receivedEnvelopes, envelope)
			ackChan <- envelope.SequenceID
		}
	}()

	// Create orchestrator
	ro := NewRequestOrchestrator(llmClient, pm, agg, ep, eb, commandChan, controlChan, aggStore, events)

	// Step 1: Process user request
	data := map[string]interface{}{
		"requestText": "Hello",
		"requestID":   "req1",
	}
	events, err := ro.ProcessUserRequestCommand(data)
	if err != nil {
		t.Fatalf("ProcessUserRequestCommand failed: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("Expected 1 event, got %d", len(events))
	}
	userEvent := events[0].(*UserRequestReceivedEvent)

	// Apply the event to aggregate
	err = agg.ApplyEvent(userEvent)
	if err != nil {
		t.Fatalf("ApplyEvent failed: %v", err)
	}

	// Step 2: Decide agent call (normally triggered by event)
	decideEvents, err := ro.DecideAgentCallCommand(userEvent)
	if err != nil {
		t.Fatalf("DecideAgentCallCommand failed: %v", err)
	}
	if len(decideEvents) != 2 {
		t.Fatalf("Expected 2 events, got %d", len(decideEvents))
	}
	if _, ok := decideEvents[0].(*AgentCallDecidedEvent); !ok {
		t.Errorf("Expected first event AgentCallDecidedEvent, got %T", decideEvents[0])
	}
	if _, ok := decideEvents[1].(*RequestCompletedEvent); !ok {
		t.Errorf("Expected second event RequestCompletedEvent, got %T", decideEvents[1])
	}

	// Apply the completion event
	err = agg.ApplyEvent(decideEvents[0])
	if err != nil {
		t.Fatalf("ApplyEvent failed: %v", err)
	}
	err = agg.ApplyEvent(decideEvents[1])
	if err != nil {
		t.Fatalf("ApplyEvent failed: %v", err)
	}

	// For direct completion, no agent state is set
	// Check that request is in RequestIDs
	found := false
	for _, id := range agg.RequestIDs {
		if id == "req1" {
			found = true
			break
		}
	}
	if !found {
		t.Error("RequestID not added")
	}
}

func TestOrchestrationFlow_WithToolCalls(t *testing.T) {
	// Integration test with tool calls
	llmClient := &mockLLMClient{
		responses: map[string]*llmprocessor.ChatResponse{
			"req1": &llmprocessor.ChatResponse{
				Choices: []llmprocessor.Choice{
					{
						Message: llmprocessor.Message{
							Role: "assistant",
						},
						ToolCalls: []llmprocessor.ToolCall{
							{
								ID:   "call1",
								Type: "function",
								Function: llmprocessor.FunctionCall{
									Name:      "testPlugin",
									Arguments: json.RawMessage(`{"query": "test"}`),
								},
							},
						},
					},
				},
			},
		},
	}
	// Mock plugin
	plugin := &mockPlugin{
		name:         "testPlugin",
		systemPrompt: "Test prompt",
		model:        "gpt-4",
		commands:     map[string]eventsourcing.CommandHandler{}, // Empty for test
	}
	pm := &mockPluginManager{
		plugins: map[string]eventsourcing.Plugin{
			"testPlugin": plugin,
		},
	}
	agg := NewOrchestrationAggregate()
	ep := &mockEventProcessor{commands: make(map[string]eventsourcing.CommandHandler)}
	eb := &mockEventBus{subscriptions: make(map[string][]eventsourcing.EventHandler)}
	commandChan := make(chan eventsourcing.CommandData, 10)
	controlChan := make(chan string, 10)
	aggStore := &mockAggregateStore{}
	events := []eventsourcing.Event{}

	ro := NewRequestOrchestrator(llmClient, pm, agg, ep, eb, commandChan, controlChan, aggStore, events)

	// Process user request
	data := map[string]interface{}{
		"requestText": "Use plugin",
		"requestID":   "req1",
	}
	events, err := ro.ProcessUserRequestCommand(data)
	if err != nil {
		t.Fatalf("ProcessUserRequestCommand failed: %v", err)
	}
	userEvent := events[0].(*UserRequestReceivedEvent)

	// Apply
	agg.ApplyEvent(userEvent)

	// Decide agent call
	decideEvents, err := ro.DecideAgentCallCommand(userEvent)
	if err != nil {
		t.Fatalf("DecideAgentCallCommand failed: %v", err)
	}
	if len(decideEvents) != 1 {
		t.Fatalf("Expected 1 event, got %d", len(decideEvents))
	}
	agentEvent := decideEvents[0].(*AgentCallDecidedEvent)

	// Apply agent event
	agg.ApplyEvent(agentEvent)

	// Execute agent call (normally triggered by event)
	// Mock the CallPluginAgent to return no tool calls
	ro.llmClient = &mockLLMClient{
		responses: map[string]*llmprocessor.ChatResponse{
			"req1": {
				Choices: []llmprocessor.Choice{
					{
						Message: llmprocessor.Message{
							Role:    "assistant",
							Content: `{"status":"success","summary":"Plugin response"}`,
						},
					},
				},
			},
		},
	}
	executeEvents, err := ro.ExecuteAgentCall(agentEvent)
	if err != nil {
		t.Fatalf("ExecuteAgentCall failed: %v", err)
	}
	if len(executeEvents) != 2 {
		t.Fatalf("Expected 2 events, got %d", len(executeEvents))
	}
	if _, ok := executeEvents[0].(*AgentResponseEvent); !ok {
		t.Errorf("Expected AgentResponseEvent, got %T", executeEvents[0])
	}
	if _, ok := executeEvents[1].(*RequestCompletedEvent); !ok {
		t.Errorf("Expected RequestCompletedEvent, got %T", executeEvents[1])
	}

	// Apply agent response and completion
	agg.ApplyEvent(executeEvents[0])
	agg.ApplyEvent(executeEvents[1])

	// Check state
	if agg.AgentStates["req1"].Status != "completed" {
		t.Errorf("Status should be completed")
	}
}

func TestEventTriggersCommand(t *testing.T) {
	// Test that publishing an event triggers the subscribed command
	llmClient := &mockLLMClient{}
	pm := &mockPluginManager{}
	agg := NewOrchestrationAggregate()
	ep := &mockEventProcessor{commands: make(map[string]eventsourcing.CommandHandler)}
	eb := &mockEventBus{subscriptions: make(map[string][]eventsourcing.EventHandler)}
	commandChan := make(chan eventsourcing.CommandData, 10)
	controlChan := make(chan string, 10)
	aggStore := &mockAggregateStore{}
	events := []eventsourcing.Event{}

	ro := NewRequestOrchestrator(llmClient, pm, agg, ep, eb, commandChan, controlChan, aggStore, events)
	_ = ro // Used to set up subscriptions

	// The initializeCommandsAndSubscriptions sets up subscriptions
	// For example, UserRequestReceived -> DecideAgentCall

	// Publish UserRequestReceived event
	event := &UserRequestReceivedEvent{
		RequestID:   "req2",
		RequestText: "Trigger test",
		Timestamp:   "2023-01-01T00:00:00Z",
	}
	eb.Publish(event)

	// Check if DecideAgentCall was executed
	executed := ep.GetExecutedCommands()
	found := false
	for _, cmd := range executed {
		if cmd == "DecideAgentCall" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("DecideAgentCall command was not triggered by UserRequestReceived event")
	}
}

func TestParseResponseText(t *testing.T) {
	tests := []struct {
		input   string
		thinks  []string
		regular string
	}{
		{
			input:   "Hello world",
			thinks:  []string{},
			regular: "Hello world",
		},
		{
			input:   "<think>hidden</think>visible",
			thinks:  []string{"hidden"},
			regular: "visible",
		},
		{
			input:   "<think>first</think>middle<think>second</think>end",
			thinks:  []string{"first", "second"},
			regular: "middleend",
		},
		{
			input:   "<think></think>empty",
			thinks:  []string{""},
			regular: "empty",
		},
	}

	for _, tt := range tests {
		thinks, regular := parseResponseText(tt.input)
		if len(thinks) != len(tt.thinks) {
			t.Errorf("Expected %d thinks, got %d", len(tt.thinks), len(thinks))
		}
		for i, think := range thinks {
			if think != tt.thinks[i] {
				t.Errorf("Expected think %q, got %q", tt.thinks[i], think)
			}
		}
		if regular != tt.regular {
			t.Errorf("Expected regular %q, got %q", tt.regular, regular)
		}
	}
}

func TestMarkdownToHTML(t *testing.T) {
	tests := []struct {
		input  string
		output string
	}{
		{
			input:  "Hello",
			output: "<p>Hello</p>",
		},
		{
			input:  "# Header",
			output: "<p><h1>Header</h1></p>",
		},
		{
			input:  "## Sub",
			output: "<p><h2>Sub</h2></p>",
		},
		{
			input:  "- item",
			output: "<p><ul><li>item</li></ul></p>",
		},
		{
			input:  "`code`",
			output: "<p><code>code</code></p>",
		},
		{
			input:  "**bold**",
			output: "<p><strong>bold</strong></p>",
		},
		{
			input:  "*italic*",
			output: "<p><em>italic</em></p>",
		},
		{
			input:  "[link](url)",
			output: "<p><a href=\"url\">link</a></p>",
		},
		{
			input:  "line\n\nnext",
			output: "<p>line</p><p>next</p>",
		},
	}

	for _, tt := range tests {
		result := markdownToHTML(tt.input)
		if string(result) != tt.output {
			t.Errorf("Expected %q, got %q", tt.output, string(result))
		}
	}
}

func TestAgentName(t *testing.T) {
	agg := NewOrchestrationAggregate()
	agg.AgentStates["req1"] = &AgentState{AgentName: "testAgent"}

	if name := agg.AgentName("req1"); name != "testAgent" {
		t.Errorf("Expected testAgent, got %s", name)
	}
}

func TestApplyEvent_ToolCallFailed(t *testing.T) {
	agg := NewOrchestrationAggregate()
	deltaChan := make(chan eventsourcing.DeltaEnvelope, 10)
	ackChan := make(chan int, 10)
	agg.SetChannels(deltaChan, ackChan)
	go func() {
		for envelope := range deltaChan {
			ackChan <- envelope.SequenceID
		}
	}()
	agg.ToolCallStates["tool1"] = &ToolCallState{
		RequestID:   "req1",
		ToolCallID:  "tool1",
		Function:    "testFunc",
		Status:      "started",
		AgentName:   "testAgent",
		LastUpdated: "2023-01-01T00:00:00Z",
	}
	agg.AgentStates["req1"] = &AgentState{
		RequestID:     "req1",
		ExecutionData: make(map[string]interface{}),
	}
	agg.PendingToolCalls["req1"] = map[string]struct{}{"tool1": {}}

	event := &ToolCallFailedEvent{
		RequestID:  "req1",
		ToolCallID: "tool1",
		Function:   "testFunc",
		AgentName:  "testAgent",
		ErrorMsg:   "error",
		Timestamp:  "2023-01-01T00:00:00Z",
	}
	err := agg.ApplyEvent(event)
	if err != nil {
		t.Fatalf("ApplyEvent failed: %v", err)
	}
	if agg.ToolCallStates["tool1"].Status != "failed" {
		t.Error("Status not set to failed")
	}
	if len(agg.PendingToolCalls["req1"]) != 0 {
		t.Error("Pending not cleared")
	}
}

func TestApplyEvent_AgentExecutionFailed(t *testing.T) {
	agg := NewOrchestrationAggregate()
	deltaChan := make(chan eventsourcing.DeltaEnvelope, 10)
	ackChan := make(chan int, 10)
	agg.SetChannels(deltaChan, ackChan)
	go func() {
		for envelope := range deltaChan {
			ackChan <- envelope.SequenceID
		}
	}()
	agg.AgentStates["req1"] = &AgentState{
		RequestID: "req1",
		Status:    "executing",
	}

	event := &AgentExecutionFailedEvent{
		RequestID: "req1",
		AgentName: "testAgent",
		ErrorMsg:  "error",
		Timestamp: "2023-01-01T00:00:00Z",
	}
	err := agg.ApplyEvent(event)
	if err != nil {
		t.Fatalf("ApplyEvent failed: %v", err)
	}
	if agg.AgentStates["req1"].Status != "failed" {
		t.Error("Status not set to failed")
	}
	if agg.AgentStates["req1"].Summary != "Agent execution failed: error" {
		t.Error("Summary not set")
	}
}

func TestBroadcast3DDelta_UserRequestReceived(t *testing.T) {
	agg := NewOrchestrationAggregate()
	agg.RequestIDs = []string{"req1"}
	event := &UserRequestReceivedEvent{
		RequestID:   "req1",
		RequestText: "test",
		Timestamp:   "2023-01-01T00:00:00Z",
	}
	envelope := agg.EmitDelta(event)
	actions := envelope.Actions
	if len(actions) == 0 {
		t.Errorf("Expected at least 1 action, got 0")
	}
	// Check first action is card
	if actions[0].NodeType != "MeshInstance3D" {
		t.Errorf("Expected MeshInstance3D, got %s", actions[0].NodeType)
	}
}

func TestBroadcast3DDelta_AgentCallDecided(t *testing.T) {
	agg := NewOrchestrationAggregate()
	agg.OrchestratorAICreated = true
	event := &AgentCallDecidedEvent{
		RequestID: "req1",
		AgentName: "test",
		CallAgent: true,
		Timestamp: "2023-01-01T00:00:00Z",
	}
	envelope := agg.EmitDelta(event)
	actions := envelope.Actions
	if len(actions) < 2 {
		t.Fatalf("Expected at least two actions")
	}
	if !hasActionType(actions, "update") {
		t.Fatalf("Expected update action for conversation refresh")
	}
}

func TestBroadcast3DDelta_ToolCallRequestPlaced(t *testing.T) {
	agg := NewOrchestrationAggregate()
	agg.OrchestratorAICreated = true
	event := &ToolCallRequestPlaced{
		RequestID:  "req1",
		ToolCallID: "tool1",
		Function:   "test",
		AgentName:  "test",
		Timestamp:  "2023-01-01T00:00:00Z",
	}
	envelope := agg.EmitDelta(event)
	actions := envelope.Actions
	if len(actions) < 2 {
		t.Fatalf("Expected create and label actions")
	}
	if actions[0].Type != "create" {
		t.Fatalf("Expected first action create, got %s", actions[0].Type)
	}
}

func TestBroadcast3DDelta_ToolCallStarted(t *testing.T) {
	agg := NewOrchestrationAggregate()
	agg.OrchestratorAICreated = true
	event := &ToolCallStarted{
		RequestID:  "req1",
		ToolCallID: "tool1",
		Function:   "test",
		AgentName:  "test",
		Timestamp:  "2023-01-01T00:00:00Z",
	}
	envelope := agg.EmitDelta(event)
	actions := envelope.Actions
	if len(actions) < 2 {
		t.Fatalf("Expected create and label actions")
	}
	if actions[0].Type != "create" {
		t.Fatalf("Expected first action create, got %s", actions[0].Type)
	}
}

func TestBroadcast3DDelta_ToolCallCompleted(t *testing.T) {
	agg := NewOrchestrationAggregate()
	agg.OrchestratorAICreated = true
	event := &ToolCallCompleted{
		RequestID:  "req1",
		ToolCallID: "tool1",
		Function:   "test",
		AgentName:  "test",
		Timestamp:  "2023-01-01T00:00:00Z",
	}
	envelope := agg.EmitDelta(event)
	actions := envelope.Actions
	if len(actions) < 1 {
		t.Fatalf("Expected at least one action")
	}
	if actions[0].Type != "create" {
		t.Fatalf("Expected first action create, got %s", actions[0].Type)
	}
	if !hasActionType(actions, "update") {
		t.Fatalf("Expected update action for conversation refresh")
	}
}

func TestBroadcast3DDelta_ToolCallFailedEvent(t *testing.T) {
	agg := NewOrchestrationAggregate()
	agg.OrchestratorAICreated = true
	event := &ToolCallFailedEvent{
		RequestID:  "req1",
		ToolCallID: "tool1",
		Function:   "test",
		AgentName:  "test",
		Timestamp:  "2023-01-01T00:00:00Z",
	}
	envelope := agg.EmitDelta(event)
	actions := envelope.Actions
	if len(actions) < 1 {
		t.Fatalf("Expected at least one action")
	}
	if !hasActionType(actions, "update") {
		t.Fatalf("Expected update action for conversation refresh")
	}
}

func TestBroadcast3DDelta_AgentExecutionFailedEvent(t *testing.T) {
	agg := NewOrchestrationAggregate()
	agg.OrchestratorAICreated = true
	agg.AgentStates["req1"] = &AgentState{AgentName: "test"}
	event := &AgentExecutionFailedEvent{
		RequestID: "req1",
		Timestamp: "2023-01-01T00:00:00Z",
	}
	envelope := agg.EmitDelta(event)
	actions := envelope.Actions
	if len(actions) < 1 {
		t.Fatalf("Expected at least one action")
	}
	if !hasActionType(actions, "update") {
		t.Fatalf("Expected update action for conversation refresh")
	}
}

func TestBroadcast3DDelta_RequestCompletedEvent(t *testing.T) {
	agg := NewOrchestrationAggregate()
	agg.OrchestratorAICreated = true
	event := &RequestCompletedEvent{
		RequestID:    "req1",
		ResponseText: "test",
		CompletedAt:  "2023-01-01T00:00:00Z",
	}
	envelope := agg.EmitDelta(event)
	actions := envelope.Actions
	if len(actions) < 2 {
		t.Fatalf("Expected at least two actions")
	}
	if !hasActionType(actions, "update") {
		t.Fatalf("Expected update action for conversation refresh")
	}
}

func TestProcessUserRequestCommand(t *testing.T) {
	llmClient := &mockLLMClient{}
	pm := &mockPluginManager{}
	agg := NewOrchestrationAggregate()
	ep := &mockEventProcessor{commands: make(map[string]eventsourcing.CommandHandler)}
	eb := &mockEventBus{subscriptions: make(map[string][]eventsourcing.EventHandler)}
	commandChan := make(chan eventsourcing.CommandData, 10)
	controlChan := make(chan string, 10)
	aggStore := &mockAggregateStore{}
	events := []eventsourcing.Event{}

	ro := NewRequestOrchestrator(llmClient, pm, agg, ep, eb, commandChan, controlChan, aggStore, events)

	data := map[string]interface{}{
		"requestText": "test",
	}
	events, err := ro.ProcessUserRequestCommand(data)
	if err != nil {
		t.Fatalf("Failed: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("Expected 1 event, got %d", len(events))
	}
	if _, ok := events[0].(*UserRequestReceivedEvent); !ok {
		t.Errorf("Wrong event type")
	}
}

func TestDecideAgentCallCommand_NoAgents(t *testing.T) {
	llmClient := &mockLLMClient{}
	pm := &mockPluginManager{}
	agg := NewOrchestrationAggregate()
	ep := &mockEventProcessor{commands: make(map[string]eventsourcing.CommandHandler)}
	eb := &mockEventBus{subscriptions: make(map[string][]eventsourcing.EventHandler)}
	commandChan := make(chan eventsourcing.CommandData, 10)
	controlChan := make(chan string, 10)
	aggStore := &mockAggregateStore{}
	events := []eventsourcing.Event{}

	ro := NewRequestOrchestrator(llmClient, pm, agg, ep, eb, commandChan, controlChan, aggStore, events)

	event := &UserRequestReceivedEvent{
		RequestID:   "req1",
		RequestText: "test",
		Timestamp:   "2023-01-01T00:00:00Z",
	}
	events, err := ro.DecideAgentCallCommand(event)
	if err != nil {
		t.Fatalf("Failed: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("Expected 2 events, got %d", len(events))
	}
	if decision, ok := events[0].(*AgentCallDecidedEvent); !ok || decision.CallAgent {
		t.Errorf("Expected AgentCallDecidedEvent with CallAgent false, got %#v", events[0])
	}
	if _, ok := events[1].(*RequestCompletedEvent); !ok {
		t.Errorf("Expected RequestCompletedEvent, got %T", events[1])
	}
}

func TestDecideAgentCallCommand_TargetAgent(t *testing.T) {
	llmClient := &mockLLMClient{}
	plugin := &mockPlugin{
		name:         "calendar",
		systemPrompt: "calendar prompt",
		model:        "model",
		commands:     map[string]eventsourcing.CommandHandler{},
		schemas:      map[string]eventsourcing.CommandInput{},
	}
	pm := &mockPluginManager{plugins: map[string]eventsourcing.Plugin{"calendar": plugin}}
	agg := NewOrchestrationAggregate()
	ep := &mockEventProcessor{commands: make(map[string]eventsourcing.CommandHandler)}
	eb := &mockEventBus{subscriptions: make(map[string][]eventsourcing.EventHandler)}
	commandChan := make(chan eventsourcing.CommandData, 10)
	controlChan := make(chan string, 10)
	aggStore := &mockAggregateStore{}
	eventsStream := []eventsourcing.Event{}

	ro := NewRequestOrchestrator(llmClient, pm, agg, ep, eb, commandChan, controlChan, aggStore, eventsStream)

	requestEvent := &UserRequestReceivedEvent{
		RequestID:   "req-target",
		RequestText: "Schedule a deep dive",
		Timestamp:   eventsourcing.ISOTimestamp(),
		TargetAgent: "Calendar",
	}

	result, err := ro.DecideAgentCallCommand(requestEvent)
	if err != nil {
		t.Fatalf("DecideAgentCallCommand failed: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("Expected 1 event, got %d", len(result))
	}
	callEvent, ok := result[0].(*AgentCallDecidedEvent)
	if !ok {
		t.Fatalf("Expected AgentCallDecidedEvent, got %T", result[0])
	}
	if !callEvent.CallAgent {
		t.Fatalf("Expected CallAgent true")
	}
	if callEvent.AgentName != "calendar" {
		t.Fatalf("Expected agent 'calendar', got %s", callEvent.AgentName)
	}
	if callEvent.Query != requestEvent.RequestText {
		t.Fatalf("Expected query to match request text")
	}
}

func TestExecuteToolCallCommand_NoPlugin(t *testing.T) {
	llmClient := &mockLLMClient{}
	pm := &mockPluginManager{}
	agg := NewOrchestrationAggregate()
	ep := &mockEventProcessor{commands: make(map[string]eventsourcing.CommandHandler)}
	eb := &mockEventBus{subscriptions: make(map[string][]eventsourcing.EventHandler)}
	commandChan := make(chan eventsourcing.CommandData, 10)
	controlChan := make(chan string, 10)
	aggStore := &mockAggregateStore{}
	events := []eventsourcing.Event{}

	ro := NewRequestOrchestrator(llmClient, pm, agg, ep, eb, commandChan, controlChan, aggStore, events)

	event := &ToolCallRequestPlaced{
		RequestID:  "req1",
		ToolCallID: "tool1",
		Function:   "nonexistent",
		AgentName:  "testAgent",
		Timestamp:  "2023-01-01T00:00:00Z",
	}
	events, err := ro.ExecuteToolCallCommand(event)
	if err != nil {
		t.Fatalf("Failed: %v", err)
	}
	if len(events) != 2 { // Started + Failed
		t.Fatalf("Expected 2 events, got %d", len(events))
	}
	if _, ok := events[1].(*ToolCallFailedEvent); !ok {
		t.Errorf("Expected ToolCallFailedEvent")
	}
}

func TestExecuteToolCallCommand_SuccessTelemetry(t *testing.T) {
	llmClient := &mockLLMClient{}
	plugin := &mockPlugin{
		name:         "demo",
		systemPrompt: "demo",
		model:        "demo",
		commands: map[string]eventsourcing.CommandHandler{
			"DoThing": eventsourcing.NewCommand(func(input *stubInput) ([]eventsourcing.Event, error) {
				if input.Query == "" {
					return nil, fmt.Errorf("missing query")
				}
				return []eventsourcing.Event{
					&AgentResponseEvent{
						RequestID:    "req-success",
						AgentName:    "demo",
						ResponseText: "handled",
						RawResponse:  "handled",
						Stage:        "final",
						Timestamp:    eventsourcing.ISOTimestamp(),
					},
				}, nil
			}),
		},
		schemas: map[string]eventsourcing.CommandInput{
			"DoThing": stubCommandInput{},
		},
	}
	pm := &mockPluginManager{
		plugins: map[string]eventsourcing.Plugin{
			"demo": plugin,
		},
	}
	agg := NewOrchestrationAggregate()
	ep := &mockEventProcessor{commands: make(map[string]eventsourcing.CommandHandler)}
	eb := &mockEventBus{subscriptions: make(map[string][]eventsourcing.EventHandler)}
	commandChan := make(chan eventsourcing.CommandData, 10)
	controlChan := make(chan string, 10)
	aggStore := &mockAggregateStore{}
	eventsStream := []eventsourcing.Event{}

	ro := NewRequestOrchestrator(llmClient, pm, agg, ep, eb, commandChan, controlChan, aggStore, eventsStream)

	event := &ToolCallRequestPlaced{
		RequestID:  "req-success",
		ToolCallID: "tool-1",
		Function:   "DoThing",
		AgentName:  "demo",
		Arguments:  map[string]interface{}{"query": "run"},
		Timestamp:  eventsourcing.ISOTimestamp(),
	}

	resultEvents, err := ro.ExecuteToolCallCommand(event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resultEvents) != 4 {
		t.Fatalf("expected 4 events (start, agent event, completion, snapshot), got %d", len(resultEvents))
	}
	if _, ok := resultEvents[0].(*ToolCallStarted); !ok {
		t.Fatalf("expected first event ToolCallStarted, got %T", resultEvents[0])
	}
	if _, ok := resultEvents[1].(*AgentResponseEvent); !ok {
		t.Fatalf("expected second event AgentResponseEvent, got %T", resultEvents[1])
	}
	completed, ok := resultEvents[2].(*ToolCallCompleted)
	if !ok {
		t.Fatalf("expected third event ToolCallCompleted, got %T", resultEvents[2])
	}
	telemetryAny := completed.Results["telemetry"]
	telemetry, ok := telemetryAny.(eventsourcing.PluginTelemetry)
	if !ok {
		t.Fatalf("expected telemetry in ToolCallCompleted results, got %T", telemetryAny)
	}
	if telemetry.Invocations != 1 || telemetry.Successes != 1 {
		t.Fatalf("unexpected telemetry counters: %+v", telemetry)
	}
	snapshot, ok := resultEvents[3].(*PluginSnapshotUpsertedEvent)
	if !ok {
		t.Fatalf("expected fourth event PluginSnapshotUpsertedEvent, got %T", resultEvents[3])
	}
	if snapshot.Snapshot.Metadata.Name != "demo" {
		t.Fatalf("expected snapshot metadata for demo plugin, got %s", snapshot.Snapshot.Metadata.Name)
	}
	if pm.telemetry["demo"].Invocations != 1 {
		t.Fatalf("mock plugin manager telemetry not updated: %+v", pm.telemetry["demo"])
	}
}
