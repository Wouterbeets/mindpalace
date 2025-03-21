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
	ollamaModel       = "qwq"
	ollamaAPIEndpoint = "http://localhost:11434/api/chat"
)

type LLMClient struct{}

func NewLLMClient() *LLMClient {
	return &LLMClient{}
}

func (c *LLMClient) CallLLM(messages []llmmodels.Message, tools []llmmodels.Tool, requestID string) (*llmmodels.OllamaResponse, error) {
	logging.Trace("in call llm, len messages: %i", len(messages))
	for i, m := range messages {
		runes := []rune(m.Content)
		limit := len(runes)
		if len(runes) > 30 {
			limit = 30
		}
		logging.Trace("message index: %i, Role: %s", i, m.Role, string(runes[:limit]))
	}
	req := llmmodels.OllamaRequest{
		Model:    ollamaModel,
		Messages: messages,
		Stream:   true,
		Tools:    tools,
		NumCtx:   131072, // Set context window size to 131,072 tokens
	}

	reqBody, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %v", err)
	}

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
