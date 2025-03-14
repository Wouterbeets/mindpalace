package core

import (
	"encoding/json"
	"fmt"
	"image/color"
	"log"
	"runtime"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"mindpalace/internal/audio"
	"mindpalace/pkg/eventsourcing"
)

// CustomTheme overrides the default theme to set text colors
type CustomTheme struct {
	fyne.Theme
}

func (t *CustomTheme) Color(name fyne.ThemeColorName, variant fyne.ThemeVariant) color.Color {
	switch name {
	case theme.ColorNameForeground:
		return color.White // Default text color
	default:
		return t.Theme.Color(name, variant)
	}
}

type App struct {
	eventProcessor *eventsourcing.EventProcessor
	globalAgg      *AppAggregate
	eventChan      chan eventsourcing.Event
	pluginManager  *PluginManager
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
}

func NewApp(pm *PluginManager, ep *eventsourcing.EventProcessor, agg *AppAggregate) *App {
	chatHistory := container.NewVBox()
	a := &App{
		globalAgg:      agg,
		eventProcessor: ep,
		pluginManager:  pm,
		commands:       make(map[string]eventsourcing.CommandHandler),
		ui:             app.New(),
		transcriber:    audio.NewVoiceTranscriber(),
		transcribing:   false,
		transcriptBox:  widget.NewMultiLineEntry(),
		chatHistory:    chatHistory,
		chatScroll:     container.NewScroll(chatHistory),
		eventLog: widget.NewList(
			func() int { return 0 },
			func() fyne.CanvasObject { return widget.NewLabel("Event") },
			func(id widget.ListItemID, obj fyne.CanvasObject) {},
		),
		eventDetail: widget.NewMultiLineEntry(),
		eventChan:   make(chan eventsourcing.Event, 10),
	}
	a.ui.Settings().SetTheme(&CustomTheme{theme.DefaultTheme()})
	
	// Get the event bus from the event processor
	eventBus := ep.EventBus
	
	// Subscribe to all events for UI updates using wildcard subscription
	eventBus.Subscribe("*", func(event eventsourcing.Event, state map[string]interface{}, commands map[string]eventsourcing.CommandHandler) ([]eventsourcing.Event, error) {
		// Update UI on the main thread
		fyne.CurrentApp().Driver().DoFromGoroutine(func() {
			a.refreshUI()
		}, false)
		return nil, nil
	})
	
	a.chatScroll.Direction = container.ScrollVerticalOnly
	a.chatScroll.SetMinSize(fyne.NewSize(0, 300))
	a.transcriptBox.SetPlaceHolder("Transcriptions will appear here...")
	a.eventDetail.Disable()
	a.eventDetail.SetText("Select an event to view details")

	// Register commands
	commands, _ := pm.RegisterCommands()
	a.commands = commands
	a.eventProcessor.RegisterCommands(commands)
	
	// Store commands in the aggregate so they're available to event handlers
	a.globalAgg.AllCommands = commands

	// Transcriber callback using executeCommand
	a.transcriber.SetSessionEventCallback(func(eventType string, data map[string]interface{}) {
		var cmdName string
		switch eventType {
		case "start":
			cmdName = "StartTranscription"
		case "stop":
			cmdName = "StopTranscription"
		default:
			log.Printf("Unknown event type: %s", eventType)
			return
		}
		err := a.eventProcessor.ExecuteCommand(cmdName, data)
		if err != nil {
			log.Printf("Failed to execute %s: %v", cmdName, err)
		}
	})

	return a
}

func (a *App) processEvents(events []eventsourcing.Event) {
	if len(events) == 0 {
		return
	}
	if err := a.eventProcessor.ProcessEvents(events, a.commands); err != nil {
		log.Printf("Failed to process events: %v", err)
		return
	}
	// UI refresh now happens via event bus subscription in the constructor
}

func (a *App) InitUI() {
	a.eventLog.Length = func() int {
		return len(a.events)
	}
	a.eventLog.UpdateItem = func(id widget.ListItemID, obj fyne.CanvasObject) {
		obj.(*widget.Label).SetText(a.events[id].Type())
	}
	a.eventLog.OnSelected = func(id widget.ListItemID) {
		if id < 0 || id >= len(a.events) {
			return
		}
		event := a.events[id].(*eventsourcing.GenericEvent)
		dataJSON, err := json.MarshalIndent(event.Data, "", "  ")
		if err != nil {
			a.eventDetail.SetText(fmt.Sprintf("Error marshaling event data: %v", err))
			return
		}
		a.eventDetail.SetText(fmt.Sprintf("Event Type: %s\nData:\n%s", event.EventType, string(dataJSON)))
	}
	a.eventLog.OnUnselected = func(id widget.ListItemID) {
		a.eventDetail.SetText("Select an event to view details")
	}

	stateText := widget.NewMultiLineEntry()
	stateText.SetText(fmt.Sprintf("%v", a.globalAgg.GetState()))
	stateText.Disable()
	a.stateDisplay = stateText
}

func (a *App) Run() {
	window := a.ui.NewWindow("MindPalace")

	startStopButton := widget.NewButton("Start Audio", nil)
	log.Println("Run running on goroutine:", runtime.NumGoroutine())
	startStopButton.OnTapped = func() {
		log.Println("OnTapped running on goroutine:", runtime.NumGoroutine())
		if !a.transcribing {
			// Clear the transcript box on the main thread
			fyne.CurrentApp().Driver().DoFromGoroutine(func() {
				a.transcriptBox.SetText("")
				log.Println("Cleared transcript box")
			}, false)

			// Start transcription with panic-safe callback
			err := a.transcriber.Start(func(text string) {
				if strings.TrimSpace(text) != "" {
					// Wrap UI update in panic recovery
					eventsourcing.SafeGo("TranscriptionCallback", map[string]interface{}{
						"text": text,
					}, func() {
						fyne.CurrentApp().Driver().DoFromGoroutine(func() {
							current := a.transcriptBox.Text
							if current == "" {
								a.transcriptBox.SetText(text)
							} else {
								a.transcriptBox.SetText(current + " " + text) // Use space instead of newline
							}
							log.Printf("Added text to transcript: '%s'", text)
						}, false)
					})
				}
			})
			if err != nil {
				log.Printf("Failed to start audio: %v", err)
				return
			}

			// Update button text on the main thread
			fyne.CurrentApp().Driver().DoFromGoroutine(func() {
				startStopButton.SetText("Stop Audio")
			}, false)
			a.transcribing = true
		} else {
			// Stop transcription (non-UI, assumed thread-safe)
			a.transcriber.Stop()

			// Update button text on the main thread
			fyne.CurrentApp().Driver().DoFromGoroutine(func() {
				startStopButton.SetText("Start Audio")
			}, false)
			a.transcribing = false
		}
	}
	a.transcriptBox.SetMinRowsVisible(5)

	submitButton := widget.NewButton("Submit", func() {
		// Wrap submit action in SafeGo for panic recovery
		eventsourcing.SafeGo("SubmitTranscription", nil, func() {
			log.Println("Submit button pressed")
			transcriptionText := a.transcriptBox.Text
			if transcriptionText == "" {
				log.Println("No transcription text to submit")
				return
			}
			data := map[string]interface{}{
				"RequestText": transcriptionText,
			}
			err := a.eventProcessor.ExecuteCommand("ReceiveRequest", data)
			if err != nil {
				log.Printf("Failed to execute ReceiveRequest: %v", err)
				return
			}
			
			// Use DoFromGoroutine with the UI operations
			fyne.CurrentApp().Driver().DoFromGoroutine(func() {
				a.transcriptBox.SetText("")
			}, false)
		})
	})
	mindPalaceTab := container.NewVBox(
		widget.NewLabel("MindPalace"),
		a.chatScroll,
		container.NewBorder(
			nil,
			nil,
			startStopButton,
			submitButton,
			a.transcriptBox,
		),
	)
	stateScrollable := container.NewScroll(a.stateDisplay)
	split := container.NewHSplit(a.eventLog, a.eventDetail)
	split.SetOffset(0.3)

	tabs := container.NewAppTabs(
		container.NewTabItem("MindPalace", mindPalaceTab),
		container.NewTabItem("Plugin Explorer", a.buildPluginExplorer()),
		container.NewTabItem("Command Execution", a.buildCommandExecution()),
		container.NewTabItem("State Display", stateScrollable),
		container.NewTabItem("Event Log", split),
	)

	window.SetContent(tabs)
	window.Resize(fyne.NewSize(800, 600))
	window.ShowAndRun()
}

func (a *App) buildPluginExplorer() fyne.CanvasObject {
	list := widget.NewList(
		func() int {
			return len(a.commands)
		},
		func() fyne.CanvasObject {
			return widget.NewLabel("Command")
		},
		func(i widget.ListItemID, o fyne.CanvasObject) {
			keys := make([]string, 0, len(a.commands))
			for k := range a.commands {
				keys = append(keys, k)
			}
			o.(*widget.Label).SetText(keys[i])
		},
	)
	return list
}

func (a *App) buildCommandExecution() fyne.CanvasObject {
	commandOptions := make([]string, 0, len(a.commands))
	for cmd := range a.commands {
		commandOptions = append(commandOptions, cmd)
	}
	commandDropdown := widget.NewSelect(commandOptions, func(s string) {
		log.Printf("Selected command: %s", s)
	})

	inputArea := widget.NewMultiLineEntry()
	inputArea.SetPlaceHolder("Enter command parameters in JSON format...")
	executeButton := widget.NewButton("Execute", func() {
		selectedCmd := commandDropdown.Selected
		if selectedCmd == "" {
			log.Println("No command selected")
			return
		}
		inputText := inputArea.Text
		var inputData map[string]interface{}
		if err := json.Unmarshal([]byte(inputText), &inputData); err != nil {
			log.Printf("Invalid JSON input: %v", err)
			return
		}
		err := a.eventProcessor.ExecuteCommand(selectedCmd, inputData)
		if err != nil {
			log.Printf("Failed to execute command %s: %v", selectedCmd, err)
		}
	})
	return container.NewVBox(
		widget.NewLabel("Select Command:"),
		commandDropdown,
		widget.NewLabel("Input Parameters (JSON):"),
		inputArea,
		executeButton,
	)
}

func (a *App) refreshUI() {
	log.Println("Refreshing UI in real-time")
	a.chatHistory.Objects = nil

	for _, msg := range a.globalAgg.ChatHistory {
		label := widget.NewLabel(msg.Role + ": " + msg.Content)
		label.Wrapping = fyne.TextWrapWord
		a.chatHistory.Add(label)
	}

	a.chatHistory.Refresh()
	if len(a.chatHistory.Objects) > 0 {
		a.chatScroll.ScrollToBottom()
	}

	a.stateDisplay.SetText(fmt.Sprintf("%v", a.globalAgg.GetState()))

	// Update event log with latest events
	a.events = a.eventProcessor.GetEvents()
	a.eventLog.Length = func() int { return len(a.events) }
	a.eventLog.UpdateItem = func(id widget.ListItemID, obj fyne.CanvasObject) {
		obj.(*widget.Label).SetText(a.events[id].Type())
	}
	a.eventLog.Refresh()
}
func (a *App) RebuildState() {
	a.globalAgg.State = make(map[string]interface{})
	a.globalAgg.ChatHistory = nil // Reset chat history
	a.events = a.eventProcessor.GetEvents()
	for _, event := range a.events {
		if err := a.globalAgg.ApplyEvent(event); err != nil {
			log.Printf("Failed to apply event during rebuild: %v", err)
		}
	}
	a.refreshUI()
}
