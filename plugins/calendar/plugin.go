package main

import (
	"encoding/json"
	"fmt"
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

func (p *CalendarPlugin) AgentModel() string {
	return "gpt-oss:20b" // Using the general-purpose model for calendar management
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
		lm := &ui3d.LayoutManager{
			Type:    "linear",
			Spacing: 2.0,
			Zone:    "calendar",
			Zones:   map[string][]float64{"calendar": {20, 2, -8}},
			Counter: i + 1,
		}
		pos := lm.NextPosition()
		builder := ui3d.NewDeltaBuilder(theme)
		labelPos := []float64{pos[0], pos[1] + 1.0, pos[2]}
		builder.CreateBox(fmt.Sprintf("calendar_event_%s", e.EventID), pos).WithExtra(map[string]interface{}{
			"event_type": "calendar_event_created",
		})
		builder.CreateLabel(fmt.Sprintf("calendar_event_%s_label", e.EventID), e.Title, labelPos).WithExtra(map[string]interface{}{
			"event_type": "calendar_event_created",
		})
		return &eventsourcing.DeltaEnvelope{
			Type:      "delta",
			Aggregate: "calendar",
			EventID:   eventsourcing.ISOTimestamp(),
			Timestamp: eventsourcing.ISOTimestamp(),
			Actions:   builder.Build(),
		}
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
		lm := &ui3d.LayoutManager{
			Type:    "linear",
			Spacing: 2.0,
			Zone:    "calendar",
			Zones:   map[string][]float64{"calendar": {20, 2, -8}},
			Counter: i + 1,
		}
		pos := lm.NextPosition()
		builder := ui3d.NewDeltaBuilder(theme)
		labelPos := []float64{pos[0], pos[1] + 1.0, pos[2]}
		// Delete old
		builder.Delete(fmt.Sprintf("calendar_event_%s", e.EventID)).Delete(fmt.Sprintf("calendar_event_%s_label", e.EventID))
		// Create new
		builder.CreateBox(fmt.Sprintf("calendar_event_%s", e.EventID), pos).WithExtra(map[string]interface{}{
			"event_type": "calendar_event_updated",
		})
		builder.CreateLabel(fmt.Sprintf("calendar_event_%s_label", e.EventID), e.Title, labelPos).WithExtra(map[string]interface{}{
			"event_type": "calendar_event_updated",
		})
		return &eventsourcing.DeltaEnvelope{Type: "delta", Aggregate: "calendar", EventID: eventsourcing.ISOTimestamp(), Timestamp: eventsourcing.ISOTimestamp(), Actions: builder.Build()}
	case *EventDeletedEvent:
		builder := ui3d.NewDeltaBuilder(theme)
		builder.Delete(fmt.Sprintf("calendar_event_%s", e.EventID)).Delete(fmt.Sprintf("calendar_event_%s_label", e.EventID))
		return &eventsourcing.DeltaEnvelope{Type: "delta", Aggregate: "calendar", EventID: eventsourcing.ISOTimestamp(), Timestamp: eventsourcing.ISOTimestamp(), Actions: builder.Build()}
	}
	return nil
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
