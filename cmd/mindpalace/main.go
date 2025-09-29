package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"

	"mindpalace/internal/godot_ws"
	"mindpalace/internal/llmprocessor"
	"mindpalace/internal/orchestration"
	"mindpalace/internal/plugins"
	"mindpalace/internal/ui"
	"mindpalace/pkg/aggregate"
	"mindpalace/pkg/eventsourcing"
	"mindpalace/pkg/logging"
	"mindpalace/pkg/world"
)

func main() {
	// Define command-line flags
	var (
		verboseFlag  bool
		debugFlag    bool
		traceFlag    bool
		helpFlag     bool
		versionFlag  bool
		headlessFlag bool
		storagePath  string
	)

	// Parse command-line flags
	flag.BoolVar(&verboseFlag, "v", false, "Enable verbose logging (info level)")
	flag.BoolVar(&debugFlag, "debug", false, "Enable debug logging")
	flag.BoolVar(&traceFlag, "trace", false, "Enable trace logging (most detailed)")
	flag.BoolVar(&helpFlag, "help", false, "Show help information")
	flag.BoolVar(&versionFlag, "version", false, "Show version information")
	flag.BoolVar(&headlessFlag, "headless", false, "Run in headless mode (no UI, web server only)")
	flag.StringVar(&storagePath, "storage", "events.db", "Path to the events storage database")
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
		logging.SetVerbosity(logging.LogLevelInfo)
		logging.Info("MindPalace starting with minimal logging")
	}

	// Register a global error handler for goroutine panics
	eventsourcing.GetGlobalRecoveryManager().RegisterErrorHandler(func(err error, stackTrace string, eventType string, recoveryData map[string]interface{}) {
		logging.Error("RECOVERED PANIC in event '%s': %v\nContext: %v\nStack trace: %s",
			eventType, err, recoveryData, stackTrace)
	})

	// Basic setup
	store, _ := eventsourcing.NewSQLiteEventStore(storagePath)
	defer store.Close()
	aggStore := aggregate.NewAggregateManager()
	ep := eventsourcing.NewEventProcessor(store, nil)
	eb := eventsourcing.NewSimpleEventBus(store, aggStore, ep.DeltaChan())
	ep.EventBus = eb
	eventsourcing.SetGlobalEventBus(eb)
	pluginManager := plugins.NewPluginManager(ep)
	llmClient := llmprocessor.NewLLMClient()

	// Migrate from old file store if exists
	oldFilePath := "events.json"
	if _, err := os.Stat(oldFilePath); err == nil {
		oldStore := eventsourcing.NewFileEventStore(oldFilePath)
		if err := oldStore.Load(); err == nil {
			eventsourcing.MigrateFromFileToSQLite(oldStore, store)
			logging.Info("Migration completed, you can remove events.json")
		} else {
			logging.Error("Failed to load old events: %v", err)
		}
	}

	// Load events
	if err := store.Load(); err != nil {
		logging.Error("Failed to load events: %v", err)
	}
	events := store.GetEvents()
	logging.Info("Loaded %d events", len(events))

	// Register aggregates
	for _, plug := range pluginManager.GetLLMPlugins() {
		aggStore.RegisterAggregate(plug.Name(), plug.Aggregate())
	}
	orchAgg := orchestration.NewOrchestrationAggregate()
	aggStore.RegisterAggregate("orchestration", orchAgg)
	aggStore.RebuildState(events)

	// Launch Godot WebSocket server
	server := godot_ws.NewGodotServer()
	server.SetDeltaChan(ep.DeltaChan())
	server.SetAggStore(aggStore)
	go server.Start()

	// Launch embedded Godot binary
	tmpPath, err := world.ExtractToTemp()
	if err != nil {
		logging.Error("Failed to extract Godot binary: %v", err)
		os.Exit(1)
	}
	defer os.Remove(tmpPath)
	cmd := exec.Command(tmpPath)
	if err := cmd.Start(); err != nil {
		logging.Error("Failed to start Godot: %v", err)
		os.Exit(1)
	}
	logging.Info("Godot binary launched")

	// Initialize orchestrator and Fyne app
	orchestrator := orchestration.NewRequestOrchestrator(llmClient, pluginManager, orchAgg, ep, ep.EventBus)
	app := ui.NewApp(ep, aggStore, orchestrator, pluginManager.GetLLMPlugins())

	// Run Fyne UI unless headless
	if !headlessFlag {
		app.InitUI()
		app.Run()
	} else {
		select {}
	}
}
