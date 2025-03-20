package llmprocessor

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
var systemPrompt = `You are MindPalace, a friendly AI assistant here to help with various queries and tasks. Provide helpful, accurate, and concise responses, using tools only when they enhance your ability to assist.

### Core Principles:
1. **Assist effectively**: Prioritize the user's needs, answer directly when possible, and use tools wisely to enhance assistance.
2. **Communicate clearly**: Provide concise, relevant responses, using context to avoid redundancy.
3. **Adapt to uncertainty**: Ask clarifying questions or make reasonable assumptions to keep the interaction smooth.

### Response Structure:
Before answering, always:
1. Think deeply in <think> tags about the user's request and whether a tool is necessary.
   - Consider if the user explicitly requested a tool.
   - Consider if the request implies a tool is needed.
   - Consider if the information is already accessible without a tool.
2. Decide if tools are needed based on the above.
3. Only then respond or call tools.

**Format**:
<think>Reasoning steps...</think>
[Tool calls OR Final Answer]

### Examples:
- **User**: "What's 5 + 7?" → "5 + 7 is 12."
- **User**: "Add a task to email Sarah" → "I've added a task: 'Email Sarah.' Anything else?"
- **User**: "What's happening tomorrow?" → "Could you specify if you mean your schedule, the weather, or something else?"

### Tone and Style:
Be friendly and approachable. Avoid jargon unless necessary. Keep responses concise yet complete.

### Final Notes:
Adapt to diverse user needs. Use tools to enhance, not replace, your intelligence. Strive for a seamless user experience.`

// LLMProcessor handles LLM-related operations
type LLMProcessor struct{}

// New creates a new LLMProcessor
func New() *LLMProcessor {
	return &LLMProcessor{}
}

// RegisterHandlers registers event handlers and commands with the event processor
func (p *LLMProcessor) RegisterHandlers(processor *eventsourcing.EventProcessor) {
	// Register event handlers
	processor.RegisterEventHandler("ToolCallsConfigured", p.HandleToolCallsConfigured)
	processor.RegisterEventHandler("AllToolCallsCompleted", p.HandleAllToolCallsCompleted)

	// Register commands
	processor.RegisterCommand("ProcessUserRequest", p.ProcessUserRequest)

	logging.Info("LLM Processor: Registered handlers and commands")
}

// GetSchemas returns the command schemas
func (p *LLMProcessor) GetSchemas() map[string]map[string]interface{} {
	return map[string]map[string]interface{}{
		"ProcessUserRequest": {
			"description": "Process a user request with the LLM",
			"parameters": map[string]interface{}{
				"RequestID":   map[string]interface{}{"type": "string", "description": "Unique identifier for the request"},
				"RequestText": map[string]interface{}{"type": "string", "description": "The text of the user's request"},
				"Tools":       map[string]interface{}{"type": "array", "description": "List of tools to provide to the LLM"},
			},
		},
	}
}

// HandleToolCallsConfigured processes the ToolCallsConfigured event.
func (p *LLMProcessor) HandleToolCallsConfigured(event eventsourcing.Event, state map[string]interface{}, commands map[string]eventsourcing.CommandHandler) ([]eventsourcing.Event, error) {
	fmt.Println("in handle tool calls configured command")
	var requestID, requestText string
	var availableTools []llmmodels.Tool

	// Handle both concrete and generic event types
	switch e := event.(type) {
	case *eventsourcing.ToolCallsConfiguredEvent:
		// Extract data from concrete event type
		requestID = e.RequestID
		requestText = e.RequestText
		availableTools = e.Tools

	case *eventsourcing.GenericEvent:
		// Fallback for backward compatibility
		var ok bool
		requestID, ok = e.Data["RequestID"].(string)
		if !ok {
			return nil, fmt.Errorf("missing RequestID in event data")
		}
		requestText, ok = e.Data["RequestText"].(string)
		if !ok {
			return nil, fmt.Errorf("missing RequestText in event data")
		}
		availableTools, ok = e.Data["Tools"].([]llmmodels.Tool)
		if !ok {
			return nil, fmt.Errorf("missing Tools in event data")
		}

	default:
		return nil, fmt.Errorf("unsupported event type: %s", event.Type())
	}

	// Process the command with extracted data
	return p.ProcessUserRequest(map[string]interface{}{
		"RequestID":   requestID,
		"RequestText": requestText,
		"Tools":       availableTools,
	}, state)
}

// HandleAllToolCallsCompleted processes the AllToolCallsCompleted event.
func (p *LLMProcessor) HandleAllToolCallsCompleted(event eventsourcing.Event, state map[string]interface{}, commands map[string]eventsourcing.CommandHandler) ([]eventsourcing.Event, error) {
	var requestID string
	var toolResults []map[string]interface{}

	// Handle both concrete and generic event types
	switch e := event.(type) {
	case *eventsourcing.AllToolCallsCompletedEvent:
		// Extract data from concrete event type
		requestID = e.RequestID
		toolResults = e.Results

	case *eventsourcing.GenericEvent:
		// Fallback for backward compatibility
		var ok bool
		requestID, ok = e.Data["RequestID"].(string)
		if !ok {
			return nil, fmt.Errorf("missing RequestID in event data")
		}
		toolResults, ok = e.Data["Results"].([]map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("missing Results in event data")
		}

	default:
		return nil, fmt.Errorf("unsupported event type: %s", event.Type())
	}

	// Process the event with extracted data
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
		logging.Debug("starting ollama response scanning: %s", line)

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
	// Start with the system prompt
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
func (p *LLMProcessor) ProcessUserRequest(data map[string]interface{}, state map[string]interface{}) ([]eventsourcing.Event, error) {
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
	fmt.Println("in proces user request tools:", tools)

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

		// Log the message count
		logging.Debug("Chat history contains %d messages before processing", len(messages))

		// Check if the current request is already in the history
		// This prevents duplicate user messages in chat history
		currentRequestExists := false
		for _, msg := range messages {
			if msg.Role == "user" && msg.Content == requestText {
				currentRequestExists = true
				logging.Debug("Current request already exists in chat history, not adding duplicate")
				break
			}
		}

		// Only add the current request if it doesn't already exist
		if !currentRequestExists {
			messages = append(messages, llmmodels.Message{Role: "user", Content: requestText})
			logging.Debug("Added current request to chat history")
		}

		// Log full messages for debugging
		for i, msg := range messages {
			logging.Trace("Message %d: Role=%s, Content=%s", i, msg.Role, msg.Content)
		}

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
