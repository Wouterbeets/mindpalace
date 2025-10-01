package eventsourcing

import (
	"log"
	"mindpalace/pkg/logging"
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
	deltaChan             chan DeltaEnvelope
}

type AggregateStore interface {
	AllAggregates() []Aggregate
}

func NewSimpleEventBus(store EventStore, aggregateStore AggregateStore, deltaChan chan DeltaEnvelope) *SimpleEventBus {
	return &SimpleEventBus{
		store:       store,
		subscribers: make(map[string][]EventHandler),
		aggStore:    aggregateStore,
		deltaChan:   deltaChan,
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
	for _, agg := range eb.aggStore.AllAggregates() {
		err := agg.ApplyEvent(event)
		if err != nil {
			logging.Error("Apply failed for event %s, on agg %s: %v", event.Type(), agg.ID(), err)
		}
	}

	// Emit 3D deltas
	for _, agg := range eb.aggStore.AllAggregates() {
		if broadcaster, ok := agg.(ThreeDUIBroadcaster); ok {
			actions := broadcaster.Broadcast3DDelta(event)
			if len(actions) > 0 {
				select {
				case eb.deltaChan <- DeltaEnvelope{
					Type:      "delta",
					Aggregate: agg.ID(),
					EventID:   ISOTimestamp(),
					Timestamp: ISOTimestamp(),
					Actions:   actions,
				}:
				default: // Drop silently to avoid blocking
				}
			}
		}
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
