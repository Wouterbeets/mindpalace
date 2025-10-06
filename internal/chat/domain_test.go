package chat

import (
	"testing"
)

func TestNewChatManager(t *testing.T) {
	cm := NewChatManager(1000, "test prompt")
	if cm == nil {
		t.Fatal("NewChatManager returned nil")
	}
	if cm.maxTokens != 1000 {
		t.Errorf("Expected maxTokens 1000, got %d", cm.maxTokens)
	}
	if cm.systemPrompt != "test prompt" {
		t.Errorf("Expected systemPrompt 'test prompt', got %s", cm.systemPrompt)
	}
}

func TestAddMessage(t *testing.T) {
	cm := NewChatManager(1000, "test")
	cm.AddMessage(RoleUser, "hello", "req1", "", nil)
	messages := cm.GetUIMessages()
	if len(messages) != 1 {
		t.Errorf("Expected 1 message, got %d", len(messages))
	}
	if messages[0].Content != "hello" {
		t.Errorf("Expected content 'hello', got %s", messages[0].Content)
	}
	if messages[0].Role != RoleUser {
		t.Errorf("Expected RoleUser, got %v", messages[0].Role)
	}
}

func TestApplyChatEvent_UserRequestReceived(t *testing.T) {
	cm := NewChatManager(1000, "test")
	event := &UserRequestReceivedEvent{
		RequestID:   "req1",
		RequestText: "test request",
	}
	err := cm.ApplyChatEvent(event)
	if err != nil {
		t.Fatalf("ApplyChatEvent failed: %v", err)
	}
	messages := cm.GetUIMessages()
	if len(messages) != 1 {
		t.Errorf("Expected 1 message, got %d", len(messages))
	}
	if messages[0].Role != RoleUser {
		t.Errorf("Expected RoleUser, got %v", messages[0].Role)
	}
}

func TestApplyChatEvent_ToolCallCompleted(t *testing.T) {
	cm := NewChatManager(1000, "test")
	event := &ToolCallCompleted{
		RequestID: "req1",
		Function:  "testFunc",
		Results:   map[string]interface{}{"result": "ok"},
	}
	err := cm.ApplyChatEvent(event)
	if err != nil {
		t.Fatalf("ApplyChatEvent failed: %v", err)
	}
	messages := cm.GetUIMessages()
	if len(messages) != 1 {
		t.Errorf("Expected 1 message, got %d", len(messages))
	}
	if messages[0].Role != RoleTool {
		t.Errorf("Expected RoleTool, got %v", messages[0].Role)
	}
	if messages[0].Metadata["function"] != "testFunc" {
		t.Errorf("Expected function 'testFunc', got %v", messages[0].Metadata["function"])
	}
}

func TestApplyChatEvent_RequestCompleted(t *testing.T) {
	cm := NewChatManager(1000, "test")
	event := &RequestCompletedEvent{
		RequestID:    "req1",
		ResponseText: "Response with <think>hidden</think> visible",
	}
	err := cm.ApplyChatEvent(event)
	if err != nil {
		t.Fatalf("ApplyChatEvent failed: %v", err)
	}
	messages := cm.GetUIMessages()
	if len(messages) != 1 {
		t.Errorf("Expected 1 message, got %d", len(messages))
	}
	if messages[0].Role != RoleMindPalace {
		t.Errorf("Expected RoleMindPalace, got %v", messages[0].Role)
	}
	if messages[0].Content != "Response with  visible" {
		t.Errorf("Expected content 'Response with  visible', got %s", messages[0].Content)
	}
	// Check hidden message
	allMessages := cm.messages[""]
	if len(allMessages) != 2 {
		t.Errorf("Expected 2 total messages, got %d", len(allMessages))
	}
	if allMessages[0].Role != RoleHidden {
		t.Errorf("First message should be hidden, got %v", allMessages[0].Role)
	}
	if allMessages[0].Content != "hidden" {
		t.Errorf("Hidden content 'hidden', got %s", allMessages[0].Content)
	}
}

func TestApplyChatEvent_Unsupported(t *testing.T) {
	cm := NewChatManager(1000, "test")
	err := cm.ApplyChatEvent("unsupported")
	if err == nil {
		t.Error("Expected error for unsupported event")
	}
}

func TestGetLLMContext(t *testing.T) {
	cm := NewChatManager(1000, "system")
	cm.AddMessage(RoleUser, "hello", "req1", "", nil)
	context := cm.GetLLMContext([]string{})
	if len(context) != 2 { // system + user
		t.Errorf("Expected 2 messages, got %d", len(context))
	}
	if context[0].Role != "system" {
		t.Errorf("First message should be system, got %s", context[0].Role)
	}
	if context[1].Role != "user" {
		t.Errorf("Second message should be user, got %s", context[1].Role)
	}
}

func TestSetPluginPrompt(t *testing.T) {
	cm := NewChatManager(1000, "system")
	cm.SetPluginPrompt("plugin1", "plugin prompt")
	context := cm.GetLLMContext([]string{"plugin1"})
	if !contains(context[0].Content, "plugin prompt") {
		t.Errorf("System prompt should include plugin prompt")
	}
}

func TestResetPluginPrompts(t *testing.T) {
	cm := NewChatManager(1000, "system")
	cm.SetPluginPrompt("plugin1", "prompt")
	cm.ResetPluginPrompts()
	context := cm.GetLLMContext([]string{"plugin1"})
	if contains(context[0].Content, "prompt") {
		t.Errorf("Plugin prompt should be reset")
	}
}

func TestGetTotalTokens(t *testing.T) {
	cm := NewChatManager(1000, "system")
	cm.AddMessage(RoleUser, "hello", "req1", "", nil)
	if cm.GetTotalTokens() == 0 {
		t.Error("Total tokens should be > 0")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && s[:len(substr)] == substr || len(s) > len(substr) && contains(s[1:], substr)
}
