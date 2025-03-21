package main

import (
	"encoding/json"
	"fmt"
	"mindpalace/pkg/eventsourcing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

// Task represents a single task's state
type Task struct {
	TaskID          string    `json:"task_id"`
	Title           string    `json:"title"`
	Description     string    `json:"description,omitempty"`
	Status          string    `json:"status"`
	Priority        string    `json:"priority"`
	Deadline        string    `json:"deadline,omitempty"`
	Dependencies    []string  `json:"dependencies,omitempty"`
	Tags            []string  `json:"tags,omitempty"`
	CompletedAt     string    `json:"completed_at,omitempty"`
	CompletionNotes string    `json:"completion_notes,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
}

// TaskAggregate manages the state of tasks
type TaskAggregate struct {
	Tasks    map[string]*Task                        `json:"tasks"`
	Commands map[string]eventsourcing.CommandHandler `json:"commands"`
}

// NewTaskAggregate creates a new TaskAggregate
func NewTaskAggregate() *TaskAggregate {
	return &TaskAggregate{
		Tasks:    make(map[string]*Task),
		Commands: make(map[string]eventsourcing.CommandHandler),
	}
}

// ID returns the aggregate's identifier
func (a *TaskAggregate) ID() string {
	return "taskmanager"
}

// GetState returns the current state
func (a *TaskAggregate) GetState() map[string]interface{} {
	return map[string]interface{}{
		"tasks": a.Tasks,
	}
}

// GetAllCommands returns the registered commands
func (a *TaskAggregate) GetAllCommands() map[string]eventsourcing.CommandHandler {
	return a.Commands
}

// ApplyEvent updates the aggregate state based on task-related events
func (a *TaskAggregate) ApplyEvent(event eventsourcing.Event) error {
	switch event.Type() {
	case "TaskCreated":
		data, err := event.Marshal()
		if err != nil {
			return fmt.Errorf("failed to marshal TaskCreated: %v", err)
		}
		var e TaskCreatedEvent
		if err := json.Unmarshal(data, &e); err != nil {
			return fmt.Errorf("failed to unmarshal TaskCreated: %v", err)
		}
		a.Tasks[e.TaskID] = &Task{
			TaskID:       e.TaskID,
			Title:        e.Title,
			Description:  e.Description,
			Status:       e.Status,
			Priority:     e.Priority,
			Deadline:     e.Deadline,
			Dependencies: e.Dependencies,
			Tags:         e.Tags,
			CreatedAt:    time.Now().UTC(),
		}

	case "TaskUpdated":
		data, err := event.Marshal()
		if err != nil {
			return fmt.Errorf("failed to marshal TaskUpdated: %v", err)
		}
		var e TaskUpdatedEvent
		if err := json.Unmarshal(data, &e); err != nil {
			return fmt.Errorf("failed to unmarshal TaskUpdated: %v", err)
		}
		if task, exists := a.Tasks[e.TaskID]; exists {
			if e.Title != "" {
				task.Title = e.Title
			}
			if e.Description != "" {
				task.Description = e.Description
			}
			if e.Status != "" {
				task.Status = e.Status
			}
			if e.Priority != "" {
				task.Priority = e.Priority
			}
			if e.Deadline != "" {
				task.Deadline = e.Deadline
			}
			if e.Dependencies != nil {
				task.Dependencies = e.Dependencies
			}
			if e.Tags != nil {
				task.Tags = e.Tags
			}
		}

	case "TaskCompleted":
		data, err := event.Marshal()
		if err != nil {
			return fmt.Errorf("failed to marshal TaskCompleted: %v", err)
		}
		var e TaskCompletedEvent
		if err := json.Unmarshal(data, &e); err != nil {
			return fmt.Errorf("failed to unmarshal TaskCompleted: %v", err)
		}
		if task, exists := a.Tasks[e.TaskID]; exists {
			task.Status = "Completed"
			task.CompletedAt = e.CompletedAt
			if e.CompletionNotes != "" {
				task.CompletionNotes = e.CompletionNotes
			}
		}

	case "TaskDeleted":
		data, err := event.Marshal()
		if err != nil {
			return fmt.Errorf("failed to marshal TaskDeleted: %v", err)
		}
		var e TaskDeletedEvent
		if err := json.Unmarshal(data, &e); err != nil {
			return fmt.Errorf("failed to unmarshal TaskDeleted: %v", err)
		}
		delete(a.Tasks, e.TaskID)

	default:
		return fmt.Errorf("unknown event type for taskmanager: %s", event.Type())
	}
	return nil
}

// TaskPlugin implements the plugin interface
type TaskPlugin struct {
	aggregate *TaskAggregate
}

func (p *TaskPlugin) Commands() map[string]eventsourcing.CommandHandler {
	return p.aggregate.Commands
}

func (p *TaskPlugin) Name() string {
	return "taskmanager"
}

func (p *TaskPlugin) Schemas() map[string]map[string]interface{} {
	// Unchanged from your original
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
			"description": "Current status of the task",
			"enum":        []string{"Pending", "In Progress", "Completed", "Blocked"},
		},
		"Priority": map[string]interface{}{
			"type":        "string",
			"description": "Priority level of the task",
			"enum":        []string{"Low", "Medium", "High", "Critical"},
		},
		"Deadline": map[string]interface{}{
			"type":        "string",
			"description": "Deadline for task completion (ISO 8601)",
		},
		"Dependencies": map[string]interface{}{
			"type":        "array",
			"description": "IDs of tasks that must be completed first",
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
			"description": "Creates a new task",
			"parameters": map[string]interface{}{
				"type":       "object",
				"properties": taskProperties,
				"required":   []string{"Title"},
			},
		},
		"UpdateTask": {
			"description": "Updates an existing task",
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
			"description": "Deletes a task",
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
						"description": "ID of the task to complete",
					},
					"CompletionNotes": map[string]interface{}{
						"type":        "string",
						"description": "Optional notes about completion",
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

// Custom Event Types (unchanged)
type TaskCreatedEvent struct {
	EventType    string                 `json:"event_type"`
	TaskID       string                 `json:"task_id"`
	Title        string                 `json:"title"`
	Description  string                 `json:"description,omitempty"`
	Status       string                 `json:"status"`
	Priority     string                 `json:"priority"`
	Deadline     string                 `json:"deadline,omitempty"`
	Dependencies []string               `json:"dependencies,omitempty"`
	Tags         []string               `json:"tags,omitempty"`
	Data         map[string]interface{} `json:"data,omitempty"`
}

func (e *TaskCreatedEvent) Type() string { return "taskmanager_TaskCreated" }
func (e *TaskCreatedEvent) Marshal() ([]byte, error) {
	e.EventType = e.Type()
	return json.Marshal(e)
}
func (e *TaskCreatedEvent) Unmarshal(data []byte) error { return json.Unmarshal(data, e) }

type TaskUpdatedEvent struct {
	EventType    string                 `json:"event_type"`
	TaskID       string                 `json:"task_id"`
	Title        string                 `json:"title,omitempty"`
	Description  string                 `json:"description,omitempty"`
	Status       string                 `json:"status,omitempty"`
	Priority     string                 `json:"priority,omitempty"`
	Deadline     string                 `json:"deadline,omitempty"`
	Dependencies []string               `json:"dependencies,omitempty"`
	Tags         []string               `json:"tags,omitempty"`
	Data         map[string]interface{} `json:"data,omitempty"`
}

func (e *TaskUpdatedEvent) Type() string { return "taskmanager_TaskUpdated" }
func (e *TaskUpdatedEvent) Marshal() ([]byte, error) {
	e.EventType = e.Type()
	return json.Marshal(e)
}
func (e *TaskUpdatedEvent) Unmarshal(data []byte) error { return json.Unmarshal(data, e) }

type TaskCompletedEvent struct {
	EventType       string                 `json:"event_type"`
	TaskID          string                 `json:"task_id"`
	CompletedAt     string                 `json:"completed_at"`
	CompletionNotes string                 `json:"completion_notes,omitempty"`
	Data            map[string]interface{} `json:"data,omitempty"`
}

func (e *TaskCompletedEvent) Type() string { return "taskmanager_TaskCompleted" }
func (e *TaskCompletedEvent) Marshal() ([]byte, error) {
	e.EventType = e.Type()
	return json.Marshal(e)
}
func (e *TaskCompletedEvent) Unmarshal(data []byte) error { return json.Unmarshal(data, e) }

type TaskDeletedEvent struct {
	EventType string                 `json:"event_type"`
	TaskID    string                 `json:"task_id"`
	Data      map[string]interface{} `json:"data,omitempty"`
}

func (e *TaskDeletedEvent) Type() string { return "taskmanager_TaskDeleted" }
func (e *TaskDeletedEvent) Marshal() ([]byte, error) {
	e.EventType = e.Type()
	return json.Marshal(e)
}
func (e *TaskDeletedEvent) Unmarshal(data []byte) error { return json.Unmarshal(data, e) }

// Register custom event types
func init() {
	eventsourcing.RegisterEvent("taskmanager_TaskCreated", func() eventsourcing.Event { return &TaskCreatedEvent{} })
	eventsourcing.RegisterEvent("taskmanager_TaskUpdated", func() eventsourcing.Event { return &TaskUpdatedEvent{} })
	eventsourcing.RegisterEvent("taskmanager_TaskCompleted", func() eventsourcing.Event { return &TaskCompletedEvent{} })
	eventsourcing.RegisterEvent("taskmanager_TaskDeleted", func() eventsourcing.Event { return &TaskDeletedEvent{} })
}

func generateTaskID() string {
	return fmt.Sprintf("task_%d", eventsourcing.GenerateUniqueID())
}

// Command Handlers
func CreateTaskHandler(data map[string]interface{}, state map[string]interface{}) ([]eventsourcing.Event, error) {
	title, ok := data["Title"].(string)
	if !ok {
		return nil, fmt.Errorf("missing Title")
	}

	taskID := generateTaskID()
	event := &TaskCreatedEvent{
		TaskID:   taskID,
		Title:    title,
		Status:   "Pending",
		Priority: "Medium",
	}
	if desc, ok := data["Description"].(string); ok {
		event.Description = desc
	}
	if status, ok := data["Status"].(string); ok {
		event.Status = status
	}
	if priority, ok := data["Priority"].(string); ok {
		event.Priority = priority
	}
	if deadline, ok := data["Deadline"].(string); ok {
		event.Deadline = deadline
	}
	if deps, ok := data["Dependencies"].([]interface{}); ok {
		event.Dependencies = make([]string, len(deps))
		for i, d := range deps {
			if dep, ok := d.(string); ok {
				event.Dependencies[i] = dep
			}
		}
	}
	if tags, ok := data["Tags"].([]interface{}); ok {
		event.Tags = make([]string, len(tags))
		for i, t := range tags {
			if tag, ok := t.(string); ok {
				event.Tags[i] = tag
			}
		}
	}

	events := []eventsourcing.Event{event}
	if requestID, ok := data["RequestID"].(string); ok && requestID != "" {
		if toolCallID, ok := data["ToolCallID"].(string); ok && toolCallID != "" {
			events = append(events, &eventsourcing.ToolCallCompleted{
				RequestID:  requestID,
				ToolCallID: toolCallID,
				Function:   "CreateTask",
				Result:     map[string]interface{}{"taskID": taskID, "title": title, "status": "created"},
			})
		}
	}
	return events, nil
}

func UpdateTaskHandler(data map[string]interface{}, state map[string]interface{}) ([]eventsourcing.Event, error) {
	taskID, ok := data["TaskID"].(string)
	if !ok {
		return nil, fmt.Errorf("missing TaskID")
	}

	event := &TaskUpdatedEvent{TaskID: taskID}
	if title, ok := data["Title"].(string); ok {
		event.Title = title
	}
	if desc, ok := data["Description"].(string); ok {
		event.Description = desc
	}
	if status, ok := data["Status"].(string); ok {
		event.Status = status
	}
	if priority, ok := data["Priority"].(string); ok {
		event.Priority = priority
	}
	if deadline, ok := data["Deadline"].(string); ok {
		event.Deadline = deadline
	}
	if deps, ok := data["Dependencies"].([]interface{}); ok {
		event.Dependencies = make([]string, len(deps))
		for i, d := range deps {
			if dep, ok := d.(string); ok {
				event.Dependencies[i] = dep
			}
		}
	}
	if tags, ok := data["Tags"].([]interface{}); ok {
		event.Tags = make([]string, len(tags))
		for i, t := range tags {
			if tag, ok := t.(string); ok {
				event.Tags[i] = tag
			}
		}
	}

	events := []eventsourcing.Event{event}
	if requestID, ok := data["RequestID"].(string); ok && requestID != "" {
		if toolCallID, ok := data["ToolCallID"].(string); ok && toolCallID != "" {
			events = append(events, &eventsourcing.ToolCallCompleted{
				RequestID:  requestID,
				ToolCallID: toolCallID,
				Function:   "UpdateTask",
				Result:     map[string]interface{}{"taskID": taskID, "status": "updated"},
			})
		}
	}
	return events, nil
}

func DeleteTaskHandler(data map[string]interface{}, state map[string]interface{}) ([]eventsourcing.Event, error) {
	taskID, ok := data["TaskID"].(string)
	if !ok {
		return nil, fmt.Errorf("missing TaskID")
	}

	event := &TaskDeletedEvent{TaskID: taskID}
	events := []eventsourcing.Event{event}
	if requestID, ok := data["RequestID"].(string); ok && requestID != "" {
		if toolCallID, ok := data["ToolCallID"].(string); ok && toolCallID != "" {
			events = append(events, &eventsourcing.ToolCallCompleted{
				RequestID:  requestID,
				ToolCallID: toolCallID,
				Function:   "DeleteTask",
				Result:     map[string]interface{}{"taskID": taskID, "status": "deleted"},
			})
		}
	}
	return events, nil
}

func CompleteTaskHandler(data map[string]interface{}, state map[string]interface{}) ([]eventsourcing.Event, error) {
	taskID, ok := data["TaskID"].(string)
	if !ok {
		return nil, fmt.Errorf("missing TaskID")
	}

	event := &TaskCompletedEvent{
		TaskID:      taskID,
		CompletedAt: eventsourcing.ISOTimestamp(),
	}
	if notes, ok := data["CompletionNotes"].(string); ok {
		event.CompletionNotes = notes
	}

	events := []eventsourcing.Event{event}
	if requestID, ok := data["RequestID"].(string); ok && requestID != "" {
		if toolCallID, ok := data["ToolCallID"].(string); ok && toolCallID != "" {
			events = append(events, &eventsourcing.ToolCallCompleted{
				RequestID:  requestID,
				ToolCallID: toolCallID,
				Function:   "CompleteTask",
				Result:     map[string]interface{}{"taskID": taskID, "status": "completed"},
			})
		}
	}
	return events, nil
}

func ListTasksHandler(data map[string]interface{}, state map[string]interface{}) ([]eventsourcing.Event, error) {
	tasksMap, ok := state["tasks"].(map[string]*Task)
	if !ok {
		return createListTasksResponse([]*Task{}, data, "ListTasks")
	}

	tasks := make([]*Task, 0, len(tasksMap))
	for _, task := range tasksMap {
		tasks = append(tasks, task)
	}

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

	filteredTasks := make([]*Task, 0)
	for _, task := range tasks {
		if statusFilter != "" && task.Status != statusFilter {
			continue
		}
		if priorityFilter != "" && task.Priority != priorityFilter {
			continue
		}
		if tagFilter != "" {
			tagFound := false
			for _, t := range task.Tags {
				if t == tagFilter {
					tagFound = true
					break
				}
			}
			if !tagFound {
				continue
			}
		}
		filteredTasks = append(filteredTasks, task)
	}

	return createListTasksResponse(filteredTasks, data, "ListTasks")
}

func createListTasksResponse(tasks []*Task, data map[string]interface{}, toolName string) ([]eventsourcing.Event, error) {
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

func (p *TaskPlugin) GetCustomUI(agg eventsourcing.Aggregate) fyne.CanvasObject {
	taskAgg, ok := agg.(*TaskAggregate)
	if !ok {
		return widget.NewLabel(fmt.Sprintf("Error: Invalid aggregate type for taskmanager: %T", agg))
	}

	tasks := make([]*Task, 0, len(taskAgg.Tasks))
	for _, task := range taskAgg.Tasks {
		tasks = append(tasks, task)
	}

	list := widget.NewList(
		func() int { return len(tasks) },
		func() fyne.CanvasObject { return widget.NewLabel("") },
		func(i widget.ListItemID, o fyne.CanvasObject) {
			label := fmt.Sprintf("%s (%s)", tasks[i].Title, tasks[i].Status)
			o.(*widget.Label).SetText(label)
		},
	)
	return container.NewScroll(list)
}

func (p *TaskPlugin) Aggregate() eventsourcing.Aggregate {
	return p.aggregate
}

func NewPlugin() eventsourcing.Plugin {
	agg := NewTaskAggregate()
	p := &TaskPlugin{aggregate: agg}
	agg.Commands = map[string]eventsourcing.CommandHandler{
		"CreateTask":   CreateTaskHandler,
		"UpdateTask":   UpdateTaskHandler,
		"DeleteTask":   DeleteTaskHandler,
		"CompleteTask": CompleteTaskHandler,
		"ListTasks":    ListTasksHandler,
	}
	return p
}

func (p *TaskPlugin) Type() eventsourcing.PluginType {
	return eventsourcing.LLMPlugin
}

func (p *TaskPlugin) EventHandlers() map[string]eventsourcing.EventHandler {
	return nil
}
