package eventsourcing

import (
	"fmt"
	"mindpalace/pkg/logging"
)

type CommandHandler interface {
	Execute(data any) ([]Event, error)
}

type Command[T any] struct {
	handler func(data T) ([]Event, error)
}

type CommandInput interface {
	// New creates a new instance of the input struct
	New() any
	// Schema returns the JSON schema for LLM compatibility (optional, for backwards compatibility)
	Schema() map[string]interface{}
}

func NewCommand[T any](handler func(T) ([]Event, error)) Command[T] {
	return Command[T]{handler: handler}
}

func (c Command[T]) Execute(data any) ([]Event, error) {
	typedData, ok := data.(T)
	if !ok {
		return nil, fmt.Errorf("expected %T, got %T", *new(T), data)
	}
	return c.handler(typedData)
}

type EventProcessor struct {
	store    EventStore
	commands map[string]CommandHandler
	EventBus EventBus // Changed from unexported to exported
}

func NewEventProcessor(store EventStore, eventBus EventBus) *EventProcessor {
	ep := &EventProcessor{
		store:    store,
		commands: make(map[string]CommandHandler),
		EventBus: eventBus,
	}
	SetGlobalEventBus(eventBus)
	logging.Trace("Event processor created with event bus")
	return ep
}

func (ep *EventProcessor) GetEvents() []Event {
	events := ep.store.GetEvents()
	logging.Trace("Retrieved %d events from store", len(events))
	return events
}

func (ep *EventProcessor) RegisterCommand(name string, handler CommandHandler) {
	ep.commands[name] = handler
	logging.Debug("Registered command: %s", name)
}

func (ep *EventProcessor) ExecuteCommand(commandName string, data any) error {
	logging.Command(commandName, data)
	handler, exists := ep.commands[commandName]
	if !exists {
		logging.Error("Command %s not found", commandName)
		return fmt.Errorf("command %s not found", commandName)
	}
	events, err := handler.Execute(data)
	if err != nil {
		logging.Error("Error executing command %s: %v", commandName, err)
		return err
	}
	logging.Debug("Command %s generated %d events", commandName, len(events))
	for _, event := range events {
		marsh, _ := event.Marshal()
		logging.Debug("Pugblishing event %s, %s", event.Type(), marsh)
		ep.EventBus.Publish(event)
	}
	return nil
}
