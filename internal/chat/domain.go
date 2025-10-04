package chat

import (
	"encoding/json"
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
	RequestID string
	Function  string
	Results   map[string]interface{}
}

type ToolCallFailedEvent struct {
	RequestID string
	ErrorMsg  string
}

type AgentCallDecidedEvent struct {
	RequestID string
	AgentName string
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
	RequestID string
	Function  string
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

// GetLLMContext now includes logging for debugging
func (cm *ChatManager) GetLLMContext(activeAgents []string) []llmmodels.Message {
	logging.Info("Building LLM context for active agents: %v", activeAgents)
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

	// Sort by relevance (messages with relevant tags first) then by timestamp
	sort.Slice(mergedMessages, func(i, j int) bool {
		iHasRelevantTag := cm.hasRelevantTag(mergedMessages[i], relevantTags)
		jHasRelevantTag := cm.hasRelevantTag(mergedMessages[j], relevantTags)

		if iHasRelevantTag != jHasRelevantTag {
			return iHasRelevantTag // Relevant messages first
		}
		// If both have same relevance, sort by timestamp (most recent first)
		return mergedMessages[i].Timestamp.After(mergedMessages[j].Timestamp)
	})
	logging.Info("Sorted %d visible messages for LLM context (prioritizing %d relevant tags)", len(mergedMessages), len(relevantTags))

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

// ApplyChatEvent applies chat-related events to the ChatManager
func (cm *ChatManager) ApplyChatEvent(event interface{}) error {
	switch e := event.(type) {
	case *UserRequestReceivedEvent:
		cm.AddMessage(RoleUser, e.RequestText, e.RequestID, "", nil)
	case *ToolCallCompleted:
		bytes, _ := json.Marshal(e.Results)
		agentName := "" // Will be set by caller if needed
		cm.AddMessage(RoleTool, string(bytes), e.RequestID, agentName, map[string]interface{}{
			"function": e.Function,
		})
	case *ToolCallFailedEvent:
		agentName := "" // Will be set by caller if needed
		cm.AddMessage(RoleSystem, fmt.Sprintf("Tool Call failed '%s'", e.ErrorMsg), e.RequestID, agentName, nil)
	case *AgentCallDecidedEvent:
		cm.AddMessage(RoleSystem, fmt.Sprintf("Calling agent '%s'...", e.AgentName), e.RequestID, e.AgentName, nil)
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
		cm.AddMessage(RoleSystem, fmt.Sprintf("Tool Call started'%s'", e.Function), e.RequestID, "", nil)
	default:
		return fmt.Errorf("unsupported event type: %T", event)
	}
	return nil
}
