package llmmodels

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMessageJSON(t *testing.T) {
	msg := Message{
		Role:    "user",
		Name:    "test",
		Content: "hello",
	}
	data, err := json.Marshal(msg)
	assert.NoError(t, err)
	var decoded Message
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)
	assert.Equal(t, msg, decoded)
}

func TestOllamaRequestJSON(t *testing.T) {
	req := OllamaRequest{
		Model: "llama2",
		Messages: []Message{
			{Role: "user", Content: "hi"},
		},
		Stream: true,
		NumCtx: 2048,
	}
	data, err := json.Marshal(req)
	assert.NoError(t, err)
	var decoded OllamaRequest
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)
	assert.Equal(t, req, decoded)
}

func TestOllamaResponseJSON(t *testing.T) {
	resp := OllamaResponse{
		Message: OllamaMessage{
			Role:    "assistant",
			Content: "response",
			ToolCalls: []OllamaToolCall{
				{
					Function: OllamaFunction{
						Name: "test_func",
						Arguments: map[string]interface{}{
							"arg": "value",
						},
					},
				},
			},
		},
		Done: true,
	}
	data, err := json.Marshal(resp)
	assert.NoError(t, err)
	var decoded OllamaResponse
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)
	assert.Equal(t, resp, decoded)
}

func TestOllamaStreamingEventJSON(t *testing.T) {
	event := OllamaStreamingEvent{
		RequestID:      "req1",
		PartialContent: "partial",
		IsFinal:        false,
		HasToolCalls:   true,
	}
	data, err := json.Marshal(event)
	assert.NoError(t, err)
	var decoded OllamaStreamingEvent
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)
	assert.Equal(t, event, decoded)
}
