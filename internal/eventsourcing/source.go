package eventsourcing

import (
	"fmt"
	"mindpalace/internal/eventsourcing/interfaces"
	"time"

	"github.com/google/uuid"
)

type EventStorer interface {
	Append(aggregateID string, e interfaces.Event) error
	Load(aggregateID string) ([]interfaces.Event, error)
}

// CommandToEventGenerator is a function type that generates an event from a command
type CommandToEventGenerator func(command interfaces.Command, aggregate interfaces.Aggregate) (interfaces.Event, error)

type Source struct {
	EventStorer    EventStorer
	CommandToEvent CommandToEventGenerator
}

func NewSource(es EventStorer, generator CommandToEventGenerator) *Source {
	return &Source{
		EventStorer:    es,
		CommandToEvent: generator,
	}
}

func (s *Source) Dispatch(aggregate interfaces.Aggregate, command interfaces.Command) error {
	// Check if the command can be processed (basic validations if any)

	// Generate event from command using the injected function
	event, err := s.CommandToEvent(command, aggregate)
	if err != nil {
		return fmt.Errorf("failed to generate event from command: %w", err)
	}

	// Apply the event to the aggregate
	if err := aggregate.Apply(event); err != nil {
		return fmt.Errorf("failed to apply event: %w", err)
	}

	// Persist the event
	if err := s.EventStorer.Append(aggregate.ID(), event); err != nil {
		return fmt.Errorf("failed to append event to store: %w", err)
	}
	return nil
}

// BaseCommand provides a basic implementation of the Command interface
// Specific command types will embed this to inherit common fields
type BaseCommand struct {
	CommandID string    `json:"commandId"`
	AggID     string    `json:"aggregateId"`
	Issued    time.Time `json:"issuedAt"`
}

func (bc *BaseCommand) AggregateID() string {
	return bc.AggID
}

func (bc *BaseCommand) IssuedAt() time.Time {
	return bc.Issued
}

func NewBaseCommand(aggegateID string) BaseCommand {
	return BaseCommand{
		CommandID: uuid.New().String(),
		AggID:     aggegateID,
		Issued:    time.Now(),
	}
}

// BaseEvent provides common attributes for all events
type BaseEvent struct {
	ID       string
	AggID    string
	Occurred time.Time
	Vers     int
}

func NewBaseEvent(aggregateID string) BaseEvent {
	return BaseEvent{
		ID:       uuid.New().String(), // Automatically generate a unique event ID
		AggID:    aggregateID,
		Occurred: time.Now(),
		Vers:     1, // Start with version 1, increment if event structure changes
	}
}

func (be *BaseEvent) EventID() string       { return be.ID }
func (be *BaseEvent) AggregateID() string   { return be.AggID }
func (be *BaseEvent) OccurredAt() time.Time { return be.Occurred }
func (be *BaseEvent) Version() int          { return be.Vers }
