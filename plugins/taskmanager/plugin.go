package main

import (
	"fmt"
	"mindpalace/pkg/eventsourcing"
)

type TaskPlugin struct{}

func (p *TaskPlugin) GetCommands() map[string]eventsourcing.CommandHandler {
	return map[string]eventsourcing.CommandHandler{
		"CreateTask": CreateTaskHandler,
	}
}

func (p *TaskPlugin) GetSchemas() map[string]map[string]interface{} {
	return map[string]map[string]interface{}{}
}

func CreateTaskHandler(data map[string]interface{}, state map[string]interface{}) ([]eventsourcing.Event, error) {
	title, ok := data["Title"].(string)
	if !ok {
		return nil, fmt.Errorf("missing Title")
	}
	event := &eventsourcing.GenericEvent{
		EventType: "TaskCreated",
		Data:      map[string]interface{}{"Title": title},
	}
	return []eventsourcing.Event{event}, nil
}

func NewPlugin() eventsourcing.Plugin {
	return &TaskPlugin{}
}

func (p *TaskPlugin) GetType() eventsourcing.PluginType {
	return eventsourcing.LLMPlugin
}

func (p *TaskPlugin) GetEventHandlers() map[string]eventsourcing.EventHandler {
	return nil // No event handlers needed
}
