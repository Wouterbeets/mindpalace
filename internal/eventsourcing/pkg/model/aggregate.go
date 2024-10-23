package model

import "github.com/google/uuid"

// Aggregate provides an interface for applying events.
type Aggregate interface {
	LastAppliedEvent() uuid.UUID
	SetLastAppliedEvent(uuid.UUID)
	Apply(e Event) (generatedEvents []Event, err error) // Applies an event to the aggregate
	ID() uuid.UUID                                      // Returns the unique ID of the aggregate
	Type() string
	Update(Aggregate)
	UpdateReadModels() error
}
