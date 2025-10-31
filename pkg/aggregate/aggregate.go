package aggregate

import (
	"fmt"
	"time"

	"mindpalace/pkg/eventsourcing"
	"mindpalace/pkg/logging"
)

// AggregateManager acts as a facade to manage multiple plugin aggregates.
type AggregateManager struct {
	PluginAggregates map[string]eventsourcing.Aggregate // Map of plugin name to its aggregate
	SystemAggregate  map[string]eventsourcing.Aggregate
	deltaChan        chan eventsourcing.DeltaEnvelope
	ackChan          chan int
}

// NewAggregateManager creates a new AggregateManager.
func NewAggregateManager(deltaChan chan eventsourcing.DeltaEnvelope, ackChan chan int) *AggregateManager {
	return &AggregateManager{
		PluginAggregates: make(map[string]eventsourcing.Aggregate),
		SystemAggregate:  make(map[string]eventsourcing.Aggregate),
		deltaChan:        deltaChan,
		ackChan:          ackChan,
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

func (m *AggregateManager) ApplyEventToAllAggs(event eventsourcing.Event) error {
	for _, agg := range m.AllAggregates() {
		err := agg.ApplyEvent(event)
		if err != nil {
			logging.Error("Apply failed for event %s, on agg %s: %v", event.Type(), agg.ID(), err)
		}

		// Emit delta if needed
		if envelope := agg.EmitDelta(event); envelope != nil {
			if err := m.broadcastEnvelope(envelope); err != nil {
				logging.Error("Failed to broadcast envelope for aggregate %s: %v", agg.ID(), err)
			}
		}
	}
	return nil
}

func (m *AggregateManager) broadcastEnvelope(envelope *eventsourcing.DeltaEnvelope) error {
	if envelope == nil {
		return nil
	}
	if envelope.SequenceID == 0 {
		envelope.SequenceID = eventsourcing.NextSequenceID()
	}
	logging.Debug("AGGREGATE: Sending envelope: type=%s, aggregate=%s, sequence=%d, actions=%d", envelope.Type, envelope.Aggregate, envelope.SequenceID, len(envelope.Actions))
	select {
	case m.deltaChan <- *envelope:
		logging.Debug("AGGREGATE: Envelope sent to channel successfully")
	case <-time.After(10 * time.Second):
		return fmt.Errorf("timeout sending envelope for aggregate %s", envelope.Aggregate)
	}

	logging.Debug("AGGREGATE: Waiting for ack")
	select {
	case ackSeq := <-m.ackChan:
		if ackSeq != envelope.SequenceID {
			logging.Error("ACK sequence mismatch: expected %d, got %d", envelope.SequenceID, ackSeq)
		}
		logging.Debug("AGGREGATE: ack received for sequence %d", envelope.SequenceID)
	case <-time.After(5 * time.Second):
		return fmt.Errorf("ack timeout for sequence %d", envelope.SequenceID)
	}
	return nil
}

// ApplyEvent routes the event to the appropriate plugin aggregate or handles core events.
func (m *AggregateManager) RebuildState(events []eventsourcing.Event) error {
	logging.Debug("Rebuilding state for %d events across %d aggregates", len(events), len(m.AllAggregates()))

	for _, agg := range m.AllAggregates() {
		if resettable, ok := agg.(eventsourcing.ResettableAggregate); ok {
			logging.Debug("Resetting aggregate %s prior to replay", agg.ID())
			resettable.Reset()
		}
	}

	// Notify frontend to clear existing objects before replay
	resetEnvelope := &eventsourcing.DeltaEnvelope{
		Type:      "signal",
		Aggregate: "system",
		EventID:   eventsourcing.ISOTimestamp(),
		Timestamp: eventsourcing.ISOTimestamp(),
		StateSummary: map[string]interface{}{
			"signal": "reset_scene",
		},
	}
	if err := m.broadcastEnvelope(resetEnvelope); err != nil {
		logging.Error("Failed to broadcast reset signal: %v", err)
	}

	for i, event := range events {
		logging.Debug("Applied %d / %d events", i, len(events))
		logging.Debug("Applying event %s", event.Type())
		if err := m.ApplyEventToAllAggs(event); err != nil {
			return err
		}
	}
	logging.Debug("RebuildState completed")
	return nil
}
