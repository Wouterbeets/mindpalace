package core

import (
	"encoding/json"
	"fmt"
	"image/color"
	"log"
	"regexp"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

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
	chatHistory   *fyne.Container   // VBox for chat messages
	chatScroll    *container.Scroll // Reference to the scroll container
	eventChan     chan eventsourcing.Event
}

var submitEvent func(eventsourcing.Event)

// NewApp initializes the App struct with chatScroll set up
func NewApp(es *EventStore, pm *PluginManager) *App {
	chatHistory := container.NewVBox()
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
		chatHistory:   chatHistory,
		chatScroll:    container.NewScroll(chatHistory), // Initialize here
		eventLog: widget.NewList(
			func() int { return 0 },
			func() fyne.CanvasObject { return widget.NewLabel("Event") },
			func(id widget.ListItemID, obj fyne.CanvasObject) {},
		),
		eventDetail: widget.NewMultiLineEntry(),
		eventChan:   make(chan eventsourcing.Event, 10),
	}
	a.ui.Settings().SetTheme(&CustomTheme{theme.DefaultTheme()})
	submitEvent = func(event eventsourcing.Event) {
		a.eventChan <- event
	}
	go func() {
		for event := range a.eventChan {
			fyne.CurrentApp().Driver().DoFromGoroutine(func() {
				a.processEvents([]eventsourcing.Event{event})
			}, false)
		}
	}()
	a.chatScroll.Direction = container.ScrollVerticalOnly // Set direction early

	a.chatScroll.SetMinSize(fyne.NewSize(0, 300)) // Set size early
	a.transcriptBox.SetPlaceHolder("Transcriptions will appear here...")
	a.eventDetail.Disable()
	a.eventDetail.SetText("Select an event to view details")

	// Register commands and event handlers
	commands, eventHandlers := pm.RegisterCommands()
	a.commands = commands
	a.eventHandlers = eventHandlers

	// Transcriber callback
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
		a.processEvents(newEvents)
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

	startStopButton := widget.NewButton("Start Audio", nil)
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
			startStopButton.SetText("Stop Audio")
			a.transcribing = true
		} else {
			a.transcriber.Stop()
			startStopButton.SetText("Start Audio")
			a.transcribing = false
		}
	}
	a.transcriptBox.SetMinRowsVisible(5)

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
		a.processEvents(events)
		a.transcriptBox.SetText("")
		log.Printf("Submitted request: '%s'", transcriptionText)
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
	a.chatHistory.Objects = nil

	for _, event := range a.events {
		genericEvent, ok := event.(*eventsourcing.GenericEvent)
		if !ok {
			log.Printf("Skipping non-GenericEvent: %v", event)
			continue
		}

		switch genericEvent.EventType {
		case "UserRequestReceived":
			reqText, _ := genericEvent.Data["RequestText"].(string)
			label := widget.NewLabel("You: " + reqText)
			label.Wrapping = fyne.TextWrapWord
			a.chatHistory.Add(label)

		case "LLMProcessingStarted":
			reqText, _ := genericEvent.Data["RequestText"].(string)
			label := widget.NewLabel("Assistant: Processing '" + reqText + "'...")
			label.Wrapping = fyne.TextWrapWord
			a.chatHistory.Add(label)

		case "LLMProcessingCompleted":
			respText, _ := genericEvent.Data["ResponseText"].(string)
			thinks, regular := parseResponseText(respText)

			for _, think := range thinks {
				thinkLabel := widget.NewLabel("Assistant [think]: " + think)
				thinkLabel.Wrapping = fyne.TextWrapWord
				a.chatHistory.Add(thinkLabel)
			}

			if regular != "" {
				regularLabel := widget.NewLabel("Assistant: " + regular)
				regularLabel.Wrapping = fyne.TextWrapWord
				a.chatHistory.Add(regularLabel)
			}
		}
	}

	a.chatHistory.Refresh()
	if len(a.chatHistory.Objects) > 0 {
		a.chatScroll.ScrollToBottom()
	}

	a.stateDisplay.SetText(fmt.Sprintf("%v", a.globalAgg.GetState()))
	a.eventLog.Refresh()
}

// parseResponseText extracts think content and regular text from LLM response
func parseResponseText(responseText string) (thinks []string, regular string) {
	re := regexp.MustCompile(`(?s)<think>(.*?)</think>`)
	matches := re.FindAllStringSubmatch(responseText, -1)
	for _, match := range matches {
		thinks = append(thinks, match[1])
	}
	regular = re.ReplaceAllString(responseText, "")
	return thinks, strings.TrimSpace(regular)
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
