package chat

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/pkoukk/tiktoken-go"
	"mindpalace/pkg/llmmodels"
	"mindpalace/pkg/logging"
)

// Event interfaces for chat to apply without circular imports
type UserRequestReceivedEvent struct {
	RequestID   string
	RequestText string
}

type ToolCallCompleted struct {
	RequestID  string
	ToolCallID string
	Function   string
	AgentName  string
	Results    map[string]interface{}
}

type ToolCallFailedEvent struct {
	RequestID  string
	ToolCallID string
	Function   string
	AgentName  string
	ErrorMsg   string
}

type AgentCallDecidedEvent struct {
	RequestID string
	AgentName string
	CallAgent bool
}

type AgentResponseEvent struct {
	RequestID    string
	AgentName    string
	ResponseText string
	RawResponse  string
	Stage        string
}

type AgentExecutionFailedEvent struct {
	RequestID string
	ErrorMsg  string
}

type RequestCompletedEvent struct {
	RequestID    string
	ResponseText string
}

type ToolCallStarted struct {
	RequestID  string
	ToolCallID string
	Function   string
	AgentName  string
}

// Role defines the explicit roles a message can have
type Role struct {
	SystemRole string
	UIRole     string
}

var (
	RoleSystem     Role = Role{SystemRole: "system", UIRole: "MindPalace"}    // System prompt or instructions
	RoleUser       Role = Role{SystemRole: "user", UIRole: "You"}             // User input
	RoleMindPalace Role = Role{SystemRole: "assistant", UIRole: "MindPalace"} // AI response
	RoleAgent      Role = Role{SystemRole: "assistant", UIRole: "Agent"}      // AI response
	RoleTool       Role = Role{SystemRole: "tool", UIRole: "Tool"}            // Tool call results
	RoleHidden     Role = Role{SystemRole: "hidden", UIRole: "None"}          // Internal notes (e.g., "think" messages)
)

// Message now includes an Agent field to tag who "owns" it
type Message struct {
	ID        string                 // Unique identifier
	Role      Role                   // Explicit role
	Content   string                 // The text
	Timestamp time.Time              // When it was created
	RequestID string                 // Links to orchestration request
	Agent     string                 // Plugin/agent name (e.g., "taskmanager"), empty for core MindPalace
	Metadata  map[string]interface{} // Extra data
	Visible   bool                   // UI visibility
	Tags      []string               // Tags for categorization and retrieval
}

// ChatManager now tracks messages by agent
type ChatManager struct {
	messages      map[string][]Message // Agent name -> message history (empty key for core MindPalace)
	totalTokens   map[string]int       // Current token count per agent
	tokenizer     *tiktoken.Tiktoken   // Tokenizer for token counting
	maxTokens     int                  // Max tokens in LLM context
	systemPrompt  string               // Base system prompt
	pluginPrompts map[string]string    // Plugin-specific prompts
}

// NewChatManager initializes with a map for agent histories
func NewChatManager(maxTokens int, baseSystemPrompt string) *ChatManager {
	t, _ := tiktoken.EncodingForModel("gpt-4")

	return &ChatManager{
		messages:      make(map[string][]Message),
		maxTokens:     maxTokens,
		totalTokens:   make(map[string]int),
		tokenizer:     t,
		systemPrompt:  baseSystemPrompt,
		pluginPrompts: make(map[string]string),
	}
}

// AddMessage now assigns messages to an agent (or core if agent is empty)
func (cm *ChatManager) AddMessage(role Role, content string, requestID string, agent string, metadata map[string]interface{}) {
	msg := Message{
		ID:        generateMessageID(requestID),
		Role:      role,
		Content:   content,
		Timestamp: time.Now().UTC(),
		RequestID: requestID,
		Agent:     agent, // e.g., "taskmanager", "dogfoodtracker", or "" for core
		Metadata:  metadata,
		Visible:   role != RoleSystem && role != RoleHidden,
		Tags:      []string{},
	}
	if _, exists := cm.messages[agent]; !exists {
		cm.messages[agent] = make([]Message, 0)
	}
	cm.messages[agent] = append(cm.messages[agent], msg)
	tokens := len(cm.tokenizer.Encode(msg.Content, nil, nil))
	cm.totalTokens[agent] += tokens
}

// SetSystemPrompt allows higher level orchestration code to swap the active system prompt dynamically.
func (cm *ChatManager) SetSystemPrompt(prompt string) {
	cm.systemPrompt = prompt
}

// GetLLMContext now includes logging for debugging
func (cm *ChatManager) GetLLMContext(activeAgents []string) []llmmodels.Message {
	logging.Info("Building LLM context for active agents: %v", activeAgents)
	// Build dynamic system prompt
	var systemContent strings.Builder
	systemContent.WriteString(cm.systemPrompt)
	// Plugin prompts are used elsewhere (e.g., context preview) but we keep the system
	// message itself lean so it can focus on the shared charter.
	logging.Info("System prompt built: %s", systemContent.String())

	result := []llmmodels.Message{
		{Role: string(RoleSystem.SystemRole), Content: systemContent.String()},
	}

	// Merge histories for active agents + core MindPalace
	mergedMessages := make([]Message, 0)
	agentsToMerge := append(activeAgents, "") // Include core (empty agent key)

	for _, agent := range agentsToMerge {
		if agentMsgs, exists := cm.messages[agent]; exists {
			// Filter out hidden messages for LLM
			for _, msg := range agentMsgs {
				if msg.Role != RoleHidden {
					mergedMessages = append(mergedMessages, msg)
				}
			}
		}
	}

	// Sort by timestamp to maintain chronological order
	sort.Slice(mergedMessages, func(i, j int) bool {
		return mergedMessages[i].Timestamp.Before(mergedMessages[j].Timestamp)
	})
	logging.Info("Merged %d visible messages for LLM context", len(mergedMessages))

	// Trim to max tokens (most recent)
	totalTokens := len(cm.tokenizer.Encode(systemContent.String(), nil, nil))
	// Keep messages from the end (most recent) that fit within token limit
	var trimmedMessages []Message
	for i := len(mergedMessages) - 1; i >= 0; i-- {
		msg := mergedMessages[i]
		msgTokens := len(cm.tokenizer.Encode(msg.Content, nil, nil))
		if totalTokens+msgTokens <= cm.maxTokens {
			trimmedMessages = append([]Message{msg}, trimmedMessages...)
			totalTokens += msgTokens
		} else {
			break
		}
	}
	mergedMessages = trimmedMessages

	// Convert to LLM format
	for _, msg := range mergedMessages {
		result = append(result, llmmodels.Message{
			Role:    string(msg.Role.SystemRole),
			Content: msg.Content,
		})
	}
	logging.Info("LLM context prepared with %d messages", len(result))
	return result
}

// GetLLMContextWithTags builds context with tag-based prioritization
func (cm *ChatManager) GetLLMContextWithTags(activeAgents []string, relevantTags []string) []llmmodels.Message {
	logging.Info("Building LLM context for active agents: %v with relevant tags: %v", activeAgents, relevantTags)

	// Build dynamic system prompt
	var systemContent strings.Builder
	systemContent.WriteString(cm.systemPrompt)
	for _, agent := range activeAgents {
		if prompt, exists := cm.pluginPrompts[agent]; exists {
			systemContent.WriteString("\n\n")
			systemContent.WriteString(prompt)
		}
	}
	logging.Info("System prompt built: %s", systemContent.String())

	result := []llmmodels.Message{
		{Role: string(RoleSystem.SystemRole), Content: systemContent.String()},
	}

	// Merge histories for active agents + core MindPalace
	mergedMessages := make([]Message, 0)
	agentsToMerge := append(activeAgents, "") // Include core (empty agent key)

	for _, agent := range agentsToMerge {
		if agentMsgs, exists := cm.messages[agent]; exists {
			// Filter out hidden messages for LLM
			for _, msg := range agentMsgs {
				if msg.Role != RoleHidden {
					mergedMessages = append(mergedMessages, msg)
				}
			}
		}
	}

	//// Sort by relevance (messages with relevant tags first) then by timestamp
	//sort.Slice(mergedMessages, func(i, j int) bool {
	//iHasRelevantTag := cm.hasRelevantTag(mergedMessages[i], relevantTags)
	//jHasRelevantTag := cm.hasRelevantTag(mergedMessages[j], relevantTags)

	//if iHasRelevantTag != jHasRelevantTag {
	//return iHasRelevantTag // Relevant messages first
	//}
	//// If both have same relevance, sort by timestamp (most recent first)
	//return mergedMessages[i].Timestamp.After(mergedMessages[j].Timestamp)
	//})
	//logging.Info("Sorted %d visible messages for LLM context (prioritizing %d relevant tags)", len(mergedMessages), len(relevantTags))

	// Trim to max tokens
	totalTokens := len(cm.tokenizer.Encode(systemContent.String(), nil, nil))
	var trimmedMessages []Message
	for _, msg := range mergedMessages {
		msgTokens := len(cm.tokenizer.Encode(msg.Content, nil, nil))
		if totalTokens+msgTokens <= cm.maxTokens {
			trimmedMessages = append(trimmedMessages, msg)
			totalTokens += msgTokens
		} else {
			break
		}
	}

	// Convert to LLM format
	for _, msg := range trimmedMessages {
		result = append(result, llmmodels.Message{
			Role:    string(msg.Role.SystemRole),
			Content: msg.Content,
		})
	}
	logging.Info("LLM context prepared with %d messages", len(result))
	return result
}

// hasRelevantTag checks if a message has any of the relevant tags
func (cm *ChatManager) hasRelevantTag(msg Message, relevantTags []string) bool {
	for _, msgTag := range msg.Tags {
		for _, relevantTag := range relevantTags {
			if msgTag == relevantTag {
				return true
			}
		}
	}
	return false
}

// Helper function for min
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// GetUIMessages returns a unified, visible history for UI
func (cm *ChatManager) GetUIMessages() []Message {
	visible := make([]Message, 0)
	for _, agentMsgs := range cm.messages {
		for _, msg := range agentMsgs {
			if msg.Visible {
				visible = append(visible, msg)
			}
		}
	}
	sort.Slice(visible, func(i, j int) bool {
		return visible[i].Timestamp.Before(visible[j].Timestamp)
	})
	return visible
}

// GetTotalTokens returns the sum of tokens used across all agents
func (cm *ChatManager) GetTotalTokens() int {
	total := 0
	for _, tokens := range cm.totalTokens {
		total += tokens
	}
	return total
}

// SetPluginPrompt adds or updates a plugin-specific system prompt
func (cm *ChatManager) SetPluginPrompt(pluginName, prompt string) {
	cm.pluginPrompts[pluginName] = prompt
}

// GetConversation returns the most recent visible messages exchanged with the specified agent.
// Use an empty agent name to retrieve the MindPalace orchestrator conversation.
func (cm *ChatManager) GetConversation(agent string, limit int) []Message {
	if limit <= 0 {
		return []Message{}
	}
	agentMsgs, exists := cm.messages[agent]
	if !exists {
		return []Message{}
	}

	visible := make([]Message, 0, len(agentMsgs))
	for _, msg := range agentMsgs {
		if msg.Visible {
			visible = append(visible, msg)
		}
	}

	if len(visible) > limit {
		visible = visible[len(visible)-limit:]
	}

	// Return a shallow copy so callers can't mutate internal slices.
	result := make([]Message, len(visible))
	copy(result, visible)
	return result
}

// Helper to generate unique message IDs
func generateMessageID(requestID string) string {
	return fmt.Sprintf("%s_%d", requestID, time.Now().UnixNano())
}

// ParseResponseText extracts think tags and regular text from LLM responses
func ParseResponseText(responseText string) (thinks []string, regular string) {
	re := regexp.MustCompile(`(?s)<think>(.*?)</think>`)
	matches := re.FindAllStringSubmatch(responseText, -1)
	for _, match := range matches {
		thinks = append(thinks, match[1])
	}
	regular = re.ReplaceAllString(responseText, "")
	return thinks, strings.TrimSpace(regular)
}

// ResetPluginPrompts clears all plugin-specific prompts
func (cm *ChatManager) ResetPluginPrompts() {
	cm.pluginPrompts = make(map[string]string)
}

// ResetContext wipes the active conversation state and optionally replaces the system prompt.
func (cm *ChatManager) ResetContext(newSystemPrompt string) {
	cm.messages = make(map[string][]Message)
	cm.totalTokens = make(map[string]int)
	cm.pluginPrompts = make(map[string]string)
	if newSystemPrompt != "" {
		cm.systemPrompt = newSystemPrompt
	}
}

// SystemPrompt returns the current system prompt configured for the chat context.
func (cm *ChatManager) SystemPrompt() string {
	return cm.systemPrompt
}

// PluginPrompts returns a copy of the active plugin prompt map.
func (cm *ChatManager) PluginPrompts() map[string]string {
	cloned := make(map[string]string, len(cm.pluginPrompts))
	for name, prompt := range cm.pluginPrompts {
		cloned[name] = prompt
	}
	return cloned
}

// ApplyChatEvent applies chat-related events to the ChatManager
func (cm *ChatManager) ApplyChatEvent(event interface{}) error {
	switch e := event.(type) {
	case *UserRequestReceivedEvent:
		cm.AddMessage(RoleUser, e.RequestText, e.RequestID, "", nil)
	case *ToolCallCompleted:
		agentName := e.AgentName
		summary := summarizeToolCallResult(e)
		metadata := map[string]interface{}{
			"function": e.Function,
			"tool_id":  e.ToolCallID,
		}
		if success, ok := extractBool(e.Results["success"]); ok {
			metadata["success"] = success
		}
		if emitted, ok := extractInt(e.Results["events_emitted"]); ok {
			metadata["events_emitted"] = emitted
		}
		if latency, ok := extractFloat(e.Results["latency_ms"]); ok {
			metadata["latency_ms"] = latency
		}
		cm.AddMessage(RoleTool, summary, e.RequestID, agentName, metadata)
	case *ToolCallFailedEvent:
		agentName := e.AgentName
		cm.AddMessage(RoleSystem, fmt.Sprintf("Tool Call failed '%s'", e.ErrorMsg), e.RequestID, agentName, nil)
	case *AgentCallDecidedEvent:
		if e.CallAgent {
			cm.AddMessage(RoleSystem, fmt.Sprintf("Calling agent '%s'...", e.AgentName), e.RequestID, e.AgentName, nil)
		} else {
			cm.AddMessage(RoleSystem, "Handling request directly.", e.RequestID, "", nil)
		}
	case *AgentResponseEvent:
		thinks, regular := ParseResponseText(e.ResponseText)
		for _, think := range thinks {
			cm.AddMessage(RoleHidden, think, e.RequestID, e.AgentName, map[string]interface{}{
				"stage": e.Stage,
			})
		}
		if strings.TrimSpace(regular) != "" {
			role := RoleAgent
			if e.AgentName == "" {
				role = RoleMindPalace
			}
			cm.AddMessage(role, regular, e.RequestID, e.AgentName, map[string]interface{}{
				"stage": e.Stage,
			})
		}
	case *AgentExecutionFailedEvent:
		agentName := "" // Will be set by caller if needed
		cm.AddMessage(RoleMindPalace, fmt.Sprintf("Error %s", e.ErrorMsg), e.RequestID, agentName, nil)
	case *RequestCompletedEvent:
		thinks, regular := ParseResponseText(e.ResponseText)
		agentName := "" // Will be set by caller if needed
		for _, think := range thinks {
			cm.AddMessage(RoleHidden, think, e.RequestID, agentName, nil)
		}
		if regular != "" {
			cm.AddMessage(RoleMindPalace, regular, e.RequestID, agentName, nil)
		}
	case *ToolCallStarted:
		cm.AddMessage(RoleSystem, fmt.Sprintf("Tool Call started '%s'", e.Function), e.RequestID, e.AgentName, map[string]interface{}{
			"tool_id": e.ToolCallID,
		})
	default:
		return fmt.Errorf("unsupported event type: %T", event)
	}
	return nil
}

func summarizeToolCallResult(e *ToolCallCompleted) string {
	if e == nil {
		return "Tool call completed."
	}

	if s, ok := e.Results["summary"].(string); ok && strings.TrimSpace(s) != "" {
		return strings.TrimSpace(s)
	}

	var builder strings.Builder
	if e.Function != "" {
		builder.WriteString(fmt.Sprintf("Tool '%s'", e.Function))
	} else {
		builder.WriteString("Tool call")
	}

	status := "completed"
	if success, ok := extractBool(e.Results["success"]); ok {
		if success {
			status = "succeeded"
		} else {
			status = "reported a failure"
		}
	}
	builder.WriteString(" ")
	builder.WriteString(status)

	if e.AgentName != "" {
		builder.WriteString(fmt.Sprintf(" via %s", e.AgentName))
	}
	if emitted, ok := extractInt(e.Results["events_emitted"]); ok && emitted > 0 {
		builder.WriteString(fmt.Sprintf(", emitted %d event(s)", emitted))
	}
	if latency, ok := extractFloat(e.Results["latency_ms"]); ok && latency > 0 {
		builder.WriteString(fmt.Sprintf(", latency ~%.0fms", latency))
	}

	if status == "reported a failure" {
		if errText, ok := e.Results["error"].(string); ok && errText != "" {
			builder.WriteString(": ")
			builder.WriteString(errText)
		}
	}
	builder.WriteString(".")
	return builder.String()
}

func extractBool(value interface{}) (bool, bool) {
	switch v := value.(type) {
	case bool:
		return v, true
	case string:
		lower := strings.ToLower(strings.TrimSpace(v))
		switch lower {
		case "true", "yes", "1":
			return true, true
		case "false", "no", "0":
			return false, true
		}
	}
	return false, false
}

func extractInt(value interface{}) (int, bool) {
	switch v := value.(type) {
	case int:
		return v, true
	case int8:
		return int(v), true
	case int16:
		return int(v), true
	case int32:
		return int(v), true
	case int64:
		return int(v), true
	case uint:
		return int(v), true
	case uint8:
		return int(v), true
	case uint16:
		return int(v), true
	case uint32:
		return int(v), true
	case uint64:
		return int(v), true
	case float32:
		return int(v), true
	case float64:
		return int(v), true
	}
	return 0, false
}

func extractFloat(value interface{}) (float64, bool) {
	switch v := value.(type) {
	case float32:
		return float64(v), true
	case float64:
		return v, true
	case int:
		return float64(v), true
	case int8:
		return float64(v), true
	case int16:
		return float64(v), true
	case int32:
		return float64(v), true
	case int64:
		return float64(v), true
	case uint:
		return float64(v), true
	case uint8:
		return float64(v), true
	case uint16:
		return float64(v), true
	case uint32:
		return float64(v), true
	case uint64:
		return float64(v), true
	}
	return 0, false
}
