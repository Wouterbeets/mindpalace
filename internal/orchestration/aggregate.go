package orchestration

import (
	"encoding/json"
	"fmt"
	"html/template"
	"math"
	"regexp"
	"strings"
	"time"

	"mindpalace/internal/chat"
	"mindpalace/pkg/eventsourcing"
	"mindpalace/pkg/logging"
	"mindpalace/pkg/ui3d"
)

const orchestratorBasePrompt = "You are MindPalace, the orchestrator of a system designed to extent the users mind, allowing the user to store and retrieve anything with your help, you have several plugins at your disposal to help the user achieve this, the plugins are in the form of agents you can interact with by using function calls provided"

type OrchestrationAggregate struct {
	chatState                 *ChatState
	PendingToolCalls          map[string]map[string]struct{}
	ToolCallStates            map[string]*ToolCallState
	AgentStates               map[string]*AgentState
	RequestIDs                []string
	DisplayInfos              map[string]*DisplayInfo
	PositionIndex             int
	OrchestratorAICreated     bool
	OrchestratorPositionIndex int
	deltaChan                 chan eventsourcing.DeltaEnvelope
	ackChan                   <-chan int
}

func NewOrchestrationAggregate() *OrchestrationAggregate {
	// Initialize ChatManager with a base system prompt and context size
	chatManager := chat.NewChatManager(100000, orchestratorBasePrompt) // 100K tokens max for LLM context
	chatState := NewChatState(chatManager)
	return &OrchestrationAggregate{
		chatState:                 chatState,
		PendingToolCalls:          make(map[string]map[string]struct{}),
		ToolCallStates:            make(map[string]*ToolCallState),
		AgentStates:               make(map[string]*AgentState),
		RequestIDs:                make([]string, 0),
		DisplayInfos:              make(map[string]*DisplayInfo),
		PositionIndex:             0,
		OrchestratorAICreated:     false,
		OrchestratorPositionIndex: 0,
	}
}

func (a *OrchestrationAggregate) ID() string {
	return "orchestration"
}
func (a *OrchestrationAggregate) SetChannels(deltaChan chan eventsourcing.DeltaEnvelope, ackChan <-chan int) {
	a.deltaChan = deltaChan
	a.ackChan = ackChan
}

// Reset clears derived orchestration state so a replay can rebuild it deterministically.
func (a *OrchestrationAggregate) Reset() {
	chatManager := chat.NewChatManager(100000, orchestratorBasePrompt)
	a.chatState = NewChatState(chatManager)
	a.PendingToolCalls = make(map[string]map[string]struct{})
	a.ToolCallStates = make(map[string]*ToolCallState)
	a.AgentStates = make(map[string]*AgentState)
	a.RequestIDs = make([]string, 0)
	a.DisplayInfos = make(map[string]*DisplayInfo)
	a.PositionIndex = 0
	a.OrchestratorAICreated = false
	a.OrchestratorPositionIndex = 0
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
	AgentName   string
	Results     map[string]interface{}
	LastUpdated string // Timestamp for sorting or debugging
}

type DisplayInfo = ui3d.DisplayInfo

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
			AgentName:   e.AgentName,
			LastUpdated: e.Timestamp,
		}
		if _, exists := a.PendingToolCalls[e.RequestID]; !exists {
			a.PendingToolCalls[e.RequestID] = make(map[string]struct{})
		}
		a.PendingToolCalls[e.RequestID][e.ToolCallID] = struct{}{}

		// Add toolcall id to agent tool calls
		if agentState, ok := a.AgentStates[e.RequestID]; ok {
			agentState.ToolCallIDs = append(agentState.ToolCallIDs, e.ToolCallID)
			agentState.LastUpdated = e.Timestamp
		}
		a.DisplayInfos[fmt.Sprintf("tool_call_%s", e.ToolCallID)] = &DisplayInfo{
			Title:       fmt.Sprintf("Tool: %s", e.Function),
			Description: "Tool call requested",
			Details: map[string]interface{}{
				"type":      "tool_call_started",
				"function":  e.Function,
				"timestamp": e.Timestamp,
				"agent":     e.AgentName,
			},
		}

	case "orchestration_ToolCallStarted":
		e := event.(*ToolCallStarted)
		// Chat handled by chatState.ApplyEvent
		if state, exists := a.ToolCallStates[e.ToolCallID]; exists {
			state.Status = "started"
			state.LastUpdated = e.Timestamp
		}
		if displayInfo, exists := a.DisplayInfos[fmt.Sprintf("tool_call_%s", e.ToolCallID)]; exists {
			displayInfo.Details["type"] = "tool_call_started"
			displayInfo.Details["timestamp"] = e.Timestamp
		}

	case "orchestration_ToolCallCompleted":
		e := event.(*ToolCallCompleted)
		if agentState, exists := a.AgentStates[e.RequestID]; exists {
			agentState.ExecutionData[e.ToolCallID] = e.Results
			agentState.LastUpdated = e.Timestamp
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
			displayInfo.Details["timestamp"] = e.Timestamp
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
			displayInfo.Details["timestamp"] = e.Timestamp
		}

		if agentState, exists := a.AgentStates[e.RequestID]; exists {
			agentState.LastUpdated = e.Timestamp
		}
		// Chat handled by chatState.ApplyEvent

	case "orchestration_AgentCallDecided":
		e := event.(*AgentCallDecidedEvent)
		if e.CallAgent {
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
				Details: map[string]interface{}{
					"type":      "agent_call_decided",
					"agent":     e.AgentName,
					"model":     e.Model,
					"timestamp": e.Timestamp,
				},
			}
		} else {
			delete(a.AgentStates, e.RequestID)
			a.DisplayInfos[fmt.Sprintf("agent_%s", e.RequestID)] = &DisplayInfo{
				Title:       "Direct Response",
				Description: "Handled directly by MindPalace",
				Details: map[string]interface{}{
					"type":      "direct_response_decided",
					"timestamp": e.Timestamp,
				},
			}
		}
	case "orchestration_AgentResponseCreated":
		e := event.(*AgentResponseEvent)
		if agentState, exists := a.AgentStates[e.RequestID]; exists {
			if agentState.ExecutionData == nil {
				agentState.ExecutionData = make(map[string]interface{})
			}
			agentState.ExecutionData[fmt.Sprintf("response_%s", e.Stage)] = e.ResponseText
			agentState.Summary = e.ResponseText
			agentState.LastUpdated = e.Timestamp
			if e.Stage == "final" {
				agentState.Status = "completed"
			}
		}
		displayKey := fmt.Sprintf("agent_response_%s_%s", e.RequestID, sanitizeID(e.Stage))
		stageLabel := e.Stage
		if len(stageLabel) > 0 {
			stageLabel = strings.ToUpper(stageLabel[:1]) + stageLabel[1:]
		} else {
			stageLabel = "Response"
		}
		a.DisplayInfos[displayKey] = &DisplayInfo{
			Title:       fmt.Sprintf("Agent Response (%s)", stageLabel),
			Description: e.ResponseText,
			Details: map[string]interface{}{
				"type":       "agent_response",
				"agent":      e.AgentName,
				"stage":      e.Stage,
				"timestamp":  e.Timestamp,
				"raw_output": e.RawResponse,
			},
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
			agentState.LastUpdated = e.CompletedAt
		}
		a.DisplayInfos[fmt.Sprintf("completed_%s", e.RequestID)] = &DisplayInfo{
			Title:       "Request Completed",
			Description: regular,
			Details:     map[string]interface{}{"type": "request_completed", "timestamp": e.CompletedAt},
		}
	}

	// Emit delta if needed
	if envelope := a.EmitDelta(event); envelope != nil {
		envelope.SequenceID = eventsourcing.NextSequenceID()
		logging.Debug("AGGREGATE: Sending delta envelope to channel: type=%s, aggregate=%s, sequence=%d, actions=%d", envelope.Type, envelope.Aggregate, envelope.SequenceID, len(envelope.Actions))
		select {
		case a.deltaChan <- *envelope:
			logging.Debug("AGGREGATE: Delta envelope sent to channel successfully")
		case <-time.After(10 * time.Second):
			logging.Error("Timeout sending delta envelope for event %s", event.Type())
			return nil // or return error?
		}
		// Wait for ACK
		logging.Debug("AGGREGATE: Waiting for ack")
		select {
		case ackSeq := <-a.ackChan:
			if ackSeq != envelope.SequenceID {
				logging.Error("ACK sequence mismatch: expected %d, got %d", envelope.SequenceID, ackSeq)
			}
			logging.Debug("AGGREGATE: ack received for sequence %d", envelope.SequenceID)
		case <-time.After(5 * time.Second):
			logging.Error("ACK timeout for sequence %d", envelope.SequenceID)
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

func sanitizeID(input string) string {
	input = strings.ToLower(input)
	var builder strings.Builder
	for _, r := range input {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			builder.WriteRune(r)
		} else {
			builder.WriteRune('_')
		}
	}
	return strings.Trim(builder.String(), "_")
}

func prettifyEventLabel(eventType string) string {
	if eventType == "" {
		return ""
	}
	separators := func(r rune) bool {
		return r == '_' || r == ':' || r == '.' || r == '-'
	}
	parts := strings.FieldsFunc(eventType, separators)
	for i, part := range parts {
		if len(part) == 0 {
			continue
		}
		part = strings.ToLower(part)
		parts[i] = strings.ToUpper(part[:1]) + part[1:]
	}
	return strings.Join(parts, " ")
}

type ToolCallRequestPlaced struct {
	EventType  string                 `json:"event_type"`
	RequestID  string                 `json:"request_id"`
	ToolCallID string                 `json:"tool_call_id"`
	Function   string                 `json:"function"`
	AgentName  string                 `json:"agent_name"`
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
	AgentName  string `json:"agent_name"`
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
	AgentName  string                 `json:"agent_name"`
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
	// ResponseText carries the direct answer when CallAgent is false.
	ResponseText string `json:"response_text,omitempty"`
	RawResponse  string `json:"raw_response,omitempty"`
}

func (e *AgentCallDecidedEvent) Type() string { return "orchestration_AgentCallDecided" }
func (e *AgentCallDecidedEvent) Marshal() ([]byte, error) {
	e.EventType = e.Type()
	return json.Marshal(e)
}
func (e *AgentCallDecidedEvent) Unmarshal(data []byte) error { return json.Unmarshal(data, e) }

// AgentResponseEvent captures textual responses emitted by the orchestrator or delegated agents.
type AgentResponseEvent struct {
	eventsourcing.BaseEvent
	EventType    string `json:"event_type"`
	RequestID    string `json:"request_id"`
	AgentName    string `json:"agent_name"`
	ResponseText string `json:"response_text"`
	RawResponse  string `json:"raw_response"`
	Stage        string `json:"stage"` // e.g. "pre_tool", "final"
	Timestamp    string `json:"timestamp"`
}

func (e *AgentResponseEvent) Type() string { return "orchestration_AgentResponseCreated" }
func (e *AgentResponseEvent) Marshal() ([]byte, error) {
	e.EventType = e.Type()
	return json.Marshal(e)
}
func (e *AgentResponseEvent) Unmarshal(data []byte) error { return json.Unmarshal(data, e) }

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
	AgentName  string `json:"agent_name"`
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
	eventsourcing.RegisterEvent("orchestration_AgentResponseCreated", func() eventsourcing.Event { return &AgentResponseEvent{} })
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

func (a *OrchestrationAggregate) nextOrchestratorPosition() []float64 {
	radius := 2.0
	height := 0.0 - float64(a.OrchestratorPositionIndex)*0.5
	angle := 2 * math.Pi * float64(a.OrchestratorPositionIndex) / 16
	x := radius * math.Cos(angle)
	z := radius * math.Sin(angle)
	a.OrchestratorPositionIndex++
	return []float64{x, height, z}
}

func (a *OrchestrationAggregate) addBox(builder *ui3d.DeltaBuilder, boxID string, pos []float64, eventType string, color []float64, extra map[string]interface{}) {
	boxExtra := map[string]interface{}{
		"event_type": eventType,
	}
	if color != nil {
		boxExtra["material_override"] = map[string]interface{}{
			"albedo_color": color,
		}
	}
	if eventType != "orchestrator_ai" {
		boxExtra["layer"] = "underground"
	}
	for k, v := range extra {
		boxExtra[k] = v
	}
	boxBuilder := builder.CreateBox(boxID, pos).WithExtra(boxExtra)
	// Use GLTF model for orchestrator AI
	if boxID == "orchestrator_ai" {
		boxBuilder.WithModel("res://models/brain.glb")
	}
}

func (a *OrchestrationAggregate) addLabel(builder *ui3d.DeltaBuilder, labelID, labelText string, pos []float64, eventType string) {
	builder.CreateLabel(labelID, labelText, pos).WithExtra(map[string]interface{}{
		"event_type": eventType,
		"layer":      "underground",
	})
}

func (a *OrchestrationAggregate) addEventObject(builder *ui3d.DeltaBuilder, baseID string, pos []float64, eventType string, color []float64, labelText string, extra map[string]interface{}) string {
	nodeID := baseID
	if eventType != "orchestrator_ai" {
		index := a.PositionIndex - 1
		if index < 0 {
			index = 0
		}
		nodeID = fmt.Sprintf("%s_%05d", sanitizeID(baseID), index)
	}

	a.addBox(builder, nodeID, pos, eventType, color, extra)
	if displayInfo, exists := a.DisplayInfos[baseID]; exists {
		builder.WithDisplayInfo(displayInfo)
	}
	if labelText != "" {
		labelID := fmt.Sprintf("%s_label", nodeID)
		labelPos := []float64{pos[0], pos[1] + 1.0, pos[2]}
		a.addLabel(builder, labelID, labelText, labelPos, eventType)
		if displayInfo, exists := a.DisplayInfos[labelID]; exists {
			builder.WithDisplayInfo(displayInfo)
		}
	}
	return nodeID
}

func (a *OrchestrationAggregate) EmitDelta(event eventsourcing.Event) *eventsourcing.DeltaEnvelope {
	logging.GetLogger().Info("EmitDelta called for event: %s", event.Type())
	theme := ui3d.DefaultTheme()
	builder := ui3d.NewDeltaBuilder(theme)

	// Create orchestrator_ai if not already done
	if !a.OrchestratorAICreated {
		pos := []float64{0, 0, 0}         // Center position
		color := []float64{1, 0.84, 0, 1} // Gold color
		_ = a.addEventObject(builder, "orchestrator_ai", pos, "orchestrator_ai", color, "Orchestrator AI", nil)
		a.OrchestratorAICreated = true
		logging.GetLogger().Info("Created orchestrator_ai object in the middle")
	}

	switch e := event.(type) {
	// Orchestration events
	case *UserRequestReceivedEvent:
		pos := a.nextPosition()
		boxID := fmt.Sprintf("request_%s", e.RequestID)
		extra := map[string]interface{}{
			"request_id": e.RequestID,
			"text":       e.RequestText,
			"timestamp":  e.Timestamp,
		}
		_ = a.addEventObject(builder, boxID, pos, "user_request_received", []float64{0, 1, 0, 1}, "User Request", extra)
		// Animation: Move orchestrator AI to read the request
		orchPos := a.nextOrchestratorPosition()
		builder.AnimateMoveTo("orchestrator_ai", orchPos, 1.0, "ease_in_out")
	case *AgentCallDecidedEvent:
		pos := a.nextPosition()
		boxID := fmt.Sprintf("agent_%s", e.RequestID)
		extra := map[string]interface{}{
			"request_id": e.RequestID,
			"agent_name": e.AgentName,
			"model":      e.Model,
			"timestamp":  e.Timestamp,
			"call_agent": e.CallAgent,
		}
		_ = a.addEventObject(builder, boxID, pos, "agent_call_decided", []float64{0, 0, 1, 1}, "Agent Call Decided", extra)
		// Animation: Move orchestrator AI to read the agent call
		orchPos := a.nextOrchestratorPosition()
		builder.AnimateMoveTo("orchestrator_ai", orchPos, 1.0, "ease_in_out")
	case *AgentResponseEvent:
		pos := a.nextPosition()
		boxID := fmt.Sprintf("agent_response_%s_%s", e.RequestID, sanitizeID(e.Stage))
		extra := map[string]interface{}{
			"request_id": e.RequestID,
			"agent_name": e.AgentName,
			"stage":      e.Stage,
			"timestamp":  e.Timestamp,
		}
		_ = a.addEventObject(builder, boxID, pos, "agent_response", []float64{0.5, 0.2, 1, 1}, "Agent Response", extra)
	case *ToolCallRequestPlaced:
		pos := a.nextPosition()
		boxID := fmt.Sprintf("tool_call_%s", e.ToolCallID)
		extra := map[string]interface{}{
			"request_id":   e.RequestID,
			"tool_call_id": e.ToolCallID,
			"function":     e.Function,
			"timestamp":    e.Timestamp,
			"agent_name":   e.AgentName,
		}
		_ = a.addEventObject(builder, boxID, pos, "tool_call_started", []float64{1, 1, 0, 1}, "Tool Call Requested", extra)

		logging.GetLogger().Info("ToolCallRequestPlaced: returning 3 actions for tool_call_%s", e.ToolCallID)
	case *ToolCallStarted:
		pos := a.nextPosition()
		boxID := fmt.Sprintf("tool_call_started_%s", e.ToolCallID)
		extra := map[string]interface{}{
			"request_id":   e.RequestID,
			"tool_call_id": e.ToolCallID,
			"function":     e.Function,
			"timestamp":    e.Timestamp,
			"agent_name":   e.AgentName,
		}
		_ = a.addEventObject(builder, boxID, pos, "tool_call_started", []float64{1, 0.5, 0, 1}, "Tool Call Started", extra)
	case *ToolCallCompleted:
		pos := a.nextPosition()
		boxID := fmt.Sprintf("tool_call_completed_%s", e.ToolCallID)
		extra := map[string]interface{}{
			"request_id":   e.RequestID,
			"tool_call_id": e.ToolCallID,
			"function":     e.Function,
			"timestamp":    e.Timestamp,
			"agent_name":   e.AgentName,
		}
		_ = a.addEventObject(builder, boxID, pos, "tool_call_completed", []float64{0, 1, 0, 1}, "Tool Call Completed", extra)

	case *ToolCallFailedEvent:
		pos := a.nextPosition()
		boxID := fmt.Sprintf("tool_call_failed_%s", e.ToolCallID)
		extra := map[string]interface{}{
			"request_id":   e.RequestID,
			"tool_call_id": e.ToolCallID,
			"function":     e.Function,
			"error":        e.ErrorMsg,
			"timestamp":    e.Timestamp,
			"agent_name":   e.AgentName,
		}
		_ = a.addEventObject(builder, boxID, pos, "tool_call_failed", []float64{1, 0, 0, 1}, "Tool Call Failed", extra)
	case *AgentExecutionFailedEvent:
		pos := a.nextPosition()
		boxID := fmt.Sprintf("agent_failed_%s", e.RequestID)
		extra := map[string]interface{}{
			"request_id": e.RequestID,
			"agent_name": a.AgentName(e.RequestID),
			"error":      e.ErrorMsg,
			"timestamp":  e.Timestamp,
		}
		_ = a.addEventObject(builder, boxID, pos, "agent_execution_failed", []float64{1, 0, 0, 1}, "Agent Failed", extra)
	case *RequestCompletedEvent:
		pos := a.nextPosition()
		boxID := fmt.Sprintf("completed_%s", e.RequestID)
		extra := map[string]interface{}{
			"request_id": e.RequestID,
			"timestamp":  e.CompletedAt,
		}
		_ = a.addEventObject(builder, boxID, pos, "request_completed", []float64{0, 1, 0, 1}, "Request Completed", extra)
		// Animation: Move orchestrator AI back to center after reading
		builder.AnimateMoveTo("orchestrator_ai", []float64{0, 0, 0}, 2.0, "ease_out")

	// Task events
	case *eventsourcing.InitiatePluginCreationEvent:
		pos := a.nextPosition()
		boxID := fmt.Sprintf("plugin_%s", e.PluginName)
		extra := map[string]interface{}{
			"plugin_name": e.PluginName,
			"description": e.Description,
		}
		_ = a.addEventObject(builder, boxID, pos, "plugin_generated", nil, "Plugin Generated", extra)

	// Add more event types as needed, e.g., task events, calendar events, etc.
	// For now, handle a few key ones to visualize event history
	default:
		// For any other event, place in the underground spiral
		pos := a.nextPosition()
		baseID := fmt.Sprintf("external_%s", event.Type())
		_ = a.addEventObject(builder, baseID, pos, event.Type(), nil, prettifyEventLabel(event.Type()), nil)
	}
	return &eventsourcing.DeltaEnvelope{
		Type:      "delta",
		Aggregate: "orchestration",
		EventID:   eventsourcing.ISOTimestamp(),
		Timestamp: eventsourcing.ISOTimestamp(),
		Actions:   builder.Build(),
	}
}

func (a *OrchestrationAggregate) GetChatManager() *chat.ChatManager {
	return a.chatState.GetChatManager()
}

func (a *OrchestrationAggregate) Clone() eventsourcing.Aggregate {
	cloned := NewOrchestrationAggregate()
	cloned.RequestIDs = make([]string, len(a.RequestIDs))
	copy(cloned.RequestIDs, a.RequestIDs)
	return cloned
}
