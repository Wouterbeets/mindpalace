package interfaces

import (
	"reflect"
	"time"
)

// CommandCreator helps with dynamic command creation
type CommandCreator interface {
	Specs() map[string]reflect.Type
	Create(map[string]interface{}) (Command, error)
	Name() string
}

// Command interface defines the basic structure that all commands must adhere to
type Command interface {
	// CommandName returns a string identifier for the command type
	CommandName() string

	// AggregateID should return the ID of the aggregate this command pertains to
	AggregateID() string

	// IssuedAt returns the time when the command was issued
	IssuedAt() time.Time

	// Run executes the command and generates the associated event
	Run(aggregate Aggregate) (Event, error)
}
