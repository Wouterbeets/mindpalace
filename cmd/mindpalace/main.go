package main

import (
	"flag"
	"fmt"
	"mindpalace/internal/chat"
	"mindpalace/internal/llmprocessor"
	"mindpalace/internal/orchestration"
	"mindpalace/internal/plugins"
	"mindpalace/internal/ui"
	"mindpalace/pkg/aggregate"
	"mindpalace/pkg/eventsourcing"
	"mindpalace/pkg/logging"
	"os"
)

func main() {
	// Define command-line flags
	var (
		verboseFlag   bool
		debugFlag     bool
		traceFlag     bool
		helpFlag      bool
		versionFlag   bool
		eventFileFlag string
	)

	// Parse command-line flags
	flag.BoolVar(&verboseFlag, "v", false, "Enable verbose logging (info level)")
	flag.BoolVar(&debugFlag, "debug", false, "Enable debug logging")
	flag.BoolVar(&traceFlag, "trace", false, "Enable trace logging (most detailed)")
	flag.BoolVar(&helpFlag, "help", false, "Show help information")
	flag.BoolVar(&versionFlag, "version", false, "Show version information")
	flag.StringVar(&eventFileFlag, "events", "events.json", "Path to the events storage file")
	flag.Parse()

	// Show help if requested
	if helpFlag {
		fmt.Println("MindPalace - An event-sourced AI assistant")
		fmt.Println("\nUsage:")
		fmt.Println("  mindpalace [options]")
		fmt.Println("\nOptions:")
		flag.PrintDefaults()
		os.Exit(0)
	}

	// Show version if requested
	if versionFlag {
		fmt.Println("MindPalace Version 0.2.0")
		os.Exit(0)
	}

	// Set up logging level based on flags
	if traceFlag {
		logging.SetVerbosity(logging.LogLevelTrace)
		logging.Info("Trace logging enabled")
	} else if debugFlag {
		logging.SetVerbosity(logging.LogLevelDebug)
		logging.Info("Debug logging enabled")
	} else if verboseFlag {
		logging.SetVerbosity(logging.LogLevelInfo)
		logging.Info("Verbose logging enabled")
	} else {
		// Default is minimal logging (info level but with limited output)
		logging.SetVerbosity(logging.LogLevelInfo)
		logging.Info("MindPalace starting with minimal logging")
	}
	// Register a global error handler for goroutine panics
	eventsourcing.GetGlobalRecoveryManager().RegisterErrorHandler(func(err error, stackTrace string, eventType string, recoveryData map[string]interface{}) {
		logging.Error("RECOVERED PANIC in event '%s': %v\nContext: %v\nStack trace: %s",
			eventType, err, recoveryData, stackTrace)
	})
	pluginManager := plugins.NewPluginManager()
	store := eventsourcing.NewFileEventStore(eventFileFlag)
	if err := store.Load(); err != nil {
		logging.Error("Failed to load events: %v", err)
	}
	agg := &aggregate.AppAggregate{
		State:       make(map[string]interface{}),
		ChatHistory: []chat.ChatMessage{},
		AllCommands: make(map[string]eventsourcing.CommandHandler),
	}
	ep := eventsourcing.NewEventProcessor(store, agg)
	llmClient := llmprocessor.NewLLMClient()
	orchestrator := orchestration.NewRequestOrchestrator(llmClient, pluginManager, agg, ep.EventBus)

	// Load plugins and register commands
	pluginManager.LoadPlugins("plugins", ep)
	commands, _ := pluginManager.RegisterCommands()
	for name, handler := range commands {
		agg.AllCommands[name] = handler
	}

	// UI setup
	app := ui.NewApp(ep, agg, orchestrator)

	app.InitUI()
	app.Run()
}
