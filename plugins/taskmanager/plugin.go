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

func generateTaskID() string {
	return fmt.Sprintf("task_%d", eventsourcing.GenerateUniqueID())
}

func CreateTaskHandler(data map[string]interface{}, state map[string]interface{}) ([]eventsourcing.Event, error) {
	title, ok := data["Title"].(string)
	if !ok {
		return nil, fmt.Errorf("missing Title")
	}

	taskID := generateTaskID()
	taskData := map[string]interface{}{
		"TaskID": taskID,
		"Title":  title,
		"Status": "Pending",
	}

	if description, ok := data["Description"].(string); ok {
		taskData["Description"] = description
	}
	if status, ok := data["Status"].(string); ok {
		taskData["Status"] = status
	}
	if priority, ok := data["Priority"].(string); ok {
		taskData["Priority"] = priority
	} else {
		taskData["Priority"] = "Medium"
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

	requestID, _ := data["RequestID"].(string)
	toolCallID, _ := data["ToolCallID"].(string)

	events := []eventsourcing.Event{
		&eventsourcing.GenericEvent{
			EventType: "TaskCreated",
			Data:      taskData,
		},
	}
	if requestID != "" && toolCallID != "" {
		events = append(events, &eventsourcing.ToolCallCompleted{
			RequestID:  requestID,
			ToolCallID: toolCallID,
			Function:   "CreateTask",
			Result:     map[string]interface{}{"taskID": taskID, "title": title, "status": "created"},
		})
	}
	return events, nil
}

func UpdateTaskHandler(data map[string]interface{}, state map[string]interface{}) ([]eventsourcing.Event, error) {
	taskID, ok := data["TaskID"].(string)
	if !ok {
		return nil, fmt.Errorf("missing TaskID")
	}

	// Fix taskID format for LLM compatibility
	fixedTaskID := taskID
	if len(taskID) > 5 && taskID[:5] == "task-" {
		fixedTaskID = "task_" + taskID[5:]
		data["TaskID"] = fixedTaskID
	}

	tasks, exists := state["tasks"].(map[string]map[string]interface{})
	if !exists || tasks[fixedTaskID] == nil {
		return nil, fmt.Errorf("task with ID %s not found", fixedTaskID)
	}

	updateData := map[string]interface{}{
		"TaskID": fixedTaskID,
	}
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

	requestID, _ := data["RequestID"].(string)
	toolCallID, _ := data["ToolCallID"].(string)

	events := []eventsourcing.Event{
		&eventsourcing.GenericEvent{
			EventType: "TaskUpdated",
			Data:      updateData,
		},
	}
	if requestID != "" && toolCallID != "" {
		events = append(events, &eventsourcing.ToolCallCompleted{
			RequestID:  requestID,
			ToolCallID: toolCallID,
			Function:   "UpdateTask",
			Result:     map[string]interface{}{"taskID": fixedTaskID, "status": "updated"},
		})
	}
	return events, nil
}

func DeleteTaskHandler(data map[string]interface{}, state map[string]interface{}) ([]eventsourcing.Event, error) {
	taskID, ok := data["TaskID"].(string)
	if !ok {
		return nil, fmt.Errorf("missing TaskID")
	}

	fixedTaskID := taskID
	if len(taskID) > 5 && taskID[:5] == "task-" {
		fixedTaskID = "task_" + taskID[5:]
		data["TaskID"] = fixedTaskID
	}

	tasks, exists := state["tasks"].(map[string]map[string]interface{})
	if !exists || tasks[fixedTaskID] == nil {
		return nil, fmt.Errorf("task with ID %s not found", fixedTaskID)
	}

	requestID, _ := data["RequestID"].(string)
	toolCallID, _ := data["ToolCallID"].(string)

	events := []eventsourcing.Event{
		&eventsourcing.GenericEvent{
			EventType: "TaskDeleted",
			Data:      map[string]interface{}{"TaskID": fixedTaskID},
		},
	}
	if requestID != "" && toolCallID != "" {
		events = append(events, &eventsourcing.ToolCallCompleted{
			RequestID:  requestID,
			ToolCallID: toolCallID,
			Function:   "DeleteTask",
			Result:     map[string]interface{}{"taskID": fixedTaskID, "status": "deleted"},
		})
	}
	return events, nil
}

func CompleteTaskHandler(data map[string]interface{}, state map[string]interface{}) ([]eventsourcing.Event, error) {
	taskID, ok := data["TaskID"].(string)
	if !ok {
		return nil, fmt.Errorf("missing TaskID")
	}

	fixedTaskID := taskID
	if len(taskID) > 5 && taskID[:5] == "task-" {
		fixedTaskID = "task_" + taskID[5:]
		data["TaskID"] = fixedTaskID
	}

	tasks, exists := state["tasks"].(map[string]map[string]interface{})
	if !exists || tasks[fixedTaskID] == nil {
		return nil, fmt.Errorf("task with ID %s not found", fixedTaskID)
	}

	taskTitle := tasks[fixedTaskID]["Title"].(string)
	completionData := map[string]interface{}{
		"TaskID":      fixedTaskID,
		"Status":      "Completed",
		"CompletedAt": eventsourcing.ISOTimestamp(),
	}
	if notes, ok := data["CompletionNotes"].(string); ok {
		completionData["CompletionNotes"] = notes
	}

	requestID, _ := data["RequestID"].(string)
	toolCallID, _ := data["ToolCallID"].(string)

	events := []eventsourcing.Event{
		&eventsourcing.GenericEvent{
			EventType: "TaskCompleted",
			Data:      completionData,
		},
	}
	if requestID != "" && toolCallID != "" {
		events = append(events, &eventsourcing.ToolCallCompleted{
			RequestID:  requestID,
			ToolCallID: toolCallID,
			Function:   "CompleteTask",
			Result:     map[string]interface{}{"taskID": fixedTaskID, "title": taskTitle, "status": "completed"},
		})
	}
	return events, nil
}

func ListTasksHandler(data map[string]interface{}, state map[string]interface{}) ([]eventsourcing.Event, error) {
	tasksMap, exists := state["tasks"].(map[string]map[string]interface{})
	if !exists {
		return createListTasksResponse([]map[string]interface{}{}, data, "ListTasks")
	}

	// Convert map to slice for filtering
	tasks := make([]map[string]interface{}, 0, len(tasksMap))
	for _, task := range tasksMap {
		tasks = append(tasks, task)
	}

	// Apply filters
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

	filteredTasks := []map[string]interface{}{}
	for _, task := range tasks {
		if statusFilter != "" {
			if status, ok := task["Status"].(string); !ok || status != statusFilter {
				continue
			}
		}
		if priorityFilter != "" {
			if priority, ok := task["Priority"].(string); !ok || priority != priorityFilter {
				continue
			}
		}
		if tagFilter != "" {
			if tags, ok := task["Tags"].([]interface{}); ok {
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
				continue
			}
		}
		filteredTasks = append(filteredTasks, task)
	}

	return createListTasksResponse(filteredTasks, data, "ListTasks")
}

func createListTasksResponse(tasks []map[string]interface{}, data map[string]interface{}, toolName string) ([]eventsourcing.Event, error) {
	requestID, _ := data["RequestID"].(string)
	toolCallID, _ := data["ToolCallID"].(string)

	events := []eventsourcing.Event{}
	if requestID != "" && toolCallID != "" {
		events = append(events, &eventsourcing.ToolCallCompleted{
			RequestID:  requestID,
			ToolCallID: toolCallID,
			Function:   toolName,
			Result: map[string]interface{}{
				"tasks":  tasks,
				"count":  len(tasks),
				"status": "success",
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
	return nil
}
