package main

import (
	"flag"
	"fmt"
	"mindpalace/internal/llmprocessor"
	"mindpalace/internal/orchestration"
	"mindpalace/internal/plugins"
	"mindpalace/internal/ui"
	"mindpalace/pkg/aggregate"
	"mindpalace/pkg/eventsourcing"
	"mindpalace/pkg/logging"
	"mindpalace/plugins/taskmanager"
	"net/http"
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
	store := eventsourcing.NewFileEventStore(eventFileFlag)
	aggStore := aggregate.NewAggregateManager()
	eb := eventsourcing.NewSimpleEventBus(store, aggStore)
	ep := eventsourcing.NewEventProcessor(store, eb)
	pluginManager := plugins.NewPluginManager(ep)
	llmClient := llmprocessor.NewLLMClient()

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

	// Define handlers as closures to access aggStore
	tasksHandler := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		agg, err := aggStore.AggregateByName("taskmanager")
		if err != nil {
			http.Error(w, "Task aggregate not found", 500)
			return
		}
		taskAgg, ok := agg.(*taskmanager.TaskAggregate)
		if !ok {
			http.Error(w, "Invalid aggregate type", 500)
			return
		}

		// Safely read tasks
		taskAgg.Mu.RLock()
		tasks := make([]*taskmanager.Task, 0, len(taskAgg.Tasks))
		for _, t := range taskAgg.Tasks {
			tasks = append(tasks, t)
		}
		taskAgg.Mu.RUnlock()

		// Render HTML for tasks
		html := `
<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Tasks - MindPalace Web</title>
    <script src="https://unpkg.com/htmx.org@1.9.10"></script>
</head>
<body>
    <h1>Tasks</h1>
    <button hx-get="/tasks" hx-target="#task-list">Refresh Tasks</button>
    <div id="task-list">
`
		if len(tasks) == 0 {
			html += "<p>No tasks available.</p>"
		} else {
			html += "<ul>"
			for _, task := range tasks {
				html += fmt.Sprintf("<li><strong>%s</strong> - %s (Status: %s, Priority: %s)</li>", task.Title, task.Description, task.Status, task.Priority)
			}
			html += "</ul>"
		}
		html += `
    </div>
    <a href="/">Back to Home</a>
</body>
</html>`
		fmt.Fprint(w, html)
	}

	// Start the web server in a goroutine on port 3030
	go func() {
		http.HandleFunc("/", webHandler)
		http.HandleFunc("/tasks", tasksHandler)
		logging.Info("Starting web server on :3030")
		if err := http.ListenAndServe(":3030", nil); err != nil {
			logging.Error("Web server error: %v", err)
		}
	}()

	app.InitUI()
	app.Run()
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
