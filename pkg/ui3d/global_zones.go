package ui3d

import "sync"

// GlobalZones holds the dynamically calculated zones for plugins, set on startup
var (
	globalZones   map[string][]float64
	globalZonesMu sync.RWMutex
)

// SetGlobalZones sets the global zones map (called once on startup)
func SetGlobalZones(zones map[string][]float64) {
	globalZonesMu.Lock()
	defer globalZonesMu.Unlock()
	globalZones = zones
}

// GetGlobalZones returns a copy of the global zones map
func GetGlobalZones() map[string][]float64 {
	globalZonesMu.RLock()
	defer globalZonesMu.RUnlock()
	if globalZones == nil {
		return make(map[string][]float64)
	}
	result := make(map[string][]float64)
	for k, v := range globalZones {
		result[k] = v
	}
	return result
}
