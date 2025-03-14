package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mindpalace/internal/core"
	"mindpalace/pkg/eventsourcing"
	"net/http"
	"strings"
)

// LLMProcessorPlugin defines the plugin structure with fields for event submission and plugin management.
type LLMProcessorPlugin struct {
	submitEvent   func(eventsourcing.Event)
	pluginManager *core.PluginManager
}

// SetPluginManager sets the plugin manager for the plugin instance.
func (p *LLMProcessorPlugin) SetPluginManager(pm *core.PluginManager) {
	p.pluginManager = pm
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

// SetSubmitEvent sets the event submission function for the plugin.
func (p *LLMProcessorPlugin) SetSubmitEvent(f func(eventsourcing.Event)) {
	p.submitEvent = f
}

// Commands returns a map of command handlers, using a closure to provide access to the plugin instance.
func (p *LLMProcessorPlugin) Commands() map[string]eventsourcing.CommandHandler {
	return map[string]eventsourcing.CommandHandler{
		"ProcessUserRequest": func(data map[string]interface{}, state map[string]interface{}) ([]eventsourcing.Event, error) {
			return ProcessUserRequest(p, data, state)
		},
	}
}

// Schemas defines the schema for the ProcessUserRequest command.
func (p *LLMProcessorPlugin) Schemas() map[string]map[string]interface{} {
	return map[string]map[string]interface{}{
		"ProcessUserRequest": {
			"RequestText": "string",
		},
	}
}

// Type returns the plugin type as a system plugin.
func (p *LLMProcessorPlugin) Type() eventsourcing.PluginType {
	return eventsourcing.SystemPlugin
}

// EventHandlers defines event handlers for the plugin.
func (p *LLMProcessorPlugin) EventHandlers() map[string]eventsourcing.EventHandler {
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

// Message represents a single message in the chat.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type OllamaResponse struct {
	Message struct {
		Role      string `json:"role"`
		Content   string `json:"content"`
		ToolCalls []struct {
			Function struct {
				Name      string                 `json:"name"`
				Arguments map[string]interface{} `json:"arguments"`
			} `json:"function"`
		} `json:"tool_calls,omitempty"`
	} `json:"message"`
	Done bool `json:"done"`
}

// systemPrompt defines the behavior and tone of the MindPalace AI assistant.
var systemPrompt = `You are MindPalace, a versatile and friendly AI assistant designed to assist users with a wide range of queries and tasks. Your mission is to provide helpful, accurate, and concise responses while leveraging specialized tools (functions) through plugins when needed. Always aim to understand the user’s intent and deliver value in a natural, conversational way.

### Core Principles:
1. **User-First Approach**: Your top priority is to assist the user effectively. Answer directly whenever possible, and only use tools when they enhance your ability to meet the user’s needs.
2. **Smart Tool Usage**: You have access to plugins that enable specific functions (e.g., task creation, information retrieval). Use these tools thoughtfully:
   - **When Explicitly Requested**: If the user asks for a task requiring a tool (e.g., "Set a reminder for 3 PM").
   - **When Implied**: If the request suggests a tool is necessary (e.g., "What’s my schedule today?" if a calendar tool is available).
   - Avoid tool calls if the information is already accessible or the task can be handled without them.
3. **Contextual Intelligence**: Pay attention to the conversation flow. Use prior context to refine your responses and avoid redundant actions or tool calls.
4. **Clarity and Efficiency**: Provide concise, relevant answers. When using a tool, weave its output seamlessly into your response without unnecessary technical details—unless the user asks for them.
5. **Graceful Uncertainty**: If you’re unsure about the user’s intent or the best course of action, ask clarifying questions or make reasonable assumptions (stating them clearly) to keep the interaction smooth.

### How to Respond:
- **Direct Answers**: If no tool is needed, respond promptly and accurately.  
  *Example*: User: "What’s 5 + 7?" → "5 + 7 is 12."
- **Tool-Assisted Responses**: When a tool is required, use it efficiently and explain the outcome conversationally.  
  *Example*: User: "Add a task to email Sarah" → "I’ve added a task for you: ‘Email Sarah.’ Anything else you’d like to include?"
- **Clarification Requests**: If the query is vague, seek clarity politely.  
  *Example*: User: "What’s happening tomorrow?" → "Could you let me know if you mean your schedule, the weather, or something else?"

### Tone and Style:
- Be friendly, approachable, and engaging—like a knowledgeable friend.
- Avoid jargon or overly formal language unless the user prefers it.
- Keep responses concise but complete, balancing brevity with usefulness.

### Example Interactions:
- **User**: "What time is it in London?"  
  **Response**: "The current time in London is [time], assuming you mean London, UK. Let me know if you meant a different London!"
- **User**: "Create a task to call Mom."  
  **Response**: "I’ve created a task: ‘Call Mom.’ Want to set a specific time for it?"
- **User**: "Tell me about AI."  
  **Response**: "AI, or artificial intelligence, is a field where machines are designed to mimic human intelligence—like me helping you now! Want a deeper dive into how it works?"
- **User**: "What’s next?"  
  **Response**: "I’m not sure what you mean—next in your day, a project, or something else? Could you give me a bit more context?"

### Final Notes:
- Stay adaptable: Users may have diverse needs, so tailor your approach accordingly.
- Use tools as an enhancement, not a crutch—your intelligence shines through in how you apply them.
- Always strive to make the user’s experience seamless and enjoyable.`

// ProcessUserRequest processes a user request, accessing plugin fields via the plugin instance.
func ProcessUserRequest(p *LLMProcessorPlugin, data map[string]interface{}, state map[string]interface{}) ([]eventsourcing.Event, error) {
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

	// Collect tools from the plugin manager
	var tools []Tool
	if p.pluginManager != nil {
		for _, plugin := range p.pluginManager.GetLLMPlugins() {
			for name, schema := range plugin.Schemas() {
				tools = append(tools, Tool{
					Type: "function",
					Function: map[string]interface{}{
						"name":        name,
						"description": schema["description"],
						"parameters":  schema["parameters"],
					},
				})
			}
		}
	}

	// Perform HTTP request in a background goroutine
	go func() {
		ollamaReq := OllamaRequest{
			Model: "qwq",
			Messages: []Message{
				{Role: "system", Content: systemPrompt},
				{Role: "user", Content: requestText},
			},
			Stream: false,
			Tools:  tools,
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
		var functionCalls []call
		for _, tc := range ollamaResp.Message.ToolCalls {
			functionCalls = append(functionCalls, struct {
				Name      string
				Arguments map[string]interface{}
			}{
				Name:      tc.Function.Name,
				Arguments: tc.Function.Arguments,
			})
		}
		// Pass function calls to next step (2.5)
		if len(functionCalls) > 0 {
			p.handleFunctionCalls(functionCalls, tools, requestText, state)
		} else if eventsourcing.SubmitEvent != nil {
			completedEvent := &eventsourcing.GenericEvent{
				EventType: "LLMProcessingCompleted",
				Data: map[string]interface{}{
					"RequestText":  requestText,
					"ResponseText": responseText,
				},
			}
			eventsourcing.SubmitEvent(completedEvent)
		}
	}()
	return []eventsourcing.Event{startedEvent}, nil
}

type call struct {
	Name      string
	Arguments map[string]interface{}
}

func (p *LLMProcessorPlugin) handleFunctionCalls(calls []call, tools []Tool, requestText string, state map[string]interface{}) {
	var pluginResults []string
	for _, call := range calls {
		cmdName := call.Name
		args := call.Arguments
		var cmdHandler eventsourcing.CommandHandler
		for _, plugin := range p.pluginManager.GetLLMPlugins() {
			if handler, exists := plugin.Commands()[cmdName]; exists {
				cmdHandler = handler
				break
			}
		}
		if cmdHandler == nil {
			log.Printf("Command %s not found", cmdName)
			continue
		}
		events, err := cmdHandler(args, state)
		if err != nil {
			log.Printf("Failed to execute %s: %v", cmdName, err)
			continue
		}
		for _, event := range events {
			if eventsourcing.SubmitEvent != nil {
				eventsourcing.SubmitEvent(event)
			}
		}
		if len(events) > 0 {
			eventData, _ := json.Marshal(events[0].(*eventsourcing.GenericEvent).Data)
			pluginResults = append(pluginResults, string(eventData))
		}
	}
	if len(pluginResults) > 0 {
		ollamaReq := OllamaRequest{
			Model: "qwq",
			Messages: []Message{
				{Role: "system", Content: systemPrompt},
				{Role: "user", Content: requestText},
				{Role: "assistant", Content: "Plugin results: " + strings.Join(pluginResults, "; ")},
			},
			Stream: false,
			Tools:  tools, // Reuse tools from earlier
		}
		reqBody, _ := json.Marshal(ollamaReq)
		resp, err := http.Post("http://localhost:11434/api/chat", "application/json", bytes.NewBuffer(reqBody))
		if err != nil {
			log.Printf("Failed to send plugin results to Ollama: %v", err)
			return
		}
		defer resp.Body.Close()
		var finalResp OllamaResponse
		json.NewDecoder(resp.Body).Decode(&finalResp)
		if eventsourcing.SubmitEvent != nil {
			eventsourcing.SubmitEvent(&eventsourcing.GenericEvent{
				EventType: "LLMProcessingCompleted",
				Data: map[string]interface{}{
					"RequestText":  requestText,
					"ResponseText": finalResp.Message.Content,
				},
			})
		}
	}
}

// NewPlugin creates a new instance of the LLMProcessorPlugin.
func NewPlugin() eventsourcing.Plugin {
	return &LLMProcessorPlugin{}
}
