package eventsourcing

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// Mock EventStorer for testing
type MockEventStorer struct {
	mock.Mock
}

func (m *MockEventStorer) Append(aggregateID string, e Event) error {
	args := m.Called(aggregateID, e)
	return args.Error(0)
}

func (m *MockEventStorer) Load(aggregateID string) ([]Event, error) {
	args := m.Called(aggregateID)
	return args.Get(0).([]Event), args.Error(1)
}

// Mock Aggregate for testing
type MockAggregate struct {
	mock.Mock
}

func (a *MockAggregate) Apply(e Event) error {
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

// For testing purposes, we'll extend BaseCommand to include CommandName
type TestCommand struct {
	BaseCommand
}

func (tc *TestCommand) CommandName() string {
	return "TestCommand"
}

func TestDispatchEventGeneration(t *testing.T) {
	assert := assert.New(t)
	command := &TestCommand{BaseCommand: NewBaseCommand("aggregate-1")}
	mockAggregate := new(MockAggregate)
	mockAggregate.On("ID").Return("aggregate-1")
	mockAggregate.On("Apply", mock.Anything).Return(nil)

	// Custom CommandToEventGenerator for testing
	generator := func(c Command, a Aggregate) (Event, error) {
		return &MockEvent{
			EventIDFunc:     func() string { return "event-1" },
			EventNameFunc:   func() string { return "TestEvent" },
			AggregateIDFunc: func() string { return a.ID() },
			OccurredAtFunc:  func() time.Time { return time.Now() },
			VersionFunc:     func() int { return 1 },
		}, nil
	}

	mockStore := new(MockEventStorer)
	mockStore.On("Append", "aggregate-1", mock.Anything).Return(nil)

	source := NewSource(mockStore, generator)
	// Assuming the event generation succeeds
	err := source.Dispatch(mockAggregate, command)
	assert.NoError(err)

	// Check if Apply was called with the generated event
	mockAggregate.On("Apply", mock.AnythingOfType("eventsourcing.Event")).Return(nil)
}

func TestDispatchEventStorage(t *testing.T) {
	assert := assert.New(t)
	mockStore := new(MockEventStorer)
	// Custom CommandToEventGenerator for testing
	mockEvent := &MockEvent{AggregateIDFunc: func() string { return "aggregate-1" }}
	generator := func(c Command, a Aggregate) (Event, error) {
		return mockEvent, nil
	}

	source := NewSource(mockStore, generator)

	command := &TestCommand{BaseCommand: NewBaseCommand("aggregate-1")}
	mockAggregate := &MockAggregate{}
	mockAggregate.On("ID").Return("aggregate-1")

	// Set up mock behavior for storage
	mockStore.On("Append", "aggregate-1", mockEvent).Return(nil)

	// Mock Apply function to return our mock event
	mockAggregate.On("Apply", mock.Anything).Run(func(args mock.Arguments) {
		e := args.Get(0).(Event)
		mockEvent = e.(*MockEvent)
	}).Return(nil)

	err := source.Dispatch(mockAggregate, command)
	assert.NoError(err)
	mockStore.AssertExpectations(t)
}

func TestDispatchErrorHandling(t *testing.T) {
	assert := assert.New(t)
	mockStore := new(MockEventStorer)
	source := NewSource(mockStore, func(command Command, aggregate Aggregate) (Event, error) {
		return nil, errors.New("generation failed")
	})

	command := &TestCommand{BaseCommand: NewBaseCommand("aggregate-1")}
	mockAggregate := &MockAggregate{}

	err := source.Dispatch(mockAggregate, command)
	assert.Error(err)
	assert.Contains(err.Error(), "failed to generate event from command")
}
