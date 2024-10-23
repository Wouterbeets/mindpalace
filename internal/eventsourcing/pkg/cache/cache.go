package cache

import (
	"fmt"
	"sync"

	"github.com/la-clinique-e-sante/monorepo/back/domains/eventsourcing/pkg/model"
)

// InMemoryCache implements the Cache interface using an in-memory map.
type InMemoryCache struct {
	mu    sync.RWMutex
	store map[string]model.Aggregate
}

// NewInMemoryCache creates a new instance of InMemoryCache.
func NewInMemoryCache() *InMemoryCache {
	return &InMemoryCache{
		store: make(map[string]model.Aggregate),
	}
}

// AggregateFromCache retrieves an aggregate by its ID from the cache.
// Returns an error if the aggregate is not found.
func (c *InMemoryCache) AggregateFromCache(agg model.Aggregate) error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	aggregate, exists := c.store[agg.ID().String()+":"+agg.Type()]
	if !exists {
		return fmt.Errorf("aggregate not found in cache")
	}
	agg.Update(aggregate)
	return nil
}

// UpdateCache updates the cache with the provided aggregate.
func (c *InMemoryCache) UpdateCache(agg model.Aggregate) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.store[agg.ID().String()+":"+agg.Type()] = agg
	err := agg.UpdateReadModels()
	if err != nil {
		return fmt.Errorf("error updating readmodels: %w", err)
	}
	return nil
}
