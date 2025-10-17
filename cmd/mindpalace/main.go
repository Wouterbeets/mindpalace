package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"mindpalace/internal/audio"
	"mindpalace/internal/godot_ws"
	"mindpalace/internal/llmprocessor"
	"mindpalace/internal/orchestration"
	"mindpalace/internal/plugins"
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

	// Register 3D UI events before loading
	eventsourcing.RegisterEvent("ui_Create3DObject", func() eventsourcing.Event { return &eventsourcing.Create3DObjectEvent{} })
	eventsourcing.RegisterEvent("ui_Update3DObject", func() eventsourcing.Event { return &eventsourcing.Update3DObjectEvent{} })
	eventsourcing.RegisterEvent("ui_Delete3DObject", func() eventsourcing.Event { return &eventsourcing.Delete3DObjectEvent{} })
	eventsourcing.RegisterEvent("ui_Position3DObject", func() eventsourcing.Event { return &eventsourcing.Position3DObjectEvent{} })

	// Basic setup
	store, err := eventsourcing.NewSQLiteEventStore(storagePath)
	if err != nil {
		logging.Error("Failed to create event store: %v", err)
		os.Exit(1)
	}
	defer store.Close()
	aggStore := aggregate.NewAggregateManager()
	ep := eventsourcing.NewEventProcessor(store, nil)
	eb := eventsourcing.NewSimpleEventBus(store, aggStore)
	ep.EventBus = eb
	eventsourcing.SetGlobalEventBus(eb)
	pluginManager := plugins.NewPluginManager(ep)

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

	// Create command and control channels
	commandChan := make(chan godot_ws.Command, 10)
	controlChan := make(chan string, 10)
	ackChan := make(chan int, 10)
	deltaChan := make(chan eventsourcing.DeltaEnvelope, 10)

	// Register aggregates
	for _, plug := range pluginManager.GetLLMPlugins() {
		aggStore.RegisterAggregate(plug.Name(), plug.Aggregate())
	}
	orchAgg := orchestration.NewOrchestrationAggregate()
	orchAgg.SetChannels(deltaChan, ackChan)
	aggStore.RegisterAggregate("orchestration", orchAgg)
	uiAgg := aggregate.NewThreeDUIManagerAggregate()
	uiAgg.SetEventBus(eb)
	aggStore.RegisterAggregate("ui_manager", uiAgg)

	// Log registered aggregates
	allAggs := aggStore.AllAggregates()
	logging.Info("Registered %d aggregates:", len(allAggs))
	for _, agg := range allAggs {
		logging.Info("  - Aggregate: %s", agg.ID())
	}

	// Create real LLM client (Ollama)
	llmClient := llmprocessor.NewLLMClient()

	// Create and start orchestrator
	ro := orchestration.NewRequestOrchestrator(llmClient, pluginManager, orchAgg, ep, eb, commandChan, controlChan, aggStore, events)
	ro.Start()

	// Initialize voice transcriber with Whisper model
	modelPath, _ := filepath.Abs("models/ggml-base.en.bin")
	logging.Info("AUDIO: Initializing voice transcriber with model: %s", modelPath)
	transcriber, err := audio.NewVoiceTranscriber(modelPath)
	if err != nil {
		logging.Error("Failed to initialize voice transcriber: %v", err)
		os.Exit(1)
	}
	defer transcriber.Close()

	// Set up transcriber session event callback
	transcriber.SetSessionEventCallback(func(eventType string, data map[string]interface{}) {
		var cmdName string
		switch eventType {
		case "start":
			cmdName = "StartTranscription"
		case "stop":
			cmdName = "StopTranscription"
		default:
			logging.Error("Unknown event type: %s", eventType)
			return
		}
		err := ep.ExecuteCommand(cmdName, data)
		if err != nil {
			logging.Error("Failed to execute %s: %v", cmdName, err)
		}
	})

	// Launch Godot WebSocket server
	server := godot_ws.NewGodotServer()
	server.SetDeltaChan(deltaChan)
	server.SetTranscriber(transcriber)
	server.SetCommandChan(commandChan)
	server.SetControlChan(controlChan)
	server.SetAckChan(ackChan)

	// Start the voice transcriber (for processing, without auto-capture)
	err = transcriber.Start(func(text string) {
		logging.Info("AUDIO: Transcription result: '%s'", text)
		// Send transcription to Godot for display
		server.SendTranscription(text)
	})
	if err != nil {
		logging.Error("Failed to start voice transcriber: %v", err)
		os.Exit(1)
	}

	go server.Start()

	// Launch embedded Godot binary
	tmpPath, err := world.ExtractToTemp()
	if err != nil {
		logging.Error("Failed to extract Godot binary: %v", err)
		os.Exit(1)
	}
	defer os.Remove(tmpPath)
	cmd := exec.Command(tmpPath, "--fullscreen")

	// Capture stdout and stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		logging.Error("Failed to get stdout pipe: %v", err)
		os.Exit(1)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		logging.Error("Failed to get stderr pipe: %v", err)
		os.Exit(1)
	}

	if err := cmd.Start(); err != nil {
		logging.Error("Failed to start Godot: %v", err)
		os.Exit(1)
	}
	logging.Info("Godot binary launched")

	// Pipe Godot logs to our logging system
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			logging.Info("[Godot] %s", scanner.Text())
		}
		if err := scanner.Err(); err != nil {
			logging.Error("Error reading Godot stdout: %v", err)
		}
	}()

	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			logging.Info("[Godot] %s", scanner.Text())
		}
		if err := scanner.Err(); err != nil {
			logging.Error("Error reading Godot stderr: %v", err)
		}
	}()

	// Aggregate state will be rebuilt after frontend sends ready signal

	select {}
}
