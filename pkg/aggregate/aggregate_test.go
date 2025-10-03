package aggregate

import (
	"testing"

	"fyne.io/fyne/v2"
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

func (m *MockAggregate) GetCustomUI() fyne.CanvasObject {
	return nil
}

func TestNewAggregateManager(t *testing.T) {
	manager := NewAggregateManager()
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
	manager := NewAggregateManager()
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
	manager := NewAggregateManager()
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
	manager := NewAggregateManager()
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
	manager := NewAggregateManager()

	_, err := manager.AggregateByName("nonexistent")
	if err == nil {
		t.Error("Expected error for nonexistent aggregate, got nil")
	}
}

func TestAllAggregates(t *testing.T) {
	manager := NewAggregateManager()
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
	manager := NewAggregateManager()
	expectedID := "system"
	if manager.ID() != expectedID {
		t.Errorf("Expected ID %s, got %s", expectedID, manager.ID())
	}
}

func TestRebuildState(t *testing.T) {
	manager := NewAggregateManager()
	mockAgg := &MockAggregate{id: "test"}
	manager.RegisterAggregate("test", mockAgg)

	// Mock events - since ApplyEvent returns nil, no actual events needed
	events := []eventsourcing.Event{}

	err := manager.RebuildState(events)
	if err != nil {
		t.Errorf("RebuildState failed: %v", err)
	}
}
