package llmprocessor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mindpalace/pkg/llmmodels"
	"mindpalace/pkg/logging"
	"net/http"
	"os"
	"time"
)

const (
	ollamaModel       = "gpt-oss:20b"
	ollamaAPIEndpoint = "http://localhost:11434/api/chat"
	defaultShimmyPort = 11435
)

type OllamaClient struct {
	client *http.Client
}

func (c *OllamaClient) ChatCompletion(ctx context.Context, messages []Message, tools []Tool, stream bool) (*ChatResponse, error) {
	logging.Trace("in chat completion, len messages: %d", len(messages))
	for i, m := range messages {
		runes := []rune(m.Content)
		limit := len(runes)
		if len(runes) > 30 {
			limit = 30
		}
		logging.Trace("message index: %d, Role: %s, Context: %s", i, m.Role, string(runes[:limit]))
	}
	logging.Info("Sending %d messages to Ollama", len(messages))
	for i, m := range messages {
		contentPreview := m.Content
		if len(m.Content) > 100 {
			contentPreview = m.Content[:100] + "..."
		}
		logging.Info("Message %d: Role=%s, Content=%s", i, m.Role, contentPreview)
	}
	if len(tools) > 0 {
		logging.Info("Sending %d tools to Ollama", len(tools))
	}

	// Convert tools to llmmodels.Tool
	llmTools := make([]llmmodels.Tool, len(tools))
	for i, t := range tools {
		llmTools[i] = llmmodels.Tool{
			Type: t.Type,
			Function: map[string]interface{}{
				"name":        t.Function.Name,
				"description": t.Function.Description,
				"parameters":  t.Function.Parameters,
			},
		}
	}

	// Convert messages to llmmodels.Message
	llmMessages := make([]llmmodels.Message, len(messages))
	for i, m := range messages {
		llmMessages[i] = llmmodels.Message{
			Role:    m.Role,
			Content: m.Content,
		}
	}

	req := llmmodels.OllamaRequest{
		Model:    ollamaModel,
		Messages: llmMessages,
		Stream:   false, // Non-stream for POC
		Tools:    llmTools,
		NumCtx:   131072,
	}

	reqBody, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}
	logging.Info("Ollama Request JSON: %s", string(reqBody))

	httpReq, err := http.NewRequestWithContext(ctx, "POST", ollamaAPIEndpoint, bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to call Ollama API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("Ollama API error: %d, %s", resp.StatusCode, body)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var ollamaResp llmmodels.OllamaResponse
	if err := json.Unmarshal(body, &ollamaResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	// Map to ChatResponse
	chatResp := &ChatResponse{
		Choices: []Choice{
			{
				Message: Message{
					Role:    ollamaResp.Message.Role,
					Content: ollamaResp.Message.Content,
				},
				ToolCalls: make([]ToolCall, len(ollamaResp.Message.ToolCalls)),
			},
		},
	}
	for i, tc := range ollamaResp.Message.ToolCalls {
		args, _ := json.Marshal(tc.Function.Arguments)
		chatResp.Choices[0].ToolCalls[i] = ToolCall{
			ID:   fmt.Sprintf("call-%d", i), // Ollama doesn't provide ID, generate one
			Type: "function",
			Function: FunctionCall{
				Name:      tc.Function.Name,
				Arguments: args,
			},
		}
	}

	return chatResp, nil
}

func (c *OllamaClient) Close() error {
	return nil
}

type ShimmyClient struct {
	baseURL string
	apiKey  string
	client  *http.Client
}

func NewShimmyClient(baseURL, apiKey string) *ShimmyClient {
	return &ShimmyClient{
		baseURL: baseURL,
		apiKey:  apiKey,
		client:  &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *ShimmyClient) ChatCompletion(ctx context.Context, messages []Message, tools []Tool, stream bool) (*ChatResponse, error) {
	// Build OpenAI compatible request
	reqBody := map[string]interface{}{
		"model":       "gpt-oss:20b",
		"messages":    messages,
		"tools":       tools,
		"stream":      false,
		"max_tokens":  4096,
		"temperature": 0.7,
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/chat/completions", bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to call Shimmy API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("Shimmy API error: %d, %s", resp.StatusCode, body)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var openaiResp struct {
		Choices []struct {
			Message struct {
				Role      string     `json:"role"`
				Content   string     `json:"content"`
				ToolCalls []ToolCall `json:"tool_calls,omitempty"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(body, &openaiResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if len(openaiResp.Choices) == 0 {
		return nil, fmt.Errorf("no choices in response")
	}

	choice := openaiResp.Choices[0]
	chatResp := &ChatResponse{
		Choices: []Choice{
			{
				Message: Message{
					Role:    choice.Message.Role,
					Content: choice.Message.Content,
				},
				ToolCalls: choice.Message.ToolCalls,
			},
		},
	}

	return chatResp, nil
}

func (c *ShimmyClient) Close() error {
	return nil
}

func NewLLMClient(inference, model string, shimmyPort int) LLMClient {
	switch inference {
	case "shimmy":
		baseURL := fmt.Sprintf("http://127.0.0.1:%d/v1", shimmyPort)
		apiKey := os.Getenv("SHIMMY_API_KEY")
		if apiKey == "" {
			apiKey = "sk-local"
		}
		client := NewShimmyClient(baseURL, apiKey)
		// Note: model is not used in ShimmyClient yet, hardcoded
		return client
	case "ollama":
		fallthrough
	default:
		return &OllamaClient{client: &http.Client{Timeout: 30 * time.Second}}
	}
}
