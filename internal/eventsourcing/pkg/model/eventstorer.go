package model

import "github.com/google/uuid"

// EventStorer is an interface that handles the storage of events.
type EventStorer interface {
	Events(aggregateID uuid.UUID, fromEvent uuid.UUID) ([]Event, error) // Returns all events of an aggregate
	IsAlreadyApplied(event uuid.UUID) bool                              // Checks if the event is already applied
	Append(aggregateID uuid.UUID, e Event) error                        // Appends a new event to the aggregate
}
