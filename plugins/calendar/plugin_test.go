package main

import (
	"testing"
)

func TestCalendarAggregate_ApplyEvent_EventCreated(t *testing.T) {
	agg := NewCalendarAggregate()

	event := &EventCreatedEvent{
		EventType:   "calendar_EventCreated",
		EventID:     "event1",
		Title:       "Test Event",
		Description: "A test event",
		Status:      StatusConfirmed,
		Importance:  ImportanceMedium,
		StartTime:   "2023-12-31T10:00:00Z",
		EndTime:     "2023-12-31T11:00:00Z",
		Location:    "Office",
		Attendees:   []string{"user1", "user2"},
		Tags:        []string{"meeting"},
	}

	err := agg.ApplyEvent(event)
	if err != nil {
		t.Fatalf("ApplyEvent failed: %v", err)
	}

	if len(agg.Events) != 1 {
		t.Errorf("Expected 1 event, got %d", len(agg.Events))
	}

	calEvent, exists := agg.Events["event1"]
	if !exists {
		t.Fatal("Event not found")
	}

	if calEvent.Title != "Test Event" {
		t.Errorf("Expected title 'Test Event', got '%s'", calEvent.Title)
	}

	if calEvent.Status != StatusConfirmed {
		t.Errorf("Expected status '%s', got '%s'", StatusConfirmed, calEvent.Status)
	}
}

func TestCalendarAggregate_ApplyEvent_EventUpdated(t *testing.T) {
	agg := NewCalendarAggregate()

	// First create an event
	createEvent := &EventCreatedEvent{
		EventType:  "calendar_EventCreated",
		EventID:    "event1",
		Title:      "Original Title",
		Status:     StatusTentative,
		Importance: ImportanceLow,
		StartTime:  "2023-12-31T10:00:00Z",
	}
	agg.ApplyEvent(createEvent)

	// Now update it
	updateEvent := &EventUpdatedEvent{
		EventType:   "calendar_EventUpdated",
		EventID:     "event1",
		Title:       "Updated Title",
		Status:      StatusConfirmed,
		Importance:  ImportanceHigh,
		Description: "Updated description",
	}

	err := agg.ApplyEvent(updateEvent)
	if err != nil {
		t.Fatalf("ApplyEvent failed: %v", err)
	}

	calEvent := agg.Events["event1"]
	if calEvent.Title != "Updated Title" {
		t.Errorf("Expected title 'Updated Title', got '%s'", calEvent.Title)
	}

	if calEvent.Status != StatusConfirmed {
		t.Errorf("Expected status '%s', got '%s'", StatusConfirmed, calEvent.Status)
	}

	if calEvent.Importance != ImportanceHigh {
		t.Errorf("Expected importance '%s', got '%s'", ImportanceHigh, calEvent.Importance)
	}
}

func TestCalendarAggregate_ApplyEvent_EventDeleted(t *testing.T) {
	agg := NewCalendarAggregate()

	// Create an event
	createEvent := &EventCreatedEvent{
		EventType: "calendar_EventCreated",
		EventID:   "event1",
		Title:     "Test Event",
		StartTime: "2023-12-31T10:00:00Z",
	}
	agg.ApplyEvent(createEvent)

	// Delete it
	deleteEvent := &EventDeletedEvent{
		EventType: "calendar_EventDeleted",
		EventID:   "event1",
	}

	err := agg.ApplyEvent(deleteEvent)
	if err != nil {
		t.Fatalf("ApplyEvent failed: %v", err)
	}

	if len(agg.Events) != 0 {
		t.Errorf("Expected 0 events after delete, got %d", len(agg.Events))
	}
}

func TestCalendarAggregate_GetFull3DState(t *testing.T) {
	agg := NewCalendarAggregate()

	// Create an event
	createEvent := &EventCreatedEvent{
		EventType: "calendar_EventCreated",
		EventID:   "event1",
		Title:     "Test Event",
		StartTime: "2023-12-31T10:00:00Z",
	}
	agg.ApplyEvent(createEvent)

	actions := agg.GetFull3DState()

	// Should have 1 hub + 2 for event (box and label)
	if len(actions) != 3 {
		t.Errorf("Expected 3 actions, got %d", len(actions))
	}

	// Check hub
	hubAction := actions[0]
	if hubAction.NodeID != "calendar_hub" {
		t.Errorf("Expected NodeID 'calendar_hub', got '%s'", hubAction.NodeID)
	}

	// Check event box
	boxAction := actions[1]
	if boxAction.NodeID != "calendar_event_event1" {
		t.Errorf("Expected NodeID 'calendar_event_event1', got '%s'", boxAction.NodeID)
	}

	// Check event label
	labelAction := actions[2]
	if labelAction.NodeID != "calendar_event_event1_label" {
		t.Errorf("Expected NodeID 'calendar_event_event1_label', got '%s'", labelAction.NodeID)
	}
}

func TestCalendarAggregate_Broadcast3DDelta_EventCreated(t *testing.T) {
	agg := NewCalendarAggregate()

	event := &EventCreatedEvent{
		EventType: "calendar_EventCreated",
		EventID:   "event1",
		Title:     "Test Event",
		StartTime: "2023-12-31T10:00:00Z",
	}

	// Apply the event first
	agg.ApplyEvent(event)

	actions := agg.Broadcast3DDelta(event)

	// Should have 2 actions: box and label
	if len(actions) != 2 {
		t.Errorf("Expected 2 actions, got %d", len(actions))
	}

	boxAction := actions[0]
	if boxAction.NodeID != "calendar_event_event1" {
		t.Errorf("Expected NodeID 'calendar_event_event1', got '%s'", boxAction.NodeID)
	}

	labelAction := actions[1]
	if labelAction.NodeID != "calendar_event_event1_label" {
		t.Errorf("Expected NodeID 'calendar_event_event1_label', got '%s'", labelAction.NodeID)
	}
}

func TestCalendarAggregate_Broadcast3DDelta_EventDeleted(t *testing.T) {
	agg := NewCalendarAggregate()

	event := &EventDeletedEvent{
		EventType: "calendar_EventDeleted",
		EventID:   "event1",
	}

	actions := agg.Broadcast3DDelta(event)

	// Should have 2 actions: delete box and delete label
	if len(actions) != 2 {
		t.Errorf("Expected 2 actions, got %d", len(actions))
	}

	if actions[0].Type != "delete" || actions[0].NodeID != "calendar_event_event1" {
		t.Errorf("Expected delete action for 'calendar_event_event1', got %v", actions[0])
	}

	if actions[1].Type != "delete" || actions[1].NodeID != "calendar_event_event1_label" {
		t.Errorf("Expected delete action for 'calendar_event_event1_label', got %v", actions[1])
	}
}
