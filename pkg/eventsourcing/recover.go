package eventsourcing

import (
	"fmt"
	"log"
	"runtime/debug"
	"sync"
	"time"
)

// AsyncErrorHandler defines a function that can handle errors from goroutines
type AsyncErrorHandler func(err error, stackTrace string, eventType string, recoveryData map[string]interface{})

// ErrorRecoveryManager provides global error recovery functions for goroutines
type ErrorRecoveryManager struct {
	mu             sync.RWMutex
	errorHandlers  []AsyncErrorHandler
	recoveryCount  map[string]int
	recoveryWindow time.Duration
	maxRecoveries  int
}

// Global instance of the recovery manager
var globalRecoveryManager = NewErrorRecoveryManager(5*time.Minute, 10)

// NewErrorRecoveryManager creates a new recovery manager with the given parameters
func NewErrorRecoveryManager(window time.Duration, maxRecoveries int) *ErrorRecoveryManager {
	return &ErrorRecoveryManager{
		errorHandlers:  make([]AsyncErrorHandler, 0),
		recoveryCount:  make(map[string]int),
		recoveryWindow: window,
		maxRecoveries:  maxRecoveries,
	}
}

// RegisterErrorHandler adds an error handler function
func (rm *ErrorRecoveryManager) RegisterErrorHandler(handler AsyncErrorHandler) {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	rm.errorHandlers = append(rm.errorHandlers, handler)
}

// GetGlobalRecoveryManager returns the global recovery manager instance
func GetGlobalRecoveryManager() *ErrorRecoveryManager {
	return globalRecoveryManager
}

// Default error handler that logs the error
func defaultErrorHandler(err error, stackTrace string, eventType string, recoveryData map[string]interface{}) {
	log.Printf("RECOVERED PANIC in goroutine handling event type %s: %v\nStack trace:\n%s\nRecovery data: %v",
		eventType, err, stackTrace, recoveryData)
}

// SafeGo wraps a function to be executed in a goroutine with panic recovery
func SafeGo(eventType string, recoveryData map[string]interface{}, fn func()) {
	go func() {
		defer RecoverFromPanic(eventType, recoveryData)
		fn()
	}()
}

// RecoverFromPanic is a helper function to recover from panics in goroutines
func RecoverFromPanic(eventType string, recoveryData map[string]interface{}) {
	if r := recover(); r != nil {
		stackTrace := string(debug.Stack())
		var err error
		switch x := r.(type) {
		case string:
			err = fmt.Errorf("%s", x)
		case error:
			err = x
		default:
			err = fmt.Errorf("%v", x)
		}

		// Handle the error through registered handlers
		rm := GetGlobalRecoveryManager()
		rm.mu.RLock()
		handlers := rm.errorHandlers
		rm.mu.RUnlock()

		// If no handlers are registered, use the default
		if len(handlers) == 0 {
			defaultErrorHandler(err, stackTrace, eventType, recoveryData)
		} else {
			// Call all registered handlers
			for _, handler := range handlers {
				handler(err, stackTrace, eventType, recoveryData)
			}
		}

		// Track recovery count for this event type
		rm.trackRecovery(eventType)
	}
}

// trackRecovery counts recoveries to detect recurring problems
func (rm *ErrorRecoveryManager) trackRecovery(eventType string) {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	key := fmt.Sprintf("%s:%d", eventType, time.Now().Unix()/int64(rm.recoveryWindow.Seconds()))
	rm.recoveryCount[key]++

	// Check if we've exceeded the recovery limit
	if rm.recoveryCount[key] > rm.maxRecoveries {
		log.Printf("WARNING: Event type %s has panicked %d times in the last %v. This indicates a systemic issue.",
			eventType, rm.recoveryCount[key], rm.recoveryWindow)
	}

	// Cleanup old entries (simple approach)
	if len(rm.recoveryCount) > 1000 {
		now := time.Now().Unix() / int64(rm.recoveryWindow.Seconds())
		for k := range rm.recoveryCount {
			var keyTime int64
			fmt.Sscanf(k, "%s:%d", nil, &keyTime)
			if keyTime < now-1 {
				delete(rm.recoveryCount, k)
			}
		}
	}
}
