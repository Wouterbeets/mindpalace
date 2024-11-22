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

type Source struct {
	EventStorer EventStorer
}

func NewSource(es EventStorer) *Source {
	return &Source{
		EventStorer: es,
	}
}

func (s *Source) Dispatch(aggregate interfaces.Aggregate, command interfaces.Command) error {
	// Validate if the command can be processed, if necessary

	// Run the command to generate an event
	event, err := command.Run(aggregate)
	if err != nil {
		return fmt.Errorf("failed to execute command: %w", err)
	}

	err = event.Apply(aggregate)
	if err != nil {
		return fmt.Errorf("failed to apply event to aggregate: %w", err)
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

func NewBaseCommand(aggregateID string) BaseCommand {
	return BaseCommand{
		CommandID: uuid.New().String(),
		AggID:     aggregateID,
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
