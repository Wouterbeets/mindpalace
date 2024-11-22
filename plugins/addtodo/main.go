package main

import (
	"fmt"
	"mindpalace/internal/eventsourcing"
	"mindpalace/internal/eventsourcing/aggregates"
	"mindpalace/internal/eventsourcing/interfaces"
	"mindpalace/plugins"
	"reflect"
	"time"

	"github.com/google/uuid"
)

type AddTask struct {
	eventsourcing.BaseCommand
	Task string
}

func (c *AddTask) CommandName() string {
	return reflect.TypeOf(c).Elem().Name()
}

func (c *AddTask) Run(aggregate interfaces.Aggregate) (interfaces.Event, error) {
	event := &TaskAddedEvent{
		BaseEvent: eventsourcing.BaseEvent{
			ID:       uuid.New().String(),
			AggID:    c.AggregateID(),
			Occurred: time.Now(),
		},
		Task: c.Task,
	}
	return event, nil
}

type TaskAddedEvent struct {
	eventsourcing.BaseEvent
	Task string
}

func (e *TaskAddedEvent) EventName() string {
	return reflect.TypeOf(e).Elem().Name()
}

func (e *TaskAddedEvent) Apply(agg interfaces.Aggregate) error {
	switch a := agg.(type) {
	case *aggregates.MindPalaceAggregate:
		a.Tasks = append(a.Tasks, e.Task)
		return nil
	default:
		return fmt.Errorf("unsupported aggregate type: %T", agg)
	}
}

var CommandCreator plugins.CommandCreator

func init() {
	CommandCreator = plugins.NewCommandCreator(AddTask{})
}
