package userrequest

import (
	"fmt"
	"mindpalace/pkg/eventsourcing"
	"mindpalace/pkg/logging"
	"time"
)

// UserRequestManager manages user request-related functionality
type UserRequestManager struct {
	// Add fields if state tracking is needed in the future
}

// NewUserRequestManager creates a new UserRequestManager instance
func NewUserRequestManager() *UserRequestManager {
	return &UserRequestManager{}
}

// RegisterHandlers registers user request commands with the event processor
func (urm *UserRequestManager) RegisterHandlers(ep *eventsourcing.EventProcessor) {
	ep.RegisterCommand("ReceiveRequest", urm.ReceiveRequestHandler)
}

// ReceiveRequestHandler processes incoming user requests
func (urm *UserRequestManager) ReceiveRequestHandler(data map[string]interface{}, state map[string]interface{}) ([]eventsourcing.Event, error) {
	requestText, ok := data["RequestText"].(string)
	if !ok {
		return nil, fmt.Errorf("missing RequestText in command data")
	}

	// Generate a request ID if not provided
	requestID, _ := data["RequestID"].(string)
	if requestID == "" {
		requestID = fmt.Sprintf("req-%d", time.Now().UnixNano())
	}

	// Create a typed event
	event := &eventsourcing.UserRequestReceivedEvent{
		RequestID:   requestID,
		RequestText: requestText,
		Timestamp:   time.Now().Format(time.RFC3339),
	}
	logging.Trace("userRequestReceived event created, returning it, %+v", event)
	return []eventsourcing.Event{event}, nil
}
