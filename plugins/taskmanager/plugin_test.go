package main

import (
	"testing"
	"time"
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

func TestTaskAggregate_GetFull3DState(t *testing.T) {
	agg := NewTaskAggregate()

	// Create a task
	createEvent := &TaskCreatedEvent{
		EventType: "taskmanager_TaskCreated",
		TaskID:    "task1",
		Title:     "Test Task",
		Priority:  PriorityHigh,
	}
	agg.ApplyEvent(createEvent)

	actions := agg.GetFull3DState()

	// Should have 2 actions: box and label
	if len(actions) != 2 {
		t.Errorf("Expected 2 actions, got %d", len(actions))
	}

	// Check box action
	boxAction := actions[0]
	if boxAction.NodeID != "task1" {
		t.Errorf("Expected NodeID 'task1', got '%s'", boxAction.NodeID)
	}

	if boxAction.NodeType != "MeshInstance3D" {
		t.Errorf("Expected NodeType 'MeshInstance3D', got '%s'", boxAction.NodeType)
	}

	// Check label action
	labelAction := actions[1]
	if labelAction.NodeID != "task1_label" {
		t.Errorf("Expected NodeID 'task1_label', got '%s'", labelAction.NodeID)
	}

	if labelAction.NodeType != "Label3D" {
		t.Errorf("Expected NodeType 'Label3D', got '%s'", labelAction.NodeType)
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

	actions := agg.Broadcast3DDelta(event)

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

	actions := agg.Broadcast3DDelta(event)

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
