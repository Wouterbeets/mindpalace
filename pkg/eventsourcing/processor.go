package eventsourcing

import (
	"fmt"
	"mindpalace/pkg/logging"
)

type EventProcessor struct {
	store    EventStore
	commands map[string]Command
	EventBus EventBus // Changed from unexported to exported
}

func NewEventProcessor(store EventStore, eventBus EventBus) *EventProcessor {
	// Create an event bus that will be used for event distribution

	ep := &EventProcessor{
		store:    store,
		commands: make(map[string]Command),
		EventBus: eventBus,
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
	// Also register with the event bus
	ep.EventBus.Subscribe(eventType, handler)

	logging.Debug("Registered event handler for event type: %s", eventType)
}

func (ep *EventProcessor) RegisterCommands(commands map[string]Command) {
	for name, cmd := range commands {
		ep.commands[name] = cmd
	}
	logging.Debug("Registered %d commands", len(commands))
}

func (ep *EventProcessor) RegisterCommand(name string, handler Command) {
	ep.commands[name] = handler
	logging.Debug("Registered command: %s", name)
}

// Commands returns all registered commands
func (ep *EventProcessor) Commands() map[string]Command {
	return ep.commands
}

func (ep *EventProcessor) ExecuteCommand(commandName string, data map[string]interface{}) error { // TODO make this function take any as a type and do some typechecking
	// Log command execution
	logging.Command(commandName, data)

	handler, exists := ep.commands[commandName]
	if !exists {
		logging.Error("Command %s not found", commandName)
		return fmt.Errorf("command %s not found", commandName)
	}

	events, err := handler(data)
	if err != nil {
		logging.Error("Error executing command %s: %v", commandName, err)
		return err
	}

	logging.Debug("Command %s generated %d events", commandName, len(events))

	// Instead of using SubmitEvent directly, publish to the local event bus
	for _, event := range events {
		ep.EventBus.Publish(event)
	}
	return nil
}
