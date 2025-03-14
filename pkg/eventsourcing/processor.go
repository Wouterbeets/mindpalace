package eventsourcing

import (
	"fmt"
)

type EventProcessor struct {
	store         EventStore
	aggregate     Aggregate
	eventHandlers map[string][]EventHandler
	commands      map[string]CommandHandler
	EventBus      EventBus  // Changed from unexported to exported
}

func NewEventProcessor(store EventStore, aggregate Aggregate) *EventProcessor {
	// Create an event bus that will be used for event distribution
	eventBus := NewSimpleEventBus(store, aggregate)
	
	ep := &EventProcessor{
		store:         store,
		aggregate:     aggregate,
		eventHandlers: make(map[string][]EventHandler),
		commands:      make(map[string]CommandHandler),
		EventBus:      eventBus,
	}
	
	// Set the global event bus
	SetGlobalEventBus(eventBus)
	
	return ep
}

func (ep *EventProcessor) GetEvents() []Event {
	return ep.store.GetEvents()
}

func (ep *EventProcessor) RegisterEventHandler(eventType string, handler EventHandler) {
	// Store in local map for backward compatibility
	ep.eventHandlers[eventType] = append(ep.eventHandlers[eventType], handler)
	
	// Also register with the event bus
	ep.EventBus.Subscribe(eventType, handler)
}

func (ep *EventProcessor) RegisterCommands(commands map[string]CommandHandler) {
	ep.commands = commands
}

func (ep *EventProcessor) ProcessEvents(events []Event, commands map[string]CommandHandler) error {
	// Instead of direct processing, publish to the event bus
	// This lets the bus handle event storage, aggregate updates, and handler distribution
	for _, event := range events {
		ep.EventBus.Publish(event)
	}
	return nil
}

func (ep *EventProcessor) ExecuteCommand(commandName string, data map[string]interface{}) error {
	handler, exists := ep.commands[commandName]
	if !exists {
		return fmt.Errorf("command %s not found", commandName)
	}
	events, err := handler(data, ep.aggregate.GetState())
	if err != nil {
		return err
	}
	
	// Instead of using SubmitEvent directly, publish to the local event bus
	for _, event := range events {
		ep.EventBus.Publish(event)
	}
	return nil
}
