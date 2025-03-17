# Codebase Overview - MindPalace

## Files and Structure

```mermaid
graph LR
    subgraph internal
        subgraph audio
            voicetranscriber((voicetranscriber.go))
        end
        subgraph ui
            theme((theme.go))
            app((app.go))
        end
        subgraph llmprocessor
            llmprocessor_file((llmprocessor.go))
            %% Renamed to avoid subgraph/file name clash
        end
        subgraph orchestration
            tool_handlers((tool_handlers.go))
        end
        subgraph core
            aggregate((aggregate.go))
            eventstore((eventstore.go))
            app_core((app.go))
            pluginmanager((pluginmanager.go))
            domain((domain.go))
        end
    end
    subgraph pkg
        subgraph eventsourcing
            types((types.go))
            eventstore_pkg((eventstore.go))
            eventbus((eventbus.go))
            recover((recover.go))
            processor((processor.go))
        end
        subgraph logging
            logger((logger.go))
        end
        subgraph llmmodels
            models((models.go))
        end
    end
    subgraph cmd
        subgraph mindpalace
            main((main.go))
        end
    end
    subgraph plugins
        subgraph userrequest
            userrequest_plugin((plugin.go))
        end
        subgraph transcriptionmanager
            transcriptionmanager_plugin((plugin.go))
        end
        subgraph taskmanager
            taskmanager_plugin((plugin.go))
        end
    end
    main --> core
    main --> ui
    main --> llmprocessor
    main --> orchestration
    main --> eventsourcing
    main --> logging
    main --> llmmodels
    main --> plugins
    core --> eventsourcing
    core --> llmmodels
    ui --> core
    ui --> eventsourcing
    ui --> audio
    ui --> logging
    llmprocessor --> eventsourcing
    llmprocessor --> llmmodels
    llmprocessor --> logging
    orchestration --> eventsourcing
    eventsourcing --> llmmodels
    audio --> pkg
    app --> theme
    userrequest_plugin --> eventsourcing
    transcriptionmanager_plugin --> eventsourcing
    taskmanager_plugin --> eventsourcing```

## Types and Hierarchy

```mermaid
classDiagram
    class Event {
        <<interface>>
        Type() string
        Marshal() ([]byte, error)
        Unmarshal(data []byte) error
    }
    class GenericEvent {
        EventType string
        Data map[string]interface{}
        Type() string
        Marshal() ([]byte, error)
        Unmarshal(data []byte) error
        DecodeData(v interface{}) error
    }
    Event <|-- GenericEvent
    class ToolCallsConfiguredEvent {
        RequestID string
        RequestText string
        Tools []Tool
        Type() string
        Marshal() ([]byte, error)
        Unmarshal(data []byte) error
    }
    Event <|-- ToolCallsConfiguredEvent
    class UserRequestReceivedEvent {
        RequestID string
        RequestText string
        Timestamp string
        Type() string
        Marshal() ([]byte, error)
        Unmarshal(data []byte) error
    }
    Event <|-- UserRequestReceivedEvent
    class AllToolCallsCompletedEvent {
        RequestID string
        Results []map[string]interface{}
        Type() string
        Marshal() ([]byte, error)
        Unmarshal(data []byte) error
    }
    Event <|-- AllToolCallsCompletedEvent
    class ToolCallInitiatedEvent {
        RequestID string
        ToolCallID string
        Function string
        Arguments map[string]interface{}
        Type() string
        Marshal() ([]byte, error)
        Unmarshal(data []byte) error
    }
    Event <|-- ToolCallInitiatedEvent
    class Aggregate {
        <<interface>>
        ID() string
        ApplyEvent(event Event) error
        GetState() map[string]interface{}
    }
    class AppAggregate {
        State map[string]interface{}
        ChatHistory []ChatMessage
        PendingToolCalls map[string][]string
        ToolCallResults map[string]map[string]interface{}
        AllCommands map[string]CommandHandler
        ID() string
        ApplyEvent(event Event) error
        GetState() map[string]interface{}
        GetPendingToolCalls() map[string][]string
        GetToolCallResults() map[string]map[string]interface{}
    }
    Aggregate <|-- AppAggregate
    class CommandProvider {
        <<interface>>
        GetAllCommands() map[string]CommandHandler
    }
    AppAggregate --|> CommandProvider
    class CommandHandler {
        <<type>>
    }
    class EventHandler {
        <<type>>
    }
    class Plugin {
        <<interface>>
        Commands() map[string]CommandHandler
        Schemas() map[string]map[string]interface{}
        Type() PluginType
        EventHandlers() map[string]EventHandler
        Name() string
    }
    class UserRequestPlugin {
        Commands() map[string]CommandHandler
        Schemas() map[string]map[string]interface{}
        Type() PluginType
        EventHandlers() map[string]EventHandler
        Name() string
    }
    Plugin <|-- UserRequestPlugin
   class TaskPlugin {
        Commands() map[string]CommandHandler
        Schemas() map[string]map[string]interface{}
        Type() PluginType
        EventHandlers() map[string]EventHandler
        Name() string
    }
    Plugin <|-- TaskPlugin
    class TranscriptionPlugin {
        Commands() map[string]CommandHandler
        Schemas() map[string]map[string]interface{}
        Type() PluginType
        EventHandlers() map[string]EventHandler
        Name() string
    }
    Plugin <|-- TranscriptionPlugin
    class EventBus {
       <<interface>>
        Publish(event Event)
		Subscribe(eventType string, handler EventHandler)
		Unsubscribe(eventType string, handler EventHandler)
		PublishStreaming(eventType string, data map[string]interface{})
		SubscribeStreaming(eventType string, handler func(eventType string, data map[string]interface{}))
		UnsubscribeStreaming(eventType string, handler func(eventType string, data map[string]interface{}))
    }
    class SimpleEventBus {
       mu sync.RWMutex
	   handlers map[string][]EventHandler
	   store EventStore
	   aggregate Aggregate
	   streamingHandlers map[string][]func(eventType string, data map[string]interface{})
        Publish(event Event)
		Subscribe(eventType string, handler EventHandler)
		Unsubscribe(eventType string, handler EventHandler)
		PublishStreaming(eventType string, data map[string]interface{})
		SubscribeStreaming(eventType string, handler func(eventType string, data map[string]interface{}))
		UnsubscribeStreaming(eventType string, handler func(eventType string, data map[string]interface{}))
    }
    EventBus <|-- SimpleEventBus
    class EventStore {
        <<interface>>
        Append(events ...Event) error
        GetEvents() []Event
        Load() error
    }
    class FileEventStore {
        mu sync.Mutex
        events []Event
        filePath string
        Append(events ...Event) error
        GetEvents() []Event
        Load() error
    }
    EventStore <|-- FileEventStore
    class LLMProcessor {
         RegisterHandlers(processor *eventsourcing.EventProcessor)
         GetSchemas() map[string]map[string]interface{}
         HandleToolCallsConfigured(event eventsourcing.Event, state map[string]interface{}, commands map[string]eventsourcing.CommandHandler) ([]eventsourcing.Event, error)
         HandleAllToolCallsCompleted(event eventsourcing.Event, state map[string]interface{}, commands map[string]eventsourcing.CommandHandler) ([]eventsourcing.Event, error)
         ProcessUserRequest(data map[string]interface{}, state map[string]interface{}) ([]eventsourcing.Event, error)

    }
    class VoiceTranscriber {
        wg sync.WaitGroup
        stream *portaudio.Stream
        audioFile *os.File
        sampleCount int
        mu sync.Mutex
        running bool
        transcriptionCallback func(string)
        sessionCallback func(eventType string, data map[string]interface{})
        cmd *exec.Cmd
        writer *bufio.Writer
        reader *bufio.Reader
        audioBuffer []float32
        wordHistory []string
        historySize int
        transcriptionText string
        transcriptionHistory []string
        sessionID string
        startTime time.Time
        totalSegments int
        Start(transcriptionCallback func(string)) error
        Stop()
        processAudio(in []float32)
        processTranscriptions()
        writeWAVHeader()
        updateWAVHeader()
    }
    class App {
        eventProcessor *eventsourcing.EventProcessor
        globalAgg      *AppAggregate
        eventChan      chan eventsourcing.Event
        pluginManager  *core.PluginManager
        commands       map[string]eventsourcing.CommandHandler
        ui             fyne.App
        stateDisplay   *widget.Entry
        eventLog       *widget.List
        eventDetail    *widget.Entry
        events         []eventsourcing.Event
        transcriber    *audio.VoiceTranscriber
        transcribing   bool
        transcriptBox  *widget.Entry
        chatHistory    *fyne.Container
        chatScroll     *container.Scroll
        tasksContainer *fyne.Container
        processEvents(events []eventsourcing.Event)
        InitUI()
        Run()
        refreshUI()
        RebuildState()
    }
```

## Code Flow

```
sequenceDiagram
    participant main as main.go
    participant core as core package
    participant ui as ui package
    participant llmprocessor as llmprocessor package
    participant orchestration as orchestration package
    participant eventsourcing as eventsourcing package
    participant plugins as plugins
    participant voicetranscriber as audio.VoiceTranscriber
    participant app_ui as ui.App
    participant llm_models as llmmodels
    participant eventbus as eventsourcing.EventBus

    main->>eventsourcing: Create FileEventStore
    main->>core: Create AppAggregate
    main->>eventsourcing: Create EventProcessor (with store and aggregate)
    main->>llmprocessor: Create LLMProcessor, RegisterHandlers(ep)
    main->>pluginmanager: Create PluginManager, LoadPlugins(pluginDir, ep)
    loop For each plugin
        pluginmanager->>eventsourcing: Register Plugin commands
    end
    main->>core: Aggregate all commands
    main->>eventsourcing: Register Event Handlers (orchestration package)
    main->>ui: Create App (ui package)
    main->>app_ui: InitUI()
    main->>app_ui: RebuildState()
    main->>app_ui: Run()

    ui->>voicetranscriber:  Start()
    voicetranscriber->>transcriptionmanager_plugin: ExecuteCommand("StartTranscription", data)
    voicetranscriber->>pkg.eventsourcing: SubmitEvent(TranscriptionStarted event)
    eventsourcing->>llmprocessor:  Handle event via  EventBus.Publish
    eventsourcing->>orchestration:  Handle event via  EventBus.Publish
    eventsourcing->>core: Update Aggregate State with event
    opt User enters text or audio
      ui->>eventsourcing:  ExecuteCommand("ReceiveRequest", data)
    end
    voicetranscriber->>pkg.eventsourcing: SubmitEvent(UserRequestReceivedEvent)
    eventsourcing->>llmprocessor:  Handle event via  EventBus.Publish
    eventsourcing->>orchestration:  Handle event via  EventBus.Publish
    eventsourcing->>core: Update Aggregate State with event
    orchestration->>llmprocessor: ExecuteCommand("ProcessUserRequest", data)
    llmprocessor->>llm_models: Build OllamaRequest with chat history and available tools
    llmprocessor->>llm_models: Call Ollama API with streaming
    loop Streamed LLM response
        llmprocessor->>pkg.eventsourcing: SubmitStreamingEvent("LLMResponseStream", data)
        llmprocessor->>ui: Update UI based on streaming data
    end
    llmprocessor->>pkg.eventsourcing: SubmitEvent(LLMProcessingCompleted event)
    eventsourcing->>core: Update Aggregate State with event
    loop For each tool call in LLM response
        eventsourcing->>orchestration: ExecuteCommand("ToolCallInitiated", data)
        plugins->>eventsourcing:  ToolCallCompleted  SubmitEvent(ToolCallCompleted event)

    end
    eventsourcing->>llmprocessor: HandleAllToolCallsCompleted(event)
```

## Component Descriptions

cmd/mindpalace/main.go: The entry point of the application. It initializes the necessary components, registers event handlers, loads plugins, and starts the UI.

internal/core:

aggregate.go: Defines the AppAggregate, which holds the application's state and implements the Aggregate interface. It also defines command execution logic.

eventstore.go: Implements the EventStore interface for persisting and retrieving events.

pluginmanager.go: Manages loading, building, and registering plugins.

domain.go: Defines domain-specific types like ChatMessage.

app.go: Older Fyne UI application structure, likely not the primary used one, but still included.

internal/ui:

app.go: Implements the Fyne-based UI application, handles user input, displays the chat history, and manages the transcription process.

theme.go: Defines a custom Fyne theme for the application's look and feel.

internal/audio:

voicetranscriber.go: Handles audio recording and real-time transcription using PortAudio and a Python script.

internal/llmprocessor:

llmprocessor.go: Handles LLM-related operations such as processing user requests, interacting with the Ollama API, and managing tool calls.

internal/orchestration:

tool_handlers.go: Defines event handlers for configuring and executing tool calls based on LLM output.

pkg/eventsourcing:

types.go: Defines interfaces and types related to event sourcing, such as Event, Aggregate, CommandHandler, EventHandler, and Plugin.

eventstore.go: Implements the FileEventStore for persisting events to a file.

eventbus.go: Implements the SimpleEventBus for publishing and subscribing to events.

processor.go: Implements the EventProcessor for handling events and commands.

recover.go: Manages goroutine panic recovery with a global error handler.

pkg/logging:

logger.go: Provides a simple logging interface with verbosity controls.

pkg/llmmodels:

models.go: Defines data structures for interacting with LLMs, such as OllamaRequest, OllamaResponse, and Tool.

plugins:

userrequest/plugin.go: Plugin for handling user requests, creating UserRequestReceivedEvent.

transcriptionmanager/plugin.go: Plugin for managing transcription sessions, creating events like TranscriptionStarted and TranscriptionStopped.

taskmanager/plugin.go: Plugin for managing tasks, providing commands for creating, updating, deleting, completing, and listing tasks.
