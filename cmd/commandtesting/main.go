package main

import (
	"fmt"
	"log"
	eventsourcing "mindpalace/internal/eventsourcing"
	"mindpalace/internal/eventsourcing/aggregates"
	eventstore "mindpalace/internal/eventsourcing/eventstore"
	"mindpalace/internal/eventsourcing/interfaces"
	"mindpalace/plugins"
	"reflect"
)

func main() {
	// Initialize the event store
	store, err := eventstore.NewSQLiteEventStorer("events.db")
	if err != nil {
		panic(err.Error())
	}

	loader := plugins.NewLoader()
	loader.LoadPlugins("../../plugins") // Load from the plugins directory
	defer loader.Close()                // Register event types with the event store
	es := eventsourcing.NewSource(store)

	for _, plug := range loader.Plugins() {
		symbol, err := plug.Lookup("CommandCreator")
		if err != nil {
			log.Printf("Error looking up NewCommand in plugin: %v", err)
			continue
		}

		// Assert that the symbol is of type `func() interfaces.Command`
		cc, ok := symbol.(interfaces.CommandCreator)
		if !ok {
			log.Printf("Invalid command function signature in plugin")
			continue
		}

		specs := cc.Specs()
		fmt.Println(specs)
		creationParams := map[string]interface{}{
			"BaseCommand": eventsourcing.NewBaseCommand("mindpalace-1"),
			"Query":       "What is the biggest city in the world",
		}
		fmt.Printf("params: %+v\n", creationParams)
		// Create a new command instance
		command, err := cc.Create(creationParams)
		if err != nil {
			fmt.Printf("Error creating command: %v\n", err)
			continue
		}
		store.RegisterEventType(command.CommandName(), reflect.TypeOf(command))

		// Dispatch the command
		aggregateID := "mindpalace-1"
		mindPalace := aggregates.NewMindPalaceAggregate(aggregateID)

		err = es.Dispatch(mindPalace, command)
		if err != nil {
			fmt.Printf("Error dispatching command: %v\n", err)
			continue
		}
		fmt.Println("Command dispatched and event stored successfully.")
	}
}
