package interfaces

import "time"

// Command interface defines the basic structure that all commands must adhere to
type Command interface {
	// CommandName returns a string identifier for the command type
	CommandName() string

	// AggregateID should return the ID of the aggregate this command pertains to
	AggregateID() string

	// IssuedAt returns the time when the command was issued
	IssuedAt() time.Time
}
