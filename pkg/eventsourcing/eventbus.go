package eventsourcing

import (
	"log"
)

type EventBus interface {
	Publish(event Event)
	Subscribe(eventType string, handler EventHandler)
	SubscribeAll(handler EventHandler)
}

type SimpleEventBus struct {
	store                 EventStore
	subscribers           map[string][]EventHandler
	allUpdatesSubscribers []EventHandler
	aggStore              AggregateStore
}

type AggregateStore interface {
	AllAggregates() []Aggregate
	ApplyEventToAllAggs(event Event) error
}

func NewSimpleEventBus(store EventStore, aggregateStore AggregateStore) *SimpleEventBus {
	return &SimpleEventBus{
		store:       store,
		subscribers: make(map[string][]EventHandler),
		aggStore:    aggregateStore,
	}
}

func (eb *SimpleEventBus) Subscribe(eventType string, handler EventHandler) {
	eb.subscribers[eventType] = append(eb.subscribers[eventType], handler)
}

func (eb *SimpleEventBus) SubscribeAll(handler EventHandler) {
	eb.allUpdatesSubscribers = append(eb.allUpdatesSubscribers, handler)
}

type EventHandler func(event Event) error

func (eb *SimpleEventBus) Publish(event Event) {
	// Persist event first
	eb.store.Append(event)

	// Apply to aggregates
	if err := eb.aggStore.ApplyEventToAllAggs(event); err != nil {
		log.Printf("error during apply to all aggs: %s, %v", event.Type(), err)
	}

	for _, handler := range eb.allUpdatesSubscribers {
		err := handler(event) // frontend updates are triggered from here
		if err != nil {
			log.Printf("EventHandler failed for event %s: %v", event.Type(), err)
		}
	}
	// Notify subscribers
	if handlers, exists := eb.subscribers[event.Type()]; exists {
		for _, handler := range handlers {
			err := handler(event) // backend updates are triggered from here
			if err != nil {
				log.Printf("EventHandlerHandler failed for event %s: %v", event.Type(), err)
			}
		}
	}
}
