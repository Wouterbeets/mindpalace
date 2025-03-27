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
	eventsourcing.RegisterEvent("taskmanager_TasksListed", func() eventsourcing.Event { return &TasksListedEvent{} })
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
	commands map[string]eventsourcing.Command
	mu       sync.RWMutex
}

// NewTaskAggregate creates a new thread-safe TaskAggregate
func NewTaskAggregate() *TaskAggregate {
	return &TaskAggregate{
		tasks:    make(map[string]*Task),
		commands: make(map[string]eventsourcing.Command),
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

	data, err := json.Marshal(event)
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
		return nil
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
	agg.commands = map[string]eventsourcing.Command{
		"CreateTask": func(data map[string]interface{}) ([]eventsourcing.Event, error) {
			var input CreateTaskInput
			if err := mapToStruct(data, &input); err != nil {
				return nil, err
			}
			return p.createTaskHandler(input)
		},
		"UpdateTask": func(data map[string]interface{}) ([]eventsourcing.Event, error) {
			var input UpdateTaskInput
			if err := mapToStruct(data, &input); err != nil {
				return nil, err
			}
			return p.updateTaskHandler(input)
		},
		"DeleteTask": func(data map[string]interface{}) ([]eventsourcing.Event, error) {
			var input DeleteTaskInput
			if err := mapToStruct(data, &input); err != nil {
				return nil, err
			}
			return p.deleteTaskHandler(input)
		},
		"CompleteTask": func(data map[string]interface{}) ([]eventsourcing.Event, error) {
			var input CompleteTaskInput
			if err := mapToStruct(data, &input); err != nil {
				return nil, err
			}
			return p.completeTaskHandler(input)
		},
		"ListTasks": func(data map[string]interface{}) ([]eventsourcing.Event, error) {
			var input ListTasksInput
			if err := mapToStruct(data, &input); err != nil {
				return nil, err
			}
			return p.listTasksHandler(input)
		},
	}
	return p
}

// Commands returns the command handlers
func (p *TaskPlugin) Commands() map[string]eventsourcing.Command {
	return p.aggregate.commands
}

// Name returns the plugin name
func (p *TaskPlugin) Name() string {
	return "taskmanager"
}

// Schemas defines the command schemas
func (p *TaskPlugin) Schemas() map[string]map[string]interface{} {
	return map[string]map[string]interface{}{
		"CreateTask":   (&CreateTaskInput{}).Schema(),
		"UpdateTask":   (&UpdateTaskInput{}).Schema(),
		"DeleteTask":   (&DeleteTaskInput{}).Schema(),
		"CompleteTask": (&CompleteTaskInput{}).Schema(),
		"ListTasks":    (&ListTasksInput{}).Schema(),
	}
}

// Command Input Structs with Schema Generation

// CreateTaskInput defines the input for creating a task
type CreateTaskInput struct {
	Title        string   `json:"Title"`
	Description  string   `json:"Description,omitempty"`
	Status       string   `json:"Status,omitempty"`
	Priority     string   `json:"Priority,omitempty"`
	Deadline     string   `json:"Deadline,omitempty"`
	Dependencies []string `json:"Dependencies,omitempty"`
	Tags         []string `json:"Tags,omitempty"`
}

func (c *CreateTaskInput) Schema() map[string]interface{} {
	return map[string]interface{}{
		"description": "Creates a new task",
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
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
			},
			"required": []string{"Title"},
		},
	}
}

// UpdateTaskInput defines the input for updating a task
type UpdateTaskInput struct {
	TaskID       string   `json:"TaskID"`
	Title        string   `json:"Title,omitempty"`
	Description  string   `json:"Description,omitempty"`
	Status       string   `json:"Status,omitempty"`
	Priority     string   `json:"Priority,omitempty"`
	Deadline     string   `json:"Deadline,omitempty"`
	Dependencies []string `json:"Dependencies,omitempty"`
	Tags         []string `json:"Tags,omitempty"`
}

func (u *UpdateTaskInput) Schema() map[string]interface{} {
	return map[string]interface{}{
		"description": "Updates an existing task",
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"TaskID": map[string]interface{}{
					"type":        "string",
					"description": "ID of the task to update",
				},
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
			},
			"required": []string{"TaskID"},
		},
	}
}

// DeleteTaskInput defines the input for deleting a task
type DeleteTaskInput struct {
	TaskID string `json:"TaskID"`
}

func (d *DeleteTaskInput) Schema() map[string]interface{} {
	return map[string]interface{}{
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
	}
}

// CompleteTaskInput defines the input for completing a task
type CompleteTaskInput struct {
	TaskID          string `json:"TaskID"`
	CompletionNotes string `json:"CompletionNotes,omitempty"`
}

func (c *CompleteTaskInput) Schema() map[string]interface{} {
	return map[string]interface{}{
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
					"description": "Notes about completion",
				},
			},
			"required": []string{"TaskID"},
		},
	}
}

// ListTasksInput defines the input for listing tasks
type ListTasksInput struct {
	Status   string `json:"Status,omitempty"`
	Priority string `json:"Priority,omitempty"`
	Tag      string `json:"Tag,omitempty"`
}

func (l *ListTasksInput) Schema() map[string]interface{} {
	return map[string]interface{}{
		"description": "Lists tasks with optional filtering",
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"Status": map[string]interface{}{
					"type":        "string",
					"description": "Filter by status",
					"enum":        []string{"All", StatusPending, StatusInProgress, StatusCompleted, StatusBlocked},
				},
				"Priority": map[string]interface{}{
					"type":        "string",
					"description": "Filter by priority",
					"enum":        []string{"All", PriorityLow, PriorityMedium, PriorityHigh, PriorityCritical},
				},
				"Tag": map[string]interface{}{
					"type":        "string",
					"description": "Filter by tag",
				},
			},
		},
	}
}

// Event Types
type TasksListedEvent struct {
	EventType string  `json:"event_type"`
	Tasks     []*Task `json:"listed_tasks"`
}

func (e *TasksListedEvent) Type() string                { return "taskmanager_TasksListed" }
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

func mapToStruct(data map[string]interface{}, target interface{}) error {
	bytes, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("invalid parameters: %v", err)
	}
	return json.Unmarshal(bytes, target)
}

// Command Handlers
func (p *TaskPlugin) createTaskHandler(input CreateTaskInput) ([]eventsourcing.Event, error) {
	if input.Title == "" {
		return nil, fmt.Errorf("title is required and must be a non-empty string")
	}

	event := &TaskCreatedEvent{
		EventType:    "taskmanager_TaskCreated",
		TaskID:       generateTaskID(),
		Title:        input.Title,
		Description:  input.Description,
		Status:       StatusPending,  // Default
		Priority:     PriorityMedium, // Default
		Deadline:     input.Deadline,
		Dependencies: input.Dependencies,
		Tags:         input.Tags,
	}

	if input.Status != "" && validateStatus(input.Status) {
		event.Status = input.Status
	}
	if input.Priority != "" && validatePriority(input.Priority) {
		event.Priority = input.Priority
	}
	if input.Deadline != "" {
		if _, err := time.Parse(time.RFC3339, input.Deadline); err != nil {
			return nil, fmt.Errorf("invalid deadline format: %v", err)
		}
	}

	return []eventsourcing.Event{event}, nil
}

func (p *TaskPlugin) updateTaskHandler(input UpdateTaskInput) ([]eventsourcing.Event, error) {
	if input.TaskID == "" {
		return nil, fmt.Errorf("taskID is required and must be a non-empty string")
	}

	p.aggregate.mu.RLock()
	_, exists := p.aggregate.tasks[input.TaskID]
	p.aggregate.mu.RUnlock()
	if !exists {
		return nil, fmt.Errorf("task %s not found", input.TaskID)
	}

	event := &TaskUpdatedEvent{
		EventType:    "taskmanager_TaskUpdated",
		TaskID:       input.TaskID,
		Title:        input.Title,
		Description:  input.Description,
		Status:       input.Status,
		Priority:     input.Priority,
		Deadline:     input.Deadline,
		Dependencies: input.Dependencies,
		Tags:         input.Tags,
	}

	if input.Status != "" && !validateStatus(input.Status) {
		return nil, fmt.Errorf("invalid status: %s", input.Status)
	}
	if input.Priority != "" && !validatePriority(input.Priority) {
		return nil, fmt.Errorf("invalid priority: %s", input.Priority)
	}
	if input.Deadline != "" {
		if _, err := time.Parse(time.RFC3339, input.Deadline); err != nil {
			return nil, fmt.Errorf("invalid deadline format: %v", err)
		}
	}

	return []eventsourcing.Event{event}, nil
}

func (p *TaskPlugin) deleteTaskHandler(input DeleteTaskInput) ([]eventsourcing.Event, error) {
	if input.TaskID == "" {
		return nil, fmt.Errorf("taskID is required and must be a non-empty string")
	}

	p.aggregate.mu.RLock()
	_, exists := p.aggregate.tasks[input.TaskID]
	p.aggregate.mu.RUnlock()
	if !exists {
		return nil, fmt.Errorf("task %s not found", input.TaskID)
	}

	event := &TaskDeletedEvent{EventType: "taskmanager_TaskDeleted", TaskID: input.TaskID}
	return []eventsourcing.Event{event}, nil
}

func (p *TaskPlugin) completeTaskHandler(input CompleteTaskInput) ([]eventsourcing.Event, error) {
	if input.TaskID == "" {
		return nil, fmt.Errorf("taskID is required and must be a non-empty string")
	}

	p.aggregate.mu.RLock()
	task, exists := p.aggregate.tasks[input.TaskID]
	p.aggregate.mu.RUnlock()
	if !exists {
		return nil, fmt.Errorf("task %s not found", input.TaskID)
	}
	if task.Status == StatusCompleted {
		return nil, fmt.Errorf("task %s is already completed", input.TaskID)
	}

	now := time.Now().UTC()
	event := &TaskCompletedEvent{
		EventType:       "taskmanager_TaskCompleted",
		TaskID:          input.TaskID,
		CompletedAt:     now.Format(time.RFC3339),
		CompletionNotes: input.CompletionNotes,
	}
	return []eventsourcing.Event{event}, nil
}

func (p *TaskPlugin) listTasksHandler(input ListTasksInput) ([]eventsourcing.Event, error) {
	p.aggregate.mu.RLock()
	defer p.aggregate.mu.RUnlock()

	tasks := make([]*Task, 0, len(p.aggregate.tasks))
	for _, task := range p.aggregate.tasks {
		tasks = append(tasks, task)
	}

	// Apply filters
	var statusFilter, priorityFilter, tagFilter string
	if input.Status != "" && input.Status != "All" && validateStatus(input.Status) {
		statusFilter = input.Status
	}
	if input.Priority != "" && input.Priority != "All" && validatePriority(input.Priority) {
		priorityFilter = input.Priority
	}
	if input.Tag != "" {
		tagFilter = input.Tag
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
		filteredTaskIDs = append(filteredTaskIDs, task.TaskID)
	}

	// Sort tasks by creation time
	sort.Slice(filteredTasks, func(i, j int) bool {
		return filteredTasks[i].CreatedAt.Before(filteredTasks[j].CreatedAt)
	})

	event := &TasksListedEvent{EventType: "taskmanager_TasksListed", Tasks: filteredTasks}
	return []eventsourcing.Event{event}, nil
}

// UI
func (ta *TaskAggregate) GetCustomUI() fyne.CanvasObject {
	ta.mu.RLock()
	defer ta.mu.RUnlock()

	tasks := make([]*Task, 0, len(ta.tasks))
	for _, task := range ta.tasks {
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
		item := createTaskItem(task, ta)
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

// SystemPrompt provides a specialized prompt for the task manager agent
func (p *TaskPlugin) SystemPrompt() string {
	return `You are TaskMaster, a specialized AI for managing tasks in MindPalace.
Your job is to interpret user requests about tasks and execute the right commands
(CreateTask, UpdateTask, CompleteTask, etc.) based on the current task state.

Be concise, accurate, and always use the tools provided to manage tasks. Focus on:
1. Creating detailed tasks with proper priorities and statuses
2. Updating tasks with relevant information
3. Completing tasks with helpful completion notes
4. Listing and filtering tasks as requested

When creating or updating tasks, extract key information from user requests including:
- Task title and description
- Priority level (Low, Medium, High, Critical)
- Status (Pending, In Progress, Completed, Blocked)
- Deadlines (in ISO format)
- Tags for organization

Format your responses in a structured way and confirm actions performed.`
}

// AgentModel specifies the LLM model to use for this plugin's agent
func (p *TaskPlugin) AgentModel() string {
	return "qwq" // Using the general-purpose model for task management
}

func (p *TaskPlugin) EventHandlers() map[string]eventsourcing.EventHandler {
	return nil
}

// Helper functions
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
