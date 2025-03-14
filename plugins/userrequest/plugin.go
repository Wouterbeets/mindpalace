package main

import (
	"fmt"
	"mindpalace/pkg/eventsourcing"
	"time"
)

type UserRequestPlugin struct{}

func (p *UserRequestPlugin) Commands() map[string]eventsourcing.CommandHandler {
	return map[string]eventsourcing.CommandHandler{
		"ReceiveRequest": ReceiveRequestHandler,
	}
}

func (p *UserRequestPlugin) Schemas() map[string]map[string]interface{} {
	return map[string]map[string]interface{}{}
}

func ReceiveRequestHandler(data map[string]interface{}, state map[string]interface{}) ([]eventsourcing.Event, error) {
	requestText, ok := data["RequestText"].(string)
	if !ok {
		return nil, fmt.Errorf("missing RequestText in command data")
	}

	event := &eventsourcing.GenericEvent{
		EventType: "UserRequestReceived",
		Data: map[string]interface{}{
			"RequestText": requestText,
			"Timestamp":   time.Now().Format(time.RFC3339),
		},
	}
	return []eventsourcing.Event{event}, nil
}

func NewPlugin() eventsourcing.Plugin {
	return &UserRequestPlugin{}
}

func (p *UserRequestPlugin) Type() eventsourcing.PluginType {
	return eventsourcing.SystemPlugin
}

func (p *UserRequestPlugin) EventHandlers() map[string]eventsourcing.EventHandler {
	return nil // No event handlers needed here
}
