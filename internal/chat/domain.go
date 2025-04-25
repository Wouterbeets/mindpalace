package chat

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"mindpalace/pkg/llmmodels"
)

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
}

// ChatManager now tracks messages by agent
type ChatManager struct {
	messages       map[string][]Message // Agent name -> message history (empty key for core MindPalace)
	maxContextSize int                  // Max messages per agent in LLM context
	systemPrompt   string               // Base system prompt
	pluginPrompts  map[string]string    // Plugin-specific prompts
}

// NewChatManager initializes with a map for agent histories
func NewChatManager(maxContextSize int, baseSystemPrompt string) *ChatManager {
	return &ChatManager{
		messages:       make(map[string][]Message),
		maxContextSize: maxContextSize,
		systemPrompt:   baseSystemPrompt,
		pluginPrompts:  make(map[string]string),
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
	}
	if _, exists := cm.messages[agent]; !exists {
		cm.messages[agent] = make([]Message, 0)
	}
	cm.messages[agent] = append(cm.messages[agent], msg)
}

func (cm *ChatManager) GetLLMContext(activeAgents []string) []llmmodels.Message {
	// Build dynamic system prompt
	var systemContent strings.Builder
	systemContent.WriteString(cm.systemPrompt)
	for _, agent := range activeAgents {
		if prompt, exists := cm.pluginPrompts[agent]; exists {
			systemContent.WriteString("\n\n")
			systemContent.WriteString(prompt)
		}
	}

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

	// Trim to max context size (most recent)
	if len(mergedMessages) > cm.maxContextSize {
		mergedMessages = mergedMessages[len(mergedMessages)-cm.maxContextSize:]
	}

	// Convert to LLM format
	for _, msg := range mergedMessages {
		result = append(result, llmmodels.Message{
			Role:    string(msg.Role.SystemRole),
			Content: msg.Content,
		})
	}
	return result
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
