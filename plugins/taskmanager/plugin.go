package main

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"mindpalace/pkg/eventsourcing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// Register event types
func init() {
	eventsourcing.RegisterEvent("taskmanager_TaskCreated", func() eventsourcing.Event { return &TaskCreatedEvent{} })
	eventsourcing.RegisterEvent("taskmanager_TaskUpdated", func() eventsourcing.Event { return &TaskUpdatedEvent{} })
	eventsourcing.RegisterEvent("taskmanager_TaskCompleted", func() eventsourcing.Event { return &TaskCompletedEvent{} })
	eventsourcing.RegisterEvent("taskmanager_TaskDeleted", func() eventsourcing.Event { return &TaskDeletedEvent{} })
}

// Constants for task properties
const (
	StatusPending    = "Pending"
	StatusInProgress = "In Progress"
	StatusCompleted  = "Completed"
	StatusBlocked    = "Blocked"
	PriorityLow      = "Low"
	PriorityMedium   = "Medium"
	PriorityHigh     = "High"
	PriorityCritical = "Critical"
)

// Task represents a single task's state
type Task struct {
	TaskID          string    `json:"task_id"`
	Title           string    `json:"title"`
	Description     string    `json:"description,omitempty"`
	Status          string    `json:"status"`
	Priority        string    `json:"priority"`
	Deadline        time.Time `json:"deadline,omitempty"`
	Dependencies    []string  `json:"dependencies,omitempty"`
	Tags            []string  `json:"tags,omitempty"`
	CompletedAt     time.Time `json:"completed_at,omitempty"`
	CompletionNotes string    `json:"completion_notes,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
}

// TaskAggregate manages the state of tasks with thread safety
type TaskAggregate struct {
	tasks    map[string]*Task
	commands map[string]eventsourcing.CommandHandler
	mu       sync.RWMutex
}

// NewTaskAggregate creates a new thread-safe TaskAggregate
func NewTaskAggregate() *TaskAggregate {
	return &TaskAggregate{
		tasks:    make(map[string]*Task),
		commands: make(map[string]eventsourcing.CommandHandler),
	}
}

// ID returns the aggregate's identifier
func (a *TaskAggregate) ID() string {
	return "taskmanager"
}

// ApplyEvent updates the aggregate state based on task-related events
func (a *TaskAggregate) ApplyEvent(event eventsourcing.Event) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	data, err := event.Marshal()
	if err != nil {
		return fmt.Errorf("failed to marshal event %s: %v", event.Type(), err)
	}

	switch event.Type() {
	case "taskmanager_TaskCreated":
		var e TaskCreatedEvent
		if err := json.Unmarshal(data, &e); err != nil {
			return fmt.Errorf("failed to unmarshal TaskCreated: %v", err)
		}
		a.tasks[e.TaskID] = &Task{
			TaskID:       e.TaskID,
			Title:        e.Title,
			Description:  e.Description,
			Status:       e.Status,
			Priority:     e.Priority,
			Deadline:     parseTime(e.Deadline),
			Dependencies: e.Dependencies,
			Tags:         e.Tags,
			CreatedAt:    time.Now().UTC(),
		}

	case "taskmanager_TaskUpdated":
		var e TaskUpdatedEvent
		if err := json.Unmarshal(data, &e); err != nil {
			return fmt.Errorf("failed to unmarshal TaskUpdated: %v", err)
		}
		if task, exists := a.tasks[e.TaskID]; exists {
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
				task.Deadline = parseTime(e.Deadline)
			}
			if e.Dependencies != nil {
				task.Dependencies = e.Dependencies
			}
			if e.Tags != nil {
				task.Tags = e.Tags
			}
		}

	case "taskmanager_TaskCompleted":
		var e TaskCompletedEvent
		if err := json.Unmarshal(data, &e); err != nil {
			return fmt.Errorf("failed to unmarshal TaskCompleted: %v", err)
		}
		if task, exists := a.tasks[e.TaskID]; exists {
			task.Status = StatusCompleted
			task.CompletedAt = parseTime(e.CompletedAt)
			task.CompletionNotes = e.CompletionNotes
		}

	case "taskmanager_TaskDeleted":
		var e TaskDeletedEvent
		if err := json.Unmarshal(data, &e); err != nil {
			return fmt.Errorf("failed to unmarshal TaskDeleted: %v", err)
		}
		delete(a.tasks, e.TaskID)

	default:
		return fmt.Errorf("unknown event type: %s", event.Type())
	}
	return nil
}

// TaskPlugin implements the plugin interface
type TaskPlugin struct {
	aggregate *TaskAggregate
}

// NewPlugin creates a new TaskPlugin instance
func NewPlugin() eventsourcing.Plugin {
	agg := NewTaskAggregate()
	p := &TaskPlugin{aggregate: agg}
	agg.commands = map[string]eventsourcing.CommandHandler{
		"CreateTask":   p.createTaskHandler,
		"UpdateTask":   p.updateTaskHandler,
		"DeleteTask":   p.deleteTaskHandler,
		"CompleteTask": p.completeTaskHandler,
		"ListTasks":    p.listTasksHandler,
	}
	return p
}

// Commands returns the command handlers
func (p *TaskPlugin) Commands() map[string]eventsourcing.CommandHandler {
	return p.aggregate.commands
}

// Name returns the plugin name
func (p *TaskPlugin) Name() string {
	return "taskmanager"
}

// Schemas defines the command schemas
func (p *TaskPlugin) Schemas() map[string]map[string]interface{} {
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
			"enum":        []string{StatusPending, StatusInProgress, StatusCompleted, StatusBlocked},
		},
		"Priority": map[string]interface{}{
			"type":        "string",
			"description": "Priority level of the task",
			"enum":        []string{PriorityLow, PriorityMedium, PriorityHigh, PriorityCritical},
		},
		"Deadline": map[string]interface{}{
			"type":        "string",
			"description": "Deadline for task completion (ISO 8601)",
		},
		"Dependencies": map[string]interface{}{
			"type":        "array",
			"description": "Task IDs that must be completed first",
			"items":       map[string]interface{}{"type": "string"},
		},
		"Tags": map[string]interface{}{
			"type":        "array",
			"description": "Tags for categorizing the task",
			"items":       map[string]interface{}{"type": "string"},
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
					"TaskID":       map[string]interface{}{"type": "string", "description": "ID of the task to update"},
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
					"TaskID": map[string]interface{}{"type": "string", "description": "ID of the task to delete"},
				},
				"required": []string{"TaskID"},
			},
		},
		"CompleteTask": {
			"description": "Marks a task as completed",
			"parameters": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"TaskID":          map[string]interface{}{"type": "string", "description": "ID of the task to complete"},
					"CompletionNotes": map[string]interface{}{"type": "string", "description": "Notes about completion"},
				},
				"required": []string{"TaskID"},
			},
		},
		"ListTasks": {
			"description": "Lists tasks with optional filtering",
			"parameters": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"Status":   map[string]interface{}{"type": "string", "enum": []string{"All", StatusPending, StatusInProgress, StatusCompleted, StatusBlocked}},
					"Priority": map[string]interface{}{"type": "string", "enum": []string{"All", PriorityLow, PriorityMedium, PriorityHigh, PriorityCritical}},
					"Tag":      map[string]interface{}{"type": "string", "description": "Filter by tag"},
				},
			},
		},
	}
}

// Event Types
type TasksListedEvent struct {
	EventType string   `json:"event_type"`
	TaskIDs   []string `json:"task_ids"`
}

func (e *TasksListedEvent) Type() string { return "taskmanager_TasksListed" }
func (e *TasksListedEvent) Marshal() ([]byte, error) {
	e.EventType = e.Type()
	return json.Marshal(e)
}
func (e *TasksListedEvent) Unmarshal(data []byte) error { return json.Unmarshal(data, e) }

type TaskCreatedEvent struct {
	EventType    string   `json:"event_type"`
	TaskID       string   `json:"task_id"`
	Title        string   `json:"title"`
	Description  string   `json:"description,omitempty"`
	Status       string   `json:"status"`
	Priority     string   `json:"priority"`
	Deadline     string   `json:"deadline,omitempty"`
	Dependencies []string `json:"dependencies,omitempty"`
	Tags         []string `json:"tags,omitempty"`
}

func (e *TaskCreatedEvent) Type() string { return "taskmanager_TaskCreated" }
func (e *TaskCreatedEvent) Marshal() ([]byte, error) {
	e.EventType = e.Type()
	return json.Marshal(e)
}
func (e *TaskCreatedEvent) Unmarshal(data []byte) error { return json.Unmarshal(data, e) }

type TaskUpdatedEvent struct {
	EventType    string   `json:"event_type"`
	TaskID       string   `json:"task_id"`
	Title        string   `json:"title,omitempty"`
	Description  string   `json:"description,omitempty"`
	Status       string   `json:"status,omitempty"`
	Priority     string   `json:"priority,omitempty"`
	Deadline     string   `json:"deadline,omitempty"`
	Dependencies []string `json:"dependencies,omitempty"`
	Tags         []string `json:"tags,omitempty"`
}

func (e *TaskUpdatedEvent) Type() string { return "taskmanager_TaskUpdated" }
func (e *TaskUpdatedEvent) Marshal() ([]byte, error) {
	e.EventType = e.Type()
	return json.Marshal(e)
}
func (e *TaskUpdatedEvent) Unmarshal(data []byte) error { return json.Unmarshal(data, e) }

type TaskCompletedEvent struct {
	EventType       string `json:"event_type"`
	TaskID          string `json:"task_id"`
	CompletedAt     string `json:"completed_at"`
	CompletionNotes string `json:"completion_notes,omitempty"`
}

func (e *TaskCompletedEvent) Type() string { return "taskmanager_TaskCompleted" }
func (e *TaskCompletedEvent) Marshal() ([]byte, error) {
	e.EventType = e.Type()
	return json.Marshal(e)
}
func (e *TaskCompletedEvent) Unmarshal(data []byte) error { return json.Unmarshal(data, e) }

type TaskDeletedEvent struct {
	EventType string `json:"event_type"`
	TaskID    string `json:"task_id"`
}

func (e *TaskDeletedEvent) Type() string { return "taskmanager_TaskDeleted" }
func (e *TaskDeletedEvent) Marshal() ([]byte, error) {
	e.EventType = e.Type()
	return json.Marshal(e)
}
func (e *TaskDeletedEvent) Unmarshal(data []byte) error { return json.Unmarshal(data, e) }

// Utility functions
func generateTaskID() string {
	return fmt.Sprintf("task_%d", eventsourcing.GenerateUniqueID())
}

func parseTime(timeStr string) time.Time {
	if timeStr == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, timeStr)
	if err != nil {
		return time.Time{}
	}
	return t
}

func validateStatus(status string) bool {
	return status == StatusPending || status == StatusInProgress || status == StatusCompleted || status == StatusBlocked
}

func validatePriority(priority string) bool {
	return priority == PriorityLow || priority == PriorityMedium || priority == PriorityHigh || priority == PriorityCritical
}

// Command Handlers
func (p *TaskPlugin) createTaskHandler(data map[string]interface{}, state map[string]interface{}) ([]eventsourcing.Event, error) {
	title, ok := data["Title"].(string)
	if !ok || title == "" {
		return nil, fmt.Errorf("title is required and must be a non-empty string")
	}

	event := &TaskCreatedEvent{
		TaskID:   generateTaskID(),
		Title:    title,
		Status:   StatusPending,
		Priority: PriorityMedium,
	}
	if desc, ok := data["Description"].(string); ok {
		event.Description = desc
	}
	if status, ok := data["Status"].(string); ok && validateStatus(status) {
		event.Status = status
	}
	if priority, ok := data["Priority"].(string); ok && validatePriority(priority) {
		event.Priority = priority
	}
	if deadline, ok := data["Deadline"].(string); ok {
		if t, err := time.Parse(time.RFC3339, deadline); err == nil {
			event.Deadline = t.Format(time.RFC3339)
		}
	}
	if deps, ok := data["Dependencies"].([]interface{}); ok {
		event.Dependencies = convertToStringSlice(deps)
	}
	if tags, ok := data["Tags"].([]interface{}); ok {
		event.Tags = convertToStringSlice(tags)
	}
	return []eventsourcing.Event{event}, nil

}

func (p *TaskPlugin) updateTaskHandler(data map[string]interface{}, state map[string]interface{}) ([]eventsourcing.Event, error) {
	taskID, ok := data["TaskID"].(string)
	if !ok || taskID == "" {
		return nil, fmt.Errorf("taskID is required and must be a non-empty string")
	}

	p.aggregate.mu.RLock()
	_, exists := p.aggregate.tasks[taskID]
	p.aggregate.mu.RUnlock()
	if !exists {
		return nil, fmt.Errorf("task %s not found", taskID)
	}

	event := &TaskUpdatedEvent{TaskID: taskID}
	if title, ok := data["Title"].(string); ok && title != "" {
		event.Title = title
	}
	if desc, ok := data["Description"].(string); ok {
		event.Description = desc
	}
	if status, ok := data["Status"].(string); ok && validateStatus(status) {
		event.Status = status
	}
	if priority, ok := data["Priority"].(string); ok && validatePriority(priority) {
		event.Priority = priority
	}
	if deadline, ok := data["Deadline"].(string); ok {
		if t, err := time.Parse(time.RFC3339, deadline); err == nil {
			event.Deadline = t.Format(time.RFC3339)
		}
	}
	if deps, ok := data["Dependencies"].([]interface{}); ok {
		event.Dependencies = convertToStringSlice(deps)
	}
	if tags, ok := data["Tags"].([]interface{}); ok {
		event.Tags = convertToStringSlice(tags)
	}
	return []eventsourcing.Event{event}, nil
}

func (p *TaskPlugin) deleteTaskHandler(data map[string]interface{}, state map[string]interface{}) ([]eventsourcing.Event, error) {
	taskID, ok := data["TaskID"].(string)
	if !ok || taskID == "" {
		return nil, fmt.Errorf("taskID is required and must be a non-empty string")
	}

	p.aggregate.mu.RLock()
	_, exists := p.aggregate.tasks[taskID]
	p.aggregate.mu.RUnlock()
	if !exists {
		return nil, fmt.Errorf("task %s not found", taskID)
	}

	event := &TaskDeletedEvent{TaskID: taskID}
	return []eventsourcing.Event{event}, nil
}

func (p *TaskPlugin) completeTaskHandler(data map[string]interface{}, state map[string]interface{}) ([]eventsourcing.Event, error) {
	taskID, ok := data["TaskID"].(string)
	if !ok || taskID == "" {
		return nil, fmt.Errorf("taskID is required and must be a non-empty string")
	}

	p.aggregate.mu.RLock()
	task, exists := p.aggregate.tasks[taskID]
	p.aggregate.mu.RUnlock()
	if !exists {
		return nil, fmt.Errorf("task %s not found", taskID)
	}
	if task.Status == StatusCompleted {
		return nil, fmt.Errorf("task %s is already completed", taskID)
	}

	now := time.Now().UTC()
	event := &TaskCompletedEvent{
		TaskID:      taskID,
		CompletedAt: now.Format(time.RFC3339),
	}

	if notes, ok := data["CompletionNotes"].(string); ok {
		event.CompletionNotes = notes
	}
	return []eventsourcing.Event{event}, nil
}

func (p *TaskPlugin) listTasksHandler(data map[string]interface{}, state map[string]interface{}) ([]eventsourcing.Event, error) {
	p.aggregate.mu.RLock()
	defer p.aggregate.mu.RUnlock()

	tasks := make([]*Task, 0, len(p.aggregate.tasks))
	for _, task := range p.aggregate.tasks {
		tasks = append(tasks, task)
	}

	// Apply filters
	var statusFilter, priorityFilter, tagFilter string
	if status, ok := data["Status"].(string); ok && status != "All" && validateStatus(status) {
		statusFilter = status
	}
	if priority, ok := data["Priority"].(string); ok && priority != "All" && validatePriority(priority) {
		priorityFilter = priority
	}
	if tag, ok := data["Tag"].(string); ok {
		tagFilter = tag
	}

	filteredTasks := tasks[:0]
	var filteredTaskIDs []string
	for _, task := range tasks {
		if (statusFilter != "" && task.Status != statusFilter) ||
			(priorityFilter != "" && task.Priority != priorityFilter) ||
			(tagFilter != "" && !contains(task.Tags, tagFilter)) {
			continue
		}
		filteredTasks = append(filteredTasks, task)
	}

	// Sort tasks by creation time
	sort.Slice(filteredTasks, func(i, j int) bool {
		return filteredTasks[i].CreatedAt.Before(filteredTasks[j].CreatedAt)
	})
	event := &TasksListedEvent{TaskIDs: filteredTaskIDs}
	return []eventsourcing.Event{event}, nil
}

// UI
func (p *TaskPlugin) GetCustomUI(agg eventsourcing.Aggregate) fyne.CanvasObject {
	taskAgg, ok := agg.(*TaskAggregate)
	if !ok {
		return widget.NewLabel(fmt.Sprintf("Error: Invalid aggregate type: %T", agg))
	}

	taskAgg.mu.RLock()
	defer taskAgg.mu.RUnlock()

	tasks := make([]*Task, 0, len(taskAgg.tasks))
	for _, task := range taskAgg.tasks {
		tasks = append(tasks, task)
	}

	if len(tasks) == 0 {
		return container.NewCenter(widget.NewLabel("No tasks available. Create one to get started!"))
	}

	// Sort tasks by priority and deadline
	sort.Slice(tasks, func(i, j int) bool {
		pi, pj := priorityValue(tasks[i].Priority), priorityValue(tasks[j].Priority)
		if pi != pj {
			return pi > pj
		}
		if !tasks[i].Deadline.IsZero() && !tasks[j].Deadline.IsZero() {
			return tasks[i].Deadline.Before(tasks[j].Deadline)
		}
		return tasks[i].CreatedAt.Before(tasks[j].CreatedAt)
	})

	content := container.NewVBox()
	for _, task := range tasks {
		item := createTaskItem(task, taskAgg)
		content.Add(item)
	}

	scroll := container.NewVScroll(content)
	scroll.SetMinSize(fyne.NewSize(500, 400))
	return scroll
}

func createTaskItem(task *Task, agg *TaskAggregate) fyne.CanvasObject {
	title := widget.NewLabel(fmt.Sprintf("%s (%s)", task.Title, task.Status))
	title.TextStyle = fyne.TextStyle{Bold: true}
	if task.Status == StatusCompleted {
		title.TextStyle.Italic = true
	}

	details := container.NewVBox()
	if task.Description != "" {
		desc := widget.NewLabel(task.Description)
		desc.Wrapping = fyne.TextWrapWord
		details.Add(desc)
	}

	info := container.NewHBox(
		widget.NewLabel(fmt.Sprintf("Priority: %s", task.Priority)),
		widget.NewLabel("|"),
		widget.NewLabel(fmt.Sprintf("Status: %s", task.Status)),
	)
	if !task.Deadline.IsZero() {
		info.Add(widget.NewLabel("|"))
		info.Add(widget.NewLabel(fmt.Sprintf("Due: %s", task.Deadline.Format("2006-01-02"))))
	}
	details.Add(info)

	if len(task.Tags) > 0 {
		tags := widget.NewLabel(fmt.Sprintf("Tags: %s", strings.Join(task.Tags, ", ")))
		details.Add(tags)
	}

	accordion := widget.NewAccordion(widget.NewAccordionItem("", details))
	accordion.Items[0].Title = "" // Hide title in accordion
	accordion.Open(0)

	return container.NewBorder(
		container.NewHBox(title, widget.NewIcon(priorityIcon(task.Priority))),
		nil, nil, nil,
		accordion,
	)
}

func priorityValue(priority string) int {
	switch priority {
	case PriorityCritical:
		return 4
	case PriorityHigh:
		return 3
	case PriorityMedium:
		return 2
	case PriorityLow:
		return 1
	default:
		return 0
	}
}

func priorityIcon(priority string) fyne.Resource {
	switch priority {
	case PriorityCritical:
		return theme.ErrorIcon()
	case PriorityHigh:
		return theme.WarningIcon()
	case PriorityMedium:
		return theme.InfoIcon()
	case PriorityLow:
		return theme.ConfirmIcon()
	default:
		return theme.QuestionIcon()
	}
}

// Additional Plugin Methods
func (p *TaskPlugin) Aggregate() eventsourcing.Aggregate {
	return p.aggregate
}

func (p *TaskPlugin) Type() eventsourcing.PluginType {
	return eventsourcing.LLMPlugin
}

func (p *TaskPlugin) EventHandlers() map[string]eventsourcing.EventHandler {
	return nil
}

// Helper functions
func convertToStringSlice(slice []interface{}) []string {
	result := make([]string, 0, len(slice))
	for _, item := range slice {
		if s, ok := item.(string); ok {
			result = append(result, s)
		}
	}
	return result
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
