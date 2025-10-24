package ui3d

import (
	"math"
	"sync"
)

// Zone represents a plugin zone with angular position, radius, and grid dimensions for positioning
type Zone struct {
	Angle     float64 `json:"angle"`
	Radius    float64 `json:"radius"`
	GridRows  int     `json:"grid_rows"`
	GridCols  int     `json:"grid_cols"`
	GridDepth int     `json:"grid_depth"`
}

// ToPosition translates relative grid coordinates (relX, relY, relZ) to absolute 3D world position within the zone
func (z *Zone) ToPosition(relX, relY, relZ int) []float64 {
	// Assume zone count is 3 for now (calendar, taskmanager, plugingenerator) - can be passed or calculated
	zoneCount := 3.0
	sectorWidth := 360.0 / zoneCount // degrees per zone

	// Sector bounds
	startAngle := z.Angle - sectorWidth/2.0

	// Grid steps
	angleStep := sectorWidth / float64(z.GridCols)
	radiusStep := z.Radius / float64(z.GridDepth) // Depth along radius
	heightStep := 2.0                             // Arbitrary height per row

	// Calculate position
	currentAngle := startAngle + float64(relX)*angleStep
	currentRadius := float64(relZ) * radiusStep
	y := float64(relY) * heightStep

	x := currentRadius * math.Cos(currentAngle*math.Pi/180.0)
	z_pos := currentRadius * math.Sin(currentAngle*math.Pi/180.0)

	return []float64{x, y, z_pos}
}

// GlobalZones holds the dynamically calculated zones for plugins, set on startup
var (
	globalZones   map[string]Zone
	globalZonesMu sync.RWMutex
)

// SetGlobalZones sets the global zones map (called once on startup)
func SetGlobalZones(zones map[string]Zone) {
	globalZonesMu.Lock()
	defer globalZonesMu.Unlock()
	globalZones = zones
}

// GetGlobalZones returns a copy of the global zones map
func GetGlobalZones() map[string]Zone {
	globalZonesMu.RLock()
	defer globalZonesMu.RUnlock()
	if globalZones == nil {
		return make(map[string]Zone)
	}
	result := make(map[string]Zone)
	for k, v := range globalZones {
		result[k] = v
	}
	return result
}
