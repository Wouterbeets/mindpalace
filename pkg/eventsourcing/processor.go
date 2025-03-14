package eventsourcing

import "log"

type EventProcessor struct {
	store         EventStore
	aggregate     Aggregate
	eventHandlers map[string][]EventHandler
}

func NewEventProcessor(store EventStore, aggregate Aggregate) *EventProcessor {
	return &EventProcessor{store: store, aggregate: aggregate, eventHandlers: make(map[string][]EventHandler)}
}

func (ep *EventProcessor) GetEvents() []Event {
	return ep.store.GetEvents()
}

func (ep *EventProcessor) RegisterEventHandler(eventType string, handler EventHandler) {
	ep.eventHandlers[eventType] = append(ep.eventHandlers[eventType], handler)
}

func (ep *EventProcessor) ProcessEvents(events []Event, commands map[string]CommandHandler) error {
	if err := ep.store.Append(events...); err != nil {
		return err
	}
	for _, event := range events {
		if err := ep.aggregate.ApplyEvent(event); err != nil {
			return err
		}
		// Trigger registered handlers
		if handlers, exists := ep.eventHandlers[event.Type()]; exists {
			for _, handler := range handlers {
				newEvents, err := handler(event, ep.aggregate.GetState(), commands)
				if err != nil {
					log.Printf("Error in event handler for %s: %v", event.Type(), err)
					continue // Log and continue to avoid halting on handler errors
				}
				// Process any new events returned by the handler
				if len(newEvents) > 0 {
					if err := ep.ProcessEvents(newEvents, commands); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}
