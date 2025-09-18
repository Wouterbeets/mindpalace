package orchestration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"regexp"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"mindpalace/internal/chat"
	"mindpalace/pkg/eventsourcing"
)

type OrchestrationAggregate struct {
	chatManager      *chat.ChatManager
	PendingToolCalls map[string]map[string]struct{}
	ToolCallStates   map[string]*ToolCallState
	AgentStates      map[string]*AgentState
}

func NewOrchestrationAggregate() *OrchestrationAggregate {
	// Initialize ChatManager with a base system prompt and context size
	basePrompt := "You are MindPalace, a friendly AI assistant here to help with various queries and tasks."
	return &OrchestrationAggregate{
		chatManager:      chat.NewChatManager(10, basePrompt), // 10 messages max for LLM context
		PendingToolCalls: make(map[string]map[string]struct{}),
		ToolCallStates:   make(map[string]*ToolCallState),
		AgentStates:      make(map[string]*AgentState),
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

func (a *OrchestrationAggregate) AgentName(requestID string) string {
	var agent *AgentState
	var ok bool
	if agent, ok = a.AgentStates[requestID]; !ok {
		return ""
	}
	return agent.AgentName
}

func (a *OrchestrationAggregate) ApplyEvent(event eventsourcing.Event) error {
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

	case "orchestration_ToolCallStarted":
		e := event.(*ToolCallStarted)
		a.chatManager.AddMessage(chat.RoleSystem, fmt.Sprintf("Tool Call started'%s'", e.Function), e.RequestID, a.AgentStates[e.RequestID].AgentName, nil)

	case "orchestration_ToolCallCompleted":
		e := event.(*ToolCallCompleted)
		bytes, _ := json.Marshal(e.Results)
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
		agentName := a.AgentName(e.RequestID)
		a.chatManager.AddMessage(chat.RoleTool, string(bytes), e.RequestID, agentName, map[string]interface{}{
			"function": e.Function,
		})
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

		if agentState, exists := a.AgentStates[e.RequestID]; exists {
			agentState.LastUpdated = eventsourcing.ISOTimestamp()
		}
		agentName := a.AgentName(e.RequestID)
		a.chatManager.AddMessage(chat.RoleSystem, fmt.Sprintf("Tool Call failed '%s'", e.ErrorMsg), e.RequestID, agentName, nil)

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
		a.chatManager.AddMessage(chat.RoleSystem, fmt.Sprintf("Calling agent '%s'...", e.AgentName), e.RequestID, e.AgentName, nil)

	case "orchestration_AgentExecutionFailed":
		e := event.(*AgentExecutionFailedEvent)
		if agentState, exists := a.AgentStates[e.RequestID]; exists {
			agentState.Status = "failed"
			agentState.Summary = fmt.Sprintf("Agent execution failed: %s", e.ErrorMsg)
			agentState.LastUpdated = e.Timestamp
		}
		agentName := a.AgentName(e.RequestID)
		a.chatManager.AddMessage(chat.RoleMindPalace, fmt.Sprintf("Error %s", e.ErrorMsg), e.RequestID, agentName, nil)

	case "orchestration_UserRequestReceived":
		e := event.(*UserRequestReceivedEvent)
		agentName := a.AgentName(e.RequestID)
		a.chatManager.AddMessage(chat.RoleUser, e.RequestText, e.RequestID, agentName, nil)

	case "orchestration_RequestCompleted":
		e := event.(*RequestCompletedEvent)
		thinks, regular := parseResponseText(e.ResponseText)

		agentName := a.AgentName(e.RequestID)
		for _, think := range thinks {
			a.chatManager.AddMessage(chat.RoleHidden, think, e.RequestID, agentName, nil)
		}
		if regular != "" {
			a.chatManager.AddMessage(chat.RoleMindPalace, regular, e.RequestID, agentName, nil)
		}
		if agentState, exists := a.AgentStates[e.RequestID]; exists {
			agentState.Status = "completed"
			agentState.LastUpdated = eventsourcing.ISOTimestamp()
		}
	}
	return nil
}

func (a *OrchestrationAggregate) GetCustomUI() fyne.CanvasObject {
	var chatUIList []fyne.CanvasObject
	messages := a.chatManager.GetUIMessages()

	currentRequestID := ""
	for i, msg := range messages {
		// Check if we've moved to a new request
		if msg.RequestID != currentRequestID && currentRequestID != "" {
			// Render agent state for the previous request, if it exists
			if agentState, exists := a.AgentStates[currentRequestID]; exists {
				chatUIList = append(chatUIList, a.renderAgentState(agentState))
			}
			// Render tool call states for the previous request, if any
			for _, toolState := range a.ToolCallStates {
				if toolState.RequestID == currentRequestID {
					chatUIList = append(chatUIList, a.renderToolCallState(toolState))
				}
			}
			// Add processing indicator if the previous request is ongoing
			if a.isRequestPending(currentRequestID) {
				chatUIList = append(chatUIList, container.NewHBox(
					widget.NewProgressBarInfinite(),
					widget.NewLabel("Processing..."),
				), widget.NewSeparator())
			}
			currentRequestID = msg.RequestID
		} else if currentRequestID == "" {
			currentRequestID = msg.RequestID
		}

		// Render the chat message
		chatUIList = append(chatUIList, a.renderChatMessage(msg))
		if i < len(messages)-1 {
			chatUIList = append(chatUIList, widget.NewSeparator())
		}
	}

	// Handle the last request
	if currentRequestID != "" {
		// Render agent state for the last request, if it exists
		if agentState, exists := a.AgentStates[currentRequestID]; exists {
			chatUIList = append(chatUIList, a.renderAgentState(agentState))
		}
		// Render tool call states for the last request, if any
		for _, toolState := range a.ToolCallStates {
			if toolState.RequestID == currentRequestID {
				chatUIList = append(chatUIList, a.renderToolCallState(toolState))
			}
		}
		// Add processing indicator if the last request is ongoing
		if a.isRequestPending(currentRequestID) {
			chatUIList = append(chatUIList, container.NewHBox(
				widget.NewProgressBarInfinite(),
				widget.NewLabel("Processing..."),
			))
		}
	}

	return container.NewVBox(chatUIList...)
}

func (a *OrchestrationAggregate) renderChatMessage(msg chat.Message) fyne.CanvasObject {
	roleLabel := widget.NewLabel("")
	roleLabel.TextStyle = fyne.TextStyle{Bold: true}
	var content fyne.CanvasObject

	switch msg.Role {
	case chat.RoleUser:
		roleLabel.Text = "You"
		content = parseMarkdownToCanvas(msg.Content)
	case chat.RoleMindPalace:
		roleLabel.Text = "MindPalace"
		content = parseMarkdownToCanvas(msg.Content)
	case chat.RoleTool:
		roleLabel.Text = fmt.Sprintf("%s (tool)", msg.Metadata["function"])
		content = parseMarkdownToCanvas(msg.Content)
	}

	return container.NewVBox(roleLabel, content)
}

// Helper to check if a request is still processing
func (a *OrchestrationAggregate) isRequestPending(requestID string) bool {
	return len(a.PendingToolCalls[requestID]) > 0 || (a.AgentStates[requestID] != nil && a.AgentStates[requestID].Status != "completed")
}

func (a *OrchestrationAggregate) renderAgentState(state *AgentState) fyne.CanvasObject {
	messageContainer := container.NewVBox()
	roleLabel := widget.NewLabel("MindPalace")
	roleLabel.TextStyle = fyne.TextStyle{Bold: true}

	switch state.Status {
	case "deciding":
		statusLabel := widget.NewLabel(fmt.Sprintf("Deciding if agent '%s' is needed...", state.AgentName))
		statusLabel.TextStyle = fyne.TextStyle{Italic: true}
		spinner := widget.NewProgressBarInfinite()
		contentBox := container.NewHBox(spinner, statusLabel)
		messageContainer.Add(container.NewVBox(roleLabel, contentBox))

	case "called":
		statusLabel := widget.NewLabel(fmt.Sprintf("Agent '%s' has been called", state.AgentName))
		statusLabel.TextStyle = fyne.TextStyle{Italic: true}
		icon := widget.NewIcon(theme.InfoIcon())
		contentBox := container.NewHBox(icon, statusLabel)
		messageContainer.Add(container.NewVBox(roleLabel, contentBox))

	case "executing":
		statusLabel := widget.NewLabel(fmt.Sprintf("Agent '%s' is working...", state.AgentName))
		statusLabel.TextStyle = fyne.TextStyle{Italic: true}
		spinner := widget.NewProgressBarInfinite()
		contentBox := container.NewHBox(spinner, statusLabel)
		messageContainer.Add(container.NewVBox(roleLabel, contentBox))

	case "completed":
		statusLabel := widget.NewLabel(fmt.Sprintf("Agent '%s' finished", state.AgentName))
		statusLabel.TextStyle = fyne.TextStyle{Italic: true}
		icon := widget.NewIcon(theme.ConfirmIcon())

		var contentElements []fyne.CanvasObject
		contentElements = append(contentElements, container.NewHBox(icon, statusLabel))

		if state.Summary != "" {
			contentElements = append(contentElements, widget.NewSeparator())
			contentElements = append(contentElements, parseMarkdownToCanvas(state.Summary))
		}

		contentBox := container.NewVBox(contentElements...)
		messageContainer.Add(container.NewVBox(roleLabel, contentBox))

	case "failed":
		statusLabel := widget.NewLabel(fmt.Sprintf("Agent '%s' failed", state.AgentName))
		statusLabel.TextStyle = fyne.TextStyle{Italic: true}
		icon := widget.NewIcon(theme.ErrorIcon())

		var contentElements []fyne.CanvasObject
		contentElements = append(contentElements, container.NewHBox(icon, statusLabel))

		if state.Summary != "" {
			contentElements = append(contentElements, widget.NewSeparator())
			contentElements = append(contentElements, parseMarkdownToCanvas(state.Summary))
		}

		contentBox := container.NewVBox(contentElements...)
		messageContainer.Add(container.NewVBox(roleLabel, contentBox))
	}

	return container.NewPadded(messageContainer)
}

func (a *OrchestrationAggregate) renderToolCallState(state *ToolCallState) fyne.CanvasObject {
	messageContainer := container.NewVBox()
	roleLabel := widget.NewLabel("MindPalace")
	roleLabel.TextStyle = fyne.TextStyle{Bold: true}

	switch state.Status {
	case "requested":
		statusLabel := widget.NewLabel(fmt.Sprintf("Tool Call: %s - Requested", state.Function))
		statusLabel.TextStyle = fyne.TextStyle{Italic: true}
		icon := widget.NewIcon(theme.InfoIcon())
		contentBox := container.NewHBox(icon, statusLabel)
		messageContainer.Add(container.NewVBox(roleLabel, contentBox))

	case "started":
		statusLabel := widget.NewLabel(fmt.Sprintf("Tool Call: %s - In Progress", state.Function))
		statusLabel.TextStyle = fyne.TextStyle{Italic: true}
		spinner := widget.NewProgressBarInfinite()
		contentBox := container.NewHBox(spinner, statusLabel)
		messageContainer.Add(container.NewVBox(roleLabel, contentBox))

	case "completed":
		statusLabel := widget.NewLabel(fmt.Sprintf("Tool Call: %s - Completed", state.Function))
		statusLabel.TextStyle = fyne.TextStyle{Italic: true}
		icon := widget.NewIcon(theme.ConfirmIcon())
		resultText := fmt.Sprintf("%+v", state.Results)
		resultContent := parseMarkdownToCanvas(resultText)
		contentBox := container.NewVBox(
			container.NewHBox(icon, statusLabel),
			widget.NewSeparator(),
			resultContent,
		)
		messageContainer.Add(container.NewVBox(roleLabel, contentBox))

	case "failed":
		statusLabel := widget.NewLabel(fmt.Sprintf("Tool Call: %s - Failed", state.Function))
		statusLabel.TextStyle = fyne.TextStyle{Italic: true}
		icon := widget.NewIcon(theme.ErrorIcon())
		errorText := fmt.Sprintf("%+v", state.Results["error"])
		errorContent := parseMarkdownToCanvas(errorText)
		contentBox := container.NewVBox(
			container.NewHBox(icon, statusLabel),
			widget.NewSeparator(),
			errorContent,
		)
		messageContainer.Add(container.NewVBox(roleLabel, contentBox))
	}

	return container.NewPadded(messageContainer)
}

// parseMarkdownToCanvas converts Markdown text into a styled Fyne CanvasObject (unchanged)
func parseMarkdownToCanvas(text string) fyne.CanvasObject {
	// Create a single Entry for the entire text
	entry := widget.NewEntry()
	entry.MultiLine = true             // Enable multi-line support
	entry.Wrapping = fyne.TextWrapWord // Wrap text naturally

	// Count the number of lines to set a reasonable height
	lineCount := len(strings.Split(text, "\n"))
	if lineCount < 1 {
		lineCount = 1 // Ensure at least one line
	}
	entry.SetMinRowsVisible(lineCount + 1) // Add 1 for padding

	// Set the text and style
	if strings.HasPrefix(text, "# ") {
		entry.SetText(strings.TrimPrefix(text, "# "))
		entry.TextStyle = fyne.TextStyle{Bold: true}
	} else if strings.HasPrefix(text, "## ") {
		entry.SetText(strings.TrimPrefix(text, "## "))
		entry.TextStyle = fyne.TextStyle{Bold: true}
	} else if strings.HasPrefix(text, "- ") || strings.HasPrefix(text, "* ") {
		text = strings.ReplaceAll(text, "- ", "• ")
		text = strings.ReplaceAll(text, "* ", "• ")
		entry.SetText(text)
	} else if strings.HasPrefix(text, "```") && strings.HasSuffix(text, "```") {
		entry.SetText(strings.TrimPrefix(strings.TrimSuffix(text, "```"), "```"))
		entry.TextStyle = fyne.TextStyle{Monospace: true}
	} else {
		entry.SetText(text)
	}

	// Make read-only without disabling to preserve text color
	entry.OnChanged = func(string) {
		// Revert any changes to prevent editing
		entry.SetText(text)
	}

	return entry
}

// parseInlineMarkdown handles inline bold (**text**) and italic (*text*) formatting (unchanged)
func parseInlineMarkdown(text string) []widget.RichTextSegment {
	segments := []widget.RichTextSegment{}
	remaining := text

	for len(remaining) > 0 {
		if boldStart := strings.Index(remaining, "**"); boldStart >= 0 {
			if boldStart > 0 {
				segments = append(segments, &widget.TextSegment{
					Text:  remaining[:boldStart],
					Style: widget.RichTextStyle{TextStyle: fyne.TextStyle{}},
				})
			}
			boldEnd := strings.Index(remaining[boldStart+2:], "**")
			if boldEnd >= 0 {
				boldText := remaining[boldStart+2 : boldStart+2+boldEnd]
				if boldText != "" {
					segments = append(segments, &widget.TextSegment{
						Text:  boldText,
						Style: widget.RichTextStyle{TextStyle: fyne.TextStyle{Bold: true}},
					})
				}
				remaining = remaining[boldStart+2+boldEnd+2:]
			} else {
				segments = append(segments, &widget.TextSegment{
					Text:  remaining,
					Style: widget.RichTextStyle{TextStyle: fyne.TextStyle{}},
				})
				remaining = ""
			}
		} else if italicStart := strings.Index(remaining, "*"); italicStart >= 0 {
			if italicStart > 0 {
				segments = append(segments, &widget.TextSegment{
					Text:  remaining[:italicStart],
					Style: widget.RichTextStyle{TextStyle: fyne.TextStyle{}},
				})
			}
			italicEnd := strings.Index(remaining[italicStart+1:], "*")
			if italicEnd >= 0 {
				italicText := remaining[italicStart+1 : italicStart+1+italicEnd]
				if italicText != "" {
					segments = append(segments, &widget.TextSegment{
						Text:  italicText,
						Style: widget.RichTextStyle{TextStyle: fyne.TextStyle{Italic: true}},
					})
				}
				remaining = remaining[italicStart+1+italicEnd+1:]
			} else {
				segments = append(segments, &widget.TextSegment{
					Text:  remaining,
					Style: widget.RichTextStyle{TextStyle: fyne.TextStyle{}},
				})
				remaining = ""
			}
		} else {
			segments = append(segments, &widget.TextSegment{
				Text:  remaining,
				Style: widget.RichTextStyle{TextStyle: fyne.TextStyle{}},
				})
			remaining = ""
		}
	}

	return segments
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
	RequestID  string `json:"request_id"`
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

// GetWebUI returns HTMX-enabled HTML for the web UI
func (a *OrchestrationAggregate) GetWebUI() string {
	messages := a.chatManager.GetUIMessages()
	var buf bytes.Buffer
	err := OrchestrationPage(messages, a.AgentStates, a.ToolCallStates).Render(context.Background(), &buf)
	if err != nil {
		return "Error rendering template"
	}
	return buf.String()
}
