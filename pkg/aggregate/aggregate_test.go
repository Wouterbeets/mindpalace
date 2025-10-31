package aggregate

import (
	"testing"

	"mindpalace/pkg/eventsourcing"
)

// MockAggregate is a simple mock implementation of eventsourcing.Aggregate for testing
type MockAggregate struct {
	id string
}

func (m *MockAggregate) ID() string {
	return m.id
}

func (m *MockAggregate) ApplyEvent(event eventsourcing.Event) error {
	return nil
}

func (m *MockAggregate) EmitDelta(event eventsourcing.Event) *eventsourcing.DeltaEnvelope {
	return nil
}

type ResettableMockAggregate struct {
	id         string
	resetCount int
}

func (m *ResettableMockAggregate) ID() string {
	return m.id
}

func (m *ResettableMockAggregate) ApplyEvent(event eventsourcing.Event) error {
	return nil
}

func (m *ResettableMockAggregate) EmitDelta(event eventsourcing.Event) *eventsourcing.DeltaEnvelope {
	return nil
}

func (m *ResettableMockAggregate) Reset() {
	m.resetCount++
}

func TestNewAggregateManager(t *testing.T) {
	deltaChan := make(chan eventsourcing.DeltaEnvelope)
	ackChan := make(chan int)
	manager := NewAggregateManager(deltaChan, ackChan)
	if manager == nil {
		t.Fatal("NewAggregateManager returned nil")
	}
	if manager.PluginAggregates == nil {
		t.Error("PluginAggregates map not initialized")
	}
	if manager.SystemAggregate == nil {
		t.Error("SystemAggregate map not initialized")
	}
}

func TestRegisterAggregate(t *testing.T) {
	deltaChan := make(chan eventsourcing.DeltaEnvelope)
	ackChan := make(chan int)
	manager := NewAggregateManager(deltaChan, ackChan)
	mockAgg := &MockAggregate{id: "test_plugin"}

	manager.RegisterAggregate("test_plugin", mockAgg)

	if len(manager.PluginAggregates) != 1 {
		t.Errorf("Expected 1 plugin aggregate, got %d", len(manager.PluginAggregates))
	}
	if manager.PluginAggregates["test_plugin"] != mockAgg {
		t.Error("Registered aggregate not found or incorrect")
	}
}

func TestAggregateByName_Plugin(t *testing.T) {
	deltaChan := make(chan eventsourcing.DeltaEnvelope)
	ackChan := make(chan int)
	manager := NewAggregateManager(deltaChan, ackChan)
	mockAgg := &MockAggregate{id: "test_plugin"}
	manager.RegisterAggregate("test_plugin", mockAgg)

	agg, err := manager.AggregateByName("test_plugin")
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if agg != mockAgg {
		t.Error("Returned aggregate is not the expected one")
	}
}

func TestAggregateByName_System(t *testing.T) {
	deltaChan := make(chan eventsourcing.DeltaEnvelope)
	ackChan := make(chan int)
	manager := NewAggregateManager(deltaChan, ackChan)
	mockAgg := &MockAggregate{id: "test_system"}
	manager.SystemAggregate["test_system"] = mockAgg

	agg, err := manager.AggregateByName("test_system")
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if agg != mockAgg {
		t.Error("Returned aggregate is not the expected one")
	}
}

func TestAggregateByName_NotFound(t *testing.T) {
	deltaChan := make(chan eventsourcing.DeltaEnvelope)
	ackChan := make(chan int)
	manager := NewAggregateManager(deltaChan, ackChan)

	_, err := manager.AggregateByName("nonexistent")
	if err == nil {
		t.Error("Expected error for nonexistent aggregate, got nil")
	}
}

func TestAllAggregates(t *testing.T) {
	deltaChan := make(chan eventsourcing.DeltaEnvelope)
	ackChan := make(chan int)
	manager := NewAggregateManager(deltaChan, ackChan)
	mockAgg1 := &MockAggregate{id: "plugin1"}
	mockAgg2 := &MockAggregate{id: "plugin2"}
	mockAgg3 := &MockAggregate{id: "system1"}

	manager.RegisterAggregate("plugin1", mockAgg1)
	manager.RegisterAggregate("plugin2", mockAgg2)
	manager.SystemAggregate["system1"] = mockAgg3

	aggs := manager.AllAggregates()
	if len(aggs) != 3 {
		t.Errorf("Expected 3 aggregates, got %d", len(aggs))
	}

	// Check that all aggregates are present
	found := make(map[string]bool)
	for _, agg := range aggs {
		found[agg.ID()] = true
	}
	if !found["plugin1"] || !found["plugin2"] || !found["system1"] {
		t.Error("Not all aggregates found in AllAggregates result")
	}
}

func TestID(t *testing.T) {
	deltaChan := make(chan eventsourcing.DeltaEnvelope)
	ackChan := make(chan int)
	manager := NewAggregateManager(deltaChan, ackChan)
	expectedID := "system"
	if manager.ID() != expectedID {
		t.Errorf("Expected ID %s, got %s", expectedID, manager.ID())
	}
}

func TestRebuildState(t *testing.T) {
	deltaChan := make(chan eventsourcing.DeltaEnvelope, 1)
	ackChan := make(chan int, 1)
	manager := NewAggregateManager(deltaChan, ackChan)
	mockAgg := &MockAggregate{id: "test"}
	manager.RegisterAggregate("test", mockAgg)

	go func() {
		env := <-deltaChan
		ackChan <- env.SequenceID
	}()

	// Mock events - since ApplyEvent returns nil, no actual events needed
	events := []eventsourcing.Event{}

	err := manager.RebuildState(events)
	if err != nil {
		t.Errorf("RebuildState failed: %v", err)
	}
}

func TestRebuildStateResetsAggregates(t *testing.T) {
	deltaChan := make(chan eventsourcing.DeltaEnvelope, 1)
	ackChan := make(chan int, 1)
	manager := NewAggregateManager(deltaChan, ackChan)
	resetAgg := &ResettableMockAggregate{id: "reset"}
	manager.RegisterAggregate("reset", resetAgg)

	go func() {
		env := <-deltaChan
		ackChan <- env.SequenceID
	}()

	err := manager.RebuildState(nil)
	if err != nil {
		t.Fatalf("RebuildState failed: %v", err)
	}
	if resetAgg.resetCount != 1 {
		t.Errorf("Expected reset to be called once, got %d", resetAgg.resetCount)
	}
}
