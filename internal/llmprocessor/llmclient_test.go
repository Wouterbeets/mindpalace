package llmprocessor

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestShimmyClient_ChatCompletion(t *testing.T) {
	// Mock OpenAI response
	mockResponse := map[string]interface{}{
		"choices": []map[string]interface{}{
			{
				"message": map[string]interface{}{
					"role":    "assistant",
					"content": "Hello, world!",
					"tool_calls": []map[string]interface{}{
						{
							"id":   "call-1",
							"type": "function",
							"function": map[string]interface{}{
								"name":      "test_func",
								"arguments": `{"arg": "value"}`,
							},
						},
					},
				},
			},
		},
	}
	responseBody, _ := json.Marshal(mockResponse)

	// Create test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(responseBody)
	}))
	defer server.Close()

	// Create client
	client := NewShimmyClient(server.URL+"/v1", "sk-test")

	// Test
	messages := []Message{
		{Role: "user", Content: "Hello"},
	}
	tools := []Tool{
		{
			Type: "function",
			Function: FunctionDef{
				Name:        "test_func",
				Description: "Test function",
				Parameters:  json.RawMessage(`{"type": "object"}`),
			},
		},
	}

	ctx := context.Background()
	resp, err := client.ChatCompletion(ctx, messages, tools, false)

	// Assert
	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Len(t, resp.Choices, 1)
	assert.Equal(t, "assistant", resp.Choices[0].Message.Role)
	assert.Equal(t, "Hello, world!", resp.Choices[0].Message.Content)
	assert.Len(t, resp.Choices[0].ToolCalls, 1)
	assert.Equal(t, "call-1", resp.Choices[0].ToolCalls[0].ID)
	assert.Equal(t, "function", resp.Choices[0].ToolCalls[0].Type)
	assert.Equal(t, "test_func", resp.Choices[0].ToolCalls[0].Function.Name)
	assert.Equal(t, json.RawMessage(`"{\"arg\": \"value\"}"`), resp.Choices[0].ToolCalls[0].Function.Arguments)
}
