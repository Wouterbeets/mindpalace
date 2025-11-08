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
	if len(actions) < 4 {
		t.Fatalf("Expected at least 4 actions, got %d", len(actions))
	}
	expectedNodes := map[string]bool{
		"task1":        false,
		"task1_paper":  false,
		"task1_accent": false,
		"task1_shadow": false,
		"task1_label":  false,
		"task1_meta":   false,
	}
	for _, act := range actions {
		if act.Type != "create" {
			continue
		}
		if _, ok := expectedNodes[act.NodeID]; ok {
			expectedNodes[act.NodeID] = true
		}
	}
	for node, seen := range expectedNodes {
		if !seen {
			t.Errorf("Expected create action for node %s", node)
		}
	}
}

func TestTaskAggregate_Broadcast3DDelta_TaskCompleted(t *testing.T) {
	agg := NewTaskAggregate()

	create := &TaskCreatedEvent{
		EventType: "taskmanager_TaskCreated",
		TaskID:    "task1",
		Title:     "Complete me",
		Status:    StatusPending,
	}
	if err := agg.ApplyEvent(create); err != nil {
		t.Fatalf("apply create failed: %v", err)
	}

	zones := map[string]ui3d.Zone{
		"taskmanager": {Angle: 0, Radius: 80, GridRows: 1, GridCols: 3, GridDepth: 10},
	}
	ui3d.SetGlobalZones(zones)

	event := &TaskCompletedEvent{
		EventType:   "taskmanager_TaskCompleted",
		TaskID:      "task1",
		CompletedAt: time.Now().UTC().Format(time.RFC3339),
	}
	if err := agg.ApplyEvent(event); err != nil {
		t.Fatalf("apply complete failed: %v", err)
	}

	signal := agg.EmitDelta(event)
	if signal == nil {
		t.Fatal("expected delta for completion")
	}
	actions := signal.Actions
	if len(actions) == 0 {
		t.Fatal("expected non-empty actions for completion")
	}

	expectedDeletes := map[string]bool{
		"task1":        false,
		"task1_paper":  false,
		"task1_accent": false,
		"task1_shadow": false,
		"task1_label":  false,
		"task1_meta":   false,
		"task1_model":  false,
	}
	expectedCreates := map[string]bool{
		"task1":       false,
		"task1_paper": false,
	}
	for _, act := range actions {
		switch act.Type {
		case "delete":
			if _, ok := expectedDeletes[act.NodeID]; ok {
				expectedDeletes[act.NodeID] = true
			}
		case "create":
			if _, ok := expectedCreates[act.NodeID]; ok {
				expectedCreates[act.NodeID] = true
			}
		}
	}
	for node, seen := range expectedDeletes {
		if !seen {
			t.Errorf("Expected delete action for '%s'", node)
		}
	}
	for node, seen := range expectedCreates {
		if !seen {
			t.Errorf("Expected create action for '%s'", node)
		}
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

	expectedDeletes := map[string]bool{
		"task1":        false,
		"task1_paper":  false,
		"task1_accent": false,
		"task1_shadow": false,
		"task1_label":  false,
		"task1_meta":   false,
		"task1_model":  false,
	}
	for _, act := range signal.Actions {
		if act.Type != "delete" {
			continue
		}
		if _, ok := expectedDeletes[act.NodeID]; ok {
			expectedDeletes[act.NodeID] = true
		}
	}
	for node, seen := range expectedDeletes {
		if !seen {
			t.Errorf("Expected delete action for '%s'", node)
		}
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
		"delete:task_a", "delete:task_a_paper", "delete:task_a_accent", "delete:task_a_shadow", "delete:task_a_label", "delete:task_a_meta", "delete:task_a_model",
		"create:task_a", "create:task_a_paper", "create:task_a_accent", "create:task_a_shadow", "create:task_a_label", "create:task_a_meta",
		"delete:task_b", "delete:task_b_paper", "delete:task_b_accent", "delete:task_b_shadow", "delete:task_b_label", "delete:task_b_meta", "delete:task_b_model",
		"create:task_b", "create:task_b_paper", "create:task_b_accent", "create:task_b_shadow", "create:task_b_label", "create:task_b_meta",
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

	if len(signal.Actions) < 4 {
		t.Fatalf("Expected at least 4 actions, got %d", len(signal.Actions))
	}
	expectedNodes := map[string]bool{
		"task1":        false,
		"task1_paper":  false,
		"task1_accent": false,
		"task1_shadow": false,
		"task1_label":  false,
		"task1_meta":   false,
	}
	for _, act := range signal.Actions {
		if act.Type != "create" {
			continue
		}
		if _, ok := expectedNodes[act.NodeID]; ok {
			expectedNodes[act.NodeID] = true
		}
	}
	for node, seen := range expectedNodes {
		if !seen {
			t.Errorf("Expected create action for '%s'", node)
		}
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

	foundDelete := false
	foundCreate := false
	var newPos []float64

	for _, act := range signal.Actions {
		if act.NodeID != "task1" {
			continue
		}
		switch act.Type {
		case "delete":
			foundDelete = true
		case "create":
			foundCreate = true
			if pos, ok := act.Properties["position"].([]float64); ok {
				newPos = pos
			}
		}
	}

	if !foundDelete {
		t.Error("Expected delete action for task1")
	}
	if !foundCreate {
		t.Error("Expected create action for task1 after status change")
	}
	if len(newPos) != 3 {
		t.Fatalf("Expected position vector for recreated task, got %v", newPos)
	}
	if newPos[0] == 0 {
		t.Errorf("Expected task to be repositioned along X for new status, got %f", newPos[0])
	}
}
