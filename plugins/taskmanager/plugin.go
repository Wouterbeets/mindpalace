package main

import (
	"fmt"
	"mindpalace/pkg/eventsourcing"
)

type TaskPlugin struct{}

func (p *TaskPlugin) Commands() map[string]eventsourcing.CommandHandler {
	return map[string]eventsourcing.CommandHandler{
		"CreateTask":   CreateTaskHandler,
		"UpdateTask":   UpdateTaskHandler,
		"DeleteTask":   DeleteTaskHandler,
		"CompleteTask": CompleteTaskHandler,
		"ListTasks":    ListTasksHandler,
	}
}

func (p *TaskPlugin) Name() string {
	return "TaskPlugin"
}

func (p *TaskPlugin) Schemas() map[string]map[string]interface{} {
	// Common task properties schema for reuse
	taskProperties := map[string]interface{}{
		"Title": map[string]interface{}{
			"type":        "string",
			"description": "The title of the task",
		},
		"Description": map[string]interface{}{
			"type":        "string",
			"description": "Detailed description of the task",
		},
		"Status": map[string]interface{}{
			"type":        "string",
			"description": "Current status of the task (Pending, In Progress, Completed, Blocked)",
			"enum":        []string{"Pending", "In Progress", "Completed", "Blocked"},
		},
		"Priority": map[string]interface{}{
			"type":        "string",
			"description": "Priority level of the task",
			"enum":        []string{"Low", "Medium", "High", "Critical"},
		},
		"Deadline": map[string]interface{}{
			"type":        "string",
			"description": "Deadline for task completion (ISO 8601 format, e.g. 2025-04-01)",
		},
		"Dependencies": map[string]interface{}{
			"type":        "array",
			"description": "IDs of tasks that must be completed before this task",
			"items": map[string]interface{}{
				"type": "string",
			},
		},
		"Tags": map[string]interface{}{
			"type":        "array",
			"description": "List of tags for categorizing the task",
			"items": map[string]interface{}{
				"type": "string",
			},
		},
	}

	return map[string]map[string]interface{}{
		"CreateTask": {
			"description": "Creates a new task with comprehensive details",
			"parameters": map[string]interface{}{
				"type":       "object",
				"properties": taskProperties,
				"required":   []string{"Title"},
			},
		},
		"UpdateTask": {
			"description": "Updates an existing task's details",
			"parameters": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"TaskID": map[string]interface{}{
						"type":        "string",
						"description": "ID of the task to update",
					},
					"Title":        taskProperties["Title"],
					"Description":  taskProperties["Description"],
					"Status":       taskProperties["Status"],
					"Priority":     taskProperties["Priority"],
					"Deadline":     taskProperties["Deadline"],
					"Dependencies": taskProperties["Dependencies"],
					"Tags":         taskProperties["Tags"],
				},
				"required": []string{"TaskID"},
			},
		},
		"DeleteTask": {
			"description": "Deletes a task by ID",
			"parameters": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"TaskID": map[string]interface{}{
						"type":        "string",
						"description": "ID of the task to delete",
					},
				},
				"required": []string{"TaskID"},
			},
		},
		"CompleteTask": {
			"description": "Marks a task as completed",
			"parameters": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"TaskID": map[string]interface{}{
						"type":        "string",
						"description": "ID of the task to mark as completed",
					},
					"CompletionNotes": map[string]interface{}{
						"type":        "string",
						"description": "Optional notes about task completion",
					},
				},
				"required": []string{"TaskID"},
			},
		},
		"ListTasks": {
			"description": "Lists tasks with optional filtering",
			"parameters": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"Status": map[string]interface{}{
						"type":        "string",
						"description": "Filter tasks by status",
						"enum":        []string{"All", "Pending", "In Progress", "Completed", "Blocked"},
					},
					"Priority": map[string]interface{}{
						"type":        "string",
						"description": "Filter tasks by priority",
						"enum":        []string{"All", "Low", "Medium", "High", "Critical"},
					},
					"Tag": map[string]interface{}{
						"type":        "string",
						"description": "Filter tasks by tag",
					},
				},
			},
		},
	}
}

// Generate a unique ID for tasks
func generateTaskID() string {
	return fmt.Sprintf("task_%d", eventsourcing.GenerateUniqueID())
}

func CreateTaskHandler(data map[string]interface{}, state map[string]interface{}) ([]eventsourcing.Event, error) {
	title, ok := data["Title"].(string)
	if !ok {
		return nil, fmt.Errorf("missing Title")
	}

	// Generate a unique task ID
	taskID := generateTaskID()

	// Create a task object with all possible fields
	taskData := map[string]interface{}{
		"TaskID": taskID,
		"Title":  title,
		"Status": "Pending", // Default status
	}

	// Add optional fields if they exist
	if description, ok := data["Description"].(string); ok {
		taskData["Description"] = description
	}

	if status, ok := data["Status"].(string); ok {
		taskData["Status"] = status
	}

	if priority, ok := data["Priority"].(string); ok {
		taskData["Priority"] = priority
	} else {
		taskData["Priority"] = "Medium" // Default priority
	}

	if deadline, ok := data["Deadline"].(string); ok {
		taskData["Deadline"] = deadline
	}

	if dependencies, ok := data["Dependencies"].([]interface{}); ok {
		taskData["Dependencies"] = dependencies
	}

	if tags, ok := data["Tags"].([]interface{}); ok {
		taskData["Tags"] = tags
	}

	// Standard event metadata
	requestID, _ := data["RequestID"].(string)
	toolCallID, _ := data["ToolCallID"].(string)

	events := []eventsourcing.Event{
		&eventsourcing.GenericEvent{
			EventType: "TaskCreated",
			Data:      taskData,
		},
	}

	if requestID != "" && toolCallID != "" {
		events = append(events, &eventsourcing.GenericEvent{
			EventType: "ToolCallCompleted",
			Data: map[string]interface{}{
				"RequestID":  requestID,
				"ToolCallID": toolCallID,
				"Function":   "CreateTask", // Added tool name
				"Result":     map[string]interface{}{"taskID": taskID, "title": title, "status": "created"},
			},
		})
	}
	return events, nil
}

func UpdateTaskHandler(data map[string]interface{}, state map[string]interface{}) ([]eventsourcing.Event, error) {
	taskID, ok := data["TaskID"].(string)
	if !ok {
		return nil, fmt.Errorf("missing TaskID")
	}

	// Fix taskID format: convert "task-123" to "task_123" for LLM compatibility
	fixedTaskID := taskID
	if len(taskID) > 5 && taskID[:5] == "task-" {
		fixedTaskID = "task_" + taskID[5:]
		data["TaskID"] = fixedTaskID
	}

	// Get current tasks from state to check if task exists
	tasksEvents, exists := state["TaskCreated"].([]interface{})
	if !exists {
		return nil, fmt.Errorf("no tasks exist")
	}

	// Check if task exists (using our fixed ID)
	taskExists := false
	for _, taskEvent := range tasksEvents {
		if taskEventMap, ok := taskEvent.(map[string]interface{}); ok {
			if existingTaskID, ok := taskEventMap["TaskID"].(string); ok && existingTaskID == fixedTaskID {
				taskExists = true
				break
			}
		}
	}

	if !taskExists {
		return nil, fmt.Errorf("task with ID %s not found", fixedTaskID)
	}

	// Prepare update data
	updateData := map[string]interface{}{
		"TaskID": fixedTaskID, // Use the fixed task ID
	}

	// Only include fields that are being updated
	if title, ok := data["Title"].(string); ok {
		updateData["Title"] = title
	}

	if description, ok := data["Description"].(string); ok {
		updateData["Description"] = description
	}

	if status, ok := data["Status"].(string); ok {
		updateData["Status"] = status
	}

	if priority, ok := data["Priority"].(string); ok {
		updateData["Priority"] = priority
	}

	if deadline, ok := data["Deadline"].(string); ok {
		updateData["Deadline"] = deadline
	}

	if dependencies, ok := data["Dependencies"].([]interface{}); ok {
		updateData["Dependencies"] = dependencies
	}

	if tags, ok := data["Tags"].([]interface{}); ok {
		updateData["Tags"] = tags
	}

	// Standard event metadata
	requestID, _ := data["RequestID"].(string)
	toolCallID, _ := data["ToolCallID"].(string)

	events := []eventsourcing.Event{
		&eventsourcing.GenericEvent{
			EventType: "TaskUpdated",
			Data:      updateData,
		},
	}

	if requestID != "" && toolCallID != "" {
		events = append(events, &eventsourcing.GenericEvent{
			EventType: "ToolCallCompleted",
			Data: map[string]interface{}{
				"RequestID":  requestID,
				"ToolCallID": toolCallID,
				"Function":   "UpdateTask", // Added tool name
				"Result":     map[string]interface{}{"taskID": taskID, "status": "updated"},
			},
		})
	}
	return events, nil
}

func DeleteTaskHandler(data map[string]interface{}, state map[string]interface{}) ([]eventsourcing.Event, error) {
	taskID, ok := data["TaskID"].(string)
	if !ok {
		return nil, fmt.Errorf("missing TaskID")
	}

	// Fix taskID format: convert "task-123" to "task_123" for LLM compatibility
	fixedTaskID := taskID
	if len(taskID) > 5 && taskID[:5] == "task-" {
		fixedTaskID = "task_" + taskID[5:]
		data["TaskID"] = fixedTaskID
	}

	// Get current tasks from state to check if task exists
	tasksEvents, exists := state["TaskCreated"].([]interface{})
	if !exists {
		return nil, fmt.Errorf("no tasks exist")
	}

	// Check if task exists (using our fixed ID)
	taskExists := false
	for _, taskEvent := range tasksEvents {
		if taskEventMap, ok := taskEvent.(map[string]interface{}); ok {
			if existingTaskID, ok := taskEventMap["TaskID"].(string); ok && existingTaskID == fixedTaskID {
				taskExists = true
				break
			}
		}
	}

	if !taskExists {
		return nil, fmt.Errorf("task with ID %s not found", fixedTaskID)
	}

	// Standard event metadata
	requestID, _ := data["RequestID"].(string)
	toolCallID, _ := data["ToolCallID"].(string)

	events := []eventsourcing.Event{
		&eventsourcing.GenericEvent{
			EventType: "TaskDeleted",
			Data:      map[string]interface{}{"TaskID": fixedTaskID},
		},
	}

	if requestID != "" && toolCallID != "" {
		events = append(events, &eventsourcing.GenericEvent{
			EventType: "ToolCallCompleted",
			Data: map[string]interface{}{
				"RequestID":  requestID,
				"ToolCallID": toolCallID,
				"Function":   "DeleteTask", // Added tool name
				"Result":     map[string]interface{}{"taskID": fixedTaskID, "status": "deleted"},
			},
		})
	}
	return events, nil
}

func CompleteTaskHandler(data map[string]interface{}, state map[string]interface{}) ([]eventsourcing.Event, error) {
	taskID, ok := data["TaskID"].(string)
	if !ok {
		return nil, fmt.Errorf("missing TaskID")
	}

	// Fix taskID format: convert "task-123" to "task_123" for LLM compatibility
	fixedTaskID := taskID
	if len(taskID) > 5 && taskID[:5] == "task-" {
		fixedTaskID = "task_" + taskID[5:]
		data["TaskID"] = fixedTaskID
	}

	// Get current tasks from state to check if task exists
	tasksEvents, exists := state["TaskCreated"].([]interface{})
	if !exists {
		return nil, fmt.Errorf("no tasks exist")
	}

	// Check if task exists (using our fixed ID)
	taskExists := false
	var taskTitle string
	for _, taskEvent := range tasksEvents {
		if taskEventMap, ok := taskEvent.(map[string]interface{}); ok {
			if existingTaskID, ok := taskEventMap["TaskID"].(string); ok && existingTaskID == fixedTaskID {
				taskExists = true
				if title, ok := taskEventMap["Title"].(string); ok {
					taskTitle = title
				}
				break
			}
		}
	}

	if !taskExists {
		return nil, fmt.Errorf("task with ID %s not found", fixedTaskID)
	}

	completionData := map[string]interface{}{
		"TaskID":      fixedTaskID, // Use the fixed task ID
		"Status":      "Completed",
		"CompletedAt": eventsourcing.ISOTimestamp(),
	}

	// Add completion notes if provided
	if notes, ok := data["CompletionNotes"].(string); ok {
		completionData["CompletionNotes"] = notes
	}

	// Standard event metadata
	requestID, _ := data["RequestID"].(string)
	toolCallID, _ := data["ToolCallID"].(string)

	events := []eventsourcing.Event{
		&eventsourcing.GenericEvent{
			EventType: "TaskCompleted",
			Data:      completionData,
		},
	}

	if requestID != "" && toolCallID != "" {
		events = append(events, &eventsourcing.GenericEvent{
			EventType: "ToolCallCompleted",
			Data: map[string]interface{}{
				"RequestID":  requestID,
				"ToolCallID": toolCallID,
				"Function":   "CompleteTask", // Added tool name
				"Result": map[string]interface{}{
					"taskID": taskID,
					"title":  taskTitle,
					"status": "completed",
				},
			},
		})
	}
	return events, nil
}

func ListTasksHandler(data map[string]interface{}, state map[string]interface{}) ([]eventsourcing.Event, error) {
	// Get current tasks from state
	fmt.Println("checking state", state["TaskCreated"])
	tasksEvents, exists := state["TaskCreated"].([]interface{})
	if !exists {
		fmt.Println("no tasks found")
		// No tasks exist yet
		return createListTasksResponse([]map[string]interface{}{}, data, "ListTasks")
	}

	// Filter parameters
	var statusFilter, priorityFilter, tagFilter string
	if status, ok := data["Status"].(string); ok && status != "All" {
		statusFilter = status
	}
	if priority, ok := data["Priority"].(string); ok && priority != "All" {
		priorityFilter = priority
	}
	if tag, ok := data["Tag"].(string); ok && tag != "" {
		tagFilter = tag
	}

	// Get task updates to overlay on top of created tasks
	var taskUpdates, taskCompletions, taskDeletions []map[string]interface{}

	if updatesEvents, ok := state["TaskUpdated"].([]interface{}); ok {
		for _, event := range updatesEvents {
			if updateMap, ok := event.(map[string]interface{}); ok {
				taskUpdates = append(taskUpdates, updateMap)
			}
		}
	}

	if completionsEvents, ok := state["TaskCompleted"].([]interface{}); ok {
		for _, event := range completionsEvents {
			if completionMap, ok := event.(map[string]interface{}); ok {
				taskCompletions = append(taskCompletions, completionMap)
			}
		}
	}

	if deletionsEvents, ok := state["TaskDeleted"].([]interface{}); ok {
		for _, event := range deletionsEvents {
			if deletionMap, ok := event.(map[string]interface{}); ok {
				taskDeletions = append(taskDeletions, deletionMap)
			}
		}
	}

	// Merge task states and filter
	tasks := make([]map[string]interface{}, 0)

	for _, taskEvent := range tasksEvents {
		if taskData, ok := taskEvent.(map[string]interface{}); ok {
			taskID, _ := taskData["TaskID"].(string)

			// Skip if task was deleted
			deleted := false
			for _, deletion := range taskDeletions {
				if deletedID, ok := deletion["TaskID"].(string); ok && deletedID == taskID {
					deleted = true
					break
				}
			}
			if deleted {
				continue
			}

			// Apply updates
			for _, update := range taskUpdates {
				if updatedID, ok := update["TaskID"].(string); ok && updatedID == taskID {
					for k, v := range update {
						if k != "TaskID" { // Don't overwrite the ID
							taskData[k] = v
						}
					}
				}
			}

			// Apply completions
			for _, completion := range taskCompletions {
				if completedID, ok := completion["TaskID"].(string); ok && completedID == taskID {
					taskData["Status"] = "Completed"
					if completedAt, ok := completion["CompletedAt"].(string); ok {
						taskData["CompletedAt"] = completedAt
					}
					if notes, ok := completion["CompletionNotes"].(string); ok {
						taskData["CompletionNotes"] = notes
					}
				}
			}

			// Apply filters
			if statusFilter != "" && taskData["Status"] != statusFilter {
				continue
			}

			if priorityFilter != "" {
				priority, ok := taskData["Priority"].(string)
				if !ok || priority != priorityFilter {
					continue
				}
			}

			if tagFilter != "" {
				if tags, ok := taskData["Tags"].([]interface{}); ok {
					tagFound := false
					for _, t := range tags {
						if tagStr, ok := t.(string); ok && tagStr == tagFilter {
							tagFound = true
							break
						}
					}
					if !tagFound {
						continue
					}
				} else {
					continue // No tags, so can't match
				}
			}

			// Task passed all filters, add to results
			tasks = append(tasks, taskData)
		}
	}

	return createListTasksResponse(tasks, data, "ListTasks")
}

func createListTasksResponse(tasks []map[string]interface{}, data map[string]interface{}, toolName string) ([]eventsourcing.Event, error) {
	requestID, _ := data["RequestID"].(string)
	toolCallID, _ := data["ToolCallID"].(string)

	events := []eventsourcing.Event{}

	if requestID != "" && toolCallID != "" {
		events = append(events, &eventsourcing.GenericEvent{
			EventType: "ToolCallCompleted",
			Data: map[string]interface{}{
				"RequestID":  requestID,
				"ToolCallID": toolCallID,
				"Function":   toolName, // Added tool name as parameter
				"Result": map[string]interface{}{
					"tasks":  tasks,
					"count":  len(tasks),
					"status": "success",
				},
			},
		})
	}
	return events, nil
}

func NewPlugin() eventsourcing.Plugin {
	return &TaskPlugin{}
}

func (p *TaskPlugin) Type() eventsourcing.PluginType {
	return eventsourcing.LLMPlugin
}

func (p *TaskPlugin) EventHandlers() map[string]eventsourcing.EventHandler {
	return nil // No event handlers needed
}
