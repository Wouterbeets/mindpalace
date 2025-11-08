package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"mindpalace/internal/audio"
	"mindpalace/internal/entities"
	"mindpalace/internal/godot_ws"
	"mindpalace/internal/llmprocessor"
	"mindpalace/internal/nightscorer"
	"mindpalace/internal/orchestration"
	"mindpalace/internal/plugins"
	"mindpalace/internal/testapi"
	"mindpalace/pkg/aggregate"
	"mindpalace/pkg/eventsourcing"
	"mindpalace/pkg/logging"
	"mindpalace/pkg/modellib"
	"mindpalace/pkg/ui3d"
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
		inference    string
		model        string
		shimmyPort   int
		apiAddr      string
	)

	// Parse command-line flags
	flag.BoolVar(&verboseFlag, "v", false, "Enable verbose logging (info level)")
	flag.BoolVar(&debugFlag, "debug", false, "Enable debug logging")
	flag.BoolVar(&traceFlag, "trace", false, "Enable trace logging (most detailed)")
	flag.BoolVar(&helpFlag, "help", false, "Show help information")
	flag.BoolVar(&versionFlag, "version", false, "Show version information")
	flag.BoolVar(&headlessFlag, "headless", false, "Run in headless mode (no UI, web server only)")
	flag.StringVar(&storagePath, "storage", "events.db", "Path to the events storage database")
	flag.StringVar(&inference, "inference", "ollama", "Inference engine: ollama or shimmy")
	flag.StringVar(&model, "model", "gpt-oss:20b", "Model name")
	flag.IntVar(&shimmyPort, "shimmy-port", 11435, "Port for Shimmy server")
	flag.StringVar(&apiAddr, "api-addr", ":8092", "Address for the testing HTTP API (set empty to disable)")
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
		logging.SetVerbosity(logging.LogLevelError)
		logging.Info("MindPalace starting with minimal logging")
	}

	// Register a global error handler for goroutine panics
	eventsourcing.GetGlobalRecoveryManager().RegisterErrorHandler(func(err error, stackTrace string, eventType string, recoveryData map[string]interface{}) {
		logging.Error("RECOVERED PANIC in event '%s': %v\nContext: %v\nStack trace: %s",
			eventType, err, recoveryData, stackTrace)
	})

	// Basic setup
	store, err := eventsourcing.NewSQLiteEventStore(storagePath)
	if err != nil {
		logging.Error("Failed to create event store: %v", err)
		os.Exit(1)
	}
	defer store.Close()

	// Create command and control channels
	commandChan := make(chan eventsourcing.CommandData, 10)
	controlChan := make(chan string, 10)
	ackChan := make(chan int, 10)
	deltaChan := make(chan eventsourcing.DeltaEnvelope, 10)

	aggManager := aggregate.NewAggregateManager(deltaChan, ackChan)
	ep := eventsourcing.NewEventProcessor(store, nil)
	eb := eventsourcing.NewSimpleEventBus(store, aggManager)
	ep.EventBus = eb
	eventsourcing.SetGlobalEventBus(eb)

	var (
		worldRoot    string
		modelCatalog *modellib.Catalog
	)
	if root, err := filepath.Abs("world"); err != nil {
		logging.Info("MODEL: unable to resolve world directory: %v", err)
	} else {
		worldRoot = root
		if catalog, err := modellib.LoadCatalog(worldRoot); err != nil {
			logging.Info("MODEL: no baked catalog found: %v", err)
		} else {
			modelCatalog = catalog
			logging.Info("MODEL: loaded catalog with %d entries", len(modelCatalog.Entries()))
		}
	}

	pluginManager := plugins.NewPluginManager(ep, modelCatalog)

	// Set global zones for plugins
	ui3d.SetGlobalZones((&ui3d.LayoutManager{}).CalculateDynamicZones(plugins.PluginNames))

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
		aggManager.RegisterAggregate(plug.Name(), plug.Aggregate())
	}
	orchAgg := orchestration.NewOrchestrationAggregate()
	orchAgg.SetChannels(deltaChan, ackChan)
	if modelCatalog != nil {
		orchAgg.SetModelLibrary(modelCatalog)
	} else if worldRoot == "" {
		logging.Info("MODEL: no model catalog configured")
	}
	aggManager.RegisterAggregate("orchestration", orchAgg)

	// Entities/world aggregate for placing models via UI
	entitiesAgg := entities.NewAggregate()
	if modelCatalog != nil {
		entitiesAgg.SetModelLibrary(modelCatalog)
	}
	aggManager.RegisterAggregate("entities", entitiesAgg)

	// Log registered aggregates
	allAggs := aggManager.AllAggregates()
	logging.Info("Registered %d aggregates:", len(allAggs))
	for _, agg := range allAggs {
		logging.Info("  - Aggregate: %s", agg.ID())
	}

	// Create LLM client
	llmClient := llmprocessor.NewLLMClient(inference, model, shimmyPort)
	logging.Info("Using %s inference (model: %s)", inference, model)
	pluginManager.InjectLLMClient(llmClient)

	// Create and start orchestrator
	ro := orchestration.NewRequestOrchestrator(llmClient, pluginManager, orchAgg, ep, eb, commandChan, controlChan, aggManager, events)
	ro.Start()

	if apiAddr != "" {
		testAPIServer := testapi.NewServer(apiAddr, ep, pluginManager, ro)
		testAPIServer.Start()
	}

	// Register system commands for purge and nightly scoring
	type NightlyScoringInput struct {
		Aggregate string `json:"Aggregate"`
		ScoreName string `json:"ScoreName"`
		Label     string `json:"Label"`
	}
	ep.RegisterCommand("RunNightlyScoring", eventsourcing.NewCommand(func(input *NightlyScoringInput) ([]eventsourcing.Event, error) {
		aggName := input.Aggregate
		if aggName == "" {
			aggName = "taskmanager"
		}
		name := input.ScoreName
		if name == "" {
			name = "relevance"
		}
		label := input.Label
		if label == "" {
			label = "Task List"
		}
		logging.Info("NIGHT: Running scoring replay on aggregate='%s' name='%s' label='%s'", aggName, name, label)
		events := ep.GetEvents()
		if err := nightscorer.RunScoringReplay(aggManager, events, aggName, llmClient, name, label); err != nil {
			return nil, fmt.Errorf("night scoring failed: %w", err)
		}
		return nil, nil
	}))

	// Command: PlaceEntity — places a model from the catalog at a position
	ep.RegisterCommand("PlaceEntity", eventsourcing.NewCommand(func(input map[string]interface{}) ([]eventsourcing.Event, error) {
		conv := func(v interface{}) float64 {
			switch t := v.(type) {
			case float64:
				return t
			case float32:
				return float64(t)
			case int:
				return float64(t)
			case int64:
				return float64(t)
			case int32:
				return float64(t)
			default:
				return 0
			}
		}
		modelIDRaw, ok := input["ModelID"]
		if !ok {
			return nil, fmt.Errorf("ModelID is required")
		}
		modelID, ok := modelIDRaw.(string)
		if !ok || modelID == "" {
			return nil, fmt.Errorf("ModelID must be a non-empty string")
		}

		// Parse position
		var pos []float64
		if p, ok := input["Position"].([]interface{}); ok && len(p) >= 3 {
			pos = []float64{conv(p[0]), conv(p[1]), conv(p[2])}
		} else if p2, ok := input["Position"].([]float64); ok && len(p2) >= 3 {
			pos = []float64{p2[0], p2[1], p2[2]}
		} else {
			pos = []float64{0, 0, 0}
		}

		// Optional rotation
		var rot []float64
		if r, ok := input["Rotation"].([]interface{}); ok && len(r) >= 3 {
			rot = []float64{conv(r[0]), conv(r[1]), conv(r[2])}
		} else if r2, ok := input["Rotation"].([]float64); ok && len(r2) >= 3 {
			rot = []float64{r2[0], r2[1], r2[2]}
		}

		// Optional scale
		var scale []float64
		if s, ok := input["Scale"].([]interface{}); ok && len(s) >= 3 {
			scale = []float64{conv(s[0]), conv(s[1]), conv(s[2])}
		} else if s2, ok := input["Scale"].([]float64); ok && len(s2) >= 3 {
			scale = []float64{s2[0], s2[1], s2[2]}
		}

		label, _ := input["Label"].(string)
		entityID, _ := input["EntityID"].(string)
		if entityID == "" {
			entityID = fmt.Sprintf("entity_%d", eventsourcing.GenerateUniqueID())
		}

		return []eventsourcing.Event{
			&entities.EntityPlacedEvent{
				EntityID: entityID,
				ModelID:  modelID,
				Position: pos,
				Rotation: rot,
				Scale:    scale,
				Label:    label,
			},
		}, nil
	}))

	ep.RegisterCommand("PurgeAllData", eventsourcing.NewCommand(func(_ *struct{}) ([]eventsourcing.Event, error) {
		logging.Info("SYSTEM: Purging all events and resetting state")
		// store is already a *SQLiteEventStore; call DeleteAll directly
		if err := store.DeleteAll(); err != nil {
			return nil, fmt.Errorf("purge failed: %w", err)
		}
		// Reset runtime state & notify UI
		if err := aggManager.RebuildState([]eventsourcing.Event{}); err != nil {
			logging.Error("Failed to rebuild state after purge: %v", err)
		}
		return nil, nil
	}))

	// Initialize voice transcriber with Whisper model
	modelPath, _ := filepath.Abs("models/ggml-base.en.bin")
	logging.Info("AUDIO: Initializing voice transcriber with model: %s", modelPath)
	transcriber, err := audio.NewVoiceTranscriber(modelPath)
	if err != nil {
		logging.Error("Failed to initialize voice transcriber: %v", err)
		os.Exit(1)
	}
	defer transcriber.Close()

	// Launch Godot WebSocket server
	server := godot_ws.NewGodotServer()
	server.SetDeltaChan(deltaChan)
	server.SetTranscriber(transcriber)
	server.SetCommandChan(commandChan)
	server.SetControlChan(controlChan)
	server.SetAckChan(ackChan)
	server.SetEventProcessor(ep)

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
			logging.Debug("[Godot] %s", scanner.Text())
		}
		if err := scanner.Err(); err != nil {
			logging.Error("Error reading Godot stdout: %v", err)
		}
	}()

	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			logging.Debug("[Godot] %s", scanner.Text())
		}
		if err := scanner.Err(); err != nil {
			logging.Error("Error reading Godot stderr: %v", err)
		}
	}()

	// Aggregate state will be rebuilt after frontend sends ready signal

	select {}
}
