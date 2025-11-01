package main

import (
	"fmt"
	"testing"
	"time"

	"mindpalace/pkg/ui3d"
)

func TestTaskAggregate_ApplyEvent_TaskCreated(t *testing.T) {
	agg := NewTaskAggregate()

	event := &TaskCreatedEvent{
		EventType:    "taskmanager_TaskCreated",
		TaskID:       "task1",
		Title:        "Test Task",
		Description:  "A test task",
		Status:       StatusPending,
		Priority:     PriorityMedium,
		Deadline:     "2023-12-31T23:59:59Z",
		Dependencies: []string{},
		Tags:         []string{"test"},
	}

	err := agg.ApplyEvent(event)
	if err != nil {
		t.Fatalf("ApplyEvent failed: %v", err)
	}

	if len(agg.Tasks) != 1 {
		t.Errorf("Expected 1 task, got %d", len(agg.Tasks))
	}

	task, exists := agg.Tasks["task1"]
	if !exists {
		t.Fatal("Task not found")
	}

	if task.Title != "Test Task" {
		t.Errorf("Expected title 'Test Task', got '%s'", task.Title)
	}

	if task.Status != StatusPending {
		t.Errorf("Expected status '%s', got '%s'", StatusPending, task.Status)
	}
}

func TestTaskAggregate_ApplyEvent_TaskUpdated(t *testing.T) {
	agg := NewTaskAggregate()

	// First create a task
	createEvent := &TaskCreatedEvent{
		EventType: "taskmanager_TaskCreated",
		TaskID:    "task1",
		Title:     "Original Title",
		Status:    StatusPending,
		Priority:  PriorityLow,
	}
	agg.ApplyEvent(createEvent)

	// Now update it
	updateEvent := &TaskUpdatedEvent{
		EventType: "taskmanager_TaskUpdated",
		TaskID:    "task1",
		Title:     "Updated Title",
		Status:    StatusInProgress,
	}

	err := agg.ApplyEvent(updateEvent)
	if err != nil {
		t.Fatalf("ApplyEvent failed: %v", err)
	}

	task := agg.Tasks["task1"]
	if task.Title != "Updated Title" {
		t.Errorf("Expected title 'Updated Title', got '%s'", task.Title)
	}

	if task.Status != StatusInProgress {
		t.Errorf("Expected status '%s', got '%s'", StatusInProgress, task.Status)
	}
}

func TestTaskAggregate_ApplyEvent_TaskCompleted(t *testing.T) {
	agg := NewTaskAggregate()

	// Create a task
	createEvent := &TaskCreatedEvent{
		EventType: "taskmanager_TaskCreated",
		TaskID:    "task1",
		Title:     "Test Task",
		Status:    StatusPending,
	}
	agg.ApplyEvent(createEvent)

	// Complete it
	completedAt := time.Now().UTC().Format(time.RFC3339)
	completeEvent := &TaskCompletedEvent{
		EventType:       "taskmanager_TaskCompleted",
		TaskID:          "task1",
		CompletedAt:     completedAt,
		CompletionNotes: "Done!",
	}

	err := agg.ApplyEvent(completeEvent)
	if err != nil {
		t.Fatalf("ApplyEvent failed: %v", err)
	}

	task := agg.Tasks["task1"]
	if task.Status != StatusCompleted {
		t.Errorf("Expected status '%s', got '%s'", StatusCompleted, task.Status)
	}

	if task.CompletionNotes != "Done!" {
		t.Errorf("Expected completion notes 'Done!', got '%s'", task.CompletionNotes)
	}
}

func TestTaskAggregate_ApplyEvent_TaskDeleted(t *testing.T) {
	agg := NewTaskAggregate()

	// Create a task
	createEvent := &TaskCreatedEvent{
		EventType: "taskmanager_TaskCreated",
		TaskID:    "task1",
		Title:     "Test Task",
	}
	agg.ApplyEvent(createEvent)

	// Delete it
	deleteEvent := &TaskDeletedEvent{
		EventType: "taskmanager_TaskDeleted",
		TaskID:    "task1",
	}

	err := agg.ApplyEvent(deleteEvent)
	if err != nil {
		t.Fatalf("ApplyEvent failed: %v", err)
	}

	if len(agg.Tasks) != 0 {
		t.Errorf("Expected 0 tasks after delete, got %d", len(agg.Tasks))
	}
}

func TestTaskAggregate_Clone(t *testing.T) {
	agg := NewTaskAggregate()

	// Create a task
	createEvent := &TaskCreatedEvent{
		EventType: "taskmanager_TaskCreated",
		TaskID:    "task1",
		Title:     "Test Task",
		Priority:  PriorityHigh,
	}
	agg.ApplyEvent(createEvent)

	cloned := agg.Clone()
	if cloned == nil {
		t.Fatal("Clone returned nil")
	}
	if cloned.ID() != agg.ID() {
		t.Errorf("Cloned ID mismatch")
	}
	// Check that tasks are copied
	clonedAgg, ok := cloned.(*TaskAggregate)
	if !ok {
		t.Fatal("Clone not *TaskAggregate")
	}
	if len(clonedAgg.Tasks) != 1 {
		t.Errorf("Expected 1 task in clone, got %d", len(clonedAgg.Tasks))
	}
}

func TestTaskAggregate_Broadcast3DDelta_TaskCreated(t *testing.T) {
	agg := NewTaskAggregate()

	event := &TaskCreatedEvent{
		EventType: "taskmanager_TaskCreated",
		TaskID:    "task1",
		Title:     "Test Task",
		Priority:  PriorityHigh,
	}

	// Apply the event first to add to aggregate
	agg.ApplyEvent(event)

	// Set up zones for testing
	zones := map[string]ui3d.Zone{
		"taskmanager": {Angle: 0, Radius: 100, GridRows: 1, GridCols: 3, GridDepth: 10},
	}
	ui3d.SetGlobalZones(zones)

	signal := agg.EmitDelta(event)
	actions := signal.Actions

	// Should have 2 actions: box and label
	if len(actions) != 2 {
		t.Errorf("Expected 2 actions, got %d", len(actions))
	}

	boxAction := actions[0]
	if boxAction.NodeID != "task1" {
		t.Errorf("Expected NodeID 'task1', got '%s'", boxAction.NodeID)
	}

	labelAction := actions[1]
	if labelAction.NodeID != "task1_label" {
		t.Errorf("Expected NodeID 'task1_label', got '%s'", labelAction.NodeID)
	}
}

func TestTaskAggregate_Broadcast3DDelta_TaskCompleted(t *testing.T) {
	agg := NewTaskAggregate()

	event := &TaskCompletedEvent{
		EventType: "taskmanager_TaskCompleted",
		TaskID:    "task1",
	}

	signal := agg.EmitDelta(event)
	actions := signal.Actions

	// Should have 2 actions: delete box and delete label
	if len(actions) != 2 {
		t.Errorf("Expected 2 actions, got %d", len(actions))
	}

	if actions[0].Type != "delete" || actions[0].NodeID != "task1" {
		t.Errorf("Expected delete action for 'task1', got %v", actions[0])
	}

	if actions[1].Type != "delete" || actions[1].NodeID != "task1_label" {
		t.Errorf("Expected delete action for 'task1_label', got %v", actions[1])
	}
}

func TestTaskAggregate_Broadcast3DDelta_TaskDeleted(t *testing.T) {
	agg := NewTaskAggregate()

	event := &TaskDeletedEvent{
		EventType: "taskmanager_TaskDeleted",
		TaskID:    "task1",
	}

	signal := agg.EmitDelta(event)
	if signal == nil {
		t.Fatal("Expected non-nil delta for task deletion")
	}

	if len(signal.Actions) != 2 {
		t.Fatalf("Expected 2 actions, got %d", len(signal.Actions))
	}

	if signal.Actions[0].Type != "delete" || signal.Actions[0].NodeID != "task1" {
		t.Errorf("Expected delete action for 'task1', got %v", signal.Actions[0])
	}
	if signal.Actions[1].Type != "delete" || signal.Actions[1].NodeID != "task1_label" {
		t.Errorf("Expected delete action for 'task1_label', got %v", signal.Actions[1])
	}
}

func TestTaskAggregate_Broadcast3DDelta_TasksListed(t *testing.T) {
	agg := NewTaskAggregate()

	createA := &TaskCreatedEvent{
		EventType: "taskmanager_TaskCreated",
		TaskID:    "task_a",
		Title:     "Task Alpha",
		Status:    StatusPending,
		Priority:  PriorityHigh,
	}
	createB := &TaskCreatedEvent{
		EventType: "taskmanager_TaskCreated",
		TaskID:    "task_b",
		Title:     "Task Beta",
		Status:    StatusInProgress,
		Priority:  PriorityMedium,
	}
	if err := agg.ApplyEvent(createA); err != nil {
		t.Fatalf("apply createA failed: %v", err)
	}
	time.Sleep(1 * time.Millisecond) // ensure distinct CreatedAt ordering
	if err := agg.ApplyEvent(createB); err != nil {
		t.Fatalf("apply createB failed: %v", err)
	}

	zones := map[string]ui3d.Zone{
		"taskmanager": {Angle: 0, Radius: 50, GridRows: 1, GridCols: 3, GridDepth: 10},
	}
	ui3d.SetGlobalZones(zones)

	listEvent := &TasksListedEvent{
		EventType: "taskmanager_TasksListed",
		Tasks: []*Task{
			agg.Tasks["task_a"],
			agg.Tasks["task_b"],
		},
	}

	signal := agg.EmitDelta(listEvent)
	if signal == nil {
		t.Fatal("Expected non-nil delta for tasks list refresh")
	}
	if signal.SequenceID == 0 {
		t.Error("Expected non-zero sequence ID for list delta")
	}

	actionCounts := map[string]int{}
	for _, act := range signal.Actions {
		key := fmt.Sprintf("%s:%s", act.Type, act.NodeID)
		actionCounts[key]++
	}

	expectedKeys := []string{
		"delete:task_a", "delete:task_a_label",
		"create:task_a", "create:task_a_label",
		"delete:task_b", "delete:task_b_label",
		"create:task_b", "create:task_b_label",
	}
	for _, key := range expectedKeys {
		if actionCounts[key] == 0 {
			t.Errorf("Expected action %s to be present", key)
		}
	}
}

func TestTaskAggregate_GetCurrent3DState(t *testing.T) {
	agg := NewTaskAggregate()

	// Create a task
	createEvent := &TaskCreatedEvent{
		EventType: "taskmanager_TaskCreated",
		TaskID:    "task1",
		Title:     "Test Task",
		Status:    StatusPending,
		Priority:  PriorityMedium,
	}
	err := agg.ApplyEvent(createEvent)
	if err != nil {
		t.Fatalf("ApplyEvent failed: %v", err)
	}

	signal := agg.EmitDelta(createEvent)

	// Should have 2 actions: create box and create label
	if len(signal.Actions) != 2 {
		t.Errorf("Expected 2 actions, got %d", len(signal.Actions))
	}

	boxAction := signal.Actions[0]
	if boxAction.Type != "create" || boxAction.NodeID != "task1" {
		t.Errorf("Expected create action for 'task1', got %v", boxAction)
	}

	labelAction := signal.Actions[1]
	if labelAction.Type != "create" || labelAction.NodeID != "task1_label" {
		t.Errorf("Expected create action for 'task1_label', got %v", labelAction)
	}

	// TODO: Check state summary
	// if signal.StateSummary == nil {
	// 	t.Error("Expected state summary, got nil")
	// }
	// tasks, ok := signal.StateSummary["tasks"].([]map[string]interface{})
	// if !ok || len(tasks) != 1 {
	// 	t.Errorf("Expected 1 task in summary, got %v", signal.StateSummary)
	// }
}

func TestTaskStatusUpdateTriggersPositionAnimation(t *testing.T) {
	agg := NewTaskAggregate()

	// Set up mock zones to avoid panic
	zones := map[string]ui3d.Zone{
		"taskmanager": {Angle: 0, Radius: 100, GridRows: 1, GridCols: 3, GridDepth: 10},
	}
	ui3d.SetGlobalZones(zones)

	// Create a task in Pending
	createEvent := &TaskCreatedEvent{
		EventType: "taskmanager_TaskCreated",
		TaskID:    "task1",
		Title:     "Test Task",
		Status:    StatusPending,
		Priority:  PriorityMedium,
	}
	agg.ApplyEvent(createEvent)

	// Update to In Progress
	updateEvent := &TaskUpdatedEvent{
		EventType: "taskmanager_TaskUpdated",
		TaskID:    "task1",
		Status:    StatusInProgress,
	}
	agg.ApplyEvent(updateEvent)

	// Emit delta for update
	signal := agg.EmitDelta(updateEvent)
	if signal == nil {
		t.Fatal("Expected non-nil delta for status update")
	}

	// Should have animation actions for move (at least 1 for card, expect 2 with label)
	if len(signal.Actions) < 1 {
		t.Errorf("Expected at least 1 animation action, got %d", len(signal.Actions))
	}

	// Check first action is animate for task card
	animAction := signal.Actions[0]
	if animAction.Type != "animate" || animAction.NodeID != "task1" {
		t.Errorf("Expected animate for 'task1', got %v", animAction)
	}
	if animAction.Animation.Property != "position" {
		t.Errorf("Expected position animation, got %s", animAction.Animation.Property)
	}

	// Target position should be different (e.g., x-offset for In Progress)
	targetPos, ok := animAction.Animation.To.([]float64)
	if !ok || len(targetPos) != 3 {
		t.Errorf("Expected 3D position in To, got %v", animAction.Animation.To)
	}
	x := targetPos[0]
	if x < 5.0 { // Assume In Progress at x>=5
		t.Errorf("Expected new x position >=5 for In Progress, got %f", x)
	}
}
