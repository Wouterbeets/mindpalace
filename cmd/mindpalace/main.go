package main

import (
	"flag"
	"fmt"
	"mindpalace/internal/chat"
	"mindpalace/internal/llmprocessor"
	"mindpalace/internal/orchestration"
	"mindpalace/internal/plugins"
	"mindpalace/internal/transcription"
	"mindpalace/internal/ui"
	"mindpalace/internal/userrequest"
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

	// Initialize plugin manager
	pluginManager := plugins.NewPluginManager()
	logging.Debug("Plugin manager initialized")

	// Set up event store and aggregate
	store := eventsourcing.NewFileEventStore(eventFileFlag)
	if err := store.Load(); err != nil {
		logging.Error("Failed to load events: %v", err)
	}
	logging.Debug("Event store loaded from %s", eventFileFlag)

	// Create aggregate
	agg := &aggregate.AppAggregate{
		State:            make(map[string]interface{}),
		ChatHistory:      []chat.ChatMessage{},
		PendingToolCalls: make(map[string][]string),
		ToolCallResults:  make(map[string]map[string]interface{}),
		AllCommands:      make(map[string]eventsourcing.CommandHandler),
	}
	logging.Debug("Application aggregate created")

	// Create event processor
	ep := eventsourcing.NewEventProcessor(store, agg)
	logging.Debug("Event processor initialized")

	// Initialize the LLM processor as an internal component
	llmProc := llmprocessor.New()
	llmProc.RegisterHandlers(ep)
	logging.Info("LLM processor initialized")

	// Initialize the transcription manager as an internal component
	transMgr := transcription.NewTranscriptionManager()
	transMgr.RegisterHandlers(ep)
	logging.Info("Transcription manager initialized")

	// Initialize the user request manager as an internal component
	userReqMgr := userrequest.NewUserRequestManager()
	userReqMgr.RegisterHandlers(ep)
	logging.Info("User request manager initialized")

	// Load plugins (excluding LLMProcessor, TranscriptionManager, and UserRequestManager, now internal)
	pluginManager.LoadPlugins("plugins", ep)
	commands, _ := pluginManager.RegisterCommands()

	// For debugging, log the LLM processor commands
	for name := range llmProc.GetSchemas() {
		logging.Debug("Available LLM command: %s", name)
	}

	// Collect all commands into the aggregate
	allCommands := make(map[string]eventsourcing.CommandHandler)

	// Add plugin commands
	for name, handler := range commands {
		allCommands[name] = handler
	}

	// Add commands from the processor
	for name, handler := range ep.Commands() {
		allCommands[name] = handler
	}

	logging.Info("Total commands available: %d", len(allCommands))

	// Store commands in the aggregate so they're available to event handlers
	agg.AllCommands = allCommands

	// Register event handlers for orchestration
	ep.RegisterEventHandler("UserRequestReceived", orchestration.NewToolCallConfigFunc(allCommands, agg, pluginManager.GetLLMPlugins()))
	ep.RegisterEventHandler("LLMProcessingCompleted", orchestration.NewToolCallFunc(ep, agg, pluginManager.GetLLMPlugins()))
	ep.RegisterEventHandler("ToolCallInitiated", orchestration.NewToolCallExecutor(ep))
	ep.RegisterEventHandler("ToolCallCompleted", orchestration.NewToolCallCompletionHandler(ep, agg))
	logging.Debug("Event handlers registered")

	// Create and run the UI application
	logging.Info("Starting UI application")
	app := ui.NewApp(pluginManager, ep, agg)
	app.InitUI()
	logging.Debug("UI initialized")
	app.RebuildState()
	logging.Debug("Application state rebuilt")
	app.Run()
}
