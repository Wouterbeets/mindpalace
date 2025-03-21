package ui

import (
	"encoding/json"
	"fmt"
	"regexp"
	"runtime"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"mindpalace/internal/audio"
	"mindpalace/internal/chat"
	"mindpalace/internal/orchestration"
	"mindpalace/pkg/aggregate"
	"mindpalace/pkg/eventsourcing"
	"mindpalace/pkg/logging"
)

// App represents the UI application
type App struct {
	eventProcessor *eventsourcing.EventProcessor
	aggManager     *aggregate.AggregateManager // Updated to use AggregateManager
	eventChan      chan eventsourcing.Event
	commands       map[string]eventsourcing.CommandHandler
	ui             fyne.App
	stateDisplay   *widget.Entry
	eventLog       *widget.List
	eventDetail    *widget.Entry
	transcriber    *audio.VoiceTranscriber
	transcribing   bool
	transcriptBox  *widget.Entry
	ChatHistory    *fyne.Container
	chatScroll     *container.Scroll
	pluginTabs     *container.AppTabs // New field for plugin-specific UIs
	orchestrator   *orchestration.RequestOrchestrator
	plugins        []eventsourcing.Plugin // Store plugins for UI access
}

// NewApp creates a new UI application with the updated aggregate manager
func NewApp(ep *eventsourcing.EventProcessor, agg *aggregate.AggregateManager, orch *orchestration.RequestOrchestrator, plugins []eventsourcing.Plugin) *App {
	ChatHistory := container.NewVBox()
	fyneApp := app.NewWithID("com.mindpalace.app")

	a := &App{
		eventProcessor: ep,
		aggManager:     agg,
		orchestrator:   orch,
		commands:       make(map[string]eventsourcing.CommandHandler),
		ui:             fyneApp,
		transcriber:    audio.NewVoiceTranscriber(),
		transcribing:   false,
		transcriptBox:  widget.NewMultiLineEntry(),
		ChatHistory:    ChatHistory,
		chatScroll:     container.NewScroll(ChatHistory),
		eventLog: widget.NewList(
			func() int { return len(ep.GetEvents()) },
			func() fyne.CanvasObject { return widget.NewLabel("Event") },
			func(id widget.ListItemID, obj fyne.CanvasObject) {
				events := ep.GetEvents()
				if id >= 0 && id < len(events) {
					obj.(*widget.Label).SetText(events[id].Type())
				}
			},
		),
		eventDetail: widget.NewMultiLineEntry(),
		eventChan:   make(chan eventsourcing.Event, 10),
		plugins:     plugins, // Store plugins for UI generation
	}
	a.ui.Settings().SetTheme(NewCustomTheme())

	// Simplified event handling
	go func() {
		for range a.eventChan {
			fyne.CurrentApp().Driver().DoFromGoroutine(func() {
				a.refreshUI()
			}, false)
		}
	}()

	ep.EventBus.Subscribe("RequestCompleted", func(event eventsourcing.Event, state map[string]interface{}, commands map[string]eventsourcing.CommandHandler) ([]eventsourcing.Event, error) {
		a.eventChan <- event
		return nil, nil
	})
	ep.EventBus.Subscribe("UserRequestReceived", func(event eventsourcing.Event, state map[string]interface{}, commands map[string]eventsourcing.CommandHandler) ([]eventsourcing.Event, error) {
		a.eventChan <- event
		return nil, nil
	})

	a.chatScroll.Direction = container.ScrollVerticalOnly
	a.transcriptBox.SetPlaceHolder("Transcriptions will appear here...")
	a.eventDetail.Disable()
	a.eventDetail.SetText("Select an event to view details")

	a.transcriber.SetSessionEventCallback(func(eventType string, data map[string]interface{}) {
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
		err := a.eventProcessor.ExecuteCommand(cmdName, data)
		if err != nil {
			logging.Error("Failed to execute %s: %v", cmdName, err)
		}
	})

	return a
}

// InitUI initializes the UI components
func (a *App) InitUI() {
	events := a.eventProcessor.GetEvents()
	a.eventLog.Length = func() int {
		return len(events)
	}
	a.eventLog.UpdateItem = func(id widget.ListItemID, obj fyne.CanvasObject) {
		obj.(*widget.Label).SetText(events[id].Type())
	}
	a.eventLog.OnSelected = func(id widget.ListItemID) {
		if id < 0 || id >= len(events) {
			return
		}
		event := events[id]
		dataJSON, err := event.Marshal()
		if err != nil {
			a.eventDetail.SetText(fmt.Sprintf("Error marshaling event data: %v", err))
			return
		}
		a.eventDetail.SetText(fmt.Sprintf("Event Type: %s\nData:\n%s", event.Type(), string(dataJSON)))
	}
	a.eventLog.OnUnselected = func(id widget.ListItemID) {
		a.eventDetail.SetText("Select an event to view details")
	}

	stateText := widget.NewMultiLineEntry()
	stateText.SetText(fmt.Sprintf("%v", a.aggManager.GetState()))
	stateText.Disable()
	a.stateDisplay = stateText
	a.RebuildState()
}

// Run starts the UI application
func (a *App) Run() {
	window := a.ui.NewWindow("MindPalace")

	// Create a stylish header with the app name
	appHeader := widget.NewLabel("MindPalace")
	appHeader.TextStyle = fyne.TextStyle{Bold: true}
	appHeader.Alignment = fyne.TextAlignCenter

	// Create a more stylish audio control section
	startStopButton := widget.NewButton("Start Audio", nil)
	startStopButton.Importance = widget.MediumImportance

	logging.Trace("Run running on goroutine: %d", runtime.NumGoroutine())
	startStopButton.OnTapped = func() {
		logging.Trace("Button tapped on goroutine: %d", runtime.NumGoroutine())
		if !a.transcribing {
			fyne.CurrentApp().Driver().DoFromGoroutine(func() {
				a.transcriptBox.SetText("")
				logging.Trace("Cleared transcript box")
			}, false)

			err := a.transcriber.Start(func(text string) {
				if strings.TrimSpace(text) != "" {
					eventsourcing.SafeGo("TranscriptionCallback", map[string]interface{}{
						"text": text,
					}, func() {
						fyne.CurrentApp().Driver().DoFromGoroutine(func() {
							current := a.transcriptBox.Text
							if current == "" {
								a.transcriptBox.SetText(text)
							} else {
								a.transcriptBox.SetText(current + " " + text)
							}
							logging.Trace("Added text to transcript: %s", text)
						}, false)
					})
				}
			})
			if err != nil {
				logging.Error("Failed to start audio: %v", err)
				fyne.CurrentApp().Driver().DoFromGoroutine(func() {
					message := fmt.Sprintf("Audio error: %v\n\nPlease type your request instead.", err)
					errorDialog := dialog.NewInformation("Audio Unavailable", message, fyne.CurrentApp().Driver().AllWindows()[0])
					errorDialog.Show()
					startStopButton.Importance = widget.WarningImportance
					startStopButton.SetText("Audio Unavailable")
					startStopButton.Disable()
				}, false)
				return
			}

			fyne.CurrentApp().Driver().DoFromGoroutine(func() {
				startStopButton.SetText("Stop Audio")
				startStopButton.Importance = widget.DangerImportance
			}, false)
			a.transcribing = true
		} else {
			eventsourcing.SafeGo("StopTranscription", nil, func() {
				a.transcriber.Stop()
			})
			fyne.CurrentApp().Driver().DoFromGoroutine(func() {
				startStopButton.SetText("Start Audio")
				startStopButton.Importance = widget.MediumImportance
			}, false)
			a.transcribing = false
		}
	}
	a.transcriptBox.SetPlaceHolder("Type your request or speak using the 'Start Audio' button...")
	a.transcriptBox.SetMinRowsVisible(5)
	a.transcriptBox.Wrapping = fyne.TextWrapWord

	processingSpinner := widget.NewProgressBarInfinite()
	processingSpinner.Hide()

	var submitButton *widget.Button
	submitButton = widget.NewButton("Submit", func() {
		eventsourcing.SafeGo("SubmitTranscription", nil, func() {
			logging.Trace("Submit button pressed")
			transcriptionText := a.transcriptBox.Text
			if transcriptionText == "" {
				logging.Trace("No transcription text to submit")
				return
			}

			fyne.CurrentApp().Driver().DoFromGoroutine(func() {
				a.transcriptBox.SetText("Processing request...")
				a.transcriptBox.Disable()
				submitButton.Disable()
				processingSpinner.Show()
			}, false)

			err := a.orchestrator.ProcessRequest(transcriptionText, "")
			if err != nil {
				logging.Error(err.Error())
			}
			fyne.CurrentApp().Driver().DoFromGoroutine(func() {
				a.transcriptBox.SetText("")
				a.transcriptBox.Enable()
				submitButton.Enable()
				processingSpinner.Hide()
			}, false)
		})
	})
	submitButton.Importance = widget.HighImportance

	transcriptScroll := container.NewScroll(a.transcriptBox)
	transcriptScroll.SetMinSize(fyne.NewSize(0, 100))

	inputWithProgress := container.NewBorder(
		nil,
		processingSpinner,
		nil, nil,
		transcriptScroll,
	)

	inputArea := container.NewBorder(
		nil, nil,
		startStopButton, submitButton,
		inputWithProgress,
	)

	// Main chat interface
	chatInterface := container.NewBorder(
		container.NewVBox(appHeader, widget.NewSeparator()),
		container.NewVBox(widget.NewSeparator(), inputArea),
		nil,
		nil,
		a.chatScroll,
	)

	logging.Debug("all plugin: %+v", a.aggManager.PluginAggregates)
	// Plugin tabs (replacing tasksContainer)
	a.pluginTabs = container.NewAppTabs()
	for _, plugin := range a.plugins {
		logging.Debug("plugin: %+v", a.aggManager.PluginAggregates[plugin.Name()])
		a.pluginTabs.Append(container.NewTabItem(plugin.Name(), plugin.GetCustomUI(a.aggManager.PluginAggregates[plugin.Name()])))
	}

	// Debug panels
	stateScrollable := container.NewScroll(a.stateDisplay)
	split := container.NewHSplit(a.eventLog, a.eventDetail)
	split.SetOffset(0.3)

	tabs := container.NewAppTabs(
		container.NewTabItem("MindPalace", chatInterface),
		container.NewTabItem("Plugins", a.pluginTabs),
		container.NewTabItem("Plugin Explorer", a.buildPluginExplorer()),
		container.NewTabItem("Command Execution", a.buildCommandExecution()),
		container.NewTabItem("State Display", stateScrollable),
		container.NewTabItem("Event Log", split),
	)

	window.SetContent(tabs)
	window.Resize(fyne.NewSize(1000, 700))
	window.ShowAndRun()
}

// buildPluginExplorer builds a plugin explorer UI
func (a *App) buildPluginExplorer() fyne.CanvasObject {
	list := widget.NewList(
		func() int { return len(a.commands) },
		func() fyne.CanvasObject { return widget.NewLabel("Command") },
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

// buildCommandExecution builds a command execution UI
func (a *App) buildCommandExecution() fyne.CanvasObject {
	commandOptions := make([]string, 0, len(a.commands))
	for cmd := range a.commands {
		commandOptions = append(commandOptions, cmd)
	}
	commandDropdown := widget.NewSelect(commandOptions, func(s string) {
		logging.Trace("Selected command: %s", s)
	})

	inputArea := widget.NewMultiLineEntry()
	inputArea.SetPlaceHolder("Enter command parameters in JSON format...")
	executeButton := widget.NewButton("Execute", func() {
		selectedCmd := commandDropdown.Selected
		if selectedCmd == "" {
			logging.Trace("No command selected")
			return
		}
		inputText := inputArea.Text
		var inputData map[string]interface{}
		if err := json.Unmarshal([]byte(inputText), &inputData); err != nil {
			logging.Error("Invalid JSON input: %v", err)
			return
		}
		err := a.eventProcessor.ExecuteCommand(selectedCmd, inputData)
		if err != nil {
			logging.Error("Failed to execute command %s: %v", selectedCmd, err)
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

// refreshUI updates the UI components
func (a *App) refreshUI() {
	// Clear chat history container
	a.ChatHistory.Objects = nil

	// Update chat history (unchanged)
	for _, msg := range a.aggManager.ChatHistory {
		var content fyne.CanvasObject

		if strings.Contains(msg.Role, "[think]") {
			detailsContent := parseMarkdownToCanvas(msg.Content)
			detailsItem := widget.NewAccordionItem("Thinking details...", detailsContent)
			details := widget.NewAccordion(detailsItem)
			details.MultiOpen = false

			header := widget.NewLabel("🧠 Assistant thinking process...")
			header.TextStyle = fyne.TextStyle{Italic: true}

			content = container.NewVBox(header, details)
		} else {
			messageContainer := container.NewVBox()

			var roleLabel *widget.Label
			if msg.Role == "You" {
				roleLabel = widget.NewLabel("You")
				roleLabel.TextStyle = fyne.TextStyle{Bold: true}
			} else {
				roleLabel = widget.NewLabel("MindPalace")
				roleLabel.TextStyle = fyne.TextStyle{Bold: true}
			}

			messageContent := parseMarkdownToCanvas(msg.Content)
			messageContainer.Add(roleLabel)
			messageContainer.Add(messageContent)

			content = container.NewPadded(messageContainer)
		}

		if len(a.ChatHistory.Objects) > 0 {
			a.ChatHistory.Add(widget.NewSeparator())
		}
		a.ChatHistory.Add(content)
	}

	// Refresh chat history
	a.ChatHistory.Refresh()
	if len(a.ChatHistory.Objects) > 0 {
		a.chatScroll.ScrollToBottom()
	}

	// Update plugin UIs (e.g., taskmanager)
	if a.pluginTabs != nil && len(a.pluginTabs.Items) > 0 {
		for i, tab := range a.pluginTabs.Items {
			pluginName := tab.Text
			for _, plugin := range a.plugins {
				if plugin.Name() == pluginName {
					if agg, exists := a.aggManager.PluginAggregates[pluginName]; exists {
						a.pluginTabs.Items[i].Content = plugin.GetCustomUI(agg)
					}
				}
			}
		}
		a.pluginTabs.Refresh()

	}

	// Update state display and event log
	a.stateDisplay.SetText(fmt.Sprintf("%v", a.aggManager.GetState()))
	events := a.eventProcessor.GetEvents()
	a.eventLog.Length = func() int { return len(events) }
	a.eventLog.UpdateItem = func(id widget.ListItemID, obj fyne.CanvasObject) {
		obj.(*widget.Label).SetText(events[id].Type())
	}
	a.eventLog.Refresh()
}

// parseMarkdownToCanvas converts Markdown text into a styled Fyne CanvasObject (unchanged)
func parseMarkdownToCanvas(text string) fyne.CanvasObject {
	lines := strings.Split(text, "\n")
	content := container.NewVBox()

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		switch {
		case strings.HasPrefix(line, "# "):
			label := widget.NewLabel(strings.TrimPrefix(line, "# "))
			label.TextStyle = fyne.TextStyle{Bold: true}
			content.Add(label)
		case strings.HasPrefix(line, "## "):
			label := widget.NewLabel(strings.TrimPrefix(line, "## "))
			label.TextStyle = fyne.TextStyle{Bold: true}
			content.Add(label)
		case strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "* "):
			label := widget.NewLabel("• " + strings.TrimPrefix(strings.TrimPrefix(line, "- "), "* "))
			content.Add(label)
		case strings.HasPrefix(line, "```") && strings.HasSuffix(line, "```"):
			codeText := strings.TrimPrefix(strings.TrimSuffix(line, "```"), "```")
			codeLabel := widget.NewLabel(codeText)
			codeLabel.TextStyle = fyne.TextStyle{Monospace: true}
			codeLabel.Wrapping = fyne.TextWrapWord
			content.Add(container.NewPadded(codeLabel))
		default:
			richText := widget.NewRichText()
			richText.Segments = parseInlineMarkdown(line)
			richText.Wrapping = fyne.TextWrapWord
			content.Add(richText)
		}
	}

	return content
}

// parseInlineMarkdown handles inline bold (**text**) and italic (*text*) formatting (unchanged)
func parseInlineMarkdown(text string) []widget.RichTextSegment {
	segments := []widget.RichTextSegment{}
	remaining := text

	for len(remaining) > 0 {
		if boldStart := strings.Index(remaining, "**"); boldStart >= 0 {
			if boldStart > 0 {
				segments = append(segments, &widget.TextSegment{
					Text:  remaining[:boldStart],
					Style: widget.RichTextStyle{TextStyle: fyne.TextStyle{}},
				})
			}
			boldEnd := strings.Index(remaining[boldStart+2:], "**")
			if boldEnd >= 0 {
				boldText := remaining[boldStart+2 : boldStart+2+boldEnd]
				if boldText != "" {
					segments = append(segments, &widget.TextSegment{
						Text:  boldText,
						Style: widget.RichTextStyle{TextStyle: fyne.TextStyle{Bold: true}},
					})
				}
				remaining = remaining[boldStart+2+boldEnd+2:]
			} else {
				segments = append(segments, &widget.TextSegment{
					Text:  remaining,
					Style: widget.RichTextStyle{TextStyle: fyne.TextStyle{}},
				})
				remaining = ""
			}
		} else if italicStart := strings.Index(remaining, "*"); italicStart >= 0 {
			if italicStart > 0 {
				segments = append(segments, &widget.TextSegment{
					Text:  remaining[:italicStart],
					Style: widget.RichTextStyle{TextStyle: fyne.TextStyle{}},
				})
			}
			italicEnd := strings.Index(remaining[italicStart+1:], "*")
			if italicEnd >= 0 {
				italicText := remaining[italicStart+1 : italicStart+1+italicEnd]
				if italicText != "" {
					segments = append(segments, &widget.TextSegment{
						Text:  italicText,
						Style: widget.RichTextStyle{TextStyle: fyne.TextStyle{Italic: true}},
					})
				}
				remaining = remaining[italicStart+1+italicEnd+1:]
			} else {
				segments = append(segments, &widget.TextSegment{
					Text:  remaining,
					Style: widget.RichTextStyle{TextStyle: fyne.TextStyle{}},
				})
				remaining = ""
			}
		} else {
			segments = append(segments, &widget.TextSegment{
				Text:  remaining,
				Style: widget.RichTextStyle{TextStyle: fyne.TextStyle{}},
			})
			remaining = ""
		}
	}

	return segments
}

// RebuildState rebuilds the state from events
func (a *App) RebuildState() {
	events := a.eventProcessor.GetEvents()
	eventsCopy := make([]eventsourcing.Event, len(events))
	copy(eventsCopy, events)

	for _, event := range eventsCopy {
		if err := a.aggManager.ApplyEvent(event); err != nil {
			logging.Error("Failed to apply event during rebuild: %v", err)
		}
	}
	a.refreshUI()
}

// parseStreamingContent extracts think tags and regular text from streaming content (unchanged)
func parseStreamingContent(content string) (thinks []string, regular string) {
	re := regexp.MustCompile(`(?s)<think>(.*?)</think>`)
	matches := re.FindAllStringSubmatch(content, -1)
	for _, match := range matches {
		thinks = append(thinks, match[1])
	}
	regular = re.ReplaceAllString(content, "")
	return thinks, strings.TrimSpace(regular)
}

// handleStreamingUpdate processes streaming updates from the LLM and updates the UI (unchanged)
func (a *App) handleStreamingUpdate(data map[string]interface{}) {
	requestID, _ := data["RequestID"].(string)
	partialContent, _ := data["PartialContent"].(string)
	isFinal, _ := data["IsFinal"].(bool)

	thinks, regularContent := parseStreamingContent(partialContent)

	if len(thinks) > 0 {
		thinkContent := strings.Join(thinks, "\n\n")
		thinkMessageFound := false
		for i, msg := range a.aggManager.ChatHistory {
			if msg.RequestID == requestID && msg.Role == "Assistant [think]" {
				a.aggManager.ChatHistory[i].Content = thinkContent
				thinkMessageFound = true
				break
			}
		}
		if !thinkMessageFound {
			thinkMessage := chat.ChatMessage{
				Role:              "Assistant [think]",
				Content:           thinkContent,
				RequestID:         requestID,
				StreamingComplete: true,
			}
			a.aggManager.ChatHistory = append(a.aggManager.ChatHistory, thinkMessage)
		}
	}

	var assistantMessageFound bool
	for i, msg := range a.aggManager.ChatHistory {
		if msg.RequestID != requestID || msg.Role != "MindPalace" {
			continue
		}
		assistantMessageFound = true
		a.aggManager.ChatHistory[i].Content = regularContent
		if isFinal {
			a.aggManager.ChatHistory[i].StreamingComplete = true
		}
		break
	}

	if !assistantMessageFound && regularContent != "" {
		newMessage := chat.ChatMessage{
			Role:              "MindPalace",
			Content:           regularContent,
			RequestID:         requestID,
			StreamingComplete: isFinal,
		}
		a.aggManager.ChatHistory = append(a.aggManager.ChatHistory, newMessage)
	}

	a.refreshUI()
	a.chatScroll.ScrollToBottom()
}
