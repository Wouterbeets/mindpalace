package main

import (
	"testing"

	"mindpalace/pkg/eventsourcing"
)

func TestNewPlugin(t *testing.T) {
	plugin := NewPlugin()
	if plugin == nil {
		t.Fatal("NewPlugin returned nil")
	}
	if plugin.Name() != "notes" {
		t.Errorf("Expected name 'notes', got %s", plugin.Name())
	}
	if plugin.Type() != eventsourcing.LLMPlugin {
		t.Errorf("Expected type LLMPlugin, got %v", plugin.Type())
	}
}

func TestNotesAggregate_ID(t *testing.T) {
	agg := NewNotesAggregate()
	if agg.ID() != "notes" {
		t.Errorf("Expected ID 'notes', got %s", agg.ID())
	}
}

func TestNotesAggregate_ApplyEvent_NoteCreated(t *testing.T) {
	agg := NewNotesAggregate()
	event := &NoteCreatedEvent{
		NoteID:  "test_note",
		Title:   "Test Title",
		Content: "Test Content",
		Tags:    []string{"tag1"},
	}

	err := agg.ApplyEvent(event)
	if err != nil {
		t.Fatalf("ApplyEvent failed: %v", err)
	}

	if len(agg.Notes) != 1 {
		t.Errorf("Expected 1 note, got %d", len(agg.Notes))
	}
	note := agg.Notes["test_note"]
	if note.Title != "Test Title" {
		t.Errorf("Title mismatch")
	}
	if note.CreatedAt.IsZero() {
		t.Error("CreatedAt not set")
	}
}

func TestNotesAggregate_ApplyEvent_NoteUpdated(t *testing.T) {
	agg := NewNotesAggregate()
	// First create
	createEvent := &NoteCreatedEvent{
		NoteID:  "test_note",
		Title:   "Old Title",
		Content: "Old Content",
	}
	agg.ApplyEvent(createEvent)

	// Then update
	updateEvent := &NoteUpdatedEvent{
		NoteID:  "test_note",
		Title:   "New Title",
		Content: "New Content",
	}
	err := agg.ApplyEvent(updateEvent)
	if err != nil {
		t.Fatalf("ApplyEvent failed: %v", err)
	}

	note := agg.Notes["test_note"]
	if note.Title != "New Title" {
		t.Errorf("Title not updated")
	}
	if note.UpdatedAt.IsZero() {
		t.Error("UpdatedAt not set")
	}
}

func TestNotesAggregate_ApplyEvent_NoteDeleted(t *testing.T) {
	agg := NewNotesAggregate()
	// Create
	createEvent := &NoteCreatedEvent{NoteID: "test_note"}
	agg.ApplyEvent(createEvent)

	// Delete
	deleteEvent := &NoteDeletedEvent{NoteID: "test_note"}
	err := agg.ApplyEvent(deleteEvent)
	if err != nil {
		t.Fatalf("ApplyEvent failed: %v", err)
	}

	if len(agg.Notes) != 0 {
		t.Errorf("Note not deleted")
	}
}

func TestCreateNoteHandler(t *testing.T) {
	plugin := NewPlugin().(*NotesPlugin)
	input := &CreateNoteInput{
		Title:   "Test Note",
		Content: "Test Content",
		Tags:    []string{"test"},
	}

	events, err := plugin.createNoteHandler(input)
	if err != nil {
		t.Fatalf("createNoteHandler failed: %v", err)
	}
	if len(events) != 1 {
		t.Errorf("Expected 1 event, got %d", len(events))
	}
	if e, ok := events[0].(*NoteCreatedEvent); ok {
		if e.Title != "Test Note" {
			t.Errorf("Title mismatch")
		}
	} else {
		t.Errorf("Wrong event type")
	}
}

func TestCreateNoteHandler_Validation(t *testing.T) {
	plugin := NewPlugin().(*NotesPlugin)

	// Missing title
	input := &CreateNoteInput{Content: "Content"}
	_, err := plugin.createNoteHandler(input)
	if err == nil {
		t.Error("Expected error for missing title")
	}

	// Missing content
	input = &CreateNoteInput{Title: "Title"}
	_, err = plugin.createNoteHandler(input)
	if err == nil {
		t.Error("Expected error for missing content")
	}
}

func TestUpdateNoteHandler(t *testing.T) {
	plugin := NewPlugin().(*NotesPlugin)
	// Create note first
	createInput := &CreateNoteInput{Title: "Old", Content: "Old"}
	createEvents, _ := plugin.createNoteHandler(createInput)
	noteID := createEvents[0].(*NoteCreatedEvent).NoteID
	plugin.aggregate.ApplyEvent(createEvents[0])

	input := &UpdateNoteInput{
		NoteID:  noteID,
		Title:   "New Title",
		Content: "New Content",
	}

	events, err := plugin.updateNoteHandler(input)
	if err != nil {
		t.Fatalf("updateNoteHandler failed: %v", err)
	}
	if len(events) != 1 {
		t.Errorf("Expected 1 event, got %d", len(events))
	}
}

func TestDeleteNoteHandler(t *testing.T) {
	plugin := NewPlugin().(*NotesPlugin)
	// Create
	createInput := &CreateNoteInput{Title: "Test", Content: "Test"}
	createEvents, _ := plugin.createNoteHandler(createInput)
	noteID := createEvents[0].(*NoteCreatedEvent).NoteID
	plugin.aggregate.ApplyEvent(createEvents[0])

	input := &DeleteNoteInput{NoteID: noteID}
	events, err := plugin.deleteNoteHandler(input)
	if err != nil {
		t.Fatalf("deleteNoteHandler failed: %v", err)
	}
	if len(events) != 1 {
		t.Errorf("Expected 1 event, got %d", len(events))
	}
}

func TestListNotesHandler(t *testing.T) {
	plugin := NewPlugin().(*NotesPlugin)
	// Create some notes
	e1, _ := plugin.createNoteHandler(&CreateNoteInput{Title: "Note1", Content: "Content1", Tags: []string{"tag1"}})
	e2, _ := plugin.createNoteHandler(&CreateNoteInput{Title: "Note2", Content: "Content2", Tags: []string{"tag2"}})
	plugin.aggregate.ApplyEvent(e1[0])
	plugin.aggregate.ApplyEvent(e2[0])

	input := &ListNotesInput{}
	events, err := plugin.listNotesHandler(input)
	if err != nil {
		t.Fatalf("listNotesHandler failed: %v", err)
	}
	if len(events) != 1 {
		t.Errorf("Expected 1 event, got %d", len(events))
	}
	if e, ok := events[0].(*NotesListedEvent); ok {
		if len(e.Notes) != 2 {
			t.Errorf("Expected 2 notes, got %d", len(e.Notes))
		}
	} else {
		t.Errorf("Wrong event type")
	}
}

func TestGenerateNoteID(t *testing.T) {
	id1 := generateNoteID()
	id2 := generateNoteID()
	if id1 == id2 {
		t.Error("Generated IDs are not unique")
	}
	if len(id1) == 0 {
		t.Error("Generated ID is empty")
	}
}

func TestContains(t *testing.T) {
	if !contains([]string{"a", "b"}, "a") {
		t.Error("contains failed for existing element")
	}
	if contains([]string{"a", "b"}, "c") {
		t.Error("contains failed for non-existing element")
	}
}
