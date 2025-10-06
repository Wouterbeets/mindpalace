package orchestration

import (
	"mindpalace/internal/chat"
)

// ChatState wraps the ChatManager and handles chat-related event application
type ChatState struct {
	manager *chat.ChatManager
}

// NewChatState creates a new ChatState with the given ChatManager
func NewChatState(manager *chat.ChatManager) *ChatState {
	return &ChatState{manager: manager}
}

// ApplyEvent applies chat-related events to the ChatManager
func (cs *ChatState) ApplyEvent(event interface{}) error {
	// Convert orchestration events to chat event types
	switch e := event.(type) {
	case *UserRequestReceivedEvent:
		chatEvent := &chat.UserRequestReceivedEvent{
			RequestID:   e.RequestID,
			RequestText: e.RequestText,
		}
		return cs.manager.ApplyChatEvent(chatEvent)
	case *ToolCallCompleted:
		chatEvent := &chat.ToolCallCompleted{
			RequestID: e.RequestID,
			Function:  e.Function,
			Results:   e.Results,
		}
		return cs.manager.ApplyChatEvent(chatEvent)
	case *ToolCallFailedEvent:
		chatEvent := &chat.ToolCallFailedEvent{
			RequestID: e.RequestID,
			ErrorMsg:  e.ErrorMsg,
		}
		return cs.manager.ApplyChatEvent(chatEvent)
	case *AgentCallDecidedEvent:
		chatEvent := &chat.AgentCallDecidedEvent{
			RequestID: e.RequestID,
			AgentName: e.AgentName,
		}
		return cs.manager.ApplyChatEvent(chatEvent)
	case *AgentExecutionFailedEvent:
		chatEvent := &chat.AgentExecutionFailedEvent{
			RequestID: e.RequestID,
			ErrorMsg:  e.ErrorMsg,
		}
		return cs.manager.ApplyChatEvent(chatEvent)
	case *RequestCompletedEvent:
		chatEvent := &chat.RequestCompletedEvent{
			RequestID:    e.RequestID,
			ResponseText: e.ResponseText,
		}
		return cs.manager.ApplyChatEvent(chatEvent)
	case *ToolCallStarted:
		chatEvent := &chat.ToolCallStarted{
			RequestID: e.RequestID,
			Function:  e.Function,
		}
		return cs.manager.ApplyChatEvent(chatEvent)
	default:
		// Not a chat event, ignore
		return nil
	}
}

// GetChatManager returns the underlying ChatManager
func (cs *ChatState) GetChatManager() *chat.ChatManager {
	return cs.manager
}
