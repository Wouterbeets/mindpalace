package eventsourcing

import (
	"fmt"
	"mindpalace/pkg/logging"
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
	
	logging.Trace("Event processor created with event bus")
	return ep
}

func (ep *EventProcessor) GetEvents() []Event {
	events := ep.store.GetEvents()
	logging.Trace("Retrieved %d events from store", len(events))
	return events
}

func (ep *EventProcessor) RegisterEventHandler(eventType string, handler EventHandler) {
	// Store in local map for backward compatibility
	ep.eventHandlers[eventType] = append(ep.eventHandlers[eventType], handler)
	
	// Also register with the event bus
	ep.EventBus.Subscribe(eventType, handler)
	
	logging.Debug("Registered event handler for event type: %s", eventType)
}

func (ep *EventProcessor) RegisterCommands(commands map[string]CommandHandler) {
	for name, cmd := range commands {
		ep.commands[name] = cmd
	}
	logging.Debug("Registered %d commands", len(commands))
}

func (ep *EventProcessor) RegisterCommand(name string, handler CommandHandler) {
	ep.commands[name] = handler
	logging.Debug("Registered command: %s", name)
}

// Commands returns all registered commands
func (ep *EventProcessor) Commands() map[string]CommandHandler {
	return ep.commands
}

func (ep *EventProcessor) ProcessEvents(events []Event, commands map[string]CommandHandler) error {
	// Instead of direct processing, publish to the event bus
	// This lets the bus handle event storage, aggregate updates, and handler distribution
	for _, event := range events {
		// Log event at appropriate level
		if genericEvent, ok := event.(*GenericEvent); ok {
			logging.Event(genericEvent.EventType, genericEvent.Data)
		} else {
			logging.Event(event.Type(), nil)
		}
		
		ep.EventBus.Publish(event)
	}
	logging.Trace("Processed %d events", len(events))
	return nil
}

func (ep *EventProcessor) ExecuteCommand(commandName string, data map[string]interface{}) error {
	// Log command execution
	logging.Command(commandName, data)
	
	handler, exists := ep.commands[commandName]
	if !exists {
		logging.Error("Command %s not found", commandName)
		return fmt.Errorf("command %s not found", commandName)
	}
	
	events, err := handler(data, ep.aggregate.GetState())
	if err != nil {
		logging.Error("Error executing command %s: %v", commandName, err)
		return err
	}
	
	logging.Debug("Command %s generated %d events", commandName, len(events))
	
	// Instead of using SubmitEvent directly, publish to the local event bus
	for _, event := range events {
		// Log event at appropriate level
		if genericEvent, ok := event.(*GenericEvent); ok {
			logging.Event(genericEvent.EventType, genericEvent.Data)
		} else {
			logging.Event(event.Type(), nil)
		}
		
		ep.EventBus.Publish(event)
	}
	return nil
}
