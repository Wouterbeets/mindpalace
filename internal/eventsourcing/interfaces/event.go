package interfaces

import "time"

type Event interface {
	// EventID returns a unique identifier for the event
	EventID() string

	// EventName returns a string identifier for the event type
	EventName() string

	// AggregateID returns the ID of the aggregate this event pertains to
	AggregateID() string

	// OccurredAt returns the time when the event occurred
	OccurredAt() time.Time

	// Version might be used for versioning events if you evolve event structures
	Version() int

	// Applies the event to the aggregate, must not error on unknown aggregates
	Apply(Aggregate) error
}
