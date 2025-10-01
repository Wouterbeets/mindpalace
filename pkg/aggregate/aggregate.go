package aggregate

import (
	"fmt"
	"mindpalace/pkg/eventsourcing"
	"mindpalace/pkg/logging"
)

// AggregateManager acts as a facade to manage multiple plugin aggregates.
type AggregateManager struct {
	PluginAggregates map[string]eventsourcing.Aggregate // Map of plugin name to its aggregate
	SystemAggregate  map[string]eventsourcing.Aggregate
}

// NewAggregateManager creates a new AggregateManager.
func NewAggregateManager() *AggregateManager {
	return &AggregateManager{
		PluginAggregates: make(map[string]eventsourcing.Aggregate),
		SystemAggregate:  make(map[string]eventsourcing.Aggregate),
	}
}

// RegisterPluginAggregate adds a plugin's aggregate to the manager.
func (m *AggregateManager) RegisterAggregate(name string, agg eventsourcing.Aggregate) {
	m.PluginAggregates[name] = agg
	// Merge plugin commands into the global command set
	logging.Info("Registered aggregate for plugin: %s", name)
}

func (m *AggregateManager) AggregateByName(requestedName string) (eventsourcing.Aggregate, error) {
	for pluginAggName, agg := range m.PluginAggregates {
		if requestedName == pluginAggName {
			return agg, nil
		}
	}
	for pluginAggName, agg := range m.SystemAggregate {
		if requestedName == pluginAggName {
			return agg, nil
		}
	}
	return nil, fmt.Errorf("Unable to get aggregate by name")
}

func (m *AggregateManager) AllAggregates() (aggs []eventsourcing.Aggregate) {
	for _, agg := range m.PluginAggregates {
		aggs = append(aggs, agg)
	}
	for _, agg := range m.SystemAggregate {
		aggs = append(aggs, agg)
	}
	return aggs
}

// ID returns a generic identifier for the manager (not tied to a single aggregate).
func (m *AggregateManager) ID() string {
	return "system"
}

// ApplyEvent routes the event to the appropriate plugin aggregate or handles core events.
func (m *AggregateManager) RebuildState(events []eventsourcing.Event) error {
	logging.Info("Rebuilding state for %d events across %d aggregates", len(events), len(m.AllAggregates()))
	for _, event := range events {
		logging.Debug("Applying event %s", event.Type())
		for _, agg := range m.AllAggregates() {
			err := agg.ApplyEvent(event)
			if err != nil {
				return fmt.Errorf("Failed to apply event %s: %v", event.Type(), err)
			}
		}
	}
	return nil
}
