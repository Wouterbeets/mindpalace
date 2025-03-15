package eventsourcing

import (
	"mindpalace/pkg/logging"
	"sync"
)

// EventBus interface defines methods for publishing events and subscribing to them
type EventBus interface {
	Publish(event Event)
	Subscribe(eventType string, handler EventHandler)
	Unsubscribe(eventType string, handler EventHandler)
}

// SimpleEventBus implements the EventBus interface
type SimpleEventBus struct {
	mu       sync.RWMutex
	handlers map[string][]EventHandler
	store    EventStore
	aggregate Aggregate
}

// NewSimpleEventBus creates a new SimpleEventBus
func NewSimpleEventBus(store EventStore, aggregate Aggregate) *SimpleEventBus {
	return &SimpleEventBus{
		handlers: make(map[string][]EventHandler),
		store:    store,
		aggregate: aggregate,
	}
}

// Publish sends an event to all subscribers and stores it
func (eb *SimpleEventBus) Publish(event Event) {
	// Store the event first
	if eb.store != nil {
		eb.store.Append(event)
	}

	// Apply to aggregate
	if eb.aggregate != nil {
		if err := eb.aggregate.ApplyEvent(event); err != nil {
			logging.Error("Error applying event %s to aggregate: %v", event.Type(), err)
		}
	}

	eb.mu.RLock()
	defer eb.mu.RUnlock()

	// Get handlers for this event type
	handlers := eb.handlers[event.Type()]
	
	// Also get wildcard handlers
	if wildcardHandlers, exists := eb.handlers["*"]; exists {
		handlers = append(handlers, wildcardHandlers...)
	}

	if len(handlers) > 0 {
		state := eb.aggregate.GetState()
		
		// Get all registered commands from plugins
		var allCommands map[string]CommandHandler
		if cmdProvider, ok := eb.aggregate.(CommandProvider); ok {
			allCommands = cmdProvider.GetAllCommands()
		} else {
			allCommands = make(map[string]CommandHandler)
		}
		
		for _, handler := range handlers {
			// Execute handlers in a goroutine with panic recovery
			handlerCopy := handler // Create a copy to avoid closure issues
			SafeGo(event.Type(), map[string]interface{}{
				"event_id":   event.Type(),
				"event_data": event,
			}, func() {
				newEvents, err := handlerCopy(event, state, allCommands)
				if err != nil {
					logging.Error("Error in event handler for %s: %v", event.Type(), err)
					return
				}
				// Publish any new events
				for _, newEvent := range newEvents {
					eb.Publish(newEvent)
				}
			})
		}
	}
}

// Subscribe adds a handler for a specific event type
func (eb *SimpleEventBus) Subscribe(eventType string, handler EventHandler) {
	eb.mu.Lock()
	defer eb.mu.Unlock()
	eb.handlers[eventType] = append(eb.handlers[eventType], handler)
}

// Unsubscribe removes a handler for a specific event type
func (eb *SimpleEventBus) Unsubscribe(eventType string, handler EventHandler) {
	eb.mu.Lock()
	defer eb.mu.Unlock()
	
	if handlers, exists := eb.handlers[eventType]; exists {
		for i, h := range handlers {
			// This comparison won't always work for functions, but it's a start
			if &h == &handler {
				eb.handlers[eventType] = append(handlers[:i], handlers[i+1:]...)
				break
			}
		}
	}
}