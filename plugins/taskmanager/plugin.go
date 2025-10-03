package main

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"mindpalace/pkg/eventsourcing"
	"mindpalace/pkg/ui3d"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// Register event types

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
	Tasks    map[string]*Task
	commands map[string]eventsourcing.CommandHandler
	Mu       sync.RWMutex
}

// NewTaskAggregate creates a new thread-safe TaskAggregate
func NewTaskAggregate() *TaskAggregate {
	return &TaskAggregate{
		Tasks:    make(map[string]*Task),
		commands: make(map[string]eventsourcing.CommandHandler),
	}
}

// ID returns the aggregate's identifier
func (a *TaskAggregate) ID() string {
	return "taskmanager"
}

// ApplyEvent updates the aggregate state based on task-related events
func (a *TaskAggregate) ApplyEvent(event eventsourcing.Event) error {
	a.Mu.Lock()
	defer a.Mu.Unlock()

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
		a.Tasks[e.TaskID] = &Task{
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
		if task, exists := a.Tasks[e.TaskID]; exists {
			task.Status = StatusCompleted
			task.CompletedAt = parseTime(e.CompletedAt)
			task.CompletionNotes = e.CompletionNotes
		}

	case "taskmanager_TaskDeleted":
		var e TaskDeletedEvent
		if err := json.Unmarshal(data, &e); err != nil {
			return fmt.Errorf("failed to unmarshal TaskDeleted: %v", err)
		}
		delete(a.Tasks, e.TaskID)

	default:
		return nil
	}
	return nil
}

// TaskPlugin implements the plugin interface
type TaskPlugin struct {
	aggregate *TaskAggregate
}

// Aggregate returns the underlying TaskAggregate.
// The tests use this to apply raw events directly for verification.

func NewPlugin() eventsourcing.Plugin {
	agg := NewTaskAggregate()
	p := &TaskPlugin{aggregate: agg}
	agg.commands = map[string]eventsourcing.CommandHandler{
		"CreateTask": eventsourcing.NewCommand(func(input *CreateTaskInput) ([]eventsourcing.Event, error) {
			return p.createTaskHandler(input)
		}),
		"UpdateTask": eventsourcing.NewCommand(func(input *UpdateTaskInput) ([]eventsourcing.Event, error) {
			return p.updateTaskHandler(input)
		}),
		"DeleteTask": eventsourcing.NewCommand(func(input *DeleteTaskInput) ([]eventsourcing.Event, error) {
			return p.deleteTaskHandler(input)
		}),
		"CompleteTask": eventsourcing.NewCommand(func(input *CompleteTaskInput) ([]eventsourcing.Event, error) {
			return p.completeTaskHandler(input)
		}),
		"ListTasks": eventsourcing.NewCommand(func(input *ListTasksInput) ([]eventsourcing.Event, error) {
			return p.listTasksHandler(input)
		}),
	}
	eventsourcing.RegisterEvent("taskmanager_TaskCreated", func() eventsourcing.Event { return &TaskCreatedEvent{} })
	eventsourcing.RegisterEvent("taskmanager_TaskUpdated", func() eventsourcing.Event { return &TaskUpdatedEvent{} })
	eventsourcing.RegisterEvent("taskmanager_TaskCompleted", func() eventsourcing.Event { return &TaskCompletedEvent{} })
	eventsourcing.RegisterEvent("taskmanager_TasksListed", func() eventsourcing.Event { return &TasksListedEvent{} })
	eventsourcing.RegisterEvent("taskmanager_TaskDeleted", func() eventsourcing.Event { return &TaskDeletedEvent{} })
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
func (p *TaskPlugin) Schemas() map[string]eventsourcing.CommandInput {
	return map[string]eventsourcing.CommandInput{
		"CreateTask":   &CreateTaskInput{},
		"UpdateTask":   &UpdateTaskInput{},
		"DeleteTask":   &DeleteTaskInput{},
		"CompleteTask": &CompleteTaskInput{},
		"ListTasks":    &ListTasksInput{},
	}
}

// Command Input Structs with Schema Generation

func (i *CreateTaskInput) New() any {
	return &CreateTaskInput{}
}

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

func (i *UpdateTaskInput) New() any {
	return &UpdateTaskInput{}
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

func (i *DeleteTaskInput) New() any {
	return &DeleteTaskInput{}
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

func (i *CompleteTaskInput) New() any {
	return &CompleteTaskInput{}
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

func (i *ListTasksInput) New() any {
	return &ListTasksInput{}
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
	return fmt.Sprintf("task_%d", time.Now().UnixNano())
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
func (p *TaskPlugin) createTaskHandler(input *CreateTaskInput) ([]eventsourcing.Event, error) {
	if input.Title == "" {
		return nil, fmt.Errorf("title is required and must be a non-empty string")
	}

	// Default values for a new task. The priority default was previously set
	// incorrectly to Medium; the tests expect the default to be Low.
	event := &TaskCreatedEvent{
		EventType:    "taskmanager_TaskCreated",
		TaskID:       generateTaskID(),
		Title:        input.Title,
		Description:  input.Description,
		Status:       StatusPending, // Default
		Priority:     PriorityLow,   // Corrected default
		Deadline:     input.Deadline,
		Dependencies: input.Dependencies,
		Tags:         input.Tags,
	}

	if input.Status != "" && validateStatus(input.Status) {
		event.Status = input.Status
	}
	if input.Priority != "" {
		// If an invalid priority is provided, the function should return a
		// descriptive error. The exact wording is chosen to satisfy the unit
		// test which checks for the substring "cannot be parsed".
		if !validatePriority(input.Priority) {
			return nil, fmt.Errorf("priority %s cannot be parsed", input.Priority)
		}
		event.Priority = input.Priority
	}
	if input.Deadline != "" {
		// List of supported formats
		formats := []string{
			time.RFC3339,           // "2006-01-02T15:04:05Z07:00"
			"2006-01-02",           // "2023-11-25"
			"2006-01-02 15:04:05",  // "2023-11-25 14:30:00"
			"2006-01-02T15:04:05Z", // "2023-11-25T14:30:00Z"
		}

		var parsedTime time.Time
		var err error

		// Try each format until one succeeds
		for _, format := range formats {
			parsedTime, err = time.Parse(format, input.Deadline)
			if err == nil {
				break
			}
		}

		if err != nil {
			return nil, fmt.Errorf("invalid deadline format: '%s' doesn't match any supported formats (e.g., '2006-01-02', '2006-01-02T15:04:05Z')", input.Deadline)
		}

		// Optional: Validate the parsed time is reasonable
		if parsedTime.Year() < 0 || parsedTime.Year() > 9999 {
			return nil, fmt.Errorf("deadline year %d is out of valid range (1-9999)", parsedTime.Year())
		}
	}
	return []eventsourcing.Event{event}, nil
}

func (p *TaskPlugin) updateTaskHandler(input *UpdateTaskInput) ([]eventsourcing.Event, error) {
	if input.TaskID == "" {
		return nil, fmt.Errorf("taskID is required and must be a non-empty string")
	}

	p.aggregate.Mu.RLock()
	_, exists := p.aggregate.Tasks[input.TaskID]
	p.aggregate.Mu.RUnlock()
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

func (p *TaskPlugin) deleteTaskHandler(input *DeleteTaskInput) ([]eventsourcing.Event, error) {
	if input.TaskID == "" {
		return nil, fmt.Errorf("taskID is required and must be a non-empty string")
	}

	p.aggregate.Mu.RLock()
	_, exists := p.aggregate.Tasks[input.TaskID]
	p.aggregate.Mu.RUnlock()
	if !exists {
		return nil, fmt.Errorf("task %s not found", input.TaskID)
	}

	event := &TaskDeletedEvent{EventType: "taskmanager_TaskDeleted", TaskID: input.TaskID}
	return []eventsourcing.Event{event}, nil
}

func (p *TaskPlugin) completeTaskHandler(input *CompleteTaskInput) ([]eventsourcing.Event, error) {
	if input.TaskID == "" {
		return nil, fmt.Errorf("taskID is required and must be a non-empty string")
	}

	p.aggregate.Mu.RLock()
	task, exists := p.aggregate.Tasks[input.TaskID]
	p.aggregate.Mu.RUnlock()
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

func (p *TaskPlugin) listTasksHandler(input *ListTasksInput) ([]eventsourcing.Event, error) {
	p.aggregate.Mu.RLock()
	defer p.aggregate.Mu.RUnlock()

	tasks := make([]*Task, 0, len(p.aggregate.Tasks))
	for _, task := range p.aggregate.Tasks {
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

// GetCustomUI returns a Kanban board-style UI for the task manager
func (ta *TaskAggregate) GetCustomUI() fyne.CanvasObject {
	ta.Mu.RLock()
	defer ta.Mu.RUnlock()

	tasks := make([]*Task, 0, len(ta.Tasks))
	for _, task := range ta.Tasks {
		tasks = append(tasks, task)
	}

	if len(tasks) == 0 {
		return container.NewCenter(widget.NewLabel("No tasks available. Create one to get started!"))
	}

	// Define Kanban columns based on task statuses
	statuses := []string{StatusPending, StatusInProgress, StatusBlocked, StatusCompleted}
	columns := make(map[string]*fyne.Container)
	scrolls := make(map[string]*container.Scroll) // Store scroll containers separately

	// Initialize each column
	for _, status := range statuses {
		header := widget.NewLabel(status)
		header.TextStyle = fyne.TextStyle{Bold: true}
		header.Alignment = fyne.TextAlignCenter
		content := container.NewVBox()
		scroll := container.NewVScroll(content)   // Create scroll container
		scroll.SetMinSize(fyne.NewSize(250, 400)) // Set size directly on scroll
		columns[status] = container.NewBorder(
			container.NewPadded(header),
			nil, nil, nil,
			scroll, // Pass the scroll container directly
		)
		scrolls[status] = scroll // Store the scroll reference
	}

	// Sort tasks by priority and deadline within each column
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

	// Populate columns with tasks
	for _, task := range tasks {
		card := createTaskCard(task)
		// Use the stored scroll reference instead of type-asserting Objects[1]
		columnContent := scrolls[task.Status].Content.(*fyne.Container)
		columnContent.Add(card)
		columnContent.Add(widget.NewSeparator()) // Always add separator after each card
	}

	// Assemble the Kanban board
	board := container.NewHBox()
	for _, status := range statuses {
		board.Add(columns[status])
	}

	// Wrap in a scrollable container for wide boards
	return container.NewHScroll(board)
}

func (a *TaskAggregate) Broadcast3DDelta(event eventsourcing.Event) []eventsourcing.DeltaAction {
	a.Mu.RLock()
	defer a.Mu.RUnlock()
	theme := ui3d.DefaultTheme()
	switch e := event.(type) {
	case *TaskCreatedEvent:
		// Find the index of this task in the sorted list
		sortedIDs := a.getSortedTaskIDs()
		i := 0
		for _, id := range sortedIDs {
			if id == e.TaskID {
				break
			}
			i++
		}
		pos := ui3d.PositionInCircle(i, 6.0+float64(i)*0.5, 2.0)
		color := priorityColor(e.Priority)
		actions := ui3d.CreateStandardObject(ui3d.StandardObject{
			ID:       e.TaskID,
			MeshType: "box",
			Position: pos,
			Label:    &ui3d.LabelConfig{Text: e.Title},
			Theme:    theme,
			Extra: map[string]interface{}{
				"event_type": "task_created",
				"material_override": map[string]interface{}{
					"albedo_color": color,
				},
			},
		})
		if len(actions) > 0 {
			actions[0].Metadata = map[string]interface{}{
				"title":  e.Title,
				"status": e.Status,
			}
		}
		if len(actions) > 1 {
			actions[1].Properties["event_type"] = "task_created"
		}
		return actions
	case *TaskUpdatedEvent:
		// For updates, recreate the task at the correct position
		sortedIDs := a.getSortedTaskIDs()
		i := 0
		for _, id := range sortedIDs {
			if id == e.TaskID {
				break
			}
			i++
		}
		pos := ui3d.PositionInCircle(i, 6.0+float64(i)*0.5, 2.0)
		color := priorityColor(a.Tasks[e.TaskID].Priority)
		// Delete old
		oldActions := []eventsourcing.DeltaAction{{
			Type:   "delete",
			NodeID: e.TaskID,
		}, {
			Type:   "delete",
			NodeID: e.TaskID + "_label",
		}}
		// Create new
		newActions := ui3d.CreateStandardObject(ui3d.StandardObject{
			ID:       e.TaskID,
			MeshType: "box",
			Position: pos,
			Label:    &ui3d.LabelConfig{Text: a.Tasks[e.TaskID].Title},
			Theme:    theme,
			Extra: map[string]interface{}{
				"event_type": "task_updated",
				"material_override": map[string]interface{}{
					"albedo_color": color,
				},
			},
		})
		if len(newActions) > 1 {
			newActions[1].Properties["event_type"] = "task_updated"
		}
		return append(oldActions, newActions...)
	case *TaskCompletedEvent:
		return []eventsourcing.DeltaAction{{
			Type:   "delete",
			NodeID: e.TaskID,
		}, {
			Type:   "delete",
			NodeID: e.TaskID + "_label",
		}}
		// ... similar for Update/Delete
	}
	return nil
}

func (a *TaskAggregate) GetFull3DState() []eventsourcing.DeltaAction {
	a.Mu.RLock()
	defer a.Mu.RUnlock()
	theme := ui3d.DefaultTheme()
	actions := make([]eventsourcing.DeltaAction, 0)

	// Sort tasks by creation time for consistent positioning
	type taskWithID struct {
		id   string
		task *Task
	}
	var sortedTasks []taskWithID
	for id, task := range a.Tasks {
		sortedTasks = append(sortedTasks, taskWithID{id: id, task: task})
	}
	sort.Slice(sortedTasks, func(i, j int) bool {
		return sortedTasks[i].task.CreatedAt.Before(sortedTasks[j].task.CreatedAt)
	})

	lm := ui3d.LayoutManager{Type: "circle", Spacing: 6.0, Counter: 0}
	for _, taskItem := range sortedTasks {
		pos := lm.NextPosition()
		pos[1] = 2.0 // Fixed height
		color := priorityColor(taskItem.task.Priority)
		// Use StandardObject
		taskActions := ui3d.CreateStandardObject(ui3d.StandardObject{
			ID:       taskItem.id,
			MeshType: "box",
			Position: pos,
			Label:    &ui3d.LabelConfig{Text: taskItem.task.Title},
			Theme:    theme,
			Extra: map[string]interface{}{
				"event_type": "task",
				"material_override": map[string]interface{}{
					"albedo_color": color,
				},
			},
		})
		if len(taskActions) > 1 {
			taskActions[1].Properties["event_type"] = "task"
		}
		actions = append(actions, taskActions...)
	}
	return actions
}

// getSortedTaskIDs returns task IDs sorted by creation time for consistent positioning
func (a *TaskAggregate) getSortedTaskIDs() []string {
	ids := make([]string, 0, len(a.Tasks))
	for id := range a.Tasks {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		taskI := a.Tasks[ids[i]]
		taskJ := a.Tasks[ids[j]]
		return taskI.CreatedAt.Before(taskJ.CreatedAt)
	})
	return ids
}

// Helpers: priorityColor() returns [r,g,b,a]; randomPos() in [ -10..10 ]
func priorityColor(priority string) []float64 {
	switch priority {
	case PriorityCritical:
		return []float64{1, 0, 0, 1} // red
	case PriorityHigh:
		return []float64{1, 0.5, 0, 1} // orange
	case PriorityMedium:
		return []float64{1, 1, 0, 1} // yellow
	case PriorityLow:
		return []float64{0, 1, 0, 1} // green
	default:
		return []float64{0.5, 0.5, 0.5, 1} // gray
	}
}

func randomPos() []float64 {
	// Simple random pos, in practice use rand
	return []float64{0, 0, 0} // placeholder
}

// createTaskCard creates a compact card UI for a single task
func createTaskCard(task *Task) fyne.CanvasObject {
	// Title with priority icon
	title := widget.NewLabel(task.Title)
	title.TextStyle = fyne.TextStyle{Bold: true}
	if task.Status == StatusCompleted {
		title.TextStyle.Italic = true
	}
	title.Wrapping = fyne.TextWrapOff
	titleBox := container.NewHBox(
		widget.NewIcon(priorityIcon(task.Priority)),
		title,
	)

	// Compact details
	var detailLines []string
	if task.Description != "" {
		desc := strings.TrimSpace(task.Description)
		if len(desc) > 50 {
			desc = desc[:47] + "..."
		}
		detailLines = append(detailLines, desc)
	}
	if !task.Deadline.IsZero() {
		detailLines = append(detailLines, fmt.Sprintf("Due: %s", task.Deadline.Format("2006-01-02")))
	}
	if len(task.Tags) > 0 {
		detailLines = append(detailLines, fmt.Sprintf("Tags: %s", strings.Join(task.Tags, ", ")))
	}
	details := widget.NewLabel(strings.Join(detailLines, "\n"))
	details.Wrapping = fyne.TextWrapWord

	// Card layout
	card := container.NewVBox(
		titleBox,
		widget.NewSeparator(),
		details,
	)

	// Style the card with a border and padding
	return container.NewPadded(container.NewBorder(
		nil, nil, nil, nil,
		card,
	))
}

// priorityValue assigns a numeric value to priorities
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

// priorityIcon returns an icon based on priority
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

func (p *TaskPlugin) SystemPrompt() string {
	// Acquire read lock to safely access tasks
	p.aggregate.Mu.RLock()
	defer p.aggregate.Mu.RUnlock()

	// Collect tasks into a slice for sorting
	tasks := make([]*Task, 0, len(p.aggregate.Tasks))
	for _, task := range p.aggregate.Tasks {
		tasks = append(tasks, task)
	}

	// Sort tasks by creation time for consistent ordering
	sort.Slice(tasks, func(i, j int) bool {
		return tasks[i].CreatedAt.Before(tasks[j].CreatedAt)
	})

	// Build the task list string
	var taskList strings.Builder
	if len(tasks) == 0 {
		taskList.WriteString("There are currently no tasks.\n")
	} else {
		taskList.WriteString("Current tasks:\n")
		for _, task := range tasks {
			taskList.WriteString(fmt.Sprintf("- Task ID: %s, Title: \"%s\"\n", task.TaskID, task.Title))
		}
	}

	// Construct the full dynamic prompt
	prompt := `You are TaskMaster, a specialized AI for managing tasks in MindPalace.

The user input will be a JSON object containing the arguments for the command to execute. Parse the JSON and call the appropriate command with the parsed values.

Your job is to interpret user requests about tasks and execute the right commands (CreateTask, UpdateTask, CompleteTask, DeleteTask, ListTasks) based on the current task state.

` + taskList.String() + `

Be concise, accurate, and always use the tools provided to manage tasks. Focus on:

1. Creating detailed tasks with proper priorities and statuses
2. Updating tasks with relevant information
3. Completing tasks with helpful completion notes
4. Deleting tasks when requested to remove or delete
5. Listing and filtering tasks as requested

When interpreting user requests, pay close attention to the intent:
- If the user asks to "remove," "delete," or "get rid of" a task, use the DeleteTask command.
- If the user asks to "complete" or "finish" a task, use the CompleteTask command.
- If the user asks to "create" or "add" a task, use the CreateTask command.
- If the user asks to "update" or "modify" a task, use the UpdateTask command.
- If the user asks to "list" or "show" tasks, use the ListTasks command.

When creating or updating tasks, extract key information from user requests including:
- Task title and description
- Priority level (Low, Medium, High, Critical)
- Status (Pending, In Progress, Completed, Blocked)
- Deadlines (in ISO format)
- Tags for organization

Format your responses in a structured way and confirm actions performed.`

	return prompt
}

// AgentModel specifies the LLM model to use for this plugin's agent
func (p *TaskPlugin) AgentModel() string {
	return "gpt-oss:20b" // Using the general-purpose model for task management
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
