package main

import (
	"log"
	"mindpalace/internal/core"
	"mindpalace/internal/orchestration"
	"mindpalace/internal/ui"
	"mindpalace/pkg/eventsourcing"
)

func main() {
	// Register a global error handler for goroutine panics
	eventsourcing.GetGlobalRecoveryManager().RegisterErrorHandler(func(err error, stackTrace string, eventType string, recoveryData map[string]interface{}) {
		log.Printf("RECOVERED PANIC in event '%s': %v\nContext: %v\nStack trace: %s", 
			eventType, err, recoveryData, stackTrace)
	})

	// Initialize plugin manager
	pluginManager := core.NewPluginManager()
	
	// Set up event store and aggregate
	store := eventsourcing.NewFileEventStore("events.json")
	if err := store.Load(); err != nil {
		log.Printf("Failed to load events: %v", err)
	}
	
	// Create aggregate
	agg := &core.AppAggregate{
		State:            make(map[string]interface{}),
		ChatHistory:      []core.ChatMessage{},
		PendingToolCalls: make(map[string][]string),
		ToolCallResults:  make(map[string]map[string]interface{}),
		AllCommands:      make(map[string]eventsourcing.CommandHandler),
	}
	
	// Create event processor
	ep := eventsourcing.NewEventProcessor(store, agg)
	
	// Load plugins
	pluginManager.LoadPlugins("plugins", ep)
	commands, _ := pluginManager.RegisterCommands()
	
	// Store commands in the aggregate so they're available to event handlers
	agg.AllCommands = commands
	
	// Register event handlers for orchestration
	ep.RegisterEventHandler("UserRequestReceived", orchestration.NewToolCallConfigFunc(commands, agg, pluginManager.GetLLMPlugins()))
	ep.RegisterEventHandler("LLMProcessingCompleted", orchestration.NewToolCallFunc(ep, agg, pluginManager.GetLLMPlugins()))
	ep.RegisterEventHandler("ToolCallInitiated", orchestration.NewToolCallExecutor(ep))
	ep.RegisterEventHandler("ToolCallCompleted", orchestration.NewToolCallCompletionHandler(ep, agg))
	
	// Create and run the UI application
	app := ui.NewApp(pluginManager, ep, agg)
	app.InitUI()
	app.RebuildState()
	app.Run()
}
