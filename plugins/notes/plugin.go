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
	"fyne.io/fyne/v2/widget"
)

// Note represents a single note's state
type Note struct {
	NoteID    string    `json:"note_id"`
	Title     string    `json:"title"`
	Content   string    `json:"content"`
	Tags      []string  `json:"tags,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// NotesAggregate manages the state of notes with thread safety
type NotesAggregate struct {
	Notes    map[string]*Note
	commands map[string]eventsourcing.CommandHandler
	Mu       sync.RWMutex
}

// NewNotesAggregate creates a new thread-safe NotesAggregate
func NewNotesAggregate() *NotesAggregate {
	return &NotesAggregate{
		Notes:    make(map[string]*Note),
		commands: make(map[string]eventsourcing.CommandHandler),
	}
}

// ID returns the aggregate's identifier
func (a *NotesAggregate) ID() string {
	return "notes"
}

// ApplyEvent updates the aggregate state based on note-related events
func (a *NotesAggregate) ApplyEvent(event eventsourcing.Event) error {
	a.Mu.Lock()
	defer a.Mu.Unlock()

	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal event %s: %v", event.Type(), err)
	}

	switch event.Type() {
	case "notes_NoteCreated":
		var e NoteCreatedEvent
		if err := json.Unmarshal(data, &e); err != nil {
			return fmt.Errorf("failed to unmarshal NoteCreated: %v", err)
		}
		a.Notes[e.NoteID] = &Note{
			NoteID:    e.NoteID,
			Title:     e.Title,
			Content:   e.Content,
			Tags:      e.Tags,
			CreatedAt: time.Now().UTC(),
			UpdatedAt: time.Now().UTC(),
		}

	case "notes_NoteUpdated":
		var e NoteUpdatedEvent
		if err := json.Unmarshal(data, &e); err != nil {
			return fmt.Errorf("failed to unmarshal NoteUpdated: %v", err)
		}
		if note, exists := a.Notes[e.NoteID]; exists {
			if e.Title != "" {
				note.Title = e.Title
			}
			if e.Content != "" {
				note.Content = e.Content
			}
			if e.Tags != nil {
				note.Tags = e.Tags
			}
			note.UpdatedAt = time.Now().UTC()
		}

	case "notes_NoteDeleted":
		var e NoteDeletedEvent
		if err := json.Unmarshal(data, &e); err != nil {
			return fmt.Errorf("failed to unmarshal NoteDeleted: %v", err)
		}
		delete(a.Notes, e.NoteID)

	default:
		return nil
	}
	return nil
}

// NotesPlugin implements the plugin interface
type NotesPlugin struct {
	aggregate *NotesAggregate
}

func NewPlugin() eventsourcing.Plugin {
	agg := NewNotesAggregate()
	p := &NotesPlugin{aggregate: agg}
	agg.commands = map[string]eventsourcing.CommandHandler{
		"CreateNote": eventsourcing.NewCommand(func(input *CreateNoteInput) ([]eventsourcing.Event, error) {
			return p.createNoteHandler(input)
		}),
		"UpdateNote": eventsourcing.NewCommand(func(input *UpdateNoteInput) ([]eventsourcing.Event, error) {
			return p.updateNoteHandler(input)
		}),
		"DeleteNote": eventsourcing.NewCommand(func(input *DeleteNoteInput) ([]eventsourcing.Event, error) {
			return p.deleteNoteHandler(input)
		}),
		"ListNotes": eventsourcing.NewCommand(func(input *ListNotesInput) ([]eventsourcing.Event, error) {
			return p.listNotesHandler(input)
		}),
	}
	eventsourcing.RegisterEvent("notes_NoteCreated", func() eventsourcing.Event { return &NoteCreatedEvent{} })
	eventsourcing.RegisterEvent("notes_NoteUpdated", func() eventsourcing.Event { return &NoteUpdatedEvent{} })
	eventsourcing.RegisterEvent("notes_NotesListed", func() eventsourcing.Event { return &NotesListedEvent{} })
	eventsourcing.RegisterEvent("notes_NoteDeleted", func() eventsourcing.Event { return &NoteDeletedEvent{} })
	return p
}

// Commands returns the command handlers
func (p *NotesPlugin) Commands() map[string]eventsourcing.CommandHandler {
	return p.aggregate.commands
}

// Name returns the plugin name
func (p *NotesPlugin) Name() string {
	return "notes"
}

// Schemas defines the command schemas
func (p *NotesPlugin) Schemas() map[string]eventsourcing.CommandInput {
	return map[string]eventsourcing.CommandInput{
		"CreateNote": &CreateNoteInput{},
		"UpdateNote": &UpdateNoteInput{},
		"DeleteNote": &DeleteNoteInput{},
		"ListNotes":  &ListNotesInput{},
	}
}

// Command Input Structs with Schema Generation

func (i *CreateNoteInput) New() any {
	return &CreateNoteInput{}
}

// CreateNoteInput defines the input for creating a note
type CreateNoteInput struct {
	Title   string   `json:"Title"`
	Content string   `json:"Content"`
	Tags    []string `json:"Tags,omitempty"`
}

func (c *CreateNoteInput) Schema() map[string]interface{} {
	return map[string]interface{}{
		"description": "Creates a new note",
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"Title": map[string]interface{}{
					"type":        "string",
					"description": "The title of the note",
				},
				"Content": map[string]interface{}{
					"type":        "string",
					"description": "The content of the note",
				},
				"Tags": map[string]interface{}{
					"type":        "array",
					"description": "Tags for categorizing the note",
					"items":       map[string]interface{}{"type": "string"},
				},
			},
			"required": []string{"Title", "Content"},
		},
	}
}

func (i *UpdateNoteInput) New() any {
	return &UpdateNoteInput{}
}

// UpdateNoteInput defines the input for updating a note
type UpdateNoteInput struct {
	NoteID  string   `json:"NoteID"`
	Title   string   `json:"Title,omitempty"`
	Content string   `json:"Content,omitempty"`
	Tags    []string `json:"Tags,omitempty"`
}

func (u *UpdateNoteInput) Schema() map[string]interface{} {
	return map[string]interface{}{
		"description": "Updates an existing note",
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"NoteID": map[string]interface{}{
					"type":        "string",
					"description": "ID of the note to update",
				},
				"Title": map[string]interface{}{
					"type":        "string",
					"description": "The title of the note",
				},
				"Content": map[string]interface{}{
					"type":        "string",
					"description": "The content of the note",
				},
				"Tags": map[string]interface{}{
					"type":        "array",
					"description": "Tags for categorizing the note",
					"items":       map[string]interface{}{"type": "string"},
				},
			},
			"required": []string{"NoteID"},
		},
	}
}

func (i *DeleteNoteInput) New() any {
	return &DeleteNoteInput{}
}

// DeleteNoteInput defines the input for deleting a note
type DeleteNoteInput struct {
	NoteID string `json:"NoteID"`
}

func (d *DeleteNoteInput) Schema() map[string]interface{} {
	return map[string]interface{}{
		"description": "Deletes a note",
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"NoteID": map[string]interface{}{
					"type":        "string",
					"description": "ID of the note to delete",
				},
			},
			"required": []string{"NoteID"},
		},
	}
}

func (i *ListNotesInput) New() any {
	return &ListNotesInput{}
}

// ListNotesInput defines the input for listing notes
type ListNotesInput struct {
	Tag string `json:"Tag,omitempty"`
}

func (l *ListNotesInput) Schema() map[string]interface{} {
	return map[string]interface{}{
		"description": "Lists notes with optional filtering",
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"Tag": map[string]interface{}{
					"type":        "string",
					"description": "Filter by tag",
				},
			},
		},
	}
}

// Event Types
type NotesListedEvent struct {
	EventType string  `json:"event_type"`
	Notes     []*Note `json:"listed_notes"`
}

func (e *NotesListedEvent) Type() string { return "notes_NotesListed" }
func (e *NotesListedEvent) Marshal() ([]byte, error) {
	e.EventType = e.Type()
	return json.Marshal(e)
}
func (e *NotesListedEvent) Unmarshal(data []byte) error { return json.Unmarshal(data, e) }

type NoteCreatedEvent struct {
	EventType string   `json:"event_type"`
	NoteID    string   `json:"note_id"`
	Title     string   `json:"title"`
	Content   string   `json:"content"`
	Tags      []string `json:"tags,omitempty"`
}

func (e *NoteCreatedEvent) Type() string { return "notes_NoteCreated" }
func (e *NoteCreatedEvent) Marshal() ([]byte, error) {
	e.EventType = e.Type()
	return json.Marshal(e)
}
func (e *NoteCreatedEvent) Unmarshal(data []byte) error { return json.Unmarshal(data, e) }

type NoteUpdatedEvent struct {
	EventType string   `json:"event_type"`
	NoteID    string   `json:"note_id"`
	Title     string   `json:"title,omitempty"`
	Content   string   `json:"content,omitempty"`
	Tags      []string `json:"tags,omitempty"`
}

func (e *NoteUpdatedEvent) Type() string { return "notes_NoteUpdated" }
func (e *NoteUpdatedEvent) Marshal() ([]byte, error) {
	e.EventType = e.Type()
	return json.Marshal(e)
}
func (e *NoteUpdatedEvent) Unmarshal(data []byte) error { return json.Unmarshal(data, e) }

type NoteDeletedEvent struct {
	EventType string `json:"event_type"`
	NoteID    string `json:"note_id"`
}

func (e *NoteDeletedEvent) Type() string { return "notes_NoteDeleted" }
func (e *NoteDeletedEvent) Marshal() ([]byte, error) {
	e.EventType = e.Type()
	return json.Marshal(e)
}
func (e *NoteDeletedEvent) Unmarshal(data []byte) error { return json.Unmarshal(data, e) }

// Utility functions
func generateNoteID() string {
	return fmt.Sprintf("note_%d", time.Now().UnixNano())
}

// Command Handlers
func (p *NotesPlugin) createNoteHandler(input *CreateNoteInput) ([]eventsourcing.Event, error) {
	if input.Title == "" {
		return nil, fmt.Errorf("title is required and must be a non-empty string")
	}
	if input.Content == "" {
		return nil, fmt.Errorf("content is required and must be a non-empty string")
	}

	event := &NoteCreatedEvent{
		EventType: "notes_NoteCreated",
		NoteID:    generateNoteID(),
		Title:     input.Title,
		Content:   input.Content,
		Tags:      input.Tags,
	}
	return []eventsourcing.Event{event}, nil
}

func (p *NotesPlugin) updateNoteHandler(input *UpdateNoteInput) ([]eventsourcing.Event, error) {
	if input.NoteID == "" {
		return nil, fmt.Errorf("noteID is required and must be a non-empty string")
	}

	p.aggregate.Mu.RLock()
	_, exists := p.aggregate.Notes[input.NoteID]
	p.aggregate.Mu.RUnlock()
	if !exists {
		return nil, fmt.Errorf("note %s not found", input.NoteID)
	}

	event := &NoteUpdatedEvent{
		EventType: "notes_NoteUpdated",
		NoteID:    input.NoteID,
		Title:     input.Title,
		Content:   input.Content,
		Tags:      input.Tags,
	}
	return []eventsourcing.Event{event}, nil
}

func (p *NotesPlugin) deleteNoteHandler(input *DeleteNoteInput) ([]eventsourcing.Event, error) {
	if input.NoteID == "" {
		return nil, fmt.Errorf("noteID is required and must be a non-empty string")
	}

	p.aggregate.Mu.RLock()
	_, exists := p.aggregate.Notes[input.NoteID]
	p.aggregate.Mu.RUnlock()
	if !exists {
		return nil, fmt.Errorf("note %s not found", input.NoteID)
	}

	event := &NoteDeletedEvent{EventType: "notes_NoteDeleted", NoteID: input.NoteID}
	return []eventsourcing.Event{event}, nil
}

func (p *NotesPlugin) listNotesHandler(input *ListNotesInput) ([]eventsourcing.Event, error) {
	p.aggregate.Mu.RLock()
	defer p.aggregate.Mu.RUnlock()

	notes := make([]*Note, 0, len(p.aggregate.Notes))
	for _, note := range p.aggregate.Notes {
		notes = append(notes, note)
	}

	// Apply filters
	var tagFilter string
	if input.Tag != "" {
		tagFilter = input.Tag
	}

	filteredNotes := notes[:0]
	for _, note := range notes {
		if tagFilter != "" && !contains(note.Tags, tagFilter) {
			continue
		}
		filteredNotes = append(filteredNotes, note)
	}

	// Sort notes by creation time
	sort.Slice(filteredNotes, func(i, j int) bool {
		return filteredNotes[i].CreatedAt.Before(filteredNotes[j].CreatedAt)
	})

	event := &NotesListedEvent{EventType: "notes_NotesListed", Notes: filteredNotes}
	return []eventsourcing.Event{event}, nil
}

// GetCustomUI returns a list view for the notes
func (na *NotesAggregate) GetCustomUI() fyne.CanvasObject {
	na.Mu.RLock()
	notes := make([]*Note, 0, len(na.Notes))
	for _, note := range na.Notes {
		notes = append(notes, note)
	}
	na.Mu.RUnlock()

	if len(notes) == 0 {
		return container.NewCenter(widget.NewLabel("No notes available. Create one to get started!"))
	}

	// Sort notes by creation time
	sort.Slice(notes, func(i, j int) bool {
		return notes[i].CreatedAt.Before(notes[j].CreatedAt)
	})

	content := container.NewVBox()
	for _, note := range notes {
		card := createNoteCard(note)
		content.Add(card)
		content.Add(widget.NewSeparator())
	}

	return container.NewVScroll(content)
}

// createNoteCard creates a compact card UI for a single note
func createNoteCard(note *Note) fyne.CanvasObject {
	// Title
	title := widget.NewLabel(note.Title)
	title.TextStyle = fyne.TextStyle{Bold: true}
	title.Wrapping = fyne.TextWrapOff

	// Compact content preview
	content := strings.TrimSpace(note.Content)
	if len(content) > 100 {
		content = content[:97] + "..."
	}
	contentLabel := widget.NewLabel(content)
	contentLabel.Wrapping = fyne.TextWrapWord

	// Tags
	var tagsText string
	if len(note.Tags) > 0 {
		tagsText = fmt.Sprintf("Tags: %s", strings.Join(note.Tags, ", "))
	}
	tagsLabel := widget.NewLabel(tagsText)
	tagsLabel.TextStyle = fyne.TextStyle{Italic: true}

	// Card layout
	card := container.NewVBox(
		title,
		widget.NewSeparator(),
		contentLabel,
		tagsLabel,
	)

	// Style the card with padding
	return container.NewPadded(card)
}

// Additional Plugin Methods
func (p *NotesPlugin) Aggregate() eventsourcing.Aggregate {
	return p.aggregate
}

func (p *NotesPlugin) Type() eventsourcing.PluginType {
	return eventsourcing.LLMPlugin
}

func (p *NotesPlugin) SystemPrompt() string {
	// Acquire read lock to safely access notes
	p.aggregate.Mu.RLock()
	defer p.aggregate.Mu.RUnlock()

	// Collect notes into a slice for sorting
	notes := make([]*Note, 0, len(p.aggregate.Notes))
	for _, note := range p.aggregate.Notes {
		notes = append(notes, note)
	}

	// Sort notes by creation time for consistent ordering
	sort.Slice(notes, func(i, j int) bool {
		return notes[i].CreatedAt.Before(notes[j].CreatedAt)
	})

	// Build the note list string
	var noteList strings.Builder
	if len(notes) == 0 {
		noteList.WriteString("There are currently no notes.\n")
	} else {
		noteList.WriteString("Current notes:\n")
		for _, note := range notes {
			noteList.WriteString(fmt.Sprintf("- Note ID: %s, Title: \"%s\"\n", note.NoteID, note.Title))
		}
	}

	// Construct the full dynamic prompt
	prompt := `You are NotesMaster, a specialized AI for managing notes in MindPalace.

The user input will be a JSON object containing the arguments for the command to execute. Parse the JSON and call the appropriate command with the parsed values.

Your job is to interpret user requests about notes and execute the right commands (CreateNote, UpdateNote, DeleteNote, ListNotes) based on the current note state.

` + noteList.String() + `

Be concise, accurate, and always use the tools provided to manage notes. Focus on:

1. Creating detailed notes with proper titles and content
2. Updating notes with relevant information
3. Deleting notes when requested
4. Listing and filtering notes as requested

When interpreting user requests, pay close attention to the intent:
- If the user asks to "remove," "delete," or "erase" a note, use the DeleteNote command.
- If the user asks to "create" or "add" a note, use the CreateNote command.
- If the user asks to "update" or "modify" a note, use the UpdateNote command.
- If the user asks to "list" or "show" notes, use the ListNotes command.

When creating or updating notes, extract key information from user requests including:
- Note title and content
- Tags for organization

Format your responses in a structured way and confirm actions performed.`

	return prompt
}

// AgentModel specifies the LLM model to use for this plugin's agent
func (p *NotesPlugin) AgentModel() string {
	return "gpt-oss:20b" // Using the general-purpose model for notes management
}

func (p *NotesPlugin) EventHandlers() map[string]eventsourcing.EventHandler {
	return nil
}

func (a *NotesAggregate) Broadcast3DDelta(event eventsourcing.Event) eventsourcing.Signal {
	a.Mu.RLock()
	defer a.Mu.RUnlock()
	switch e := event.(type) {
	case *NoteCreatedEvent:
		return eventsourcing.Signal{Actions: []eventsourcing.DeltaAction{{
			Type:     "create",
			NodeID:   e.NoteID,
			NodeType: "MeshInstance3D",
			Properties: map[string]interface{}{
				"mesh":       "box",
				"position":   []float64{-20, 0, 0}, // Note zone position
				"event_type": "note_created",
				"material_override": map[string]interface{}{
					"albedo_color": []float64{0.8, 0.8, 0.8, 1.0}}, // Light gray for notes
			},
			Metadata: map[string]interface{}{
				"title": e.Title,
			},
		}, {
			Type:     "create",
			NodeID:   e.NoteID + "_label",
			NodeType: "Label3D",
			Properties: map[string]interface{}{
				"text":       e.Title,
				"position":   []interface{}{0, 1, 0}, // Relative
				"event_type": "note_created",
			},
		}}}
	case *NoteDeletedEvent:
		return eventsourcing.Signal{Actions: []eventsourcing.DeltaAction{{
			Type:   "delete",
			NodeID: e.NoteID,
		}, {
			Type:   "delete",
			NodeID: e.NoteID + "_label",
		}}}
	}
	return eventsourcing.Signal{}
}

func (a *NotesAggregate) GetCurrent3DState() eventsourcing.Signal {
	a.Mu.RLock()
	defer a.Mu.RUnlock()
	theme := ui3d.DefaultTheme()
	actions := make([]eventsourcing.DeltaAction, 0)
	stateSummary := make(map[string]interface{})
	noteSummaries := []map[string]interface{}{}
	i := 0
	for _, note := range a.Notes {
		pos := ui3d.PositionInCircle(i, 6.0+float64(i)*0.5, 2.0)
		// Offset by note zone position
		pos[0] += -20
		pos[1] += 0
		pos[2] += 0
		cards := ui3d.CreateCard(note.NoteID, note.Title, pos, theme)
		for j := range cards {
			if cards[j].Properties == nil {
				cards[j].Properties = make(map[string]interface{})
			}
			cards[j].Properties["event_type"] = "note"
		}
		actions = append(actions, cards...)
		noteSummaries = append(noteSummaries, map[string]interface{}{
			"id":    note.NoteID,
			"title": note.Title,
		})
		i++
	}
	stateSummary["notes"] = noteSummaries
	return eventsourcing.Signal{
		Actions:      actions,
		StateSummary: stateSummary,
	}
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

func randomPos() []float64 {
	return []float64{0, 0, 0} // placeholder
}
