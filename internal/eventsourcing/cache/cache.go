package cache

import (
	"sync"

	"mindpalace/internal/eventsourcing/eventsourcing"
)

// InMemoryCache implements the Cache interface using an in-memory map.
type InMemoryCache struct {
	mu    sync.RWMutex
	store map[string]eventsourcing.Aggregate
}

// NewInMemoryCache creates a new instance of InMemoryCache.
func NewInMemoryCache() *InMemoryCache {
	return &InMemoryCache{
		store: make(map[string]eventsourcing.Aggregate),
	}
}

// AggregateFromCache retrieves an aggregate by its ID from the cache.
// Returns an error if the aggregate is not found.
func (c *InMemoryCache) AggregateFromCache(agg eventsourcing.Aggregate) (eventsourcing.Aggregate, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	aggregate, exists := c.store[agg.ID()]
	if !exists {
		return agg, nil // on cache miss return orginal agg
	}
	return aggregate, nil
}

// UpdateCache updates the cache with the provided aggregate.
func (c *InMemoryCache) UpdateCache(agg eventsourcing.Aggregate) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.store[agg.ID()] = agg
	return nil
}
