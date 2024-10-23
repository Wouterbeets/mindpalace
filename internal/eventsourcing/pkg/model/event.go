package model

import (
	"time"

	"github.com/google/uuid"
)

// Event interface represents an event in the system.
type Event interface {
	GetEventID() uuid.UUID // Returns the unique ID of the event
	DispatchAt() time.Time
}

// ScheduledTask represents a task and its scheduled time.
type ScheduledTask struct {
	ScheduledTime time.Time
	TaskFunc      func() error
	Status        string // "pending", "completed", or "cancelled"
}
