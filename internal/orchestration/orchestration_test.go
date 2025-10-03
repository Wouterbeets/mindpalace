package orchestration

import (
	"fmt"
	"testing"

	"mindpalace/pkg/eventsourcing"
	"mindpalace/pkg/llmmodels"
)

// Mock implementations for testing

type mockLLMClient struct {
	responses map[string]*llmmodels.OllamaResponse
}

func (m *mockLLMClient) CallLLM(messages []llmmodels.Message, tools []llmmodels.Tool, requestID, model string) (*llmmodels.OllamaResponse, error) {
	if resp, ok := m.responses[requestID]; ok {
		return resp, nil
	}
	// Default response
	return &llmmodels.OllamaResponse{
		Message: llmmodels.OllamaMessage{
			Content: "Mock response",
		},
		Done: true,
	}, nil
}

type mockPluginManager struct {
	plugins map[string]eventsourcing.Plugin
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

type mockPlugin struct {
	name         string
	systemPrompt string
	model        string
	commands     map[string]eventsourcing.CommandHandler
	schemas      map[string]map[string]interface{}
}

func (m *mockPlugin) Name() string                                         { return m.name }
func (m *mockPlugin) Type() eventsourcing.PluginType                       { return eventsourcing.LLMPlugin }
func (m *mockPlugin) EventHandlers() map[string]eventsourcing.EventHandler { return nil }
func (m *mockPlugin) Commands() map[string]eventsourcing.CommandHandler    { return m.commands }
func (m *mockPlugin) Schemas() map[string]eventsourcing.CommandInput       { return nil }
func (m *mockPlugin) Aggregate() eventsourcing.Aggregate                   { return nil }
func (m *mockPlugin) SystemPrompt() string                                 { return m.systemPrompt }
func (m *mockPlugin) AgentModel() string                                   { return m.model }

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
	// Setup tool call state
	agg.ToolCallStates["tool1"] = &ToolCallState{
		RequestID:   "req1",
		ToolCallID:  "tool1",
		Function:    "testFunc",
		Status:      "started",
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
	event := &AgentCallDecidedEvent{
		RequestID: "req1",
		AgentName: "testAgent",
		Model:     "gpt-4",
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

func TestGetCustomUI(t *testing.T) {
	agg := NewOrchestrationAggregate()
	ui := agg.GetCustomUI()
	if ui == nil {
		t.Error("GetCustomUI returned nil")
	}
	// Further testing would require Fyne mocking, which is complex
}

func TestGetFull3DState(t *testing.T) {
	agg := NewOrchestrationAggregate()
	agg.RequestIDs = []string{"req1", "req2"}
	actions := agg.GetFull3DState()
	if len(actions) == 0 {
		t.Error("GetFull3DState returned no actions")
	}
	// Check for orchestrator_ai node
	found := false
	for _, a := range actions {
		if a.NodeID == "orchestrator_ai" {
			found = true
			break
		}
	}
	if !found {
		t.Error("orchestrator_ai node not found")
	}
}

func TestBroadcast3DDelta(t *testing.T) {
	agg := NewOrchestrationAggregate()
	event := &ToolCallRequestPlaced{
		RequestID:  "req1",
		ToolCallID: "tool1",
		Function:   "test",
		Timestamp:  "2023-01-01T00:00:00Z",
	}
	actions := agg.Broadcast3DDelta(event)
	if len(actions) != 2 {
		t.Errorf("Expected 2 actions, got %d", len(actions))
	}
	if actions[0].NodeType != "MeshInstance3D" {
		t.Errorf("Expected first action to be MeshInstance3D, got %s", actions[0].NodeType)
	}
	if actions[1].NodeType != "Label3D" {
		t.Errorf("Expected second action to be Label3D, got %s", actions[1].NodeType)
	}
}

// RequestOrchestrator tests require full interface implementations, skipped for now

func TestInitiatePluginCreationCommand(t *testing.T) {
	// Test the saga command
	data := map[string]interface{}{"test": "data"}
	events, err := InitiatePluginCreationCommand(data)
	if err != nil {
		t.Fatalf("InitiatePluginCreationCommand failed: %v", err)
	}
	// It returns nil, nil
	if events != nil {
		t.Errorf("Expected nil events, got %v", events)
	}
}

func TestOrchestrationFlow_UserRequestToCompletion(t *testing.T) {
	// Integration test: simulate the flow from user request to completion
	llmClient := &mockLLMClient{} // No tool calls, direct response
	pm := &mockPluginManager{}
	agg := NewOrchestrationAggregate()
	ep := &mockEventProcessor{commands: make(map[string]eventsourcing.CommandHandler)}
	eb := &mockEventBus{subscriptions: make(map[string][]eventsourcing.EventHandler)}

	// Create orchestrator
	ro := NewRequestOrchestrator(llmClient, pm, agg, ep, eb)

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
	if len(decideEvents) != 1 {
		t.Fatalf("Expected 1 event, got %d", len(decideEvents))
	}
	// Since no plugins, it should be RequestCompletedEvent
	if _, ok := decideEvents[0].(*RequestCompletedEvent); !ok {
		t.Errorf("Expected RequestCompletedEvent, got %v", decideEvents[0])
	}

	// Apply the completion event
	err = agg.ApplyEvent(decideEvents[0])
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
		responses: map[string]*llmmodels.OllamaResponse{
			"req1": {
				Message: llmmodels.OllamaMessage{
					ToolCalls: []llmmodels.OllamaToolCall{
						{
							Function: llmmodels.OllamaFunction{
								Name:      "testPlugin",
								Arguments: map[string]interface{}{"query": "test"},
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

	ro := NewRequestOrchestrator(llmClient, pm, agg, ep, eb)

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
		responses: map[string]*llmmodels.OllamaResponse{
			"req1": {
				Message: llmmodels.OllamaMessage{
					Content: "Plugin response",
				},
			},
		},
	}
	executeEvents, err := ro.ExecuteAgentCall(agentEvent)
	if err != nil {
		t.Fatalf("ExecuteAgentCall failed: %v", err)
	}
	if len(executeEvents) != 1 {
		t.Fatalf("Expected 1 event, got %d", len(executeEvents))
	}
	if _, ok := executeEvents[0].(*RequestCompletedEvent); !ok {
		t.Errorf("Expected RequestCompletedEvent, got %v", executeEvents[0])
	}

	// Apply completion
	agg.ApplyEvent(executeEvents[0])

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

	ro := NewRequestOrchestrator(llmClient, pm, agg, ep, eb)
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
	if name := agg.AgentName("nonexistent"); name != "" {
		t.Errorf("Expected empty, got %s", name)
	}
}

func TestIsRequestPending(t *testing.T) {
	agg := NewOrchestrationAggregate()
	agg.AgentStates["req1"] = &AgentState{Status: "executing"}
	agg.PendingToolCalls["req1"] = map[string]struct{}{"tool1": {}}

	if !agg.isRequestPending("req1") {
		t.Error("Expected true for pending tools")
	}

	delete(agg.PendingToolCalls["req1"], "tool1")
	if !agg.isRequestPending("req1") {
		t.Error("Expected true for executing agent")
	}

	agg.AgentStates["req1"].Status = "completed"
	if agg.isRequestPending("req1") {
		t.Error("Expected false for completed")
	}

	if agg.isRequestPending("nonexistent") {
		t.Error("Expected false for nonexistent")
	}
}

func TestApplyEvent_ToolCallFailed(t *testing.T) {
	agg := NewOrchestrationAggregate()
	agg.ToolCallStates["tool1"] = &ToolCallState{
		RequestID:   "req1",
		ToolCallID:  "tool1",
		Function:    "testFunc",
		Status:      "started",
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
	actions := agg.Broadcast3DDelta(event)
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
	event := &AgentCallDecidedEvent{
		RequestID: "req1",
		AgentName: "test",
		Timestamp: "2023-01-01T00:00:00Z",
	}
	actions := agg.Broadcast3DDelta(event)
	if len(actions) != 2 {
		t.Errorf("Expected 2 actions, got %d", len(actions))
	}
}

func TestBroadcast3DDelta_ToolCallRequestPlaced(t *testing.T) {
	agg := NewOrchestrationAggregate()
	event := &ToolCallRequestPlaced{
		RequestID:  "req1",
		ToolCallID: "tool1",
		Function:   "test",
		Timestamp:  "2023-01-01T00:00:00Z",
	}
	actions := agg.Broadcast3DDelta(event)
	if len(actions) != 2 {
		t.Errorf("Expected 2 actions, got %d", len(actions))
	}
}

func TestBroadcast3DDelta_ToolCallStarted(t *testing.T) {
	agg := NewOrchestrationAggregate()
	event := &ToolCallStarted{
		RequestID:  "req1",
		ToolCallID: "tool1",
		Function:   "test",
		Timestamp:  "2023-01-01T00:00:00Z",
	}
	actions := agg.Broadcast3DDelta(event)
	if len(actions) != 1 {
		t.Errorf("Expected 1 action, got %d", len(actions))
	}
	if actions[0].Type != "update" {
		t.Errorf("Expected update, got %s", actions[0].Type)
	}
}

func TestBroadcast3DDelta_ToolCallCompleted(t *testing.T) {
	agg := NewOrchestrationAggregate()
	event := &ToolCallCompleted{
		RequestID:  "req1",
		ToolCallID: "tool1",
		Function:   "test",
		Timestamp:  "2023-01-01T00:00:00Z",
	}
	actions := agg.Broadcast3DDelta(event)
	if len(actions) != 1 {
		t.Errorf("Expected 1 action, got %d", len(actions))
	}
}

func TestBroadcast3DDelta_ToolCallFailedEvent(t *testing.T) {
	agg := NewOrchestrationAggregate()
	event := &ToolCallFailedEvent{
		RequestID:  "req1",
		ToolCallID: "tool1",
		Function:   "test",
		Timestamp:  "2023-01-01T00:00:00Z",
	}
	actions := agg.Broadcast3DDelta(event)
	if len(actions) != 1 {
		t.Errorf("Expected 1 action, got %d", len(actions))
	}
}

func TestBroadcast3DDelta_AgentExecutionFailedEvent(t *testing.T) {
	agg := NewOrchestrationAggregate()
	agg.AgentStates["req1"] = &AgentState{AgentName: "test"}
	event := &AgentExecutionFailedEvent{
		RequestID: "req1",
		Timestamp: "2023-01-01T00:00:00Z",
	}
	actions := agg.Broadcast3DDelta(event)
	if len(actions) != 1 {
		t.Errorf("Expected 1 action, got %d", len(actions))
	}
}

func TestBroadcast3DDelta_RequestCompletedEvent(t *testing.T) {
	agg := NewOrchestrationAggregate()
	event := &RequestCompletedEvent{
		RequestID:    "req1",
		ResponseText: "test",
		CompletedAt:  "2023-01-01T00:00:00Z",
	}
	actions := agg.Broadcast3DDelta(event)
	if len(actions) != 2 {
		t.Errorf("Expected 2 actions, got %d", len(actions))
	}
}

func TestProcessUserRequestCommand(t *testing.T) {
	llmClient := &mockLLMClient{}
	pm := &mockPluginManager{}
	agg := NewOrchestrationAggregate()
	ep := &mockEventProcessor{commands: make(map[string]eventsourcing.CommandHandler)}
	eb := &mockEventBus{subscriptions: make(map[string][]eventsourcing.EventHandler)}

	ro := NewRequestOrchestrator(llmClient, pm, agg, ep, eb)

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

	ro := NewRequestOrchestrator(llmClient, pm, agg, ep, eb)

	event := &UserRequestReceivedEvent{
		RequestID:   "req1",
		RequestText: "test",
		Timestamp:   "2023-01-01T00:00:00Z",
	}
	events, err := ro.DecideAgentCallCommand(event)
	if err != nil {
		t.Fatalf("Failed: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("Expected 1 event, got %d", len(events))
	}
	if _, ok := events[0].(*RequestCompletedEvent); !ok {
		t.Errorf("Expected RequestCompletedEvent, got %T", events[0])
	}
}

func TestExecuteToolCallCommand_NoPlugin(t *testing.T) {
	llmClient := &mockLLMClient{}
	pm := &mockPluginManager{}
	agg := NewOrchestrationAggregate()
	ep := &mockEventProcessor{commands: make(map[string]eventsourcing.CommandHandler)}
	eb := &mockEventBus{subscriptions: make(map[string][]eventsourcing.EventHandler)}

	ro := NewRequestOrchestrator(llmClient, pm, agg, ep, eb)

	event := &ToolCallRequestPlaced{
		RequestID:  "req1",
		ToolCallID: "tool1",
		Function:   "nonexistent",
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

func TestCompleteRequestCommand_Pending(t *testing.T) {
	llmClient := &mockLLMClient{}
	pm := &mockPluginManager{}
	agg := NewOrchestrationAggregate()
	agg.PendingToolCalls["req1"] = map[string]struct{}{"tool1": {}}
	ep := &mockEventProcessor{commands: make(map[string]eventsourcing.CommandHandler)}
	eb := &mockEventBus{subscriptions: make(map[string][]eventsourcing.EventHandler)}

	ro := NewRequestOrchestrator(llmClient, pm, agg, ep, eb)

	event := &ToolCallCompleted{
		RequestID:  "req1",
		ToolCallID: "tool1",
		Function:   "test",
		Timestamp:  "2023-01-01T00:00:00Z",
	}
	events, err := ro.CompleteRequestCommand(event)
	if err != nil {
		t.Fatalf("Failed: %v", err)
	}
	if events != nil {
		t.Errorf("Expected nil events when pending")
	}
}

func TestCompleteRequestWithErrorCommand(t *testing.T) {
	llmClient := &mockLLMClient{}
	pm := &mockPluginManager{}
	agg := NewOrchestrationAggregate()
	ep := &mockEventProcessor{commands: make(map[string]eventsourcing.CommandHandler)}
	eb := &mockEventBus{subscriptions: make(map[string][]eventsourcing.EventHandler)}

	ro := NewRequestOrchestrator(llmClient, pm, agg, ep, eb)

	event := &ToolCallFailedEvent{
		RequestID: "req1",
		ErrorMsg:  "error",
		Timestamp: "2023-01-01T00:00:00Z",
	}
	events, err := ro.CompleteRequestWithErrorCommand(event)
	if err != nil {
		t.Fatalf("Failed: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("Expected 1 event, got %d", len(events))
	}
	if _, ok := events[0].(*RequestCompletedEvent); !ok {
		t.Errorf("Expected RequestCompletedEvent")
	}
}
