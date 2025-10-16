package orchestration

import (
	"encoding/json"
	"fmt"
	"html/template"
	"math"
	"regexp"
	"strings"

	"mindpalace/internal/chat"
	"mindpalace/pkg/eventsourcing"
	"mindpalace/pkg/ui3d"
)

type OrchestrationAggregate struct {
	chatState        *ChatState
	PendingToolCalls map[string]map[string]struct{}
	ToolCallStates   map[string]*ToolCallState
	AgentStates      map[string]*AgentState
	RequestIDs       []string
	DisplayInfos     map[string]*DisplayInfo
	PositionIndex    int
}

func NewOrchestrationAggregate() *OrchestrationAggregate {
	// Initialize ChatManager with a base system prompt and context size
	basePrompt := "You are MindPalace, a friendly AI assistant here to help with various queries and tasks."
	chatManager := chat.NewChatManager(100000, basePrompt) // 100K tokens max for LLM context
	chatState := NewChatState(chatManager)
	return &OrchestrationAggregate{
		chatState:        chatState,
		PendingToolCalls: make(map[string]map[string]struct{}),
		ToolCallStates:   make(map[string]*ToolCallState),
		AgentStates:      make(map[string]*AgentState),
		RequestIDs:       make([]string, 0),
		DisplayInfos:     make(map[string]*DisplayInfo),
		PositionIndex:    0,
	}
}

func (a *OrchestrationAggregate) ID() string {
	return "orchestration"
}

// AgentState represents the current state of an agent interaction
type AgentState struct {
	RequestID     string                 // ID of the request
	AgentName     string                 // Name of the agent
	Status        string                 // "deciding", "called", "executing", "summarizing", "completed"
	ToolCallIDs   []string               // IDs of tool calls made by this agent
	ExecutionData map[string]interface{} // Any data from execution
	Summary       string                 // Final summary from agent
	LastUpdated   string                 // Timestamp of last update
	Model         string
}

type ToolCallState struct {
	RequestID   string
	ToolCallID  string
	Function    string
	Status      string // "requested", "started", "completed"
	Results     map[string]interface{}
	LastUpdated string // Timestamp for sorting or debugging
}

type DisplayInfo struct {
	Title       string                 `json:"title"`
	Description string                 `json:"description"`
	Details     map[string]interface{} `json:"details"`
}

func (a *OrchestrationAggregate) AgentName(requestID string) string {
	var agent *AgentState
	var ok bool
	if agent, ok = a.AgentStates[requestID]; !ok {
		return ""
	}
	return agent.AgentName
}

func (a *OrchestrationAggregate) ApplyEvent(event eventsourcing.Event) error {
	// Apply chat-related events first
	if err := a.chatState.ApplyEvent(event); err != nil {
		return err
	}

	switch event.Type() {
	case "orchestration_ToolCallRequestPlaced":
		e := event.(*ToolCallRequestPlaced)
		if a.ToolCallStates == nil {
			a.ToolCallStates = make(map[string]*ToolCallState)
		}
		a.ToolCallStates[e.ToolCallID] = &ToolCallState{
			RequestID:   e.RequestID,
			ToolCallID:  e.ToolCallID,
			Function:    e.Function,
			Status:      "requested",
			LastUpdated: e.Timestamp,
		}
		if _, exists := a.PendingToolCalls[e.RequestID]; !exists {
			a.PendingToolCalls[e.RequestID] = make(map[string]struct{})
		}
		a.PendingToolCalls[e.RequestID][e.ToolCallID] = struct{}{}

		// Add toolcall id to agent tool calls
		a.AgentStates[e.RequestID].ToolCallIDs = append(a.AgentStates[e.RequestID].ToolCallIDs, e.ToolCallID)
		a.DisplayInfos[fmt.Sprintf("tool_call_%s", e.ToolCallID)] = &DisplayInfo{
			Title:       fmt.Sprintf("Tool: %s", e.Function),
			Description: "Tool call requested",
			Details:     map[string]interface{}{"type": "tool_call_started", "function": e.Function, "timestamp": e.Timestamp},
		}

	case "orchestration_ToolCallStarted":
		e := event.(*ToolCallStarted)
		// Chat handled by chatState.ApplyEvent
		if displayInfo, exists := a.DisplayInfos[fmt.Sprintf("tool_call_%s", e.ToolCallID)]; exists {
			displayInfo.Details["type"] = "tool_call_started"
		}

	case "orchestration_ToolCallCompleted":
		e := event.(*ToolCallCompleted)
		if agentState, exists := a.AgentStates[e.RequestID]; exists {
			agentState.ExecutionData[e.ToolCallID] = e.Results
			agentState.LastUpdated = eventsourcing.ISOTimestamp()
		}

		if state, exists := a.ToolCallStates[e.ToolCallID]; exists {
			state.Status = "success"
			state.Results = e.Results
			state.LastUpdated = e.Timestamp
			delete(a.PendingToolCalls[e.RequestID], e.ToolCallID)
			if len(a.PendingToolCalls[e.RequestID]) == 0 {
				delete(a.PendingToolCalls, e.RequestID)
			}
		}
		if displayInfo, exists := a.DisplayInfos[fmt.Sprintf("tool_call_%s", e.ToolCallID)]; exists {
			displayInfo.Details["type"] = "tool_call_completed"
			displayInfo.Description = "Tool call completed"
		}
		// Chat handled by chatState.ApplyEvent
	case "orchestration_ToolCallFailed":
		e := event.(*ToolCallFailedEvent)
		if state, exists := a.ToolCallStates[e.ToolCallID]; exists {
			state.Status = "failed"
			state.Results = map[string]interface{}{"error": e.ErrorMsg}
			state.LastUpdated = e.Timestamp
			delete(a.PendingToolCalls[e.RequestID], e.ToolCallID)
			if len(a.PendingToolCalls[e.RequestID]) == 0 {
				delete(a.PendingToolCalls, e.RequestID)
			}
		}
		if displayInfo, exists := a.DisplayInfos[fmt.Sprintf("tool_call_%s", e.ToolCallID)]; exists {
			displayInfo.Details["type"] = "tool_call_failed"
			displayInfo.Description = fmt.Sprintf("Tool call failed: %s", e.ErrorMsg)
		}

		if agentState, exists := a.AgentStates[e.RequestID]; exists {
			agentState.LastUpdated = eventsourcing.ISOTimestamp()
		}
		// Chat handled by chatState.ApplyEvent

	case "orchestration_AgentCallDecided":
		e := event.(*AgentCallDecidedEvent)
		a.AgentStates[e.RequestID] = &AgentState{
			RequestID:     e.RequestID,
			AgentName:     e.AgentName,
			Status:        "executing",
			ToolCallIDs:   []string{},
			ExecutionData: make(map[string]interface{}),
			LastUpdated:   e.Timestamp,
			Model:         e.Model,
		}
		// Chat handled by chatState.ApplyEvent
		a.DisplayInfos[fmt.Sprintf("agent_%s", e.RequestID)] = &DisplayInfo{
			Title:       fmt.Sprintf("Agent: %s", e.AgentName),
			Description: "Agent called for request",
			Details:     map[string]interface{}{"type": "agent_call_decided", "agent": e.AgentName, "model": e.Model, "timestamp": e.Timestamp},
		}

	case "orchestration_AgentExecutionFailed":
		e := event.(*AgentExecutionFailedEvent)
		if agentState, exists := a.AgentStates[e.RequestID]; exists {
			agentState.Status = "failed"
			agentState.Summary = fmt.Sprintf("Agent execution failed: %s", e.ErrorMsg)
			agentState.LastUpdated = e.Timestamp
		}
		if displayInfo, exists := a.DisplayInfos[fmt.Sprintf("agent_%s", e.RequestID)]; exists {
			displayInfo.Details["type"] = "agent_execution_failed"
			displayInfo.Description = fmt.Sprintf("Agent execution failed: %s", e.ErrorMsg)
		}
		// Chat handled by chatState.ApplyEvent

	case "orchestration_UserRequestReceived":
		e := event.(*UserRequestReceivedEvent)
		a.RequestIDs = append(a.RequestIDs, e.RequestID)
		a.DisplayInfos[fmt.Sprintf("request_%s", e.RequestID)] = &DisplayInfo{
			Title:       "User Request",
			Description: e.RequestText,
			Details:     map[string]interface{}{"type": "user_request_received", "timestamp": e.Timestamp},
		}

	case "orchestration_RequestCompleted":
		e := event.(*RequestCompletedEvent)
		_, regular := parseResponseText(e.ResponseText)

		if agentState, exists := a.AgentStates[e.RequestID]; exists {
			agentState.Status = "completed"
			agentState.LastUpdated = eventsourcing.ISOTimestamp()
		}
		a.DisplayInfos[fmt.Sprintf("completed_%s", e.RequestID)] = &DisplayInfo{
			Title:       "Request Completed",
			Description: regular,
			Details:     map[string]interface{}{"type": "request_completed", "timestamp": e.CompletedAt},
		}
	}
	return nil
}

// markdownToHTML converts basic Markdown to HTML for web display
func markdownToHTML(text string) template.HTML {
	// Handle headers
	text = regexp.MustCompile(`(?m)^# (.+)$`).ReplaceAllString(text, "<h1>$1</h1>")
	text = regexp.MustCompile(`(?m)^## (.+)$`).ReplaceAllString(text, "<h2>$1</h2>")
	text = regexp.MustCompile(`(?m)^### (.+)$`).ReplaceAllString(text, "<h3>$1</h3>")

	// Handle lists (simple replacement, assumes single-level lists)
	text = regexp.MustCompile(`(?m)^- (.+)$`).ReplaceAllString(text, "<li>$1</li>")
	text = regexp.MustCompile(`(?m)^\* (.+)$`).ReplaceAllString(text, "<li>$1</li>")
	// Wrap consecutive <li> in <ul>
	text = regexp.MustCompile(`(<li>.*?</li>\n?)+`).ReplaceAllStringFunc(text, func(match string) string {
		return "<ul>" + match + "</ul>"
	})

	// Handle code blocks
	text = regexp.MustCompile("(?s)```(.*?)```").ReplaceAllString(text, "<pre><code>$1</code></pre>")

	// Handle inline code
	text = regexp.MustCompile("`(.*?)`").ReplaceAllString(text, "<code>$1</code>")

	// Handle bold
	text = regexp.MustCompile(`\*\*(.*?)\*\*`).ReplaceAllString(text, "<strong>$1</strong>")

	// Handle italic
	text = regexp.MustCompile(`\*(.*?)\*`).ReplaceAllString(text, "<em>$1</em>")

	// Handle links (basic)
	text = regexp.MustCompile(`\[([^\]]+)\]\(([^)]+)\)`).ReplaceAllString(text, `<a href="$2">$1</a>`)

	// Handle line breaks (double newline to paragraph, single to <br>)
	text = regexp.MustCompile(`\n\n`).ReplaceAllString(text, "</p><p>")
	text = regexp.MustCompile(`\n`).ReplaceAllString(text, "<br>")
	text = "<p>" + text + "</p>"

	// Clean up empty paragraphs
	text = regexp.MustCompile(`<p>\s*</p>`).ReplaceAllString(text, "")

	return template.HTML(text)
}

type RequestCompletedEvent struct {
	eventsourcing.BaseEvent
	EventType    string `json:"event_type"`
	RequestID    string
	ResponseText string
	CompletedAt  string
}

func (e *RequestCompletedEvent) Type() string { return "orchestration_RequestCompleted" }
func (e *RequestCompletedEvent) Marshal() ([]byte, error) {
	e.EventType = e.Type()
	return json.Marshal(e)
}

// parseResponseText extracts <think> tags and regular content from the response.
func parseResponseText(responseText string) (thinks []string, regular string) {
	re := regexp.MustCompile(`(?s)<think>(.*?)</think>`)
	matches := re.FindAllStringSubmatch(responseText, -1)
	for _, match := range matches {
		thinks = append(thinks, match[1])
	}
	regular = re.ReplaceAllString(responseText, "")
	return thinks, strings.TrimSpace(regular)
}

// UserRequestReceivedEvent is a strongly typed event for when a user request is received
type UserRequestReceivedEvent struct {
	EventType   string `json:"event_type"`
	RequestID   string `json:"request_id"`
	RequestText string `json:"request_text"`
	Timestamp   string `json:"timestamp"`
}

func (e *UserRequestReceivedEvent) Type() string {
	return "orchestration_UserRequestReceived"
}

func (e *UserRequestReceivedEvent) Marshal() ([]byte, error) {
	e.EventType = e.Type()
	return json.Marshal(e)
}

func (e *UserRequestReceivedEvent) Unmarshal(data []byte) error {
	return json.Unmarshal(data, e)
}

type RequestCompleted struct {
	EventType   string `json:"event_type"`
	RequestID   string `json:"request_id"`
	RequestText string `json:"request_text"`
	Timestamp   string `json:"timestamp"`
}

func (e *RequestCompleted) Type() string {
	return "orchestration_RequestCompleted"
}

func (e *RequestCompleted) Marshal() ([]byte, error) {
	e.EventType = e.Type()
	return json.Marshal(e)
}

type InitiatePluginCreationEvent struct {
	EventType   string `json:"event_type"`
	RequestID   string `json:"request_id"`
	PluginName  string `json:"plugin_name"`
	Description string `json:"description"`
	Goal        string `json:"goal"`
	Result      string `json:"result"`
}

func (e *InitiatePluginCreationEvent) Type() string { return "orchestration_InitiatePluginCreation" }
func (e *InitiatePluginCreationEvent) Marshal() ([]byte, error) {
	e.EventType = e.Type()
	return json.Marshal(e)
}
func (e *InitiatePluginCreationEvent) Unmarshal(data []byte) error { return json.Unmarshal(data, e) }

type ToolCallRequestPlaced struct {
	EventType  string                 `json:"event_type"`
	RequestID  string                 `json:"request_id"`
	ToolCallID string                 `json:"tool_call_id"`
	Function   string                 `json:"function"`
	Arguments  map[string]interface{} `json:"arguments"`
	Timestamp  string                 `json:"timestamp"`
}

func (e *ToolCallRequestPlaced) Type() string { return "orchestration_ToolCallRequestPlaced" }
func (e *ToolCallRequestPlaced) Marshal() ([]byte, error) {
	e.EventType = e.Type()
	return json.Marshal(e)
}
func (e *ToolCallRequestPlaced) Unmarshal(data []byte) error { return json.Unmarshal(data, e) }

type ToolCallStarted struct {
	EventType  string `json:"event_type"`
	RequestID  string `json:"request_id"`
	ToolCallID string `json:"tool_call_id"`
	Function   string `json:"function"`
	Timestamp  string `json:"timestamp"`
}

func (e *ToolCallStarted) Type() string { return "orchestration_ToolCallStarted" }
func (e *ToolCallStarted) Marshal() ([]byte, error) {
	e.EventType = e.Type()
	return json.Marshal(e)
}
func (e *ToolCallStarted) Unmarshal(data []byte) error { return json.Unmarshal(data, e) }

type ToolCallCompleted struct {
	EventType  string                 `json:"event_type"`
	RequestID  string                 `json:"request_id"`
	ToolCallID string                 `json:"tool_call_id"`
	Function   string                 `json:"function"`
	Results    map[string]interface{} `json:"results"`
	Timestamp  string                 `json:"timestamp"`
}

func (e *ToolCallCompleted) Type() string { return "orchestration_ToolCallCompleted" }
func (e *ToolCallCompleted) Marshal() ([]byte, error) {
	e.EventType = e.Type()
	return json.Marshal(e)
}
func (e *ToolCallCompleted) Unmarshal(data []byte) error { return json.Unmarshal(data, e) }

// Define agent-related event types
type AgentCallDecidedEvent struct {
	EventType string `json:"event_type"`
	RequestID string `json:"request_id"`
	AgentName string `json:"agent_name"`
	Model     string `json:"model"`
	CallAgent bool   `json:"call_agent"` // Whether to call the agent or not
	Timestamp string `json:"timestamp"`
	Query     string `json:"query"`
}

func (e *AgentCallDecidedEvent) Type() string { return "orchestration_AgentCallDecided" }
func (e *AgentCallDecidedEvent) Marshal() ([]byte, error) {
	e.EventType = e.Type()
	return json.Marshal(e)
}
func (e *AgentCallDecidedEvent) Unmarshal(data []byte) error { return json.Unmarshal(data, e) }

// AgentExecutionFailedEvent represents a failure in agent execution
type AgentExecutionFailedEvent struct {
	EventType   string `json:"event_type"`
	RequestID   string `json:"request_id"`
	AgentName   string `json:"agent_name"`
	ErrorMsg    string `json:"error_msg"`
	Timestamp   string `json:"timestamp"`
	Recoverable bool   `json:"recoverable"` // Whether the error is recoverable
}

func (e *AgentExecutionFailedEvent) Type() string { return "orchestration_AgentExecutionFailed" }
func (e *AgentExecutionFailedEvent) Marshal() ([]byte, error) {
	e.EventType = e.Type()
	return json.Marshal(e)
}
func (e *AgentExecutionFailedEvent) Unmarshal(data []byte) error { return json.Unmarshal(data, e) }

// ToolCallFailedEvent represents a failure in a tool call
type ToolCallFailedEvent struct {
	EventType  string `json:"event_type"`
	RequestID  string `json:"request_id"`
	ToolCallID string `json:"tool_call_id"`
	Function   string `json:"function"`
	ErrorMsg   string `json:"error_msg"`
	Timestamp  string `json:"timestamp"`
}

func (e *ToolCallFailedEvent) Type() string { return "orchestration_ToolCallFailed" }
func (e *ToolCallFailedEvent) Marshal() ([]byte, error) {
	e.EventType = e.Type()
	return json.Marshal(e)
}
func (e *ToolCallFailedEvent) Unmarshal(data []byte) error { return json.Unmarshal(data, e) }

func init() {
	eventsourcing.RegisterEvent("orchestration_UserRequestReceived", func() eventsourcing.Event { return &UserRequestReceivedEvent{} })

	// ToolCalling events
	eventsourcing.RegisterEvent("orchestration_ToolCallRequestPlaced", func() eventsourcing.Event { return &ToolCallRequestPlaced{} })
	eventsourcing.RegisterEvent("orchestration_ToolCallStarted", func() eventsourcing.Event { return &ToolCallStarted{} })
	eventsourcing.RegisterEvent("orchestration_ToolCallCompleted", func() eventsourcing.Event { return &ToolCallCompleted{} })
	eventsourcing.RegisterEvent("orchestration_ToolCallFailed", func() eventsourcing.Event { return &ToolCallFailedEvent{} })

	// Agent-related events
	eventsourcing.RegisterEvent("orchestration_AgentCallDecided", func() eventsourcing.Event { return &AgentCallDecidedEvent{} })
	eventsourcing.RegisterEvent("orchestration_AgentExecutionFailed", func() eventsourcing.Event { return &AgentExecutionFailedEvent{} })

	eventsourcing.RegisterEvent("orchestration_InitiatePluginCreation", func() eventsourcing.Event { return &InitiatePluginCreationEvent{} })

	// Last event in chain
	eventsourcing.RegisterEvent("orchestration_RequestCompleted", func() eventsourcing.Event { return &RequestCompletedEvent{} })
}

func (a *OrchestrationAggregate) nextPosition() []float64 {
	radius := 6.0
	height := -1.0 - float64(a.PositionIndex)*0.25
	angle := 2 * math.Pi * float64(a.PositionIndex) / 64
	x := radius * math.Cos(angle)
	z := radius * math.Sin(angle)
	a.PositionIndex++
	return []float64{x, height, z}
}

func (a *OrchestrationAggregate) Broadcast3DDelta(event eventsourcing.Event) eventsourcing.Signal {
	theme := ui3d.DefaultTheme()
	switch e := event.(type) {
	// Orchestration events
	case *UserRequestReceivedEvent:
		pos := a.nextPosition()
		box := ui3d.CreateBox(fmt.Sprintf("request_%s", e.RequestID), pos, ui3d.DefaultTheme())
		box.Properties["event_type"] = "user_request_received"
		box.Properties["material_override"] = map[string]interface{}{
			"albedo_color": []float64{0, 1, 0, 1}, // Green for user request
		}
		if displayInfo, exists := a.DisplayInfos[box.NodeID]; exists {
			box.Properties["display_info"] = map[string]interface{}{
				"title":       displayInfo.Title,
				"description": displayInfo.Description,
				"details":     displayInfo.Details,
			}
		}
		label := ui3d.CreateLabel(fmt.Sprintf("request_%s_label", e.RequestID), "User Request", []float64{pos[0], pos[1] + 1.0, pos[2]}, theme)
		label.Properties["event_type"] = "user_request_received"
		if displayInfo, exists := a.DisplayInfos[label.NodeID]; exists {
			label.Properties["display_info"] = map[string]interface{}{
				"title":       displayInfo.Title,
				"description": displayInfo.Description,
				"details":     displayInfo.Details,
			}
		}
		// Animation: Move request towards orchestrator
		anim := ui3d.CreateMoveToTouch(fmt.Sprintf("request_%s", e.RequestID), "orchestrator_ai", 3.0, "on_touch_fade")
		return eventsourcing.Signal{Actions: []eventsourcing.DeltaAction{box, label, anim}}
	case *AgentCallDecidedEvent:
		pos := a.nextPosition()
		box := ui3d.CreateBox(fmt.Sprintf("agent_%s", e.RequestID), pos, theme)
		box.Properties["event_type"] = "agent_call_decided"
		// Color the block based on the agent (blue for agents)
		box.Properties["material_override"] = map[string]interface{}{
			"albedo_color": []float64{0, 0, 1, 1}, // Blue for agent
		}
		if displayInfo, exists := a.DisplayInfos[box.NodeID]; exists {
			box.Properties["display_info"] = map[string]interface{}{
				"title":       displayInfo.Title,
				"description": displayInfo.Description,
				"details":     displayInfo.Details,
			}
		}
		label := ui3d.CreateLabel(fmt.Sprintf("agent_%s_label", e.RequestID), fmt.Sprintf("Agent: %s", e.AgentName), []float64{pos[0], pos[1] + 1.0, pos[2]}, theme)
		label.Properties["event_type"] = "agent_call_decided"
		if displayInfo, exists := a.DisplayInfos[label.NodeID]; exists {
			label.Properties["display_info"] = map[string]interface{}{
				"title":       displayInfo.Title,
				"description": displayInfo.Description,
				"details":     displayInfo.Details,
			}
		}
		// Animation: Move agent towards orchestrator
		anim := ui3d.CreateMoveToTouch(fmt.Sprintf("agent_%s", e.RequestID), "orchestrator_ai", 2.0, "")
		return eventsourcing.Signal{Actions: []eventsourcing.DeltaAction{box, label, anim}}
	case *ToolCallRequestPlaced:
		pos := a.nextPosition()
		box := ui3d.CreateBox(fmt.Sprintf("tool_call_%s", e.ToolCallID), pos, theme)
		box.Properties["event_type"] = "tool_call_started"
		box.Properties["material_override"] = map[string]interface{}{
			"albedo_color": []float64{1, 1, 0, 1}, // Yellow for tool call
		}
		if displayInfo, exists := a.DisplayInfos[box.NodeID]; exists {
			box.Properties["display_info"] = map[string]interface{}{
				"title":       displayInfo.Title,
				"description": displayInfo.Description,
				"details":     displayInfo.Details,
			}
		}
		label := ui3d.CreateLabel(fmt.Sprintf("tool_call_%s_label", e.ToolCallID), fmt.Sprintf("Tool: %s", e.Function), []float64{pos[0], pos[1] + 1.0, pos[2]}, theme)
		label.Properties["event_type"] = "tool_call_started"
		if displayInfo, exists := a.DisplayInfos[label.NodeID]; exists {
			label.Properties["display_info"] = map[string]interface{}{
				"title":       displayInfo.Title,
				"description": displayInfo.Description,
				"details":     displayInfo.Details,
			}
		}
		// Animation: Move tool call to a tool zone (simplified, move to fixed position)
		toolZone := []float64{10.0, -2.0, 0.0} // Tool execution zone
		anim := ui3d.CreateMoveTo(fmt.Sprintf("tool_call_%s", e.ToolCallID), toolZone, 1.5, "ease_in")
		return eventsourcing.Signal{Actions: []eventsourcing.DeltaAction{box, label, anim}}
	case *ToolCallStarted:
		pos := a.nextPosition()
		box := ui3d.CreateBox(fmt.Sprintf("tool_call_started_%s", e.ToolCallID), pos, theme)
		box.Properties["event_type"] = "tool_call_started"
		box.Properties["material_override"] = map[string]interface{}{
			"albedo_color": []float64{1, 0.5, 0, 1}, // Orange for tool call started
		}
		return eventsourcing.Signal{Actions: []eventsourcing.DeltaAction{box}}
	case *ToolCallCompleted:
		pos := a.nextPosition()
		box := ui3d.CreateBox(fmt.Sprintf("tool_call_completed_%s", e.ToolCallID), pos, theme)
		box.Properties["event_type"] = "tool_call_completed"
		box.Properties["material_override"] = map[string]interface{}{
			"albedo_color": []float64{0, 1, 0, 1}, // Green for completed
		}
		// Animation: Move result back to agent
		agentID := fmt.Sprintf("agent_%s", e.RequestID)
		anim := ui3d.CreateMoveToTouch(fmt.Sprintf("tool_call_completed_%s", e.ToolCallID), agentID, 2.5, "on_touch_fade")
		return eventsourcing.Signal{Actions: []eventsourcing.DeltaAction{box, anim}}
	case *ToolCallFailedEvent:
		pos := a.nextPosition()
		box := ui3d.CreateBox(fmt.Sprintf("tool_call_failed_%s", e.ToolCallID), pos, theme)
		box.Properties["event_type"] = "tool_call_failed"
		box.Properties["material_override"] = map[string]interface{}{
			"albedo_color": []float64{1, 0, 0, 1}, // Red for failed
		}
		return eventsourcing.Signal{Actions: []eventsourcing.DeltaAction{box}}
	case *AgentExecutionFailedEvent:
		pos := a.nextPosition()
		box := ui3d.CreateBox(fmt.Sprintf("agent_failed_%s", e.RequestID), pos, theme)
		box.Properties["event_type"] = "agent_execution_failed"
		box.Properties["material_override"] = map[string]interface{}{
			"albedo_color": []float64{1, 0, 0, 1}, // Red for failed
		}
		return eventsourcing.Signal{Actions: []eventsourcing.DeltaAction{box}}
	case *RequestCompletedEvent:
		pos := a.nextPosition()
		box := ui3d.CreateBox(fmt.Sprintf("completed_%s", e.RequestID), pos, theme)
		box.Properties["event_type"] = "request_completed"
		box.Properties["material_override"] = map[string]interface{}{
			"albedo_color": []float64{0, 1, 0, 1}, // Green for completed
		}
		if displayInfo, exists := a.DisplayInfos[box.NodeID]; exists {
			box.Properties["display_info"] = map[string]interface{}{
				"title":       displayInfo.Title,
				"description": displayInfo.Description,
				"details":     displayInfo.Details,
			}
		}
		label := ui3d.CreateLabel(fmt.Sprintf("completed_%s_label", e.RequestID), "Request Completed", []float64{pos[0], pos[1] + 1.0, pos[2]}, theme)
		label.Properties["event_type"] = "request_completed"
		if displayInfo, exists := a.DisplayInfos[label.NodeID]; exists {
			label.Properties["display_info"] = map[string]interface{}{
				"title":       displayInfo.Title,
				"description": displayInfo.Description,
				"details":     displayInfo.Details,
			}
		}
		// Animation: Fade out orchestrator to indicate completion
		anim := ui3d.CreateFade("orchestrator_ai", 0.3, 2.0)
		return eventsourcing.Signal{Actions: []eventsourcing.DeltaAction{box, label, anim}}

	// Task events
	case *eventsourcing.InitiatePluginCreationEvent:
		pos := []float64{-2, -2, 0} // Underground
		box := ui3d.CreateBox(fmt.Sprintf("plugin_%s", e.PluginName), pos, theme)
		box.Properties["event_type"] = "plugin_generated"
		label := ui3d.CreateLabel(fmt.Sprintf("plugin_%s_label", e.PluginName), fmt.Sprintf("Plugin: %s", e.PluginName), []float64{pos[0], pos[1] + 1.0, pos[2]}, theme)
		label.Properties["event_type"] = "plugin_generated"
		return eventsourcing.Signal{Actions: []eventsourcing.DeltaAction{box, label}}

	// Add more event types as needed, e.g., task events, calendar events, etc.
	// For now, handle a few key ones to visualize event history
	default:
		// For any other event, place in the underground spiral
		pos := a.nextPosition()
		box := ui3d.CreateBox(fmt.Sprintf("external_%s_%d", event.Type(), a.PositionIndex-1), pos, theme)
		box.Properties["event_type"] = event.Type()
		label := ui3d.CreateLabel(fmt.Sprintf("external_%s_%d_label", event.Type(), a.PositionIndex-1), event.Type(), []float64{pos[0], pos[1] + 1.0, pos[2]}, theme)
		label.Properties["event_type"] = event.Type()
		return eventsourcing.Signal{Actions: []eventsourcing.DeltaAction{box, label}}
	}
	return eventsourcing.Signal{}
}

func (a *OrchestrationAggregate) GetCurrent3DState() eventsourcing.Signal {
	var actions []eventsourcing.DeltaAction
	theme := ui3d.DefaultTheme()

	// Create the central orchestrator AI object
	sphere := ui3d.CreateSphere("orchestrator_ai", []float64{0, 0, 0}, theme)
	sphere.Properties["event_type"] = "orchestrator_ai"
	sphere.Properties["scale"] = []float64{2, 2, 2}
	label := ui3d.CreateLabel("orchestrator_ai_label", "MindPalace Orchestrator", []float64{0, 1.2, 0}, theme)
	label.Properties["event_type"] = "orchestrator_ai"
	label.Properties["parent_id"] = "orchestrator_ai"
	label.Properties["mesh_type"] = "sphere"
	actions = append(actions, sphere, label)

	// Create actions for user requests
	for _, requestID := range a.RequestIDs {
		if displayInfo, exists := a.DisplayInfos[fmt.Sprintf("request_%s", requestID)]; exists {
			pos := a.nextPosition()
			box := ui3d.CreateBox(fmt.Sprintf("request_%s", requestID), pos, theme)
			box.Properties["event_type"] = "user_request_received"
			box.Properties["material_override"] = map[string]interface{}{
				"albedo_color": []float64{0, 1, 0, 1}, // Green for user request
			}
			box.Properties["display_info"] = map[string]interface{}{
				"title":       displayInfo.Title,
				"description": displayInfo.Description,
				"details":     displayInfo.Details,
			}
			label := ui3d.CreateLabel(fmt.Sprintf("request_%s_label", requestID), "User Request", []float64{pos[0], pos[1] + 1.0, pos[2]}, theme)
			label.Properties["event_type"] = "user_request_received"
			label.Properties["display_info"] = map[string]interface{}{
				"title":       displayInfo.Title,
				"description": displayInfo.Description,
				"details":     displayInfo.Details,
			}
			actions = append(actions, box, label)
		}

		// Create actions for completed requests
		if displayInfo, exists := a.DisplayInfos[fmt.Sprintf("completed_%s", requestID)]; exists {
			pos := a.nextPosition()
			box := ui3d.CreateBox(fmt.Sprintf("completed_%s", requestID), pos, theme)
			box.Properties["event_type"] = "request_completed"
			box.Properties["material_override"] = map[string]interface{}{
				"albedo_color": []float64{0, 1, 0, 1}, // Green for completed
			}
			box.Properties["display_info"] = map[string]interface{}{
				"title":       displayInfo.Title,
				"description": displayInfo.Description,
				"details":     displayInfo.Details,
			}
			label := ui3d.CreateLabel(fmt.Sprintf("completed_%s_label", requestID), "Request Completed", []float64{pos[0], pos[1] + 1.0, pos[2]}, theme)
			label.Properties["event_type"] = "request_completed"
			label.Properties["display_info"] = map[string]interface{}{
				"title":       displayInfo.Title,
				"description": displayInfo.Description,
				"details":     displayInfo.Details,
			}
			actions = append(actions, box, label)
		}

		// Create actions for agents
		if agentState, exists := a.AgentStates[requestID]; exists {
			if displayInfo, exists := a.DisplayInfos[fmt.Sprintf("agent_%s", requestID)]; exists {
				pos := a.nextPosition()
				box := ui3d.CreateBox(fmt.Sprintf("agent_%s", requestID), pos, theme)
				box.Properties["event_type"] = "agent_call_decided"
				box.Properties["material_override"] = map[string]interface{}{
					"albedo_color": []float64{0, 0, 1, 1}, // Blue for agent
				}
				box.Properties["display_info"] = map[string]interface{}{
					"title":       displayInfo.Title,
					"description": displayInfo.Description,
					"details":     displayInfo.Details,
				}
				label := ui3d.CreateLabel(fmt.Sprintf("agent_%s_label", requestID), fmt.Sprintf("Agent: %s", agentState.AgentName), []float64{pos[0], pos[1] + 1.0, pos[2]}, theme)
				label.Properties["event_type"] = "agent_call_decided"
				label.Properties["display_info"] = map[string]interface{}{
					"title":       displayInfo.Title,
					"description": displayInfo.Description,
					"details":     displayInfo.Details,
				}
				actions = append(actions, box, label)
			}
		}
	}

	// Create actions for tool calls
	for _, toolState := range a.ToolCallStates {
		if displayInfo, exists := a.DisplayInfos[fmt.Sprintf("tool_call_%s", toolState.ToolCallID)]; exists {
			pos := a.nextPosition()
			box := ui3d.CreateBox(fmt.Sprintf("tool_call_%s", toolState.ToolCallID), pos, theme)
			box.Properties["event_type"] = "tool_call_started"
			box.Properties["material_override"] = map[string]interface{}{
				"albedo_color": []float64{1, 1, 0, 1}, // Yellow for tool call
			}
			box.Properties["display_info"] = map[string]interface{}{
				"title":       displayInfo.Title,
				"description": displayInfo.Description,
				"details":     displayInfo.Details,
			}
			label := ui3d.CreateLabel(fmt.Sprintf("tool_call_%s_label", toolState.ToolCallID), fmt.Sprintf("Tool: %s", toolState.Function), []float64{pos[0], pos[1] + 1.0, pos[2]}, theme)
			label.Properties["event_type"] = "tool_call_started"
			label.Properties["display_info"] = map[string]interface{}{
				"title":       displayInfo.Title,
				"description": displayInfo.Description,
				"details":     displayInfo.Details,
			}
			actions = append(actions, box, label)
		}
	}

	return eventsourcing.Signal{Actions: actions}
}

func (a *OrchestrationAggregate) Clone() eventsourcing.Aggregate {
	// Create a new aggregate with copied state
	newAgg := NewOrchestrationAggregate()
	// Copy chat state
	newAgg.chatState = a.chatState // Assuming chatState has its own copy if needed
	// Copy other fields
	newAgg.PendingToolCalls = make(map[string]map[string]struct{})
	for k, v := range a.PendingToolCalls {
		newAgg.PendingToolCalls[k] = make(map[string]struct{})
		for kk := range v {
			newAgg.PendingToolCalls[k][kk] = struct{}{}
		}
	}
	newAgg.ToolCallStates = make(map[string]*ToolCallState)
	for k, v := range a.ToolCallStates {
		newToolCall := *v // Shallow copy
		newAgg.ToolCallStates[k] = &newToolCall
	}
	newAgg.AgentStates = make(map[string]*AgentState)
	for k, v := range a.AgentStates {
		newAgent := *v // Shallow copy
		newAgg.AgentStates[k] = &newAgent
	}
	newAgg.RequestIDs = make([]string, len(a.RequestIDs))
	copy(newAgg.RequestIDs, a.RequestIDs)
	newAgg.DisplayInfos = make(map[string]*DisplayInfo)
	for k, v := range a.DisplayInfos {
		newDisplay := *v // Shallow copy
		newAgg.DisplayInfos[k] = &newDisplay
	}
	return newAgg
}

func (a *OrchestrationAggregate) GetChatManager() *chat.ChatManager {
	return a.chatState.GetChatManager()
}
