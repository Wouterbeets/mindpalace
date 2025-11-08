package main

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"sync"
	"time"

	"mindpalace/pkg/eventsourcing"
	"mindpalace/pkg/ui3d"
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

const (
	calendarColumns     = 7
	calendarRows        = 6
	calendarCellWidth   = 1.65
	calendarCellHeight  = 1.1
	calendarBoardHeight = 1.6
	calendarBoardZDepth = 0.05
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
    nodes    []string
    // Event scoring sink (optional): map[event_id]EventScore
    Scores map[string]eventsourcing.EventScore
    // viewMonthStart holds the first day of the month currently being rendered.
    // If zero, rendering falls back to the earliest event's month or the current month.
    viewMonthStart time.Time
}

// NewCalendarAggregate creates a new thread-safe CalendarAggregate
func NewCalendarAggregate() *CalendarAggregate {
    return &CalendarAggregate{
        Events:   make(map[string]*CalendarEvent),
        commands: make(map[string]eventsourcing.CommandHandler),
        nodes:    make([]string, 0),
        Scores:   make(map[string]eventsourcing.EventScore),
    }
}

// ID returns the aggregate's identifier
func (a *CalendarAggregate) ID() string {
	return "calendar"
}

// Reset clears tracked calendar events while preserving command handlers.
func (a *CalendarAggregate) Reset() {
    a.Mu.Lock()
    defer a.Mu.Unlock()
    a.Events = make(map[string]*CalendarEvent)
    a.nodes = nil
    a.Scores = make(map[string]eventsourcing.EventScore)
    a.viewMonthStart = time.Time{}
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
		// No-op for unrelated events
	}
	return nil
}

// RecordEventScore implements eventsourcing.EventScorable.
func (a *CalendarAggregate) RecordEventScore(eventID string, score eventsourcing.EventScore) {
    a.Mu.Lock()
    defer a.Mu.Unlock()
    if a.Scores == nil {
        a.Scores = make(map[string]eventsourcing.EventScore)
    }
    a.Scores[eventID] = score
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
        // Navigate or jump the calendar month view without mutating domain events.
        "NavigateMonth": eventsourcing.NewCommand(func(input *NavigateMonthInput) ([]eventsourcing.Event, error) {
            return p.navigateMonthHandler(input)
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

func (p *CalendarPlugin) AgentModel() string {
	return "gpt-oss:20b" // Using the general-purpose model for calendar management
}

// Description returns a short description of how the orchestrator AI can use this agent
func (p *CalendarPlugin) Description() string {
	return "use this to manage calendar events, talk to me in natural language about scheduling, updating, or listing events."
}

func (p *CalendarPlugin) Metadata() eventsourcing.PluginMetadata {
	return eventsourcing.PluginMetadata{
		Name:      p.Name(),
		Summary:   "Organizes calendar events with priorities, attendees, and tagging.",
		UsageHint: "Delegate scheduling, rescheduling, or availability lookups that need structured timelines.",
		Capabilities: []eventsourcing.PluginCapability{
			{Name: "CreateEvent", Description: "Capture richly described calendar events with time and importance."},
			{Name: "UpdateEvent", Description: "Adjust the details of existing calendar events."},
			{Name: "DeleteEvent", Description: "Remove events that are no longer relevant."},
			{Name: "ListEvents", Description: "Generate timeline summaries filtered by status, importance, or tags."},
		},
		Examples: []string{
			"Schedule a high-importance planning session on Saturday at 15:00 with the travel team.",
			"What upcoming events tagged 'roadtrip' do we have next week?",
		},
		Tags:           []string{"calendar", "planning", "timeline"},
		Maintainer:     "MindPalace Core Team",
		DefaultTimeout: 8 * time.Second,
		Safety:         "trusted",
		Reliability:    "battle-tested",
		Lifecycle:      "maintained",
		ModelAsset:     "69166",
	}
}

func (p *CalendarPlugin) Aggregate() eventsourcing.Aggregate {
	return p.aggregate
}

func (p *CalendarPlugin) SystemPrompt() string {
	return `You are a calendar management assistant. You can help users create, update, delete, and list calendar events.

Available commands:
- CreateEvent: Create a new calendar event with title, description, start time, end time, and importance level
- UpdateEvent: Modify an existing event's details
- DeleteEvent: Remove an event from the calendar
- ListEvents: Show all events, optionally filtered by date range

When creating events, ensure start time is before end time. Importance levels are: low, medium, high.
Format times as ISO 8601 strings (e.g., "2024-01-15T10:00:00Z").`
}

func (p *CalendarPlugin) Type() eventsourcing.PluginType {
	return eventsourcing.LLMPlugin
}

// Schemas defines the command schemas
func (p *CalendarPlugin) Schemas() map[string]eventsourcing.CommandInput {
    return map[string]eventsourcing.CommandInput{
        "CreateEvent": &CreateEventInput{},
        "UpdateEvent": &UpdateEventInput{},
        "DeleteEvent": &DeleteEventInput{},
        "ListEvents":  &ListEventsInput{},
        "NavigateMonth": &NavigateMonthInput{},
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

func (p *CalendarPlugin) EventHandlers() map[string]eventsourcing.EventHandler {
	return nil
}

func (a *CalendarAggregate) EmitDelta(event eventsourcing.Event) *eventsourcing.DeltaEnvelope {
	a.Mu.Lock()
	defer a.Mu.Unlock()
	switch event.(type) {
	case *EventCreatedEvent, *EventUpdatedEvent, *EventDeletedEvent, *EventsListedEvent:
		return a.buildMonthViewEnvelope()
	default:
		return nil
	}
}

func (a *CalendarAggregate) buildMonthViewEnvelope() *eventsourcing.DeltaEnvelope {
    theme := ui3d.DefaultTheme()
    builder := ui3d.NewDeltaBuilder(theme)
    for _, id := range a.nodes {
        builder.Delete(id)
    }
    newIDs := a.appendMonthView(builder)
    a.nodes = newIDs
    actions := builder.Build()
    // Attach a month-level UI component with navigation actions.
    monthStart := a.currentMonthStart()
    components := []interface{}{NewCalendarMonthComponent(monthStart)}
    return &eventsourcing.DeltaEnvelope{
        Type:      "delta",
        Aggregate: "calendar",
        EventID:   eventsourcing.ISOTimestamp(),
        Timestamp: eventsourcing.ISOTimestamp(),
        Actions:   actions,
        Components: components,
    }
}

func (a *CalendarAggregate) appendMonthView(builder *ui3d.DeltaBuilder) []string {
	zones := ui3d.GetGlobalZones()
	zone, ok := zones[a.ID()]
	if !ok {
		zone = ui3d.Zone{Angle: 0, Radius: 0, GridCols: calendarColumns, GridRows: calendarRows}
	}
	// Pull the calendar board closer to the hub by using mid-radius positioning
    calendarRadius := zone.Radius * 0.5
    centerX := calendarRadius * math.Cos(zone.Angle*math.Pi/180)
    centerZ := calendarRadius * math.Sin(zone.Angle*math.Pi/180)
    baseY := calendarBoardHeight
    dayCellWidth := calendarCellWidth
    dayCellHeight := calendarCellHeight
    boardWidth := dayCellWidth * calendarColumns
    boardHeight := dayCellHeight * calendarRows

    // Build a radial basis so the calendar plane faces the hub.
    // n: normal pointing toward origin, r: right vector along the calendar row axis, u: vertical up.
    nx, nz := -centerX, -centerZ
    nlen := math.Hypot(nx, nz)
    if nlen < 1e-6 {
        nx, nz = 0, -1 // default facing -Z
        nlen = 1
    }
    nx /= nlen
    nz /= nlen
    // r = up x n
    rx, rz := nz, -nx
    // Helper to position a point using the radial basis
    posAt := func(hx, vy, dz float64) []float64 {
        x := centerX + rx*hx
        y := baseY + vy
        z := centerZ + rz*hx + nz*dz
        return []float64{x, y, z}
    }
    // Common vertical anchor helpers
    topY := boardHeight/2 - dayCellHeight/2

    monthStart := a.currentMonthStart()
    monthLabel := monthStart.Format("January 2006")
    monthLabelID := "calendar_month_label"
    builder.AddAction(eventsourcing.DeltaAction{
        Type:     "create",
        NodeID:   monthLabelID,
        NodeType: "Label3D",
        Properties: map[string]interface{}{
            "text":                 monthLabel,
            // Slightly above the grid center and on the plane facing the hub
            "position":             posAt(0, boardHeight/2+dayCellHeight*0.7, 0),
            "billboard":            "enabled",
            "font_size":            36,
            "modulate":             []float64{0.85, 0.95, 1.0, 1.0},
            "horizontal_alignment": "center",
        },
    })

	dayNames := []string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"}
	newIDs := []string{monthLabelID}
    for col := 0; col < calendarColumns; col++ {
        headerID := fmt.Sprintf("calendar_header_%d", col)
        headerHX := -boardWidth/2 + dayCellWidth/2 + float64(col)*dayCellWidth
        headerVY := topY + dayCellHeight*0.6
        builder.AddAction(eventsourcing.DeltaAction{
            Type:     "create",
            NodeID:   headerID,
            NodeType: "Label3D",
            Properties: map[string]interface{}{
                "text":                 dayNames[col],
                "position":             posAt(headerHX, headerVY, 0),
                "billboard":            "enabled",
                "font_size":            24,
                "modulate":             []float64{0.7, 0.86, 1.0, 1.0},
                "horizontal_alignment": "center",
            },
        })
        newIDs = append(newIDs, headerID)
    }

    firstDayIndex := int(monthStart.Weekday())
    _ = monthStart.AddDate(0, 1, -1).Day() // retained to emphasize month bounds; value unused here
    currentUTC := time.Now().UTC()
    currentMonth := currentUTC.Year() == monthStart.Year() && currentUTC.Month() == monthStart.Month()
    dayEventMap := a.eventsByDay(monthStart)

    // Fill full 6x7 grid (including leading/trailing days) and rotate to face the hub.
    maxPerDay := 3
    for index := 0; index < calendarColumns*calendarRows; index++ {
        row := index / calendarColumns
        col := index % calendarColumns
        // Compute the actual date represented by this cell
        date := monthStart.AddDate(0, 0, index-firstDayIndex)
        inMonth := date.Month() == monthStart.Month()

        // Cell center offsets
        hx := -boardWidth/2 + dayCellWidth/2 + float64(col)*dayCellWidth
        vy := topY - float64(row)*dayCellHeight

        // Background color
        color := []float64{0.08, 0.14, 0.22, 0.9}
        if col == 0 || col == 6 {
            color = []float64{0.12, 0.18, 0.3, 0.9}
        }
        if inMonth && currentMonth && date.Day() == currentUTC.Day() {
            color = []float64{0.2, 0.32, 0.52, 0.95}
        }
        if !inMonth {
            color = []float64{0.06, 0.1, 0.16, 0.6}
        }

        // Node IDs (preserve IDs for in-month days to keep tests/clients happy)
        cellID := ""
        if inMonth {
            cellID = fmt.Sprintf("calendar_cell_bg_%02d", date.Day())
        } else if index < firstDayIndex {
            cellID = fmt.Sprintf("calendar_cell_bg_prev_%02d", date.Day())
        } else {
            cellID = fmt.Sprintf("calendar_cell_bg_next_%02d", date.Day())
        }

        builder.AddAction(eventsourcing.DeltaAction{
            Type:     "create",
            NodeID:   cellID,
            NodeType: "MeshInstance3D",
            Properties: map[string]interface{}{
                "mesh":      "box",
                "position":  posAt(hx, vy, -calendarBoardZDepth),
                "scale":     []float64{dayCellWidth * 0.92, dayCellHeight * 0.9, 0.05},
                "auxiliary": true,
                "material_override": map[string]interface{}{
                    "albedo_color": color,
                },
            },
        })
        newIDs = append(newIDs, cellID)

        // Day number
        dayLabelID := ""
        if inMonth {
            dayLabelID = fmt.Sprintf("calendar_day_number_%02d", date.Day())
        } else if index < firstDayIndex {
            dayLabelID = fmt.Sprintf("calendar_day_number_prev_%02d", date.Day())
        } else {
            dayLabelID = fmt.Sprintf("calendar_day_number_next_%02d", date.Day())
        }
        dayColor := []float64{0.85, 0.95, 1.0, 1.0}
        if !inMonth {
            dayColor = []float64{0.65, 0.78, 0.9, 0.6}
        }
        builder.AddAction(eventsourcing.DeltaAction{
            Type:     "create",
            NodeID:   dayLabelID,
            NodeType: "Label3D",
            Properties: map[string]interface{}{
                "text":                 fmt.Sprintf("%d", date.Day()),
                "position":             posAt(hx - dayCellWidth*0.38, vy + dayCellHeight*0.32, 0.01),
                "billboard":            "enabled",
                "font_size":            20,
                "horizontal_alignment": "left",
                "modulate":             dayColor,
            },
        })
        newIDs = append(newIDs, dayLabelID)

        // Skip events for out-of-month cells
        if !inMonth {
            continue
        }

        events := dayEventMap[date.Day()]
        if len(events) == 0 {
            continue
        }
        sort.Slice(events, func(i, j int) bool { return events[i].StartTime.Before(events[j].StartTime) })
        shown := 0
        for idx, event := range events {
            if shown >= maxPerDay {
                break
            }
            eventID := fmt.Sprintf("calendar_event_label_%s", event.EventID)
            eventVY := vy - dayCellHeight*0.1 - float64(idx)*0.22
            eventText := fmt.Sprintf("%s  %s", event.StartTime.Format("3:04 PM"), event.Title)
            displayInfo := map[string]interface{}{
                "title":       event.Title,
                "description": event.Description,
                "details": map[string]interface{}{
                    "start":      event.StartTime.Format(time.RFC822),
                    "end":        event.EndTime.Format(time.RFC822),
                    "status":     event.Status,
                    "importance": event.Importance,
                    "location":   event.Location,
                    "attendees":  event.Attendees,
                },
            }
            eventColor := colorForImportance(event.Importance)
            builder.AddAction(eventsourcing.DeltaAction{
                Type:     "create",
                NodeID:   eventID,
                NodeType: "Label3D",
                Properties: map[string]interface{}{
                    "text":                 eventText,
                    "position":             posAt(hx, eventVY, 0.02),
                    "billboard":            "enabled",
                    "font_size":            18,
                    "horizontal_alignment": "center",
                    "modulate":             eventColor,
                    "display_info":         displayInfo,
                },
            })
            newIDs = append(newIDs, eventID)
            shown++
        }
        if len(events) > maxPerDay {
            extra := len(events) - maxPerDay
            moreID := fmt.Sprintf("calendar_day_more_%02d", date.Day())
            // Build a simple description listing the hidden events
            lines := ""
            for _, e := range events[maxPerDay:] {
                when := e.StartTime.Format("Mon 3:04 PM")
                lines += fmt.Sprintf("%s  %s\n", when, e.Title)
            }
            builder.AddAction(eventsourcing.DeltaAction{
                Type:     "create",
                NodeID:   moreID,
                NodeType: "Label3D",
                Properties: map[string]interface{}{
                    "text":                 fmt.Sprintf("+%d more", extra),
                    "position":             posAt(hx, vy - dayCellHeight*0.1 - float64(maxPerDay)*0.22, 0.02),
                    "billboard":            "enabled",
                    "font_size":            16,
                    "horizontal_alignment": "center",
                    "modulate":             []float64{0.82, 0.95, 1.0, 0.9},
                    "display_info": map[string]interface{}{
                        "title":       date.Format("Jan 2") + " — more events",
                        "description": lines,
                    },
                },
            })
            newIDs = append(newIDs, moreID)
        }
    }

    return newIDs
}

func (a *CalendarAggregate) currentMonthStart() time.Time {
    if len(a.Events) == 0 {
        now := time.Now().UTC()
        if !a.viewMonthStart.IsZero() {
            return a.viewMonthStart
        }
        return time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
    }
    earliest := time.Time{}
    for _, event := range a.Events {
        if earliest.IsZero() || event.StartTime.Before(earliest) {
            earliest = event.StartTime
        }
    }
    if earliest.IsZero() {
        now := time.Now().UTC()
        if !a.viewMonthStart.IsZero() {
            return a.viewMonthStart
        }
        return time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
    }
    if !a.viewMonthStart.IsZero() {
        return a.viewMonthStart
    }
    return time.Date(earliest.Year(), earliest.Month(), 1, 0, 0, 0, 0, time.UTC)
}

func (a *CalendarAggregate) eventsByDay(monthStart time.Time) map[int][]*CalendarEvent {
    result := make(map[int][]*CalendarEvent)
    // Compute month range [monthStart, nextMonthStart)
    nextMonth := monthStart.AddDate(0, 1, 0)
    for _, event := range a.Events {
        // Determine event span (inclusive of end date if provided)
        start := event.StartTime
        end := event.EndTime
        if end.IsZero() || end.Before(start) {
            end = start
        }
        // Clamp to current month range
        if end.Before(monthStart) || !start.Before(nextMonth) {
            continue // entirely outside this month
        }
        // Determine day numbers in this month the event touches
        // Start day is max(event.Start, monthStart), end day is min(event.End, last day in month)
        clampedStart := start
        if clampedStart.Before(monthStart) {
            clampedStart = monthStart
        }
        clampedEnd := end
        if clampedEnd.After(nextMonth.Add(-time.Nanosecond)) {
            clampedEnd = nextMonth.Add(-time.Nanosecond)
        }
        for d := clampedStart.Day(); ; {
            result[d] = append(result[d], event)
            // Advance to next day within month
            temp := time.Date(clampedStart.Year(), clampedStart.Month(), d, 12, 0, 0, 0, time.UTC).AddDate(0, 0, 1)
            if temp.After(clampedEnd) || temp.Month() != monthStart.Month() {
                break
            }
            d = temp.Day()
        }
    }
    return result
}

// NavigateMonthInput allows the frontend to move the month view window.
type NavigateMonthInput struct {
    Offset int `json:"Offset,omitempty"` // Month offset relative to current view
    Year   int `json:"Year,omitempty"`
    Month  int `json:"Month,omitempty"` // 1-12
}

func (i *NavigateMonthInput) New() any { return &NavigateMonthInput{} }
func (i *NavigateMonthInput) Schema() map[string]interface{} {
    return map[string]interface{}{
        "description": "Navigate the calendar month view",
        "parameters": map[string]interface{}{
            "type": "object",
            "properties": map[string]interface{}{
                "Offset": map[string]interface{}{"type": "integer", "description": "Relative month offset (e.g., -1 prev, +1 next)"},
                "Year":   map[string]interface{}{"type": "integer", "description": "Absolute target year"},
                "Month":  map[string]interface{}{"type": "integer", "minimum": 1, "maximum": 12, "description": "Absolute target month (1-12)"},
            },
        },
    }
}

func (p *CalendarPlugin) navigateMonthHandler(input *NavigateMonthInput) ([]eventsourcing.Event, error) {
    a := p.aggregate
    a.Mu.Lock()
    // Determine base month
    base := a.viewMonthStart
    if base.IsZero() {
        now := time.Now().UTC()
        base = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
    }
    // Absolute jump takes precedence
    if input != nil && input.Year > 0 && input.Month >= 1 && input.Month <= 12 {
        a.viewMonthStart = time.Date(input.Year, time.Month(input.Month), 1, 0, 0, 0, 0, time.UTC)
    } else if input != nil && input.Offset != 0 {
        a.viewMonthStart = base.AddDate(0, input.Offset, 0)
    } else if a.viewMonthStart.IsZero() {
        a.viewMonthStart = base
    }
    // Build a light EventsListed event so EmitDelta can refresh month view.
    // Filter events overlapping the selected month to keep payload modest.
    monthStart := a.viewMonthStart
    nextMonth := monthStart.AddDate(0, 1, 0)
    filtered := make([]*CalendarEvent, 0)
    for _, ev := range a.Events {
        start := ev.StartTime
        end := ev.EndTime
        if end.IsZero() || end.Before(start) {
            end = start
        }
        if end.Before(monthStart) || !start.Before(nextMonth) {
            continue
        }
        filtered = append(filtered, ev)
    }
    a.Mu.Unlock()
    // Sort filtered by start time
    sort.Slice(filtered, func(i, j int) bool { return filtered[i].StartTime.Before(filtered[j].StartTime) })
    ev := &EventsListedEvent{EventType: "calendar_EventsListed", Events: filtered}
    return []eventsourcing.Event{ev}, nil
}

// CalendarMonthComponent provides navigation actions for the month view.
type CalendarMonthComponent struct {
    NodeID    string
    ViewStart time.Time
}

func NewCalendarMonthComponent(monthStart time.Time) *CalendarMonthComponent {
    return &CalendarMonthComponent{NodeID: "calendar_month_label", ViewStart: monthStart}
}

func (c *CalendarMonthComponent) Type() string { return "CalendarMonth" }

func (c *CalendarMonthComponent) Properties() map[string]interface{} {
    return map[string]interface{}{
        "node_id":    c.NodeID,
        "view_start": c.ViewStart.Format(time.RFC3339),
        "title":      "Calendar",
    }
}

func (c *CalendarMonthComponent) Actions() map[string]ui3d.Action {
    actions := make(map[string]ui3d.Action)
    prev := ui3d.NewCommandAction("button_press", ui3d.CommandDescriptor{
        Command:     "NavigateMonth",
        Arguments:   map[string]ui3d.ValueBinding{"Offset": ui3d.StaticValue(-1)},
        Description: "Previous month",
    })
    prev.Label = "Prev"
    actions["prev_month"] = prev

    next := ui3d.NewCommandAction("button_press", ui3d.CommandDescriptor{
        Command:     "NavigateMonth",
        Arguments:   map[string]ui3d.ValueBinding{"Offset": ui3d.StaticValue(1)},
        Description: "Next month",
    })
    next.Label = "Next"
    actions["next_month"] = next

    now := time.Now().UTC()
    today := ui3d.NewCommandAction("button_press", ui3d.CommandDescriptor{
        Command:   "NavigateMonth",
        Arguments: map[string]ui3d.ValueBinding{"Year": ui3d.StaticValue(now.Year()), "Month": ui3d.StaticValue(int(now.Month()))},
        Description: "Jump to current month",
    })
    today.Label = "Today"
    actions["today"] = today
    return actions
}

func (c *CalendarMonthComponent) Serialize() map[string]interface{} {
    return map[string]interface{}{
        "type":       c.Type(),
        "properties": c.Properties(),
        "actions":    c.Actions(),
    }
}

func colorForImportance(importance string) []float64 {
	switch importance {
	case ImportanceCritical:
		return []float64{0.98, 0.35, 0.35, 1.0}
	case ImportanceHigh:
		return []float64{0.99, 0.68, 0.32, 1.0}
	case ImportanceMedium:
		return []float64{0.75, 0.85, 1.0, 1.0}
	case ImportanceLow:
		return []float64{0.62, 0.9, 0.72, 1.0}
	default:
		return []float64{0.82, 0.95, 1.0, 1.0}
	}
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
