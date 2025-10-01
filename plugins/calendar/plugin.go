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

// Constants for event properties
const (
	StatusConfirmed    = "Confirmed"
	StatusTentative    = "Tentative"
	StatusCancelled    = "Cancelled"
	ImportanceLow      = "Low"
	ImportanceMedium   = "Medium"
	ImportanceHigh     = "High"
	ImportanceCritical = "Critical"
)

// CalendarEvent represents a single calendar event's state
type CalendarEvent struct {
	EventID     string    `json:"event_id"`
	Title       string    `json:"title"`
	Description string    `json:"description,omitempty"`
	Status      string    `json:"status"`
	Importance  string    `json:"importance"`
	StartTime   time.Time `json:"start_time"`
	EndTime     time.Time `json:"end_time,omitempty"`
	Location    string    `json:"location,omitempty"`
	Attendees   []string  `json:"attendees,omitempty"`
	Tags        []string  `json:"tags,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

// CalendarAggregate manages the state of calendar events with thread safety
type CalendarAggregate struct {
	Events   map[string]*CalendarEvent
	commands map[string]eventsourcing.CommandHandler
	Mu       sync.RWMutex
}

// NewCalendarAggregate creates a new thread-safe CalendarAggregate
func NewCalendarAggregate() *CalendarAggregate {
	return &CalendarAggregate{
		Events:   make(map[string]*CalendarEvent),
		commands: make(map[string]eventsourcing.CommandHandler),
	}
}

// ID returns the aggregate's identifier
func (a *CalendarAggregate) ID() string {
	return "calendar"
}

// ApplyEvent updates the aggregate state based on event-related events
func (a *CalendarAggregate) ApplyEvent(event eventsourcing.Event) error {
	a.Mu.Lock()
	defer a.Mu.Unlock()

	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal event %s: %v", event.Type(), err)
	}

	switch event.Type() {
	case "calendar_EventCreated":
		var e EventCreatedEvent
		if err := json.Unmarshal(data, &e); err != nil {
			return fmt.Errorf("failed to unmarshal EventCreated: %v", err)
		}
		a.Events[e.EventID] = &CalendarEvent{
			EventID:     e.EventID,
			Title:       e.Title,
			Description: e.Description,
			Status:      e.Status,
			Importance:  e.Importance,
			StartTime:   parseTime(e.StartTime),
			EndTime:     parseTime(e.EndTime),
			Location:    e.Location,
			Attendees:   e.Attendees,
			Tags:        e.Tags,
			CreatedAt:   time.Now().UTC(),
		}

	case "calendar_EventUpdated":
		var e EventUpdatedEvent
		if err := json.Unmarshal(data, &e); err != nil {
			return fmt.Errorf("failed to unmarshal EventUpdated: %v", err)
		}
		if event, exists := a.Events[e.EventID]; exists {
			if e.Title != "" {
				event.Title = e.Title
			}
			if e.Description != "" {
				event.Description = e.Description
			}
			if e.Status != "" {
				event.Status = e.Status
			}
			if e.Importance != "" {
				event.Importance = e.Importance
			}
			if e.StartTime != "" {
				event.StartTime = parseTime(e.StartTime)
			}
			if e.EndTime != "" {
				event.EndTime = parseTime(e.EndTime)
			}
			if e.Location != "" {
				event.Location = e.Location
			}
			if e.Attendees != nil {
				event.Attendees = e.Attendees
			}
			if e.Tags != nil {
				event.Tags = e.Tags
			}
		}

	case "calendar_EventDeleted":
		var e EventDeletedEvent
		if err := json.Unmarshal(data, &e); err != nil {
			return fmt.Errorf("failed to unmarshal EventDeleted: %v", err)
		}
		delete(a.Events, e.EventID)

	default:
		return nil
	}
	return nil
}

// CalendarPlugin implements the plugin interface
type CalendarPlugin struct {
	aggregate *CalendarAggregate
}

func NewPlugin() eventsourcing.Plugin {
	agg := NewCalendarAggregate()
	p := &CalendarPlugin{aggregate: agg}
	agg.commands = map[string]eventsourcing.CommandHandler{
		"CreateEvent": eventsourcing.NewCommand(func(input *CreateEventInput) ([]eventsourcing.Event, error) {
			return p.createEventHandler(input)
		}),
		"UpdateEvent": eventsourcing.NewCommand(func(input *UpdateEventInput) ([]eventsourcing.Event, error) {
			return p.updateEventHandler(input)
		}),
		"DeleteEvent": eventsourcing.NewCommand(func(input *DeleteEventInput) ([]eventsourcing.Event, error) {
			return p.deleteEventHandler(input)
		}),
		"ListEvents": eventsourcing.NewCommand(func(input *ListEventsInput) ([]eventsourcing.Event, error) {
			return p.listEventsHandler(input)
		}),
	}
	eventsourcing.RegisterEvent("calendar_EventCreated", func() eventsourcing.Event { return &EventCreatedEvent{} })
	eventsourcing.RegisterEvent("calendar_EventUpdated", func() eventsourcing.Event { return &EventUpdatedEvent{} })
	eventsourcing.RegisterEvent("calendar_EventsListed", func() eventsourcing.Event { return &EventsListedEvent{} })
	eventsourcing.RegisterEvent("calendar_EventDeleted", func() eventsourcing.Event { return &EventDeletedEvent{} })
	return p
}

// Commands returns the command handlers
func (p *CalendarPlugin) Commands() map[string]eventsourcing.CommandHandler {
	return p.aggregate.commands
}

// Name returns the plugin name
func (p *CalendarPlugin) Name() string {
	return "calendar"
}

// Schemas defines the command schemas
func (p *CalendarPlugin) Schemas() map[string]eventsourcing.CommandInput {
	return map[string]eventsourcing.CommandInput{
		"CreateEvent": &CreateEventInput{},
		"UpdateEvent": &UpdateEventInput{},
		"DeleteEvent": &DeleteEventInput{},
		"ListEvents":  &ListEventsInput{},
	}
}

// Command Input Structs with Schema Generation

func (i *CreateEventInput) New() any {
	return &CreateEventInput{}
}

// CreateEventInput defines the input for creating an event
type CreateEventInput struct {
	Title       string   `json:"Title"`
	Description string   `json:"Description,omitempty"`
	Status      string   `json:"Status,omitempty"`
	Importance  string   `json:"Importance,omitempty"`
	StartTime   string   `json:"StartTime"`
	EndTime     string   `json:"EndTime,omitempty"`
	Location    string   `json:"Location,omitempty"`
	Attendees   []string `json:"Attendees,omitempty"`
	Tags        []string `json:"Tags,omitempty"`
}

func (c *CreateEventInput) Schema() map[string]interface{} {
	return map[string]interface{}{
		"description": "Creates a new calendar event",
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"Title": map[string]interface{}{
					"type":        "string",
					"description": "The title of the event",
				},
				"Description": map[string]interface{}{
					"type":        "string",
					"description": "Detailed description of the event",
				},
				"Status": map[string]interface{}{
					"type":        "string",
					"description": "Status of the event",
					"enum":        []string{StatusConfirmed, StatusTentative, StatusCancelled},
				},
				"Importance": map[string]interface{}{
					"type":        "string",
					"description": "Importance level of the event",
					"enum":        []string{ImportanceLow, ImportanceMedium, ImportanceHigh, ImportanceCritical},
				},
				"StartTime": map[string]interface{}{
					"type":        "string",
					"description": "Start time of the event (ISO 8601)",
				},
				"EndTime": map[string]interface{}{
					"type":        "string",
					"description": "End time of the event (ISO 8601)",
				},
				"Location": map[string]interface{}{
					"type":        "string",
					"description": "Location of the event",
				},
				"Attendees": map[string]interface{}{
					"type":        "array",
					"description": "List of attendees",
					"items":       map[string]interface{}{"type": "string"},
				},
				"Tags": map[string]interface{}{
					"type":        "array",
					"description": "Tags for categorizing the event",
					"items":       map[string]interface{}{"type": "string"},
				},
			},
			"required": []string{"Title", "StartTime"},
		},
	}
}

func (i *UpdateEventInput) New() any {
	return &UpdateEventInput{}
}

// UpdateEventInput defines the input for updating an event
type UpdateEventInput struct {
	EventID     string   `json:"EventID"`
	Title       string   `json:"Title,omitempty"`
	Description string   `json:"Description,omitempty"`
	Status      string   `json:"Status,omitempty"`
	Importance  string   `json:"Importance,omitempty"`
	StartTime   string   `json:"StartTime,omitempty"`
	EndTime     string   `json:"EndTime,omitempty"`
	Location    string   `json:"Location,omitempty"`
	Attendees   []string `json:"Attendees,omitempty"`
	Tags        []string `json:"Tags,omitempty"`
}

func (u *UpdateEventInput) Schema() map[string]interface{} {
	return map[string]interface{}{
		"description": "Updates an existing calendar event",
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"EventID": map[string]interface{}{
					"type":        "string",
					"description": "ID of the event to update",
				},
				"Title": map[string]interface{}{
					"type":        "string",
					"description": "The title of the event",
				},
				"Description": map[string]interface{}{
					"type":        "string",
					"description": "Detailed description of the event",
				},
				"Status": map[string]interface{}{
					"type":        "string",
					"description": "Status of the event",
					"enum":        []string{StatusConfirmed, StatusTentative, StatusCancelled},
				},
				"Importance": map[string]interface{}{
					"type":        "string",
					"description": "Importance level of the event",
					"enum":        []string{ImportanceLow, ImportanceMedium, ImportanceHigh, ImportanceCritical},
				},
				"StartTime": map[string]interface{}{
					"type":        "string",
					"description": "Start time of the event (ISO 8601)",
				},
				"EndTime": map[string]interface{}{
					"type":        "string",
					"description": "End time of the event (ISO 8601)",
				},
				"Location": map[string]interface{}{
					"type":        "string",
					"description": "Location of the event",
				},
				"Attendees": map[string]interface{}{
					"type":        "array",
					"description": "List of attendees",
					"items":       map[string]interface{}{"type": "string"},
				},
				"Tags": map[string]interface{}{
					"type":        "array",
					"description": "Tags for categorizing the event",
					"items":       map[string]interface{}{"type": "string"},
				},
			},
			"required": []string{"EventID"},
		},
	}
}

func (i *DeleteEventInput) New() any {
	return &DeleteEventInput{}
}

// DeleteEventInput defines the input for deleting an event
type DeleteEventInput struct {
	EventID string `json:"EventID"`
}

func (d *DeleteEventInput) Schema() map[string]interface{} {
	return map[string]interface{}{
		"description": "Deletes a calendar event",
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"EventID": map[string]interface{}{
					"type":        "string",
					"description": "ID of the event to delete",
				},
			},
			"required": []string{"EventID"},
		},
	}
}

func (i *ListEventsInput) New() any {
	return &ListEventsInput{}
}

// ListEventsInput defines the input for listing events
type ListEventsInput struct {
	Status     string `json:"Status,omitempty"`
	Importance string `json:"Importance,omitempty"`
	Tag        string `json:"Tag,omitempty"`
	From       string `json:"From,omitempty"`
	To         string `json:"To,omitempty"`
}

func (l *ListEventsInput) Schema() map[string]interface{} {
	return map[string]interface{}{
		"description": "Lists calendar events with optional filtering",
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"Status": map[string]interface{}{
					"type":        "string",
					"description": "Filter by status",
					"enum":        []string{"All", StatusConfirmed, StatusTentative, StatusCancelled},
				},
				"Importance": map[string]interface{}{
					"type":        "string",
					"description": "Filter by importance",
					"enum":        []string{"All", ImportanceLow, ImportanceMedium, ImportanceHigh, ImportanceCritical},
				},
				"Tag": map[string]interface{}{
					"type":        "string",
					"description": "Filter by tag",
				},
				"From": map[string]interface{}{
					"type":        "string",
					"description": "Filter events from this date (ISO 8601)",
				},
				"To": map[string]interface{}{
					"type":        "string",
					"description": "Filter events to this date (ISO 8601)",
				},
			},
		},
	}
}

// Event Types
type EventsListedEvent struct {
	EventType string           `json:"event_type"`
	Events    []*CalendarEvent `json:"listed_events"`
}

func (e *EventsListedEvent) Type() string { return "calendar_EventsListed" }
func (e *EventsListedEvent) Marshal() ([]byte, error) {
	e.EventType = e.Type()
	return json.Marshal(e)
}
func (e *EventsListedEvent) Unmarshal(data []byte) error { return json.Unmarshal(data, e) }

type EventCreatedEvent struct {
	EventType   string   `json:"event_type"`
	EventID     string   `json:"event_id"`
	Title       string   `json:"title"`
	Description string   `json:"description,omitempty"`
	Status      string   `json:"status"`
	Importance  string   `json:"importance"`
	StartTime   string   `json:"start_time"`
	EndTime     string   `json:"end_time,omitempty"`
	Location    string   `json:"location,omitempty"`
	Attendees   []string `json:"attendees,omitempty"`
	Tags        []string `json:"tags,omitempty"`
}

func (e *EventCreatedEvent) Type() string { return "calendar_EventCreated" }
func (e *EventCreatedEvent) Marshal() ([]byte, error) {
	e.EventType = e.Type()
	return json.Marshal(e)
}
func (e *EventCreatedEvent) Unmarshal(data []byte) error { return json.Unmarshal(data, e) }

type EventUpdatedEvent struct {
	EventType   string   `json:"event_type"`
	EventID     string   `json:"event_id"`
	Title       string   `json:"title,omitempty"`
	Description string   `json:"description,omitempty"`
	Status      string   `json:"status,omitempty"`
	Importance  string   `json:"importance,omitempty"`
	StartTime   string   `json:"start_time,omitempty"`
	EndTime     string   `json:"end_time,omitempty"`
	Location    string   `json:"location,omitempty"`
	Attendees   []string `json:"attendees,omitempty"`
	Tags        []string `json:"tags,omitempty"`
}

func (e *EventUpdatedEvent) Type() string { return "calendar_EventUpdated" }
func (e *EventUpdatedEvent) Marshal() ([]byte, error) {
	e.EventType = e.Type()
	return json.Marshal(e)
}
func (e *EventUpdatedEvent) Unmarshal(data []byte) error { return json.Unmarshal(data, e) }

type EventDeletedEvent struct {
	EventType string `json:"event_type"`
	EventID   string `json:"event_id"`
}

func (e *EventDeletedEvent) Type() string { return "calendar_EventDeleted" }
func (e *EventDeletedEvent) Marshal() ([]byte, error) {
	e.EventType = e.Type()
	return json.Marshal(e)
}
func (e *EventDeletedEvent) Unmarshal(data []byte) error { return json.Unmarshal(data, e) }

// Utility functions
func generateEventID() string {
	return fmt.Sprintf("event_%d", eventsourcing.GenerateUniqueID())
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
	return status == StatusConfirmed || status == StatusTentative || status == StatusCancelled
}

func validateImportance(importance string) bool {
	return importance == ImportanceLow || importance == ImportanceMedium || importance == ImportanceHigh || importance == ImportanceCritical
}

// Command Handlers
func (p *CalendarPlugin) createEventHandler(input *CreateEventInput) ([]eventsourcing.Event, error) {
	if input.Title == "" {
		return nil, fmt.Errorf("title is required and must be a non-empty string")
	}
	if input.StartTime == "" {
		return nil, fmt.Errorf("startTime is required")
	}

	event := &EventCreatedEvent{
		EventType:   "calendar_EventCreated",
		EventID:     generateEventID(),
		Title:       input.Title,
		Description: input.Description,
		Status:      StatusConfirmed,  // Default
		Importance:  ImportanceMedium, // Default
		StartTime:   input.StartTime,
		EndTime:     input.EndTime,
		Location:    input.Location,
		Attendees:   input.Attendees,
		Tags:        input.Tags,
	}

	if input.Status != "" && validateStatus(input.Status) {
		event.Status = input.Status
	}
	if input.Importance != "" && validateImportance(input.Importance) {
		event.Importance = input.Importance
	}
	// Validate times
	if input.StartTime != "" {
		formats := []string{
			time.RFC3339,
			"2006-01-02",
			"2006-01-02 15:04:05",
			"2006-01-02T15:04:05Z",
		}
		var _ time.Time
		var err error
		for _, format := range formats {
			_, err = time.Parse(format, input.StartTime)
			if err == nil {
				break
			}
		}
		if err != nil {
			return nil, fmt.Errorf("invalid startTime format: '%s' doesn't match supported formats", input.StartTime)
		}
	}
	if input.EndTime != "" {
		if _, err := time.Parse(time.RFC3339, input.EndTime); err != nil {
			return nil, fmt.Errorf("invalid endTime format: %v", err)
		}
	}
	return []eventsourcing.Event{event}, nil
}

func (p *CalendarPlugin) updateEventHandler(input *UpdateEventInput) ([]eventsourcing.Event, error) {
	if input.EventID == "" {
		return nil, fmt.Errorf("eventID is required and must be a non-empty string")
	}

	p.aggregate.Mu.RLock()
	_, exists := p.aggregate.Events[input.EventID]
	p.aggregate.Mu.RUnlock()
	if !exists {
		return nil, fmt.Errorf("event %s not found", input.EventID)
	}

	event := &EventUpdatedEvent{
		EventType:   "calendar_EventUpdated",
		EventID:     input.EventID,
		Title:       input.Title,
		Description: input.Description,
		Status:      input.Status,
		Importance:  input.Importance,
		StartTime:   input.StartTime,
		EndTime:     input.EndTime,
		Location:    input.Location,
		Attendees:   input.Attendees,
		Tags:        input.Tags,
	}

	if input.Status != "" && !validateStatus(input.Status) {
		return nil, fmt.Errorf("invalid status: %s", input.Status)
	}
	if input.Importance != "" && !validateImportance(input.Importance) {
		return nil, fmt.Errorf("invalid importance: %s", input.Importance)
	}
	if input.StartTime != "" {
		if _, err := time.Parse(time.RFC3339, input.StartTime); err != nil {
			return nil, fmt.Errorf("invalid startTime format: %v", err)
		}
	}
	if input.EndTime != "" {
		if _, err := time.Parse(time.RFC3339, input.EndTime); err != nil {
			return nil, fmt.Errorf("invalid endTime format: %v", err)
		}
	}

	return []eventsourcing.Event{event}, nil
}

func (p *CalendarPlugin) deleteEventHandler(input *DeleteEventInput) ([]eventsourcing.Event, error) {
	if input.EventID == "" {
		return nil, fmt.Errorf("eventID is required and must be a non-empty string")
	}

	p.aggregate.Mu.RLock()
	_, exists := p.aggregate.Events[input.EventID]
	p.aggregate.Mu.RUnlock()
	if !exists {
		return nil, fmt.Errorf("event %s not found", input.EventID)
	}

	event := &EventDeletedEvent{EventType: "calendar_EventDeleted", EventID: input.EventID}
	return []eventsourcing.Event{event}, nil
}

func (p *CalendarPlugin) listEventsHandler(input *ListEventsInput) ([]eventsourcing.Event, error) {
	p.aggregate.Mu.RLock()
	defer p.aggregate.Mu.RUnlock()

	events := make([]*CalendarEvent, 0, len(p.aggregate.Events))
	for _, event := range p.aggregate.Events {
		events = append(events, event)
	}

	// Apply filters
	var statusFilter, importanceFilter, tagFilter string
	var fromTime, toTime time.Time
	if input.Status != "" && input.Status != "All" && validateStatus(input.Status) {
		statusFilter = input.Status
	}
	if input.Importance != "" && input.Importance != "All" && validateImportance(input.Importance) {
		importanceFilter = input.Importance
	}
	if input.Tag != "" {
		tagFilter = input.Tag
	}
	if input.From != "" {
		fromTime = parseTime(input.From)
	}
	if input.To != "" {
		toTime = parseTime(input.To)
	}

	filteredEvents := events[:0]
	for _, event := range events {
		if (statusFilter != "" && event.Status != statusFilter) ||
			(importanceFilter != "" && event.Importance != importanceFilter) ||
			(tagFilter != "" && !contains(event.Tags, tagFilter)) ||
			(!fromTime.IsZero() && event.StartTime.Before(fromTime)) ||
			(!toTime.IsZero() && event.StartTime.After(toTime)) {
			continue
		}
		filteredEvents = append(filteredEvents, event)
	}

	// Sort events by start time
	sort.Slice(filteredEvents, func(i, j int) bool {
		return filteredEvents[i].StartTime.Before(filteredEvents[j].StartTime)
	})

	event := &EventsListedEvent{EventType: "calendar_EventsListed", Events: filteredEvents}
	return []eventsourcing.Event{event}, nil
}

// GetCustomUI returns a list view for the calendar events
func (ca *CalendarAggregate) GetCustomUI() fyne.CanvasObject {
	ca.Mu.RLock()
	events := make([]*CalendarEvent, 0, len(ca.Events))
	for _, event := range ca.Events {
		events = append(events, event)
	}
	ca.Mu.RUnlock()

	if len(events) == 0 {
		return container.NewCenter(widget.NewLabel("No events available. Create one to get started!"))
	}

	// Sort events by start time
	sort.Slice(events, func(i, j int) bool {
		return events[i].StartTime.Before(events[j].StartTime)
	})

	content := container.NewVBox()
	for _, event := range events {
		card := createEventCard(event)
		content.Add(card)
		content.Add(widget.NewSeparator())
	}

	return container.NewVScroll(content)
}

// createEventCard creates a compact card UI for a single event
func createEventCard(event *CalendarEvent) fyne.CanvasObject {
	// Title with importance icon
	title := widget.NewLabel(event.Title)
	title.TextStyle = fyne.TextStyle{Bold: true}
	if event.Status == StatusCancelled {
		title.TextStyle.Italic = true
	}
	title.Wrapping = fyne.TextWrapOff
	titleBox := container.NewHBox(
		widget.NewIcon(importanceIcon(event.Importance)),
		title,
	)

	// Compact details
	var detailLines []string
	if event.Description != "" {
		desc := strings.TrimSpace(event.Description)
		if len(desc) > 50 {
			desc = desc[:47] + "..."
		}
		detailLines = append(detailLines, desc)
	}
	detailLines = append(detailLines, fmt.Sprintf("Start: %s", event.StartTime.Format("2006-01-02 15:04")))
	if !event.EndTime.IsZero() {
		detailLines = append(detailLines, fmt.Sprintf("End: %s", event.EndTime.Format("2006-01-02 15:04")))
	}
	if event.Location != "" {
		detailLines = append(detailLines, fmt.Sprintf("Location: %s", event.Location))
	}
	if len(event.Tags) > 0 {
		detailLines = append(detailLines, fmt.Sprintf("Tags: %s", strings.Join(event.Tags, ", ")))
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

// importanceIcon returns an icon based on importance
func importanceIcon(importance string) fyne.Resource {
	switch importance {
	case ImportanceCritical:
		return theme.ErrorIcon()
	case ImportanceHigh:
		return theme.WarningIcon()
	case ImportanceMedium:
		return theme.InfoIcon()
	case ImportanceLow:
		return theme.ConfirmIcon()
	default:
		return theme.QuestionIcon()
	}
}

// Additional Plugin Methods
// Additional Plugin Methods
func (p *CalendarPlugin) Aggregate() eventsourcing.Aggregate {
	return p.aggregate
}

func (p *CalendarPlugin) Type() eventsourcing.PluginType {
	return eventsourcing.LLMPlugin
}

func (p *CalendarPlugin) SystemPrompt() string {
	// Acquire read lock to safely access events
	p.aggregate.Mu.RLock()
	defer p.aggregate.Mu.RUnlock()

	// Collect events into a slice for sorting
	events := make([]*CalendarEvent, 0, len(p.aggregate.Events))
	for _, event := range p.aggregate.Events {
		events = append(events, event)
	}

	// Sort events by start time for consistent ordering
	sort.Slice(events, func(i, j int) bool {
		return events[i].StartTime.Before(events[j].StartTime)
	})

	// Build the event list string
	var eventList strings.Builder
	if len(events) == 0 {
		eventList.WriteString("There are currently no events.\n")
	} else {
		eventList.WriteString("Current events:\n")
		for _, event := range events {
			eventList.WriteString(fmt.Sprintf("- Event ID: %s, Title: \"%s\", Start: %s\n", event.EventID, event.Title, event.StartTime.Format("2006-01-02 15:04")))
		}
	}

	// Construct the full dynamic prompt
	prompt := `You are CalendarMaster, a specialized AI for managing calendar events in MindPalace.

Your job is to interpret user requests about calendar events and execute the right commands (CreateEvent, UpdateEvent, DeleteEvent, ListEvents) based on the current event state.

` + eventList.String() + `

Be concise, accurate, and always use the tools provided to manage events. Focus on:

1. Creating detailed events with proper times, locations, and attendees
2. Updating events with relevant information
3. Deleting events when requested
4. Listing and filtering events as requested

When interpreting user requests, pay close attention to the intent:
- If the user asks to "remove," "delete," or "cancel" an event, use the DeleteEvent command.
- If the user asks to "create" or "add" an event, use the CreateEvent command.
- If the user asks to "update" or "modify" an event, use the UpdateEvent command.
- If the user asks to "list" or "show" events, use the ListEvents command.

When creating or updating events, extract key information from user requests including:
- Event title and description
- Importance level (Low, Medium, High, Critical)
- Status (Confirmed, Tentative, Cancelled)
- Start and end times (in ISO format)
- Location
- Attendees
- Tags for organization

Format your responses in a structured way and confirm actions performed.`

	return prompt
}

// AgentModel specifies the LLM model to use for this plugin's agent
func (p *CalendarPlugin) AgentModel() string {
	return "gpt-oss:20b" // Using the general-purpose model for calendar management
}

func (p *CalendarPlugin) EventHandlers() map[string]eventsourcing.EventHandler {
	return nil
}

func (a *CalendarAggregate) Broadcast3DDelta(event eventsourcing.Event) []eventsourcing.DeltaAction {
	a.Mu.RLock()
	defer a.Mu.RUnlock()
	theme := ui3d.DefaultTheme()
	switch e := event.(type) {
	case *EventCreatedEvent:
		// Get sorted event IDs to determine position
		sortedIDs := a.getSortedEventIDs()
		i := 0
		for _, id := range sortedIDs {
			if id == e.EventID {
				break
			}
			i++
		}
		pos := ui3d.PositionInGrid(float64(i), 0, 2.0)
		pos[0] = pos[2] // Move Z spacing to X axis
		pos[1] = 2.0
		pos[2] = -8.0
		return ui3d.CreateCard(fmt.Sprintf("calendar_event_%s", e.EventID), e.Title, pos, theme)
	case *EventUpdatedEvent:
		// Get sorted event IDs to determine position
		sortedIDs := a.getSortedEventIDs()
		i := 0
		for _, id := range sortedIDs {
			if id == e.EventID {
				break
			}
			i++
		}
		pos := ui3d.PositionInGrid(float64(i), 0, 2.0)
		pos[0] = pos[2] // Move Z spacing to X axis
		pos[1] = 2.0
		pos[2] = -8.0
		// Delete old and create new
		oldActions := []eventsourcing.DeltaAction{
			{Type: "delete", NodeID: fmt.Sprintf("calendar_event_%s", e.EventID)},
			{Type: "delete", NodeID: fmt.Sprintf("calendar_event_%s_label", e.EventID)},
		}
		newActions := ui3d.CreateCard(fmt.Sprintf("calendar_event_%s", e.EventID), a.Events[e.EventID].Title, pos, theme)
		return append(oldActions, newActions...)
	case *EventDeletedEvent:
		return []eventsourcing.DeltaAction{
			{Type: "delete", NodeID: fmt.Sprintf("calendar_event_%s", e.EventID)},
			{Type: "delete", NodeID: fmt.Sprintf("calendar_event_%s_label", e.EventID)},
		}
	}
	return nil
}

func (a *CalendarAggregate) GetFull3DState() []eventsourcing.DeltaAction {
	a.Mu.RLock()
	defer a.Mu.RUnlock()
	theme := ui3d.DefaultTheme()
	actions := []eventsourcing.DeltaAction{ui3d.CreateSphere("calendar_hub", []float64{0.0, 0.0, -10.0}, theme)}
	// Add cards for events in sorted order
	sortedIDs := a.getSortedEventIDs()
	for i, id := range sortedIDs {
		event := a.Events[id]
		pos := ui3d.PositionInGrid(float64(i), 0, 2.0)
		pos[0] = pos[2] // Move Z spacing to X axis
		pos[1] = 2.0
		pos[2] = -8.0
		actions = append(actions, ui3d.CreateCard(fmt.Sprintf("calendar_event_%s", id), event.Title, pos, theme)...)
	}
	return actions
}

// getSortedEventIDs returns event IDs sorted by start time for consistent positioning
func (a *CalendarAggregate) getSortedEventIDs() []string {
	ids := make([]string, 0, len(a.Events))
	for id := range a.Events {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		eventI := a.Events[ids[i]]
		eventJ := a.Events[ids[j]]
		return eventI.StartTime.Before(eventJ.StartTime)
	})
	return ids
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
