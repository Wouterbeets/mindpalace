package eventsourcing

import (
	"testing"
	"time"

	"mindpalace/internal/eventsourcing/interfaces"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// Mock EventStorer for testing
type MockEventStorer struct {
	mock.Mock
}

func (m *MockEventStorer) Append(aggregateID string, e interfaces.Event) error {
	args := m.Called(aggregateID, e)
	return args.Error(0)
}

func (m *MockEventStorer) Load(aggregateID string) ([]interfaces.Event, error) {
	args := m.Called(aggregateID)
	return args.Get(0).([]interfaces.Event), args.Error(1)
}

// Mock Aggregate for testing
type MockAggregate struct {
	mock.Mock
}

func (a *MockAggregate) Apply(e interfaces.Event) error {
	args := a.Called(e)
	return args.Error(0)
}

func (a *MockAggregate) ID() string {
	args := a.Called()
	return args.String(0)
}

func (a *MockAggregate) Type() string {
	args := a.Called()
	return args.String(0)
}

// Mock Event for testing
type MockEvent struct {
	EventIDFunc     func() string
	EventNameFunc   func() string
	AggregateIDFunc func() string
	OccurredAtFunc  func() time.Time
	VersionFunc     func() int
}

func (me *MockEvent) EventID() string       { return me.EventIDFunc() }
func (me *MockEvent) EventName() string     { return me.EventNameFunc() }
func (me *MockEvent) AggregateID() string   { return me.AggregateIDFunc() }
func (me *MockEvent) OccurredAt() time.Time { return me.OccurredAtFunc() }
func (me *MockEvent) Version() int          { return me.VersionFunc() }
func (me *MockEvent) Apply(aggregate interfaces.Aggregate) error {
	return nil
}

// For testing purposes, we'll extend BaseCommand to include CommandName
type TestCommand struct {
	BaseCommand
}

func (tc *TestCommand) CommandName() string {
	return "TestCommand"
}

func (tc *TestCommand) Run(a interfaces.Aggregate) (interfaces.Event, error) {
	return &MockEvent{
		EventIDFunc:     func() string { return "event-1" },
		EventNameFunc:   func() string { return "TestEvent" },
		AggregateIDFunc: func() string { return a.ID() },
		OccurredAtFunc:  func() time.Time { return time.Now() },
		VersionFunc:     func() int { return 1 },
	}, nil
}

func TestDispatchEventGeneration(t *testing.T) {
	assert := assert.New(t)
	command := &TestCommand{BaseCommand: NewBaseCommand("aggregate-1")}
	mockAggregate := new(MockAggregate)
	mockAggregate.On("ID").Return("aggregate-1")
	mockAggregate.On("Apply", mock.Anything).Return(nil)

	mockStore := new(MockEventStorer)
	mockStore.On("Append", "aggregate-1", mock.Anything).Return(nil)

	source := NewSource(mockStore)

	err := source.Dispatch(mockAggregate, command)
	assert.NoError(err)

	mockAggregate.AssertExpectations(t)
	mockStore.AssertExpectations(t)
}

func TestDispatchEventStorage(t *testing.T) {
	assert := assert.New(t)
	mockStore := new(MockEventStorer)
	// Custom CommandToEventGenerator for testing
	mockEvent := &MockEvent{AggregateIDFunc: func() string { return "aggregate-1" }}

	source := NewSource(mockStore)

	command := &TestCommand{BaseCommand: NewBaseCommand("aggregate-1")}
	mockAggregate := &MockAggregate{}
	mockAggregate.On("ID").Return("aggregate-1")

	// Set up mock behavior for storage
	mockStore.On("Append", "aggregate-1", mockEvent).Return(nil)

	// Mock Apply function to return our mock event
	mockAggregate.On("Apply", mock.Anything).Run(func(args mock.Arguments) {
		e := args.Get(0).(interfaces.Event)
		mockEvent = e.(*MockEvent)
	}).Return(nil)

	err := source.Dispatch(mockAggregate, command)
	assert.NoError(err)
	mockStore.AssertExpectations(t)
}

func TestDispatchErrorHandling(t *testing.T) {
	assert := assert.New(t)
	mockStore := new(MockEventStorer)
	source := NewSource(mockStore)

	command := &TestCommand{BaseCommand: NewBaseCommand("aggregate-1")}
	mockAggregate := &MockAggregate{}

	err := source.Dispatch(mockAggregate, command)
	assert.Error(err)
	assert.Contains(err.Error(), "failed to generate event from command")
}
