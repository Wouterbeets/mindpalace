package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mindpalace/pkg/eventsourcing"
	"mindpalace/pkg/llmmodels"
	"mindpalace/pkg/logging"
	"net/http"
	"reflect"
	"strings"
)

// Constants for Ollama configuration
const (
	ollamaModel       = "qwq"
	ollamaAPIEndpoint = "http://localhost:11434/api/chat"
)

// systemPrompt defines the behavior and tone of the MindPalace AI assistant.
var systemPrompt = `You are MindPalace, a versatile and friendly AI assistant designed to assist users with a wide range of queries and tasks. Your mission is to provide helpful, accurate, and concise responses while leveraging specialized tools (functions) through plugins when needed. Always aim to understand the user's intent and deliver value in a natural, conversational way.

### Core Principles:
1. **User-First Approach**: Your top priority is to assist the user effectively. Answer directly whenever possible, and only use tools when they enhance your ability to meet the user's needs.
2. **Smart Tool Usage**: You have access to plugins that enable specific functions (e.g., task creation, information retrieval). Use these tools thoughtfully:
   - **When Explicitly Requested**: If the user asks for a task requiring a tool (e.g., "Set a reminder for 3 PM").
   - **When Implied**: If the request suggests a tool is necessary (e.g., "What's my schedule today?" if a calendar tool is available).
   - Avoid tool calls if the information is already accessible or the task can be handled without them.
3. **Contextual Intelligence**: Pay attention to the conversation flow. Use prior context to refine your responses and avoid redundant actions or tool calls.
4. **Clarity and Efficiency**: Provide concise, relevant answers. When using a tool, weave its output seamlessly into your response without unnecessary technical details—unless the user asks for them.
5. **Graceful Uncertainty**: If you're unsure about the user's intent or the best course of action, ask clarifying questions or make reasonable assumptions (stating them clearly) to keep the interaction smooth.

### How to Respond:
- **Direct Answers**: If no tool is needed, respond promptly and accurately.  
  *Example*: User: "What's 5 + 7?" → "5 + 7 is 12."
- **Tool-Assisted Responses**: When a tool is required, use it efficiently and explain the outcome conversationally.  
  *Example*: User: "Add a task to email Sarah" → "I've added a task for you: 'Email Sarah.' Anything else you'd like to include?"
- **Clarification Requests**: If the query is vague, seek clarity politely.  
  *Example*: User: "What's happening tomorrow?" → "Could you let me know if you mean your schedule, the weather, or something else?"

### Tone and Style:
- Be friendly, approachable, and engaging—like a knowledgeable friend.
- Avoid jargon or overly formal language unless the user prefers it.
- Keep responses concise but complete, balancing brevity with usefulness.

### Example Interactions:
- **User**: "What time is it in London?"  
  **Response**: "The current time in London is [time], assuming you mean London, UK. Let me know if you meant a different London!"
- **User**: "Create a task to call Mom."  
  **Response**: "I've created a task: 'Call Mom.' Want to set a specific time for it?"
- **User**: "Tell me about AI."  
  **Response**: "AI, or artificial intelligence, is a field where machines are designed to mimic human intelligence—like me helping you now! Want a deeper dive into how it works?"
- **User**: "What's next?"  
  **Response**: "I'm not sure what you mean—next in your day, a project, or something else? Could you give me a bit more context?"

### Final Notes:
- Stay adaptable: Users may have diverse needs, so tailor your approach accordingly.
- Use tools as an enhancement, not a crutch—your intelligence shines through in how you apply them.
- Always strive to make the user's experience seamless and enjoyable.`

// LLMProcessor implements the eventsourcing.Plugin interface for LLM processing.
type LLMProcessor struct{}

// Name returns the plugin name.
func (p *LLMProcessor) Name() string {
	return "LLMProcessor"
}

// Commands returns the command handlers provided by the plugin.
func (p *LLMProcessor) Commands() map[string]eventsourcing.CommandHandler {
	return map[string]eventsourcing.CommandHandler{
		"ProcessUserRequest": ProcessUserRequest,
	}
}

// Schemas defines the expected data structure for commands.
func (p *LLMProcessor) Schemas() map[string]map[string]interface{} {
	return map[string]map[string]interface{}{
		"ProcessUserRequest": {
			"RequestID":   "string",
			"RequestText": "string",
			"Tools":       "[]llmmodels.Tool",
		},
	}
}

// Type indicates the plugin type.
func (p *LLMProcessor) Type() eventsourcing.PluginType {
	return eventsourcing.SystemPlugin
}

// EventHandlers returns the event handlers for processing specific events.
func (p *LLMProcessor) EventHandlers() map[string]eventsourcing.EventHandler {
	return map[string]eventsourcing.EventHandler{
		"ToolCallsConfigured":   handleToolCallsConfigured,
		"AllToolCallsCompleted": handleAllToolCallsCompleted,
	}
}

// handleToolCallsConfigured processes the ToolCallsConfigured event.
func handleToolCallsConfigured(event eventsourcing.Event, state map[string]interface{}, commands map[string]eventsourcing.CommandHandler) ([]eventsourcing.Event, error) {
	genericEvent, ok := event.(*eventsourcing.GenericEvent)
	if !ok {
		return nil, fmt.Errorf("event is not a GenericEvent")
	}
	requestID, ok := genericEvent.Data["RequestID"].(string)
	if !ok {
		return nil, fmt.Errorf("missing RequestID in event data")
	}
	requestText, ok := genericEvent.Data["RequestText"].(string)
	if !ok {
		return nil, fmt.Errorf("missing RequestText in event data")
	}
	availableTools, ok := genericEvent.Data["Tools"].([]llmmodels.Tool)
	if !ok {
		return nil, fmt.Errorf("missing Tools in event data")
	}
	handler, exists := commands["ProcessUserRequest"]
	if !exists {
		return nil, fmt.Errorf("ProcessUserRequest command not found")
	}
	return handler(map[string]interface{}{
		"RequestID":   requestID,
		"RequestText": requestText,
		"Tools":       availableTools,
	}, state)
}

// handleAllToolCallsCompleted processes the AllToolCallsCompleted event.
func handleAllToolCallsCompleted(event eventsourcing.Event, state map[string]interface{}, commands map[string]eventsourcing.CommandHandler) ([]eventsourcing.Event, error) {
	genericEvent, ok := event.(*eventsourcing.GenericEvent)
	if !ok {
		return nil, fmt.Errorf("event is not a GenericEvent")
	}
	requestID, ok := genericEvent.Data["RequestID"].(string)
	if !ok {
		return nil, fmt.Errorf("missing RequestID in event data")
	}
	toolResults, ok := genericEvent.Data["Results"].([]map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("missing Results in event data")
	}
	messages, err := buildMessagesForToolCompletion(state, requestID, toolResults)
	if err != nil {
		return nil, err
	}

	// Log the conversation for debugging
	log.Printf("Tool completion with %d messages in conversation", len(messages))

	// Use SafeGo instead of a raw goroutine
	eventsourcing.SafeGo("AllToolCallsCompleted", map[string]interface{}{
		"requestID":   requestID,
		"toolResults": toolResults,
	}, func() {
		logging.Info("AllToolCallsCompleted from goroutine")
		ollamaReq := llmmodels.OllamaRequest{
			Model:    ollamaModel,
			Messages: messages,
			Stream:   true, // Enable streaming
		}

		// Call the Ollama API with streaming
		ollamaResp, err := callOllamaAPIWithStreaming(ollamaReq, requestID)
		if err != nil {
			log.Printf("Failed to call Ollama API for RequestID %s: %v", requestID, err)
			return
		}

		// Only submit completion event if we have valid content (either text or tool calls)
		if ollamaResp != nil && (ollamaResp.Message.Content != "" || len(ollamaResp.Message.ToolCalls) > 0) {
			// Submit final event for persistence
			if eventsourcing.SubmitEvent != nil {
				eventsourcing.SubmitEvent(&eventsourcing.GenericEvent{
					EventType: "LLMProcessingCompleted",
					Data: map[string]interface{}{
						"RequestID":    requestID,
						"ResponseText": ollamaResp.Message.Content,
						"ToolCalls":    ollamaResp.Message.ToolCalls,
						"Timestamp":    eventsourcing.ISOTimestamp(),
					},
				})
			}
		} else {
			log.Printf("Warning: Empty response with no tool calls from Ollama API for tool call completion with RequestID %s", requestID)
		}
	})

	return nil, nil
}

// buildMessagesForToolCompletion reconstructs the chat history for a given requestID, including tool results with tool names.
func buildMessagesForToolCompletion(state map[string]interface{}, requestID string, toolResults []map[string]interface{}) ([]llmmodels.Message, error) {
	// Create a new state that includes the latest tool calls for more accurate task list
	// Make a shallow copy of the state
	enhancedState := make(map[string]interface{})
	for k, v := range state {
		enhancedState[k] = v
	}

	// Get the conversation history with enhanced state (includes the system prompt)
	// Limited to last 10 turns to avoid context overflow
	messages := buildChatHistory(enhancedState, 10)

	// Find the specific request and make sure it's included
	var currentRequestText string
	var currentRequestFound bool

	userRequests, ok := state["UserRequestReceived"].([]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid state: UserRequestReceived not found or not a slice")
	}

	for _, req := range userRequests {
		reqMap, ok := req.(map[string]interface{})
		if !ok {
			continue
		}
		if reqMap["RequestID"] == requestID {
			currentRequestText, _ = reqMap["RequestText"].(string)
			currentRequestFound = true
			break
		}
	}

	if !currentRequestFound || currentRequestText == "" {
		return nil, fmt.Errorf("could not find request text for RequestID: %s", requestID)
	}

	// Ensure the current user request is the last one in the conversation
	// This guarantees tool calls are considered in the right context

	// First remove any existing message that matches our current request
	// (it might be in the history but we want it at the end)
	filteredMessages := []llmmodels.Message{messages[0]} // Keep system prompt

	for i := 1; i < len(messages); i++ {
		// Skip this message if it's the current user request
		if messages[i].Role == "user" && messages[i].Content == currentRequestText {
			continue
		}
		filteredMessages = append(filteredMessages, messages[i])
	}

	// Now add the current request and any tool results
	filteredMessages = append(filteredMessages, llmmodels.Message{
		Role:    "user",
		Content: currentRequestText,
	})

	// Add tool results with tool names
	for _, result := range toolResults {
		toolName, ok := result["toolName"].(string)
		if !ok {
			return nil, fmt.Errorf("missing toolName in tool result")
		}

		// Convert tool content to a more readable format
		var toolContent string
		resultData, ok := result["result"].(map[string]interface{})
		if ok {
			// Handle different types of tool results with better formatting
			if status, ok := resultData["status"].(string); ok {
				switch status {
				case "created":
					taskID, _ := resultData["taskID"].(string)
					title, _ := resultData["title"].(string)
					toolContent = fmt.Sprintf("Task created successfully: ID=%s, Title=\"%s\"", taskID, title)
				case "updated":
					taskID, _ := resultData["taskID"].(string)
					toolContent = fmt.Sprintf("Task updated successfully: ID=%s", taskID)
				case "deleted":
					taskID, _ := resultData["taskID"].(string)
					toolContent = fmt.Sprintf("Task deleted successfully: ID=%s", taskID)
				case "completed":
					taskID, _ := resultData["taskID"].(string)
					title, _ := resultData["title"].(string)
					toolContent = fmt.Sprintf("Task marked as completed: ID=%s, Title=\"%s\"", taskID, title)
				case "success":
					count, _ := resultData["count"].(float64)
					if count > 0 {
						toolContent = fmt.Sprintf("Listed %d tasks", int(count))
						// Add task list if available
						if tasks, ok := resultData["tasks"].([]interface{}); ok && len(tasks) > 0 {
							toolContent += ":\n"
							for i, task := range tasks {
								if taskMap, ok := task.(map[string]interface{}); ok {
									taskID, _ := taskMap["TaskID"].(string)
									title, _ := taskMap["Title"].(string)
									status, _ := taskMap["Status"].(string)
									if taskID != "" && title != "" {
										toolContent += fmt.Sprintf("%d. %s: \"%s\" (Status: %s)\n", i+1, taskID, title, status)
									}
								}
							}
						}
					} else {
						toolContent = "No tasks found"
					}
				default:
					// For other types, use a generic format
					toolContent = fmt.Sprintf("%v", resultData)
				}
			} else {
				// Fallback for unrecognized structure
				toolContent = fmt.Sprintf("%v", resultData)
			}
		} else {
			// Simple string conversion for non-map results
			toolContent = fmt.Sprintf("%v", result["result"])
		}

		// Add the formatted tool call result with tool name
		filteredMessages = append(filteredMessages, llmmodels.Message{
			Role:    "tool",
			Name:    toolName, // Include the tool name
			Content: toolContent,
		})
	}

	return filteredMessages, nil
}

// callOllamaAPI sends a request to the Ollama API and returns the response.
func callOllamaAPI(request llmmodels.OllamaRequest) (*llmmodels.OllamaResponse, error) {
	if !request.Stream {
		// Non-streaming request
		reqBody, err := json.Marshal(request)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal Ollama request: %v", err)
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
		var ollamaResp llmmodels.OllamaResponse
		if err := json.NewDecoder(resp.Body).Decode(&ollamaResp); err != nil {
			return nil, fmt.Errorf("failed to decode Ollama response: %v", err)
		}
		return &ollamaResp, nil
	}

	// This point is only reached for streaming requests
	return nil, fmt.Errorf("streaming requests should use callOllamaAPIWithStreaming instead")
}

// callOllamaAPIWithStreaming sends a streaming request to the Ollama API.
func callOllamaAPIWithStreaming(request llmmodels.OllamaRequest, requestID string) (*llmmodels.OllamaResponse, error) {
	request.Stream = true

	reqBody, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal Ollama request: %v", err)
	}

	logging.Info("request going out to ollama: %s", string(reqBody))
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
	var allToolCalls []llmmodels.OllamaToolCall // Accumulate tool calls across chunks
	var finalResponse *llmmodels.OllamaResponse

	scanBuf := make([]byte, 64*1024)
	scanner.Buffer(scanBuf, 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		var chunk llmmodels.OllamaResponse
		if err := json.Unmarshal([]byte(line), &chunk); err != nil {
			log.Printf("Error parsing stream chunk for RequestID %s: %v", requestID, err)
			continue
		}

		// Accumulate content
		if chunk.Message.Content != "" {
			fullContent.WriteString(chunk.Message.Content)
		}

		// Accumulate tool calls (append unique ones)
		if len(chunk.Message.ToolCalls) > 0 {
			for _, tc := range chunk.Message.ToolCalls {
				// Avoid duplicates by checking existing tool calls (optional, based on your needs)
				duplicate := false
				for _, existing := range allToolCalls {
					if existing.Function.Name == tc.Function.Name && reflect.DeepEqual(existing.Function.Arguments, tc.Function.Arguments) {
						duplicate = true
						break
					}
				}
				if !duplicate {
					allToolCalls = append(allToolCalls, tc)
				}
			}
		}

		// Send streaming event with current state
		if eventsourcing.SubmitStreamingEvent != nil {
			eventsourcing.SubmitStreamingEvent("LLMResponseStream", map[string]interface{}{
				"RequestID":      requestID,
				"PartialContent": fullContent.String(),
				"IsFinal":        chunk.Done,
				"HasToolCalls":   len(allToolCalls) > 0,
			})
		}

		// If done, set final response
		if chunk.Done {
			finalResponse = &llmmodels.OllamaResponse{
				Message: llmmodels.OllamaMessage{
					Role:      "assistant",
					Content:   fullContent.String(),
					ToolCalls: allToolCalls, // Use accumulated tool calls
				},
				Done: true,
			}
			break
		}
	}

	if err := scanner.Err(); err != nil {
		log.Printf("Stream reading error for RequestID %s: %v", requestID, err)
		return nil, fmt.Errorf("error reading stream: %v", err)
	}

	// If no done=true but chunks received, construct final response
	if finalResponse == nil && (fullContent.Len() > 0 || len(allToolCalls) > 0) {
		finalResponse = &llmmodels.OllamaResponse{
			Message: llmmodels.OllamaMessage{
				Role:      "assistant",
				Content:   fullContent.String(),
				ToolCalls: allToolCalls,
			},
			Done: true,
		}
		log.Printf("No done=true chunk for RequestID %s, constructed response with content: %s, tool calls: %d", requestID, fullContent.String(), len(allToolCalls))
	}

	// If no response at all, return error
	if finalResponse == nil {
		log.Printf("No valid chunks received for RequestID %s", requestID)
		return nil, fmt.Errorf("no response received from stream")
	}

	// Handle empty response case
	if finalResponse.Message.Content == "" && len(finalResponse.Message.ToolCalls) == 0 {
		log.Printf("Warning: Empty content and no tool calls for RequestID %s", requestID)
		finalResponse.Message.Content = "I apologize, but I wasn't able to generate a proper response."
	}

	return finalResponse, nil
}

// appendTaskInfo adds information about existing tasks to the system prompt
// getActiveTasks returns a list of active tasks with their IDs, titles, and status
func getActiveTasks(state map[string]interface{}) []map[string]interface{} {
	var activeTasks []map[string]interface{}

	// First, get all task IDs that have been deleted
	deletedTaskIDs := make(map[string]bool)
	if deletionEvents, ok := state["TaskDeleted"].([]interface{}); ok {
		for _, delEvent := range deletionEvents {
			if delData, ok := delEvent.(map[string]interface{}); ok {
				if delID, ok := delData["TaskID"].(string); ok && delID != "" {
					deletedTaskIDs[delID] = true
				}
			}
		}
	}

	// Map of task IDs to latest task data (including updates)
	taskData := make(map[string]map[string]interface{})

	// First get all created tasks
	if tasksEvents, ok := state["TaskCreated"].([]interface{}); ok {
		for _, taskEvent := range tasksEvents {
			if data, ok := taskEvent.(map[string]interface{}); ok {
				taskID, _ := data["TaskID"].(string)
				if taskID != "" && !deletedTaskIDs[taskID] {
					// Create a copy of the data
					taskCopy := make(map[string]interface{})
					for k, v := range data {
						taskCopy[k] = v
					}
					taskData[taskID] = taskCopy
				}
			}
		}
	}

	// Apply updates
	if updateEvents, ok := state["TaskUpdated"].([]interface{}); ok {
		for _, updateEvent := range updateEvents {
			if updateData, ok := updateEvent.(map[string]interface{}); ok {
				taskID, _ := updateData["TaskID"].(string)
				if taskID != "" && !deletedTaskIDs[taskID] {
					if task, exists := taskData[taskID]; exists {
						// Apply update fields to the task data
						for k, v := range updateData {
							if k != "TaskID" { // Don't overwrite the TaskID
								task[k] = v
							}
						}
					}
				}
			}
		}
	}

	// Apply completion status
	if completionEvents, ok := state["TaskCompleted"].([]interface{}); ok {
		for _, completionEvent := range completionEvents {
			if completionData, ok := completionEvent.(map[string]interface{}); ok {
				taskID, _ := completionData["TaskID"].(string)
				if taskID != "" && !deletedTaskIDs[taskID] {
					if task, exists := taskData[taskID]; exists {
						task["Status"] = "Completed"
						if completedAt, ok := completionData["CompletedAt"].(string); ok {
							task["CompletedAt"] = completedAt
						}
						if notes, ok := completionData["CompletionNotes"].(string); ok {
							task["CompletionNotes"] = notes
						}
					}
				}
			}
		}
	}

	// Convert map to slice
	for taskID, task := range taskData {
		// Double-check that task hasn't been deleted
		if !deletedTaskIDs[taskID] {
			activeTasks = append(activeTasks, task)
		}
	}

	return activeTasks
}

func appendTaskInfo(basePrompt string, state map[string]interface{}) string {
	// Get all active tasks
	activeTasks := getActiveTasks(state)

	// No tasks? Return the base prompt
	if len(activeTasks) == 0 {
		return basePrompt
	}

	// Create task info strings
	var taskInfo []string
	for _, task := range activeTasks {
		taskID, _ := task["TaskID"].(string)
		title, _ := task["Title"].(string)
		status, _ := task["Status"].(string)

		if taskID == "" || title == "" {
			continue
		}

		taskInfo = append(taskInfo, fmt.Sprintf("- %s: \"%s\" (Status: %s)", taskID, title, status))
	}

	// Add task info to prompt if tasks exist
	if len(taskInfo) > 0 {
		taskSection := "\n\n### Current Tasks\nThe following tasks already exist in the system. When updating or referencing existing tasks, use EXACTLY these IDs:\n"
		taskSection += strings.Join(taskInfo, "\n")
		taskSection += "\n\nIMPORTANT: Always use these exact task IDs when referencing existing tasks. Do not make up new IDs for existing tasks."
		return basePrompt + taskSection
	}

	return basePrompt
}

// buildChatHistory builds a full conversation history from past events
func buildChatHistory(state map[string]interface{}, maxMessages int) []llmmodels.Message {
	// Start with the system prompt, enhanced with task information
	// enhancedPrompt := appendTaskInfo(systemPrompt, state) // TODO delete?
	messages := []llmmodels.Message{
		{Role: "system", Content: systemPrompt},
	}

	// Build a chronological history of user requests and LLM responses
	var conversationTurns []struct {
		Timestamp string
		Message   llmmodels.Message
	}

	// Add user requests
	if userRequests, ok := state["UserRequestReceived"].([]interface{}); ok {
		for _, reqInterface := range userRequests {
			if req, ok := reqInterface.(map[string]interface{}); ok {
				requestText, _ := req["RequestText"].(string)
				timestamp, _ := req["Timestamp"].(string)
				if timestamp == "" {
					timestamp = "0" // Default for sorting
				}
				conversationTurns = append(conversationTurns, struct {
					Timestamp string
					Message   llmmodels.Message
				}{
					Timestamp: timestamp,
					Message:   llmmodels.Message{Role: "user", Content: requestText},
				})
			}
		}
	}

	// Add assistant responses
	if llmResponses, ok := state["LLMProcessingCompleted"].([]interface{}); ok {
		for _, respInterface := range llmResponses {
			if resp, ok := respInterface.(map[string]interface{}); ok {
				responseText, _ := resp["ResponseText"].(string)
				timestamp, _ := resp["Timestamp"].(string)
				if timestamp == "" {
					timestamp = "0" // Default for sorting
				}
				conversationTurns = append(conversationTurns, struct {
					Timestamp string
					Message   llmmodels.Message
				}{
					Timestamp: timestamp,
					Message:   llmmodels.Message{Role: "assistant", Content: responseText},
				})
			}
		}
	}

	// Sort conversation turns by timestamp (we'll use a simple implementation)
	// This is a basic insertion sort - adequate for our typical message count
	for i := 1; i < len(conversationTurns); i++ {
		j := i
		for j > 0 && conversationTurns[j-1].Timestamp > conversationTurns[j].Timestamp {
			conversationTurns[j], conversationTurns[j-1] = conversationTurns[j-1], conversationTurns[j]
			j--
		}
	}

	// Take the most recent messages up to maxMessages
	start := 0
	if len(conversationTurns) > maxMessages {
		start = len(conversationTurns) - maxMessages
	}

	// Add the messages to our result
	for _, turn := range conversationTurns[start:] {
		messages = append(messages, turn.Message)
	}

	return messages
}

// ProcessUserRequest handles the "ProcessUserRequest" command by initiating LLM processing.
func ProcessUserRequest(data map[string]interface{}, state map[string]interface{}) ([]eventsourcing.Event, error) {
	requestID, ok := data["RequestID"].(string)
	if !ok {
		return nil, fmt.Errorf("missing RequestID in command data")
	}
	requestText, ok := data["RequestText"].(string)
	if !ok {
		return nil, fmt.Errorf("missing RequestText in command data")
	}
	tools, ok := data["Tools"].([]llmmodels.Tool)
	if !ok {
		return nil, fmt.Errorf("missing Tools in command data")
	}
	log.Printf("Processing user request: %s for RequestID: %s", requestText, requestID)

	// Get the current timestamp for event ordering
	timestamp := eventsourcing.ISOTimestamp()

	startedEvent := &eventsourcing.GenericEvent{
		EventType: "LLMProcessingStarted",
		Data: map[string]interface{}{
			"RequestID":   requestID,
			"RequestText": requestText,
			"Timestamp":   timestamp,
		},
	}

	// Use SafeGo instead of a raw goroutine
	eventsourcing.SafeGo("ProcessUserRequest", map[string]interface{}{
		"requestID":   requestID,
		"requestText": requestText,
	}, func() {
		logging.Info("ProcessUserRequest from go routine")
		// Get conversation history, limited to last 10 turns
		messages := buildChatHistory(state, 10)

		// Add the current user request
		messages = append(messages, llmmodels.Message{Role: "user", Content: requestText})

		// Create the Ollama request with the conversation history
		ollamaReq := llmmodels.OllamaRequest{
			Model:    ollamaModel,
			Messages: messages,
			Tools:    tools,
			Stream:   true, // Enable streaming
		}

		// Call the Ollama API with streaming
		ollamaResp, err := callOllamaAPIWithStreaming(ollamaReq, requestID)
		if err != nil {
			log.Printf("Failed to call Ollama API for RequestID %s: %v", requestID, err)
			return
		}

		// Only submit completion event if we have valid content (either text or tool calls)
		if ollamaResp != nil && (ollamaResp.Message.Content != "" || len(ollamaResp.Message.ToolCalls) > 0) {
			// Submit the completion event - this is still needed to persist the final result
			if eventsourcing.SubmitEvent != nil {
				completedEvent := &eventsourcing.GenericEvent{
					EventType: "LLMProcessingCompleted",
					Data: map[string]interface{}{
						"RequestID":    requestID,
						"ResponseText": ollamaResp.Message.Content,
						"ToolCalls":    ollamaResp.Message.ToolCalls,
						"Timestamp":    eventsourcing.ISOTimestamp(),
					},
				}
				eventsourcing.SubmitEvent(completedEvent)
			}
		} else {
			log.Printf("Warning: Empty response with no tool calls from Ollama API for RequestID %s", requestID)
		}
	})

	return []eventsourcing.Event{startedEvent}, nil
}

// NewPlugin creates a new instance of the LLMProcessor plugin.
func NewPlugin() eventsourcing.Plugin {
	return &LLMProcessor{}
}
