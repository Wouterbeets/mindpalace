package main

import (
	"fmt"
	"mindpalace/internal/eventsourcing"
	"mindpalace/internal/eventsourcing/aggregates"
	"mindpalace/internal/eventsourcing/interfaces"
	"mindpalace/internal/usecase/orchestrate"
	"mindpalace/plugins"
	"reflect"
	"time"

	"github.com/google/uuid"
)

type SendToLLMCommand struct {
	eventsourcing.BaseCommand
	Query string
}

func (c *SendToLLMCommand) CommandName() string {
	return reflect.TypeOf(c).Elem().Name()
}

func (c *SendToLLMCommand) Run(aggregate interfaces.Aggregate) (interfaces.Event, error) {
	output, err := orchestrate.NewOrchestrator().CallAgent("mindpalace", c.Query) // TODO inject dependcies somehow, or global var in orchestrate
	if err != nil {
		return nil, err
	}
	event := &SentToLLMEvent{
		BaseEvent: eventsourcing.BaseEvent{
			ID:       uuid.New().String(),
			AggID:    c.AggregateID(),
			Occurred: time.Now(),
		},
		UserQuery:   c.Query,
		LLMResponse: output,
	}
	return event, nil
}

type SentToLLMEvent struct {
	eventsourcing.BaseEvent
	UserQuery   string
	LLMResponse string
}

func (e *SentToLLMEvent) Apply(agg interfaces.Aggregate) error {
	switch a := agg.(type) {
	case *aggregates.MindPalaceAggregate:
		a.LastUserQuery = e.UserQuery
		a.UserResponse = e.LLMResponse
		return nil
	default:
		return fmt.Errorf("unsupported aggregate type: %T", agg)
	}
}

func (e *SentToLLMEvent) EventName() string {
	return reflect.TypeOf(e).Elem().Name()
}

var CommandCreator plugins.CommandCreator

func init() {
	CommandCreator = plugins.NewCommandCreator(SendToLLMCommand{})
}
