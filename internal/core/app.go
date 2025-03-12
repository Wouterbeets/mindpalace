package core

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"mindpalace/pkg/eventsourcing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

type App struct {
	eventStore    *EventStore
	pluginManager *PluginManager
	commands      map[string]eventsourcing.CommandHandler
	ui            fyne.App
	globalAgg     *eventsourcing.GlobalAggregate
	stateDisplay  *widget.Entry
	eventLog      *widget.List
	eventDetail   *widget.Entry
	eventHandlers map[string][]eventsourcing.EventHandler
	events        []eventsourcing.Event
	transcriber   *VoiceTranscriber
	transcribing  bool
	transcriptBox *widget.Entry
}

func NewApp(es *EventStore, pm *PluginManager) *App {
	a := &App{
		eventStore:    es,
		pluginManager: pm,
		commands:      make(map[string]eventsourcing.CommandHandler),
		eventHandlers: make(map[string][]eventsourcing.EventHandler),
		ui:            app.New(),
		globalAgg:     &eventsourcing.GlobalAggregate{State: make(map[string]interface{})},
		events:        []eventsourcing.Event{},
		transcriber:   NewVoiceTranscriber(),
		transcribing:  false,
		transcriptBox: widget.NewMultiLineEntry(),
		eventLog: widget.NewList(
			func() int { return 0 },
			func() fyne.CanvasObject { return widget.NewLabel("Event") },
			func(id widget.ListItemID, obj fyne.CanvasObject) {},
		),
		eventDetail: widget.NewMultiLineEntry(),
	}
	a.transcriptBox.SetPlaceHolder("Transcriptions will appear here...")
	a.eventDetail.Disable()
	a.eventDetail.SetText("Select an event to view details")

	// Register commands and event handlers
	commands, eventHandlers := pm.RegisterCommands()
	a.commands = commands
	a.eventHandlers = eventHandlers

	// Existing transcriber callback
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
		handler, exists := a.commands[cmdName]
		if !exists {
			log.Printf("Command %s not found", cmdName)
			return
		}
		events, err := handler(data, a.globalAgg.State)
		if err != nil {
			log.Printf("Failed to execute %s: %v", cmdName, err)
			return
		}
		a.processEvents(events)
	})

	return a
}

// processEvents handles appending events and triggering handlers
func (a *App) processEvents(events []eventsourcing.Event) {
	if len(events) == 0 {
		return
	}
	if err := a.eventStore.Append(events...); err != nil {
		log.Printf("Failed to append events: %v", err)
		return
	}
	for _, event := range events {
		if err := a.globalAgg.ApplyEvent(event); err != nil {
			log.Printf("Failed to apply event: %v", err)
		}
		newEvents := a.pluginManager.ProcessEvent(event, a.globalAgg.State, a.commands)
		a.events = append(a.events, event)
		a.processEvents(newEvents) // Recursively process new events
	}
	a.refreshUI()
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

	startStopButton := widget.NewButton("Start Audio Test", nil)
	startStopButton.OnTapped = func() {
		if !a.transcribing {
			a.transcriptBox.SetText("")
			log.Println("Cleared transcript box")
			err := a.transcriber.Start(func(text string) {
				if strings.TrimSpace(text) != "" {
					current := a.transcriptBox.Text
					if current == "" {
						a.transcriptBox.SetText(text)
					} else {
						a.transcriptBox.SetText(current + "\n" + text)
					}
					log.Printf("Added text to transcript: '%s'", text)
				}
			})
			if err != nil {
				log.Printf("Failed to start audio: %v", err)
				return
			}
			startStopButton.SetText("Stop Audio Test")
			a.transcribing = true
		} else {
			a.transcriber.Stop()
			startStopButton.SetText("Start Audio Test")
			a.transcribing = false
		}
	}
	a.transcriptBox.SetMinRowsVisible(20)

	submitButton := widget.NewButton("Submit", func() {
		log.Println("Submit button pressed")
		transcriptionText := a.transcriptBox.Text
		if transcriptionText == "" {
			log.Println("No transcription text to submit")
			return
		}
		data := map[string]interface{}{
			"RequestText": transcriptionText,
		}
		handler, exists := a.commands["ReceiveRequest"]
		if !exists {
			log.Println("ReceiveRequest command not found")
			return
		}
		events, err := handler(data, a.globalAgg.State)
		if err != nil {
			log.Printf("Failed to execute ReceiveRequest: %v", err)
			return
		}
		a.processEvents(events) // Trigger the event chain
		a.transcriptBox.SetText("")
		log.Printf("Submitted request: '%s'", transcriptionText)
	})

	testTab := container.NewVBox(
		widget.NewLabel("Audio Test"),
		startStopButton,
		container.NewMax(a.transcriptBox),
		submitButton,
	)
	stateScrollable := container.NewScroll(a.stateDisplay)
	split := container.NewHSplit(a.eventLog, a.eventDetail)
	split.SetOffset(0.3)

	tabs := container.NewAppTabs(
		container.NewTabItem("Plugin Explorer", a.buildPluginExplorer()),
		container.NewTabItem("Command Execution", a.buildCommandExecution()),
		container.NewTabItem("State Display", stateScrollable),
		container.NewTabItem("Event Log", split),
		container.NewTabItem("Audio Test", testTab),
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

		handler, exists := a.commands[selectedCmd]
		if !exists {
			log.Printf("Command %s not found", selectedCmd)
			return
		}
		events, err := handler(inputData, a.globalAgg.State)
		if err != nil {
			log.Printf("Failed to execute command %s: %v", selectedCmd, err)
			return
		}

		if err := a.eventStore.Append(events...); err != nil {
			log.Printf("Failed to append events: %v", err)
			return
		}

		for _, event := range events {
			if err := a.globalAgg.ApplyEvent(event); err != nil {
				log.Printf("Failed to apply event: %v", err)
			}
		}
		a.events = append(a.events, events...)
		a.refreshUI()
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
	// Format the state display with proper indentation for readability
	state := a.globalAgg.GetState()

	// Create a nicely formatted representation of the state
	var stateStr strings.Builder
	stateStr.WriteString("{\n")

	for k, v := range state {
		// Format value with indentation for better readability
		valStr := fmt.Sprintf("%v", v)

		// Format with indentation
		stateStr.WriteString(fmt.Sprintf("  %s: %s\n", k, valStr))
	}
	stateStr.WriteString("}")

	// Update the text display with the full state (scrollable)
	a.stateDisplay.SetText(stateStr.String())
	a.eventLog.Refresh()
}

func (a *App) RebuildState() {
	a.globalAgg.State = make(map[string]interface{})
	a.events = a.eventStore.GetEvents()
	for _, event := range a.events {
		if err := a.globalAgg.ApplyEvent(event); err != nil {
			log.Printf("Failed to apply event during rebuild: %v", err)
		}
	}
	a.refreshUI()
}
