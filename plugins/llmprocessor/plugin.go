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

type LLMProcessorPlugin struct {
	submitEvent func(eventsourcing.Event)
}

func (p *LLMProcessorPlugin) SetSubmitEvent(f func(eventsourcing.Event)) {
	p.submitEvent = f
}

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

var systempPrompt = `
You are an AI assistant tasked with generating Go code for a custom plugin in the MindPalace system, based on the user’s specified functionality. MindPalace is an event-sourcing-based application written in Go that supports extensible functionality through plugins. Plugins are compiled as .so files and loaded dynamically from a plugins directory, enabling them to define commands (executable actions) and optionally react to events.

Your goal is to create a plugin that implements the eventsourcing.Plugin interface, providing commands, schemas (for LLM plugins), a plugin type, and event handlers (if applicable), all tailored to the user’s request.

# Essential Details
## Package and Imports
* Use package main for the plugin.
* Import the event-sourcing package as:

import "mindpalace/pkg/eventsourcing"

## Plugin Interface
Your plugin must implement these exact methods:
* GetCommands() map[string]eventsourcing.CommandHandler
	* Returns a map of command names (e.g., "GetWeather") to their handler functions.
	* Handler signature:
func(data map[string]interface{}, state map[string]interface{}) ([]eventsourcing.Event, error)

GetSchemas() map[string]map[string]interface{}

For LLMPlugins, returns a map of command names to schemas.
Schema structure: a map with keys "description" (string) and "parameters" (map[string]interface{}).
If no inputs are needed, use "parameters": map[string]interface{}{} (empty map).
GetType() eventsourcing.PluginType
Returns eventsourcing.LLMPlugin if invoked by an LLM, or eventsourcing.SystemPlugin otherwise.
GetEventHandlers() map[string]eventsourcing.EventHandler
Returns a map of event types to handlers, or nil if none.
Handler signature:
func(event eventsourcing.Event) error
Event Creation
Create events using:
&eventsourcing.GenericEvent{
    EventType: "EventName", // Use camelCase, e.g., "WeatherReport"
    Data: map[string]interface{}{"key": "value"},
}
EventType should reflect the event’s purpose (e.g., "WeatherReport").
Data Handling
Use mock data by default for simplicity, unless the user specifies an API or real data source.
Add comments (e.g., // TODO: Replace with API call) where real data fetching would occur.
NewPlugin Function
Include:
go

func NewPlugin() eventsourcing.Plugin
Returns an instance of your plugin struct.

# Steps to Build the Plugin
1. Understand the Request
Identify the functionality (e.g., “weather for Tokyo”).
Define necessary commands (e.g., "GetWeather").
Check if event handling is required (usually not unless specified).

2. Select Plugin Type
Use eventsourcing.LLMPlugin if the LLM invokes commands; otherwise, use eventsourcing.SystemPlugin.

3. Implement Commands
For each command:
Name it uniquely (e.g., "GetWeather").
Write a handler that processes data and state, returning events.
Use mock data with a comment for real logic.

4. Define Schemas (for LLMPlugins)
For each command, provide:
"description": A clear string (e.g., "Gets Tokyo's weather").
"parameters": An empty map if no inputs, or define parameters if needed.

5. Handle Events (if needed)
Map event types to handlers in GetEventHandlers(); return nil if unused.

6.Structure the Code
Define a struct (e.g., WeatherPlugin).
Implement all interface methods.
Export via NewPlugin().
Example: Weather Plugin for Tokyo
For a plugin providing Tokyo’s weather:

package main

import "mindpalace/pkg/eventsourcing"

type WeatherPlugin struct{}

func (p *WeatherPlugin) GetCommands() map[string]eventsourcing.CommandHandler {
    return map[string]eventsourcing.CommandHandler{
        "GetWeather": p.getWeather,
    }
}

func (p *WeatherPlugin) getWeather(data map[string]interface{}, state map[string]interface{}) ([]eventsourcing.Event, error) {
    // TODO: Replace with real API call (e.g., OpenWeatherMap)
    weatherData := map[string]interface{}{
        "city":        "Tokyo",
        "temperature": 25.0,
        "condition":   "sunny",
    }
    return []eventsourcing.Event{
        &eventsourcing.GenericEvent{
            EventType: "WeatherReport",
            Data:      weatherData,
        },
    }, nil
}

func (p *WeatherPlugin) GetSchemas() map[string]map[string]interface{} {
    return map[string]map[string]interface{}{
        "GetWeather": {
            "description": "Gets the current weather information for Tokyo",
            "parameters":  map[string]interface{}{},
        },
    }
}

func (p *WeatherPlugin) GetType() eventsourcing.PluginType {
    return eventsourcing.LLMPlugin
}

func (p *WeatherPlugin) GetEventHandlers() map[string]eventsourcing.EventHandler {
    return nil // No event handling needed
}

func NewPlugin() eventsourcing.Plugin {
    return &WeatherPlugin{}
}
Your Task
Generate complete, compilable Go code for the plugin based on the user’s request, adhering to this structure and MindPalace’s requirements.`

func ProcessUserRequest(data map[string]interface{}, state map[string]interface{}) ([]eventsourcing.Event, error) {
	requestText, ok := data["RequestText"].(string)
	if !ok {
		return nil, fmt.Errorf("missing RequestText in command data")
	}
	log.Printf("Processing user request: %s", requestText)

	// Immediate event to indicate processing has started
	startedEvent := &eventsourcing.GenericEvent{
		EventType: "LLMProcessingStarted",
		Data: map[string]interface{}{
			"RequestText": requestText,
		},
	}

	// Perform HTTP request in a background goroutine
	go func() {
		ollamaReq := OllamaRequest{
			Model: "qwq",
			Messages: []Message{
				{Role: "system", Content: systempPrompt}, // Your system prompt
				{Role: "user", Content: requestText},
			},
			Stream: false,
		}
		reqBody, err := json.Marshal(ollamaReq)
		if err != nil {
			log.Printf("Failed to marshal Ollama request: %v", err)
			return
		}

		resp, err := http.Post("http://localhost:11434/api/chat", "application/json", bytes.NewBuffer(reqBody))
		if err != nil {
			log.Printf("Failed to call Ollama API: %v", err)
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			log.Printf("Ollama API error: %d, %s", resp.StatusCode, body)
			return
		}

		var ollamaResp OllamaResponse
		if err := json.NewDecoder(resp.Body).Decode(&ollamaResp); err != nil {
			log.Printf("Failed to decode Ollama response: %v", err)
			return
		}

		responseText := ollamaResp.Message.Content
		log.Printf("Received response from Ollama: %s", responseText)

		// Submit completion event using the imported function
		if eventsourcing.SubmitEvent != nil {
			completedEvent := &eventsourcing.GenericEvent{
				EventType: "LLMProcessingCompleted",
				Data: map[string]interface{}{
					"RequestText":  requestText,
					"ResponseText": responseText,
				},
			}
			eventsourcing.SubmitEvent(completedEvent)
		} else {
			log.Println("Warning: eventsourcing.SubmitEvent is nil, cannot submit LLMProcessingCompleted event")
		}
	}()

	return []eventsourcing.Event{startedEvent}, nil
}
func NewPlugin() eventsourcing.Plugin {
	return &LLMProcessorPlugin{}
}
