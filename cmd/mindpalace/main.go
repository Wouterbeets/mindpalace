package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"mindpalace/internal/llmprocessor"
	"mindpalace/internal/orchestration"
	"mindpalace/internal/plugins"
	"mindpalace/internal/ui"
	"mindpalace/pkg/aggregate"
	"mindpalace/pkg/eventsourcing"
	"mindpalace/pkg/logging"
	"net/http"
	"os"
	"time"
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
		// Default is minimal logging (info level but with limited output)
		logging.SetVerbosity(logging.LogLevelInfo)
		logging.Info("MindPalace starting with minimal logging")
	}
	// Register a global error handler for goroutine panics
	eventsourcing.GetGlobalRecoveryManager().RegisterErrorHandler(func(err error, stackTrace string, eventType string, recoveryData map[string]interface{}) {
		logging.Error("RECOVERED PANIC in event '%s': %v\nContext: %v\nStack trace: %s",
			eventType, err, recoveryData, stackTrace)
	})
	store, _ := eventsourcing.NewSQLiteEventStore(storagePath)
	defer store.Close()
	aggStore := aggregate.NewAggregateManager()
	eb := eventsourcing.NewSimpleEventBus(store, aggStore)
	ep := eventsourcing.NewEventProcessor(store, eb)
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
	// Load events after plugins so plugin events are registered
	if err := store.Load(); err != nil {
		logging.Error("Failed to load events: %v", err)
	}

	for _, plug := range pluginManager.GetLLMPlugins() {
		aggStore.RegisterAggregate(plug.Name(), plug.Aggregate())
	}
	orchAgg := orchestration.NewOrchestrationAggregate()
	aggStore.RegisterAggregate("orchestration", orchAgg)
	aggStore.RebuildState(store.GetEvents())

	orchestrator := orchestration.NewRequestOrchestrator(llmClient, pluginManager, orchAgg, ep, ep.EventBus)
	app := ui.NewApp(ep, aggStore, orchestrator, pluginManager.GetLLMPlugins())

	tasksHandler := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		agg, err := aggStore.AggregateByName("taskmanager")
		if err != nil {
			http.Error(w, "Task aggregate not found", 500)
			return
		}
		html := agg.GetWebUI() // Direct call to new method
		fmt.Fprint(w, html)
	}

	chatHandler := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		agg, err := aggStore.AggregateByName("orchestration")
		if err != nil {
			http.Error(w, "Orchestration aggregate not found", 500)
			return
		}
		html := agg.GetWebUI()
		fmt.Fprint(w, html)
	}

	promptHandler := func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", 405)
			return
		}
		var req struct {
			Prompt string `json:"prompt"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid JSON", 400)
			return
		}
		requestID := fmt.Sprintf("req-%d", time.Now().UnixNano())
		err := ep.ExecuteCommand("ProcessUserRequest", map[string]interface{}{
			"requestText": req.Prompt,
			"requestID":   requestID,
		})
		if err != nil {
			logging.Error("Failed to process user request: %v", err)
			http.Error(w, "Failed to process request", 500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok", "request_id": requestID})
	}

	http.HandleFunc("/tasks", tasksHandler)
	http.HandleFunc("/chat", chatHandler)
	http.HandleFunc("/prompt", promptHandler)
	go func() {
		logging.Info("Starting web server on :3030")
		if err := http.ListenAndServe(":3030", nil); err != nil {
			logging.Error("Web server error: %v", err)
		}
	}()
	if !headlessFlag {
		app.InitUI()
		app.Run()
	} else {
		select {}
	}
}

// webHandler serves a basic HTMX-enabled HTML page for the web front-end
func webHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	html := `
<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>MindPalace Web</title>
    <script src="https://unpkg.com/htmx.org@1.9.10"></script>
</head>
<body>
    <h1>Welcome to MindPalace (Web)</h1>
    <p>This is the HTMX-based web interface. Migration in progress...</p>
    <nav>
        <a href="/tasks">View Tasks</a> | <a href="/chat">Chat (Coming Soon)</a>
    </nav>
    <!-- Future: Add HTMX attributes for dynamic content -->
</body>
</html>`
	fmt.Fprint(w, html)
}
