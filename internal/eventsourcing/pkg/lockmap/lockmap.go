package lockmap

import (
	"sync"

	"github.com/google/uuid"
)

type LockMap struct {
	mu    sync.RWMutex
	locks map[uuid.UUID]*sync.Mutex
}

func NewLockMap() *LockMap {
	return &LockMap{
		locks: make(map[uuid.UUID]*sync.Mutex),
	}
}

func (lm *LockMap) Lock(id uuid.UUID) {
	lm.mu.Lock()

	if _, exists := lm.locks[id]; !exists {
		lm.locks[id] = &sync.Mutex{}
	}
	lm.mu.Unlock()
	lm.locks[id].Lock()
}

func (lm *LockMap) Unlock(id uuid.UUID) {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	if lock, exists := lm.locks[id]; exists {
		lock.Unlock()
	}
}
