package eventsourcing

import (
	"log"
	"sync"

	"github.com/google/uuid"
	"github.com/la-clinique-e-sante/monorepo/back/domains/eventsourcing/pkg/model"
)

type InMemoryEventStore struct {
	events map[uuid.UUID][]model.Event
	mu     sync.RWMutex
}

func NewInMemoryEventStore() *InMemoryEventStore {
	return &InMemoryEventStore{
		events: make(map[uuid.UUID][]model.Event),
	}
}

func (s *InMemoryEventStore) Events(aggregateID uuid.UUID, lastID uuid.UUID) ([]model.Event, error) {
	log.Println("Fetching events for aggregate ID:", aggregateID)
	s.mu.RLock()
	defer s.mu.RUnlock()

	events, ok := s.events[aggregateID]
	if !ok {
		log.Println("No events found for aggregate ID:", aggregateID)
	} else {
		log.Printf("Found %d events for aggregate ID: %s\n", len(events), aggregateID)
	}
	return events, nil
}

func (s *InMemoryEventStore) IsAlreadyApplied(eventID uuid.UUID) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, events := range s.events {
		for _, event := range events {
			if event.GetEventID() == eventID {
				return true
			}
		}
	}

	return false
}

func (s *InMemoryEventStore) Append(aggregateID uuid.UUID, e model.Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.events[aggregateID] = append(s.events[aggregateID], e)

	return nil
}
