package main

import (
	"log"
	"mindpalace/internal/core"
)

func main() {
	eventStore := core.NewEventStore("events.json")
	if err := eventStore.Load(); err != nil {
		log.Fatal("Failed to load event store:", err)
	}

	pluginManager := core.NewPluginManager()
	pluginManager.LoadPlugins("plugins")

	app := core.NewApp(eventStore, pluginManager)
	app.InitUI()       // Initialize UI components
	app.RebuildState() // Now safe to rebuild state
	app.Run()
}
