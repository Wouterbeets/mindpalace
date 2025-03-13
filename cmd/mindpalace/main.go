package main

import (
	"log"
	"mindpalace/internal/core"
	"mindpalace/pkg/eventsourcing"
)

func main() {
	pluginManager := core.NewPluginManager()
	store := eventsourcing.NewFileEventStore("events.json")
	if err := store.Load(); err != nil {
		log.Printf("Failed to load events: %v", err)
	}
	agg := &eventsourcing.GlobalAggregate{State: make(map[string]interface{})}
	ep := eventsourcing.NewEventProcessor(store, agg)
	pluginManager.LoadPlugins("plugins", ep)

	app := core.NewApp(pluginManager, ep)
	app.InitUI()       // Initialize UI components
	app.RebuildState() // Now safe to rebuild state
	app.Run()
}
