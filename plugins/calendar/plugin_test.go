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

func TestCalendarAggregate_GetCurrent3DState(t *testing.T) {
	agg := NewCalendarAggregate()

	// Create an event
	createEvent := &EventCreatedEvent{
		EventType: "calendar_EventCreated",
		EventID:   "event1",
		Title:     "Test Event",
		StartTime: "2023-12-31T10:00:00Z",
	}
	agg.ApplyEvent(createEvent)

	signal := agg.EmitDelta(createEvent)
	if signal == nil {
		t.Fatal("Expected non-nil delta envelope")
	}
	actions := signal.Actions
	created := map[string]bool{}
	for _, action := range actions {
		if action.Type == "create" {
			created[action.NodeID] = true
		}
	}
	if !created["calendar_month_label"] {
		t.Errorf("Expected calendar_month_label to be created")
	}
	if !created["calendar_day_number_01"] {
		t.Errorf("Expected day label for day 1 to be created")
	}
	if !created["calendar_event_label_event1"] {
		t.Errorf("Expected event label for event1 to be created")
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

	signal := agg.EmitDelta(event)
	actions := signal.Actions
	created := map[string]bool{}
	for _, action := range actions {
		if action.Type == "create" {
			created[action.NodeID] = true
		}
	}
	if !created["calendar_event_label_event1"] {
		t.Errorf("Expected calendar_event_label_event1 to be created")
	}
}

func TestCalendarAggregate_Broadcast3DDelta_EventDeleted(t *testing.T) {
	agg := NewCalendarAggregate()

	create := &EventCreatedEvent{
		EventType: "calendar_EventCreated",
		EventID:   "event1",
		Title:     "Test Event",
		StartTime: "2023-12-31T10:00:00Z",
	}
	agg.ApplyEvent(create)
	agg.EmitDelta(create)

	event := &EventDeletedEvent{
		EventType: "calendar_EventDeleted",
		EventID:   "event1",
	}
	agg.ApplyEvent(event)

	signal := agg.EmitDelta(event)
	actions := signal.Actions
	created := map[string]bool{}
	for _, action := range actions {
		if action.Type == "create" {
			created[action.NodeID] = true
		}
	}
	if created["calendar_event_label_event1"] {
		t.Errorf("Did not expect event label for event1 after deletion")
	}
}
