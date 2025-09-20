package llmprocessor

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mindpalace/pkg/llmmodels"
	"mindpalace/pkg/logging"
	"net/http"
	"strings"
)

const (
	ollamaModel       = "gpt-oss:20b"
	ollamaAPIEndpoint = "http://localhost:11434/api/chat"
)

type LLMClient struct{}

func NewLLMClient() *LLMClient {
	return &LLMClient{}
}

func (c *LLMClient) CallLLM(messages []llmmodels.Message, tools []llmmodels.Tool, requestID string, model string) (*llmmodels.OllamaResponse, error) {
	logging.Trace("in call llm, len messages: %i", len(messages))
	for i, m := range messages {
		runes := []rune(m.Content)
		limit := len(runes)
		if len(runes) > 30 {
			limit = 30
		}
		logging.Trace("message index: %i, Role: %s, Context: %s", i, m.Role, string(runes[:limit]))
	}
	logging.Info("Sending %d messages to LLM for request %s", len(messages), requestID)
	for i, m := range messages {
		contentPreview := m.Content
		if len(m.Content) > 100 {
			contentPreview = m.Content[:100] + "..."
		}
		logging.Info("Message %d: Role=%s, Content=%s", i, m.Role, contentPreview)
	}
	if len(tools) > 0 {
		logging.Info("Sending %d tools to LLM", len(tools))
	}
	// Use specified model or default to ollamaModel
	if model == "" {
		model = ollamaModel
	}
	req := llmmodels.OllamaRequest{
		Model:    model,
		Messages: messages,
		Stream:   true,
		Tools:    tools,
		NumCtx:   131072, // Set context window size to 131,072 tokens
	}

	reqBody, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %v", err)
	}
	logging.Info("LLM Request JSON: %s", string(reqBody))

	resp, err := http.Post(ollamaAPIEndpoint, "application/json", bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to call Ollama API: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("Ollama API error: %d, %s", resp.StatusCode, body)
	}

	scanner := bufio.NewScanner(resp.Body)
	var fullContent strings.Builder
	var toolCalls []llmmodels.OllamaToolCall
	for scanner.Scan() {
		var chunk llmmodels.OllamaResponse
		if err := json.Unmarshal(scanner.Bytes(), &chunk); err != nil {
			continue
		}
		fullContent.WriteString(chunk.Message.Content)
		toolCalls = append(toolCalls, chunk.Message.ToolCalls...)
		if chunk.Done {
			return &llmmodels.OllamaResponse{
				Message: llmmodels.OllamaMessage{
					Role:      "assistant",
					Content:   fullContent.String(),
					ToolCalls: toolCalls,
				},
				Done: true,
			}, nil
		}
	}
	return nil, fmt.Errorf("no complete response received")
}
