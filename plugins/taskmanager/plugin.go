package main

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"mindpalace/pkg/eventsourcing"
	"mindpalace/pkg/modellib"
	"mindpalace/pkg/ui3d"
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

	trelloCardWidth           = 1.85
	trelloCardHeight          = 2.6
	trelloCardThickness       = 0.12
	trelloAccentHeight        = 0.34
	trelloFrontOffset         = trelloCardThickness/2 + 0.02
	trelloShadowGroundY       = 0.04
	trelloShadowForwardOffset = 0.65
	trelloBoardElevation      = trelloCardHeight/2 + 0.12
	trelloColumnSpacing       = trelloCardWidth + 0.75
	trelloRowSpacing          = trelloCardHeight * 0.78
	trelloBoardInset          = 7.0
)

var (
	trelloBaseColor    = []float64{0.86, 0.9, 0.96, 1.0}
	trelloPaperColor   = []float64{0.97, 0.99, 1.0, 1.0}
	trelloShadowColor  = []float64{0.02, 0.05, 0.08, 0.28}
	trelloTextColor    = []float64{0.12, 0.16, 0.24, 1.0}
	trelloMutedText    = []float64{0.35, 0.4, 0.48, 1.0}
	trelloAccentColors = map[string][]float64{
		PriorityCritical: {0.95, 0.32, 0.32, 1.0},
		PriorityHigh:     {0.99, 0.66, 0.2, 1.0},
		PriorityMedium:   {0.99, 0.82, 0.28, 1.0},
		PriorityLow:      {0.42, 0.73, 0.98, 1.0},
	}
)

var trelloStatusOrder = []string{StatusPending, StatusInProgress, StatusCompleted, StatusBlocked}

type cardPlacement struct {
	Position []float64
	Forward  []float64
	Right    []float64
	Up       []float64
	Rotation []float64
}

func cloneVec(in []float64) []float64 {
	if len(in) != 3 {
		return []float64{0, 0, 0}
	}
	return []float64{in[0], in[1], in[2]}
}

func vecAdd(a, b []float64) []float64 {
	return []float64{a[0] + b[0], a[1] + b[1], a[2] + b[2]}
}

func vecSub(a, b []float64) []float64 {
	return []float64{a[0] - b[0], a[1] - b[1], a[2] - b[2]}
}

func vecScale(a []float64, s float64) []float64 {
	return []float64{a[0] * s, a[1] * s, a[2] * s}
}

func vecLength(a []float64) float64 {
	return math.Sqrt(a[0]*a[0] + a[1]*a[1] + a[2]*a[2])
}

func vecNormalize(a []float64) []float64 {
	length := vecLength(a)
	if length == 0 {
		return []float64{0, 0, 0}
	}
	return []float64{a[0] / length, a[1] / length, a[2] / length}
}

func vecCross(a, b []float64) []float64 {
	return []float64{
		a[1]*b[2] - a[2]*b[1],
		a[2]*b[0] - a[0]*b[2],
		a[0]*b[1] - a[1]*b[0],
	}
}

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
	ModelAsset      string    `json:"model_asset,omitempty"`
	ModelPath       string    `json:"model_path,omitempty"`
	ModelScale      float64   `json:"model_scale,omitempty"`
}

// TaskAggregate manages the state of tasks with thread safety
type TaskAggregate struct {
	Tasks    map[string]*Task
	commands map[string]eventsourcing.CommandHandler
	Mu       sync.RWMutex
	// Event scoring sink (optional): map[event_id]EventScore
	Scores map[string]eventsourcing.EventScore
}

// NewTaskAggregate creates a new thread-safe TaskAggregate
func NewTaskAggregate() *TaskAggregate {
	return &TaskAggregate{
		Tasks:    make(map[string]*Task),
		commands: make(map[string]eventsourcing.CommandHandler),
		Scores:   make(map[string]eventsourcing.EventScore),
	}
}

// ID returns the aggregate's identifier
func (a *TaskAggregate) ID() string {
	return "taskmanager"
}

// Reset clears in-memory task state while preserving registered command handlers.
func (a *TaskAggregate) Reset() {
	a.Mu.Lock()
	defer a.Mu.Unlock()
	a.Tasks = make(map[string]*Task)
	// Preserve scores across resets? Night scoring may choose to repopulate.
	// Start clean for deterministic replays.
	a.Scores = make(map[string]eventsourcing.EventScore)
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
			ModelAsset:   e.ModelAsset,
			ModelPath:    e.ModelPath,
			ModelScale:   e.ModelScale,
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
			if e.ModelAsset != "" {
				task.ModelAsset = e.ModelAsset
			}
			if e.ModelPath != "" {
				task.ModelPath = e.ModelPath
			}
			if e.ModelScale > 0 {
				task.ModelScale = e.ModelScale
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

		// Position updates don't change task state, just acknowledge

	default:
		// No-op for unrelated events
	}
	return nil
}

// RecordEventScore implements eventsourcing.EventScorable.
func (a *TaskAggregate) RecordEventScore(eventID string, score eventsourcing.EventScore) {
	a.Mu.Lock()
	defer a.Mu.Unlock()
	if a.Scores == nil {
		a.Scores = make(map[string]eventsourcing.EventScore)
	}
	a.Scores[eventID] = score
}

// OpenTaskSummaries returns a lightweight list of open tasks for scoring.
func (a *TaskAggregate) OpenTaskSummaries() []eventsourcing.TaskSummary {
	a.Mu.RLock()
	defer a.Mu.RUnlock()
	out := make([]eventsourcing.TaskSummary, 0, len(a.Tasks))
	for _, t := range a.Tasks {
		if t == nil {
			continue
		}
		if t.Status == StatusCompleted {
			continue
		}
		ts := eventsourcing.TaskSummary{
			ID:          t.TaskID,
			Title:       t.Title,
			Description: t.Description,
			Status:      t.Status,
			Tags:        append([]string{}, t.Tags...),
		}
		if !t.Deadline.IsZero() {
			ts.Deadline = t.Deadline.UTC().Format(time.RFC3339)
		}
		out = append(out, ts)
	}
	return out
}

// TaskPlugin implements the plugin interface
type TaskPlugin struct {
	aggregate    *TaskAggregate
	modelCatalog *modellib.Catalog
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

// SetModelCatalog satisfies modellib.CatalogConsumer so the orchestrator can supply baked assets.
func (p *TaskPlugin) SetModelCatalog(catalog *modellib.Catalog) {
	p.modelCatalog = catalog
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
	ModelAsset   string   `json:"model_asset,omitempty"`
	ModelPath    string   `json:"model_path,omitempty"`
	ModelScale   float64  `json:"model_scale,omitempty"`
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
	ModelAsset   string   `json:"model_asset,omitempty"`
	ModelPath    string   `json:"model_path,omitempty"`
	ModelScale   float64  `json:"model_scale,omitempty"`
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

func accentColorForPriority(priority string) []float64 {
	if color, ok := trelloAccentColors[priority]; ok {
		return color
	}
	return []float64{0.67, 0.74, 0.86, 1.0}
}

func statusColumnIndex(status string) int {
	for idx, name := range trelloStatusOrder {
		if status == name {
			return idx
		}
	}
	if len(trelloStatusOrder) == 0 {
		return 0
	}
	return len(trelloStatusOrder) - 1
}

func createdAtValue(task *Task) time.Time {
	if task == nil {
		return time.Unix(0, 0)
	}
	if task.CreatedAt.IsZero() {
		return time.Unix(0, 0)
	}
	return task.CreatedAt
}

func (a *TaskAggregate) snapshotTasks() []*Task {
	out := make([]*Task, 0, len(a.Tasks))
	for _, task := range a.Tasks {
		if task == nil {
			continue
		}
		out = append(out, task)
	}
	return out
}

func (a *TaskAggregate) buildBoardLayout(zone ui3d.Zone, tasks []*Task) map[string]cardPlacement {
	placements := make(map[string]cardPlacement, len(tasks))
	if len(tasks) == 0 {
		return placements
	}

	active := make([]*Task, 0, len(tasks))
	for _, task := range tasks {
		if task == nil {
			continue
		}
		if strings.TrimSpace(task.TaskID) == "" {
			continue
		}
		active = append(active, task)
	}
	if len(active) == 0 {
		return placements
	}

	sort.SliceStable(active, func(i, j int) bool {
		colI := statusColumnIndex(active[i].Status)
		colJ := statusColumnIndex(active[j].Status)
		if colI != colJ {
			return colI < colJ
		}
		createdI := createdAtValue(active[i])
		createdJ := createdAtValue(active[j])
		if !createdI.Equal(createdJ) {
			return createdI.Before(createdJ)
		}
		return active[i].TaskID < active[j].TaskID
	})

	bearing := zone.Angle * math.Pi / 180.0
	radius := zone.Radius - trelloBoardInset
	if radius < 18 {
		radius = 18
	}
	boardCenter := []float64{
		radius * math.Cos(bearing),
		trelloBoardElevation,
		radius * math.Sin(bearing),
	}
	forward := vecNormalize([]float64{-boardCenter[0], 0, -boardCenter[2]})
	if vecLength(forward) == 0 {
		forward = []float64{0, 0, -1}
	}
	up := []float64{0, 1, 0}
	right := vecNormalize(vecCross(up, forward))
	if vecLength(right) == 0 {
		right = []float64{1, 0, 0}
	}

	totalCols := len(trelloStatusOrder)
	if totalCols == 0 {
		totalCols = 1
	}
	columnCounts := make(map[int]int, totalCols)

	for _, task := range active {
		colIdx := statusColumnIndex(task.Status)
		if colIdx < 0 {
			colIdx = 0
		}
		if colIdx >= totalCols {
			colIdx = totalCols - 1
		}

		rowIdx := columnCounts[colIdx]
		columnCounts[colIdx] = rowIdx + 1

		colOffset := float64(colIdx) - float64(totalCols-1)/2.0
		position := cloneVec(boardCenter)
		position = vecAdd(position, vecScale(right, colOffset*trelloColumnSpacing))
		position = vecAdd(position, vecScale(forward, float64(rowIdx)*trelloRowSpacing))
		position = vecAdd(position, vecScale(forward, trelloFrontOffset*0.4))

		rotation := []float64{0, math.Atan2(forward[0], forward[2]), 0}

		placements[task.TaskID] = cardPlacement{
			Position: position,
			Forward:  cloneVec(forward),
			Right:    cloneVec(right),
			Up:       cloneVec(up),
			Rotation: rotation,
		}
	}

	return placements
}

func (a *TaskAggregate) populateBoard(builder *ui3d.DeltaBuilder, tasks []*Task, layout map[string]cardPlacement, extraFactory func(*Task) map[string]interface{}, highlightID string) {
	if builder == nil {
		return
	}
	seen := make(map[string]struct{}, len(layout))
	for _, task := range tasks {
		if task == nil || strings.TrimSpace(task.TaskID) == "" {
			continue
		}
		placement, ok := layout[task.TaskID]
		if !ok {
			continue
		}
		builder.Delete(task.TaskID).
			Delete(task.TaskID + "_paper").
			Delete(task.TaskID + "_accent").
			Delete(task.TaskID + "_shadow").
			Delete(task.TaskID + "_label").
			Delete(task.TaskID + "_meta").
			Delete(task.TaskID + "_model")

		var extras map[string]interface{}
		if extraFactory != nil {
			extras = extraFactory(task)
		}
		if extras == nil {
			extras = map[string]interface{}{}
		}
		if highlightID != "" && task.TaskID == highlightID {
			extras["highlight"] = true
		}
		a.renderTaskCard(builder, task, placement, extras)
		if strings.TrimSpace(task.ModelPath) != "" {
			a.addModelNode(builder, task, placement)
		}
		seen[task.TaskID] = struct{}{}
	}

	for taskID := range a.Tasks {
		if _, ok := seen[taskID]; ok {
			continue
		}
		builder.Delete(taskID).
			Delete(taskID + "_paper").
			Delete(taskID + "_accent").
			Delete(taskID + "_shadow").
			Delete(taskID + "_label").
			Delete(taskID + "_meta").
			Delete(taskID + "_model")
	}
}

func (p *TaskPlugin) suggestModelForTask(title string, tags []string) *modellib.CatalogEntry {
	if p == nil || p.modelCatalog == nil {
		return nil
	}
	queryParts := make([]string, 0, 1+len(tags))
	if strings.TrimSpace(title) != "" {
		queryParts = append(queryParts, title)
	}
	if len(tags) > 0 {
		queryParts = append(queryParts, strings.Join(tags, " "))
	}
	query := strings.TrimSpace(strings.Join(queryParts, " "))
	if query == "" {
		return nil
	}
	results := p.modelCatalog.Search(query, 5)
	if len(results) == 0 {
		return nil
	}
	best := results[0]
	return &best
}

func (a *TaskAggregate) renderTaskCard(builder *ui3d.DeltaBuilder, task *Task, placement cardPlacement, labelExtras map[string]interface{}) {
	if builder == nil || task == nil {
		return
	}

	center := cloneVec(placement.Position)
	forward := vecNormalize(placement.Forward)
	if vecLength(forward) == 0 {
		forward = []float64{0, 0, -1}
	}
	up := vecNormalize(placement.Up)
	if vecLength(up) == 0 {
		up = []float64{0, 1, 0}
	}
	right := placement.Right
	if vecLength(right) == 0 {
		right = vecNormalize(vecCross(up, forward))
	} else {
		right = vecNormalize(right)
	}

	rotation := placement.Rotation
	if len(rotation) != 3 {
		yaw := math.Atan2(forward[0], forward[2])
		rotation = []float64{0, yaw, 0}
	}

	display := map[string]interface{}{
		"title":    task.Title,
		"status":   task.Status,
		"priority": task.Priority,
	}

	baseAction := eventsourcing.DeltaAction{
		Type:     "create",
		NodeID:   task.TaskID,
		NodeType: "MeshInstance3D",
		Properties: map[string]interface{}{
			"mesh":     "box",
			"position": center,
			"rotation": rotation,
			"scale":    []float64{trelloCardWidth, trelloCardHeight, trelloCardThickness},
			"material_override": map[string]interface{}{
				"albedo_color": trelloBaseColor,
				"roughness":    0.54,
				"metallic":     0.08,
			},
			"display_info": display,
		},
	}
	builder.AddAction(baseAction)

	paperPos := vecAdd(center, vecScale(forward, trelloFrontOffset*0.6))
	paperAction := eventsourcing.DeltaAction{
		Type:     "create",
		NodeID:   task.TaskID + "_paper",
		NodeType: "MeshInstance3D",
		Properties: map[string]interface{}{
			"mesh":     "box",
			"position": paperPos,
			"rotation": rotation,
			"scale":    []float64{trelloCardWidth * 0.94, trelloCardHeight * 0.92, trelloCardThickness * 0.35},
			"material_override": map[string]interface{}{
				"albedo_color": trelloPaperColor,
				"roughness":    0.18,
				"metallic":     0.02,
			},
		},
	}
	builder.AddAction(paperAction)

	accentColor := accentColorForPriority(task.Priority)
	accentPos := vecAdd(paperPos, vecScale(up, trelloCardHeight/2-trelloAccentHeight/2-0.04))
	accentAction := eventsourcing.DeltaAction{
		Type:     "create",
		NodeID:   task.TaskID + "_accent",
		NodeType: "MeshInstance3D",
		Properties: map[string]interface{}{
			"mesh":     "box",
			"position": accentPos,
			"rotation": rotation,
			"scale":    []float64{trelloCardWidth * 0.94, trelloAccentHeight, trelloCardThickness * 0.32},
			"material_override": map[string]interface{}{
				"albedo_color": accentColor,
				"emission":     []float64{accentColor[0] * 0.4, accentColor[1] * 0.4, accentColor[2] * 0.4, 1.0},
			},
		},
	}
	builder.AddAction(accentAction)

	shadowPos := vecAdd(center, vecScale(forward, trelloShadowForwardOffset))
	shadowPos[1] = trelloShadowGroundY
	shadowRotation := []float64{math.Pi / 2, rotation[1], 0}
	shadowAction := eventsourcing.DeltaAction{
		Type:     "create",
		NodeID:   task.TaskID + "_shadow",
		NodeType: "MeshInstance3D",
		Properties: map[string]interface{}{
			"mesh":     "plane",
			"position": shadowPos,
			"rotation": shadowRotation,
			"scale":    []float64{trelloCardWidth * 1.4, trelloCardWidth * 1.4, 1},
			"material_override": map[string]interface{}{
				"albedo_color": trelloShadowColor,
			},
		},
	}
	builder.AddAction(shadowAction)

	frontFace := vecAdd(paperPos, vecScale(forward, trelloFrontOffset*0.25))
	titlePos := vecAdd(frontFace, vecScale(up, 0.35))
	titlePos = vecAdd(titlePos, vecScale(right, -trelloCardWidth*0.36))
	metaPos := vecAdd(frontFace, vecScale(up, -0.55))
	metaPos = vecAdd(metaPos, vecScale(right, -trelloCardWidth*0.36))
	textWidth := trelloCardWidth * 0.82

	titleAction := eventsourcing.DeltaAction{
		Type:     "create",
		NodeID:   task.TaskID + "_label",
		NodeType: "Label3D",
		Properties: map[string]interface{}{
			"text":                 task.Title,
			"position":             titlePos,
			"rotation":             rotation,
			"modulate":             trelloTextColor,
			"font_size":            24,
			"outline_modulate":     []float64{1, 1, 1, 0.85},
			"outline_size":         0.4,
			"width":                textWidth,
			"billboard":            "disabled",
			"autowrap_mode":        "word",
			"horizontal_alignment": "left",
		},
	}
	builder.AddAction(titleAction)
	if len(labelExtras) > 0 {
		builder.WithExtra(labelExtras)
	}

	if meta := buildTaskMeta(task); meta != "" {
		metaAction := eventsourcing.DeltaAction{
			Type:     "create",
			NodeID:   task.TaskID + "_meta",
			NodeType: "Label3D",
			Properties: map[string]interface{}{
				"text":                 meta,
				"position":             metaPos,
				"rotation":             rotation,
				"modulate":             trelloMutedText,
				"font_size":            16,
				"outline_modulate":     []float64{1, 1, 1, 0.6},
				"outline_size":         0.25,
				"width":                textWidth,
				"billboard":            "disabled",
				"autowrap_mode":        "word",
				"horizontal_alignment": "left",
			},
		}
		builder.AddAction(metaAction)
	}
}

func buildTaskMeta(task *Task) string {
	if task == nil {
		return ""
	}
	parts := make([]string, 0, 4)
	if strings.TrimSpace(task.Priority) != "" {
		parts = append(parts, task.Priority)
	}
	if strings.TrimSpace(task.Status) != "" {
		parts = append(parts, task.Status)
	}
	if !task.Deadline.IsZero() {
		parts = append(parts, task.Deadline.Format("Jan 02"))
	}
	if len(task.Tags) > 0 {
		parts = append(parts, strings.Join(task.Tags, " · "))
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "  •  ")
}

// Command Handlers
func (p *TaskPlugin) createTaskHandler(input *CreateTaskInput) ([]eventsourcing.Event, error) {
	if input.Title == "" {
		return nil, fmt.Errorf("title is required and must be a non-empty string")
	}

	// Default values for a new task.
	event := &TaskCreatedEvent{
		EventType:    "taskmanager_TaskCreated",
		TaskID:       generateTaskID(),
		Title:        input.Title,
		Description:  input.Description,
		Status:       StatusPending, // Default
		Priority:     PriorityLow,
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

		if parsedTime.Year() < 0 || parsedTime.Year() > 9999 {
			return nil, fmt.Errorf("deadline year %d is out of valid range (1-9999)", parsedTime.Year())
		}
		event.Deadline = parsedTime.Format(time.RFC3339)
	}

	if match := p.suggestModelForTask(event.Title, event.Tags); match != nil {
		event.ModelAsset = match.ID
		event.ModelPath = match.ResourcePath
		if match.RecommendedScale > 0 {
			event.ModelScale = match.RecommendedScale
		}
	}
	return []eventsourcing.Event{event}, nil
}

func (p *TaskPlugin) updateTaskHandler(input *UpdateTaskInput) ([]eventsourcing.Event, error) {
	if input.TaskID == "" {
		return nil, fmt.Errorf("taskID is required and must be a non-empty string")
	}

	p.aggregate.Mu.RLock()
	existingTask := p.aggregate.Tasks[input.TaskID]
	p.aggregate.Mu.RUnlock()
	if existingTask == nil {
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

	if strings.TrimSpace(input.Title) != "" || len(input.Tags) > 0 {
		title := input.Title
		if strings.TrimSpace(title) == "" {
			title = existingTask.Title
		}
		tags := input.Tags
		if len(tags) == 0 {
			tags = existingTask.Tags
		}
		if match := p.suggestModelForTask(title, tags); match != nil {
			event.ModelAsset = match.ID
			event.ModelPath = match.ResourcePath
			if match.RecommendedScale > 0 {
				event.ModelScale = match.RecommendedScale
			}
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

func (a *TaskAggregate) EmitDelta(event eventsourcing.Event) *eventsourcing.DeltaEnvelope {
	a.Mu.RLock()
	defer a.Mu.RUnlock()
	theme := ui3d.DefaultTheme()
	zones := ui3d.GetGlobalZones()
	zone, ok := zones[a.ID()]
	if !ok {
		zone = ui3d.Zone{Angle: 0, Radius: 28, GridRows: 1, GridCols: len(trelloStatusOrder), GridDepth: 12}
	}

	switch e := event.(type) {
	case *TaskCreatedEvent:
		snapshot := a.snapshotTasks()
		layout := a.buildBoardLayout(zone, snapshot)
		builder := ui3d.NewDeltaBuilder(theme)
		a.populateBoard(builder, snapshot, layout, func(t *Task) map[string]interface{} {
			return map[string]interface{}{
				"event_type": "task_created",
				"status":     t.Status,
			}
		}, e.TaskID)
		actions := builder.Build()
		task := a.Tasks[e.TaskID]
		var component *TaskCardComponent
		if task != nil {
			if placement, ok := layout[task.TaskID]; ok {
				component = &TaskCardComponent{
					TaskID:   task.TaskID,
					Title:    task.Title,
					Status:   task.Status,
					Priority: task.Priority,
					Position: placement.Position,
					Theme:    theme,
				}
			}
		}
		if len(actions) > 0 && task != nil {
			actions[0].Metadata = map[string]interface{}{
				"title":  task.Title,
				"status": task.Status,
			}
		}
		ts := eventsourcing.ISOTimestamp()
		envelope := &eventsourcing.DeltaEnvelope{
			Type:       "delta",
			Aggregate:  "taskmanager",
			EventID:    ts,
			Timestamp:  ts,
			SequenceID: eventsourcing.NextSequenceID(),
			Actions:    actions,
		}
		if component != nil {
			envelope.Components = []interface{}{component}
		}
		return envelope
	case *TaskUpdatedEvent:
		task, exists := a.Tasks[e.TaskID]
		if !exists {
			return nil
		}
		snapshot := a.snapshotTasks()
		layout := a.buildBoardLayout(zone, snapshot)
		builder := ui3d.NewDeltaBuilder(theme)
		a.populateBoard(builder, snapshot, layout, func(t *Task) map[string]interface{} {
			return map[string]interface{}{
				"event_type": "task_updated",
				"status":     t.Status,
			}
		}, e.TaskID)
		actions := builder.Build()
		var component *TaskCardComponent
		if placement, ok := layout[task.TaskID]; ok {
			component = &TaskCardComponent{
				TaskID:   task.TaskID,
				Title:    task.Title,
				Status:   task.Status,
				Priority: task.Priority,
				Position: placement.Position,
				Theme:    theme,
			}
		}
		ts := eventsourcing.ISOTimestamp()
		envelope := &eventsourcing.DeltaEnvelope{
			Type:       "delta",
			Aggregate:  "taskmanager",
			EventID:    ts,
			Timestamp:  ts,
			SequenceID: eventsourcing.NextSequenceID(),
			Actions:    actions,
		}
		if component != nil {
			envelope.Components = []interface{}{component}
		}
		return envelope
	case *TaskCompletedEvent:
		snapshot := a.snapshotTasks()
		layout := a.buildBoardLayout(zone, snapshot)
		builder := ui3d.NewDeltaBuilder(theme)
		a.populateBoard(builder, snapshot, layout, func(t *Task) map[string]interface{} {
			return map[string]interface{}{
				"event_type": "task_completed",
				"status":     t.Status,
			}
		}, e.TaskID)
		actions := builder.Build()
		ts := eventsourcing.ISOTimestamp()
		envelope := &eventsourcing.DeltaEnvelope{
			Type:       "delta",
			Aggregate:  "taskmanager",
			EventID:    ts,
			Timestamp:  ts,
			SequenceID: eventsourcing.NextSequenceID(),
			Actions:    actions,
		}
		return envelope
	case *TaskDeletedEvent:
		snapshot := a.snapshotTasks()
		layout := a.buildBoardLayout(zone, snapshot)
		builder := ui3d.NewDeltaBuilder(theme)
		builder.Delete(e.TaskID).
			Delete(e.TaskID + "_accent").
			Delete(e.TaskID + "_paper").
			Delete(e.TaskID + "_shadow").
			Delete(e.TaskID + "_label").
			Delete(e.TaskID + "_meta").
			Delete(e.TaskID + "_model")
		a.populateBoard(builder, snapshot, layout, func(t *Task) map[string]interface{} {
			return map[string]interface{}{
				"event_type": "task_deleted",
				"status":     t.Status,
			}
		}, "")
		actions := builder.Build()
		ts := eventsourcing.ISOTimestamp()
		return &eventsourcing.DeltaEnvelope{
			Type:       "delta",
			Aggregate:  "taskmanager",
			EventID:    ts,
			Timestamp:  ts,
			SequenceID: eventsourcing.NextSequenceID(),
			Actions:    actions,
		}
	case *TasksListedEvent:
		builder := ui3d.NewDeltaBuilder(theme)
		listed := make([]*Task, 0, len(e.Tasks))
		for _, task := range e.Tasks {
			if task == nil {
				continue
			}
			listed = append(listed, task)
		}
		layout := a.buildBoardLayout(zone, listed)
		a.populateBoard(builder, listed, layout, func(t *Task) map[string]interface{} {
			return map[string]interface{}{
				"event_type": "task_listed",
				"status":     t.Status,
			}
		}, "")
		actions := builder.Build()
		if len(actions) == 0 {
			return nil
		}
		ts := eventsourcing.ISOTimestamp()
		return &eventsourcing.DeltaEnvelope{
			Type:       "delta",
			Aggregate:  "taskmanager",
			EventID:    ts,
			Timestamp:  ts,
			SequenceID: eventsourcing.NextSequenceID(),
			Actions:    actions,
		}
	}
	return nil
}

func (a *TaskAggregate) Clone() eventsourcing.Aggregate {
	a.Mu.RLock()
	defer a.Mu.RUnlock()
	// Create a new aggregate with copied state
	newAgg := NewTaskAggregate()
	for id, task := range a.Tasks {
		newTask := *task // Shallow copy, assuming no nested pointers
		newAgg.Tasks[id] = &newTask
	}
	return newAgg
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

func (a *TaskAggregate) addModelNode(builder *ui3d.DeltaBuilder, task *Task, placement cardPlacement) {
	if builder == nil || task == nil {
		return
	}
	path := strings.TrimSpace(task.ModelPath)
	if path == "" {
		return
	}
	center := cloneVec(placement.Position)
	up := vecNormalize(placement.Up)
	if vecLength(up) == 0 {
		up = []float64{0, 1, 0}
	}
	forward := vecNormalize(placement.Forward)
	if vecLength(forward) == 0 {
		forward = []float64{0, 0, -1}
	}
	pos := vecAdd(center, vecScale(up, trelloCardHeight/2+0.65))
	pos = vecAdd(pos, vecScale(forward, trelloCardThickness/2+0.05))
	scaleFactor := task.ModelScale
	if scaleFactor <= 0 {
		scaleFactor = 0.6
	}
	scale := []float64{scaleFactor, scaleFactor, scaleFactor}
	builder.CreateModel(task.TaskID+"_model", path, pos, scale).WithExtra(map[string]interface{}{
		"model_asset": task.ModelAsset,
		"event_type":  "task_model",
	})
}

// Helpers: priorityColor() returns [r,g,b,a]; randomPos() in [ -10..10 ]
func priorityColor(priority string) []float64 {
	return accentColorForPriority(priority)
}

func randomPos() []float64 {
	// Simple random pos, in practice use rand
	return []float64{0, 0, 0} // placeholder
}

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

// Description returns a short description of how the orchestrator AI can use this agent
func (p *TaskPlugin) Description() string {
	return "use this to manage the todolist, talk to me in natural language with the task related request and I will create,update,delete or modify the tasks as needed."
}

func (p *TaskPlugin) Metadata() eventsourcing.PluginMetadata {
	return eventsourcing.PluginMetadata{
		Name:      p.Name(),
		Summary:   "Coordinates complex task graphs with priorities, deadlines, and blockers.",
		UsageHint: "Delegate todo triage, dependency checks, stand-up summaries, or when tasks need structured updates.",
		Capabilities: []eventsourcing.PluginCapability{
			{Name: "CreateTask", Description: "Capture new actionable tasks with priorities, tags, and deadlines."},
			{Name: "UpdateTask", Description: "Modify task details including status, priority, and dependencies."},
			{Name: "CompleteTask", Description: "Mark tasks as complete with graceful handling of blockers."},
			{Name: "DeleteTask", Description: "Retire tasks that are no longer relevant."},
			{Name: "ListTasks", Description: "Summarize tasks filtered by status, priority, or tags."},
		},
		Examples: []string{
			"Add a critical task to renew the vehicle insurance before Friday.",
			"Show me all blocked tasks tagged 'paperwork' sorted by deadline.",
		},
		Tags:           []string{"tasks", "planning", "execution"},
		Maintainer:     "MindPalace Core Team",
		DefaultTimeout: 12 * time.Second,
		Safety:         "trusted",
		Reliability:    "battle-tested",
		Lifecycle:      "maintained",
		ModelAsset:     "82539",
	}
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

// TaskCardComponent implements ui3d.UIComponent for task cards.
type TaskCardComponent struct {
	TaskID   string
	Title    string
	Status   string
	Priority string
	Position []float64
	Theme    ui3d.Theme
}

func (t *TaskCardComponent) Type() string { return "TaskCard" }

func (t *TaskCardComponent) Properties() map[string]interface{} {
	color := priorityColor(t.Priority)
	return map[string]interface{}{
		"task_id":  t.TaskID,
		"title":    t.Title,
		"status":   t.Status,
		"priority": t.Priority,
		"position": t.Position,
		"style": map[string]interface{}{
			"background_color": []float64{trelloBaseColor[0], trelloBaseColor[1], trelloBaseColor[2], trelloBaseColor[3]},
			"accent_color":     color,
			"text_color":       []float64{trelloTextColor[0], trelloTextColor[1], trelloTextColor[2], trelloTextColor[3]},
			"shadow_color":     []float64{trelloShadowColor[0], trelloShadowColor[1], trelloShadowColor[2], trelloShadowColor[3]},
		},
		"display_info": map[string]interface{}{
			"title": t.Title,
			"type":  "task",
		},
	}
}

func (t *TaskCardComponent) Actions() map[string]ui3d.Action {
	actions := make(map[string]ui3d.Action)

	statusTargets := []string{StatusPending, StatusInProgress, StatusCompleted, StatusBlocked}

	changeStatus := ui3d.NewCommandAction("drag_to_zone", ui3d.CommandDescriptor{
		Command: "UpdateTask",
		Arguments: map[string]ui3d.ValueBinding{
			"TaskID": ui3d.StaticValue(t.TaskID),
			"Status": ui3d.ContextValue("drop.zone"),
		},
		Description: "Move task between workflow columns",
	})
	changeStatus.Label = "Move Task"
	changeStatus.Hints = map[string]interface{}{
		"interaction":    "drag_drop",
		"status_targets": statusTargets,
	}
	changeStatus.Data = map[string]interface{}{
		"command": "UpdateTask",
		"data": map[string]interface{}{
			"TaskID": t.TaskID,
		},
	}
	actions["change_status"] = changeStatus

	rename := ui3d.NewCommandAction("edit_text", ui3d.CommandDescriptor{
		Command: "UpdateTask",
		Arguments: map[string]ui3d.ValueBinding{
			"TaskID": ui3d.StaticValue(t.TaskID),
			"Title":  ui3d.UserInputValue("text"),
		},
		Description: "Rename this task card",
	})
	rename.Label = "Rename Task"
	rename.Hints = map[string]interface{}{
		"field":       "title",
		"placeholder": "Rename task",
	}
	actions["rename"] = rename

	editDescription := ui3d.NewCommandAction("edit_rich_text", ui3d.CommandDescriptor{
		Command: "UpdateTask",
		Arguments: map[string]ui3d.ValueBinding{
			"TaskID":      ui3d.StaticValue(t.TaskID),
			"Description": ui3d.UserInputValue("text"),
		},
		Description: "Update task description",
	})
	editDescription.Label = "Edit Description"
	editDescription.Hints = map[string]interface{}{
		"field":       "description",
		"placeholder": "Add more detail…",
	}
	actions["edit_description"] = editDescription

	complete := ui3d.NewCommandAction("button_press", ui3d.CommandDescriptor{
		Command: "CompleteTask",
		Arguments: map[string]ui3d.ValueBinding{
			"TaskID": ui3d.StaticValue(t.TaskID),
		},
		Description: "Mark task as complete",
		Metadata: map[string]interface{}{
			"set_status": StatusCompleted,
		},
	})
	complete.Label = "Mark Complete"
	complete.Icon = "check"
	actions["complete"] = complete

	deleteAction := ui3d.NewCommandAction("button_press", ui3d.CommandDescriptor{
		Command: "DeleteTask",
		Arguments: map[string]ui3d.ValueBinding{
			"TaskID": ui3d.StaticValue(t.TaskID),
		},
		Description:  "Delete task",
		Confirmation: fmt.Sprintf("Delete \"%s\"?", t.Title),
	})
	deleteAction.Label = "Delete Task"
	deleteAction.Icon = "trash"
	actions["delete"] = deleteAction

	return actions
}

func (t *TaskCardComponent) Serialize() map[string]interface{} {
	return map[string]interface{}{
		"type":       t.Type(),
		"properties": t.Properties(),
		"actions":    t.Actions(),
	}
}
