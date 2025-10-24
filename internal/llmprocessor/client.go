package llmprocessor

import (
	"context"
	"encoding/json"
)

// Message represents a chat message
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Tool represents a tool/function available to the LLM
type Tool struct {
	Type     string      `json:"type"`
	Function FunctionDef `json:"function"`
}

// FunctionDef defines a function/tool
type FunctionDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

// ToolCall represents a tool call made by the LLM
type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function FunctionCall `json:"function"`
}

// FunctionCall represents the function call details
type FunctionCall struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// ChatResponse represents the response from a chat completion
type ChatResponse struct {
	Choices []Choice `json:"choices"`
}

// Choice represents a single choice in the chat response
type Choice struct {
	Message   Message    `json:"message"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
}

// LLMClient interface for LLM inference engines
type LLMClient interface {
	ChatCompletion(ctx context.Context, messages []Message, tools []Tool, stream bool) (*ChatResponse, error)
	Close() error
}
