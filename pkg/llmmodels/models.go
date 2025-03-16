package llmmodels

// Message defines the structure for Ollama API chat messages
type Message struct {
	Role    string `json:"role"`
	Name    string `json:"name"`
	Content string `json:"content"`
}

// OllamaRequest represents the request structure for the Ollama API.
type OllamaRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	Stream   bool      `json:"stream"`
	Tools    []Tool    `json:"tools,omitempty"`
}

// Tool represents a function tool available to the LLM.
type Tool struct {
	Type     string                 `json:"type"`
	Function map[string]interface{} `json:"function"`
}

// OllamaFunction represents the function details within a tool call
type OllamaFunction struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments"`
}

// OllamaToolCall represents a single tool call within the message
type OllamaToolCall struct {
	Function OllamaFunction `json:"function"`
}

// OllamaMessage represents the message content in the response
type OllamaMessage struct {
	Role      string           `json:"role"`
	Content   string           `json:"content"`
	ToolCalls []OllamaToolCall `json:"tool_calls,omitempty"`
}

// OllamaResponse represents the full response structure
type OllamaResponse struct {
	Message OllamaMessage `json:"message"`
	Done    bool          `json:"done"`
}

// StreamHandler defines a callback function for handling streaming responses
type StreamHandler func(chunk *OllamaResponse)

// OllamaStreamingEvent represents the streaming event for UI updates
type OllamaStreamingEvent struct {
	RequestID      string `json:"request_id"`
	PartialContent string `json:"partial_content"`
	IsFinal        bool   `json:"is_final"`
	HasToolCalls   bool   `json:"has_tool_calls"`
}
