package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mindpalace/pkg/eventsourcing"
	"net/http"
)

type LLMProcessorPlugin struct{}

func (p *LLMProcessorPlugin) GetCommands() map[string]eventsourcing.CommandHandler {
	return map[string]eventsourcing.CommandHandler{
		"ProcessUserRequest": ProcessUserRequest,
	}
}

func (p *LLMProcessorPlugin) GetSchemas() map[string]map[string]interface{} {
	return map[string]map[string]interface{}{
		"ProcessUserRequest": {
			"RequestText": "string",
		},
	}
}

func (p *LLMProcessorPlugin) GetType() eventsourcing.PluginType {
	return eventsourcing.SystemPlugin
}

func (p *LLMProcessorPlugin) GetEventHandlers() map[string]eventsourcing.EventHandler {
	return map[string]eventsourcing.EventHandler{
		"UserRequestReceived": func(event eventsourcing.Event, state map[string]interface{}, commands map[string]eventsourcing.CommandHandler) ([]eventsourcing.Event, error) {
			log.Println("Handling UserRequestReceived event")
			genericEvent, ok := event.(*eventsourcing.GenericEvent)
			if !ok {
				return nil, fmt.Errorf("event is not a GenericEvent")
			}
			requestText, ok := genericEvent.Data["RequestText"].(string)
			if !ok {
				return nil, fmt.Errorf("missing RequestText in event data")
			}
			handler, exists := commands["ProcessUserRequest"]
			if !exists {
				return nil, fmt.Errorf("ProcessUserRequest command not found")
			}
			log.Printf("Triggering ProcessUserRequest for: %s", requestText)
			return handler(map[string]interface{}{
				"RequestText": requestText,
			}, state)
		},
	}
}

// OllamaRequest represents the request payload to the Ollama API
type OllamaRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	Stream   bool      `json:"stream"`
}

// Message represents a single message in the chat
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// OllamaResponse represents the response from the Ollama API
type OllamaResponse struct {
	Message struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"message"`
	Done bool `json:"done"`
}

func ProcessUserRequest(data map[string]interface{}, state map[string]interface{}) ([]eventsourcing.Event, error) {
	requestText, ok := data["RequestText"].(string)
	if !ok {
		return nil, fmt.Errorf("missing RequestText in command data")
	}
	log.Printf("Processing user request: %s", requestText)

	systemPrompt := `...` // Your system prompt here

	// Prepare the request to Ollama
	ollamaReq := OllamaRequest{
		Model: "qwq",
		Messages: []Message{
			{
				Role:    "system",
				Content: systemPrompt,
			},
			{
				Role:    "user",
				Content: requestText,
			},
		},
		Stream: false,
	}

	reqBody, err := json.Marshal(ollamaReq)
	if err != nil {
		log.Printf("Failed to marshal Ollama request: %v", err)
		return nil, nil
	}

	// Send the request to Ollama
	resp, err := http.Post("http://localhost:11434/api/chat", "application/json", bytes.NewBuffer(reqBody))
	if err != nil {
		log.Printf("Failed to call Ollama API: %v", err)
		return nil, nil

	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		log.Printf("Ollama API returned non-OK status: %d, body: %s", resp.StatusCode, string(body))
		return nil, nil

	}

	// Parse the response
	var ollamaResp OllamaResponse
	if err := json.NewDecoder(resp.Body).Decode(&ollamaResp); err != nil {
		log.Printf("Failed to decode Ollama response: %v", err)
		return nil, nil

	}

	responseText := ollamaResp.Message.Content
	log.Printf("Received response from Ollama: %s", responseText)

	// Generate the completion event
	event := &eventsourcing.GenericEvent{
		EventType: "LLMProcessingCompleted",
		Data: map[string]interface{}{
			"RequestText":  requestText,
			"ResponseText": responseText,
		},
	}
	return []eventsourcing.Event{event}, nil
}
func NewPlugin() eventsourcing.Plugin {
	return &LLMProcessorPlugin{}
}
