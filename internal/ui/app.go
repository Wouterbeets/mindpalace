package ui

import (
	"encoding/json"
	"fmt"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"mindpalace/internal/audio"
	"mindpalace/internal/godot_ws"
	"mindpalace/internal/orchestration"
	"mindpalace/pkg/aggregate"
	"mindpalace/pkg/eventsourcing"
	"mindpalace/pkg/logging"
)

// App represents the UI application
type App struct {
	eventProcessor *eventsourcing.EventProcessor
	aggManager     *aggregate.AggregateManager
	eventChan      chan eventsourcing.Event
	ui             fyne.App
	eventLog       *widget.List
	eventDetail    *widget.Entry
	transcriber    *audio.VoiceTranscriber
	transcribing   bool
	transcriptBox  *widget.Entry
	ChatHistory    *fyne.Container
	chatScroll     *container.Scroll
	pluginTabs     *container.AppTabs
	orchestrator   *orchestration.RequestOrchestrator
	plugins        []eventsourcing.Plugin
	godotServer    *godot_ws.GodotServer
}

// NewApp creates a new UI application
func NewApp(ep *eventsourcing.EventProcessor, agg *aggregate.AggregateManager, orch *orchestration.RequestOrchestrator, plugins []eventsourcing.Plugin, godotServer *godot_ws.GodotServer) *App {
	ChatHistory := container.NewVBox()
	fyneApp := app.NewWithID("com.mindpalace.app")

	a := &App{
		eventProcessor: ep,
		aggManager:     agg,
		orchestrator:   orch,
		ui:             fyneApp,
		transcriber: func() *audio.VoiceTranscriber {
			vt, _ := audio.NewVoiceTranscriber("models/ggml-base.en.bin")
			return vt
		}(),
		transcribing:  false,
		transcriptBox: widget.NewMultiLineEntry(),
		ChatHistory:   ChatHistory,
		chatScroll:    container.NewScroll(ChatHistory),
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
		pluginTabs:  container.NewAppTabs(),
		plugins:     plugins,
		godotServer: godotServer,
	}
	a.ui.Settings().SetTheme(NewCustomTheme())

	// Event handling
	go func() {
		for range a.eventChan {
			fyne.CurrentApp().Driver().DoFromGoroutine(func() {
				a.refreshUI()
			}, false)
		}
	}()

	ep.EventBus.SubscribeAll(func(event eventsourcing.Event) error {
		a.eventChan <- event
		return nil
	})

	a.chatScroll.Direction = container.ScrollVerticalOnly
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
		dataJSON, err := json.MarshalIndent(event, "", "  ") // Pretty-print JSON with 2-space indentation
		if err != nil {
			a.eventDetail.SetText(fmt.Sprintf("Error marshaling event data: %v", err))
			return
		}
		// Format the text with event type and pretty-printed JSON
		detailText := fmt.Sprintf("Event Type: %s\nData:\n%s", event.Type(), string(dataJSON))
		a.eventDetail.SetText(detailText)
		// No ColorName in TextStyle; rely on theme foreground color
	}
	a.eventLog.OnUnselected = func(id widget.ListItemID) {
		a.eventDetail.SetText("Select an event to view details")
	}
	a.refreshUI()
}

// Run starts the UI application
func (a *App) Run() {
	window := a.ui.NewWindow("MindPalace")

	// Declare all UI components upfront
	appHeader := widget.NewLabel("MindPalace")
	appHeader.TextStyle = fyne.TextStyle{Bold: true}
	appHeader.Alignment = fyne.TextAlignCenter

	startStopButton := widget.NewButton("Start Audio", nil)
	startStopButton.Importance = widget.MediumImportance

	processingSpinner := widget.NewProgressBarInfinite()
	processingSpinner.Hide()

	submitButton := widget.NewButton("Submit", nil)
	submitButton.Importance = widget.HighImportance

	// Configure transcript box
	a.transcriptBox.SetPlaceHolder("Type your request or speak using the 'Start Audio' button...")
	a.transcriptBox.SetMinRowsVisible(5)
	a.transcriptBox.Wrapping = fyne.TextWrapWord

	// Define button behaviors
	startStopButton.OnTapped = func() {
		if !a.transcribing {
			fyne.CurrentApp().Driver().DoFromGoroutine(func() {
				a.transcriptBox.SetText("")
			}, false)

			err := a.transcriber.Start(func(text string) {
				if strings.TrimSpace(text) != "" {
					logging.Debug("AUDIO: Transcription: %s", text)
					if a.godotServer != nil {
						a.godotServer.SendTranscription(text)
					}
					// Still update Fyne UI for now (can be removed later)
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
						}, false)
					})
				}
			})
			if err != nil {
				logging.Error("Failed to start audio: %v", err)
				fyne.CurrentApp().Driver().DoFromGoroutine(func() {
					message := fmt.Sprintf("Audio error: %v\n\nPlease type your request instead.", err)
					dialog.NewInformation("Audio Unavailable", message, fyne.CurrentApp().Driver().AllWindows()[0]).Show()
					startStopButton.Importance = widget.WarningImportance
					startStopButton.SetText("Audio Unavailable")
					startStopButton.Disable()
					submitButton.Enable() // Ensure submit remains available
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

	submitButton.OnTapped = func() {
		eventsourcing.SafeGo("SubmitTranscription", nil, func() {
			transcriptionText := a.transcriptBox.Text
			if transcriptionText == "" {
				return
			}

			fyne.CurrentApp().Driver().DoFromGoroutine(func() {
				a.transcriptBox.SetText("Processing request...")
				a.transcriptBox.Disable()
				submitButton.Disable()
				processingSpinner.Show()
			}, false)

			err := a.eventProcessor.ExecuteCommand("ProcessUserRequest", map[string]interface{}{
				"requestText": transcriptionText,
			})
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
	}

	// Assemble layout
	transcriptScroll := container.NewScroll(a.transcriptBox)
	transcriptScroll.SetMinSize(fyne.NewSize(0, 100))

	inputWithProgress := container.NewBorder(nil, processingSpinner, nil, nil, transcriptScroll)
	inputArea := container.NewBorder(nil, nil, startStopButton, submitButton, inputWithProgress)

	chatInterface := container.NewBorder(
		container.NewVBox(appHeader, widget.NewSeparator()),
		container.NewVBox(widget.NewSeparator(), inputArea),
		nil, nil,
		a.chatScroll,
	)

	// Plugin tabs
	a.pluginTabs = container.NewAppTabs()
	for _, plugin := range a.plugins {
		agg, err := a.aggManager.AggregateByName(plugin.Name())
		if err != nil {
			logging.Error("aggregate not found for plugin %s: %v", plugin.Name(), err)
			continue
		}
		logging.Debug("adding plugin tabs: %s", plugin.Name())
		ui := agg.GetCustomUI()
		if ui == nil {
			logging.Error("GetCustomUI returned nil for plugin %s", plugin.Name())
			continue
		}
		a.pluginTabs.Append(container.NewTabItem(plugin.Name(), ui))
	}

	// Event log
	split := container.NewHSplit(a.eventLog, a.eventDetail)
	split.SetOffset(0.3)

	// Welcome screen
	welcomeLabel := widget.NewLabel("Welcome to MindPalace")
	welcomeLabel.TextStyle = fyne.TextStyle{Bold: true}
	welcomeLabel.Alignment = fyne.TextAlignCenter
	welcomeDesc := widget.NewLabel("Your local-first AI assistant framework.\n\nUse voice or text to interact with plugins for tasks, calendar, and notes.\n\nClick 'Get Started' to begin.")
	welcomeDesc.Wrapping = fyne.TextWrapWord
	welcomeDesc.Alignment = fyne.TextAlignCenter
	getStartedBtn := widget.NewButton("Get Started", func() {
		window.SetContent(container.NewAppTabs(
			container.NewTabItem("MindPalace", chatInterface),
			container.NewTabItem("Plugins", a.pluginTabs),
		))
	})
	getStartedBtn.Importance = widget.HighImportance
	welcomeScreen := container.NewCenter(container.NewVBox(
		welcomeLabel,
		widget.NewSeparator(),
		welcomeDesc,
		widget.NewSeparator(),
		getStartedBtn,
	))

	// Set initial content to welcome screen
	window.SetContent(welcomeScreen)

	window.Resize(fyne.NewSize(1000, 700))
	window.ShowAndRun()
}

// refreshUI updates the UI components
func (a *App) refreshUI() {
	orchAgg, err := a.aggManager.AggregateByName("orchestration")
	if err == nil {
		chatContent := orchAgg.GetCustomUI().(*fyne.Container)
		a.ChatHistory.Objects = chatContent.Objects // Update content directly
		a.ChatHistory.Refresh()
		a.chatScroll.ScrollToBottom() // Scroll to the latest message
	} else {
		logging.Error("Failed to get orchestration aggregate: %v", err)
	}

	// Refresh plugin tabs if needed
	if a.pluginTabs != nil && len(a.pluginTabs.Items) > 0 {
		for i, tab := range a.pluginTabs.Items {
			pluginName := tab.Text
			for _, plugin := range a.plugins {
				if plugin.Name() == pluginName {
					if agg, exists := a.aggManager.PluginAggregates[pluginName]; exists {
						ui := agg.GetCustomUI()
						if ui != nil {
							a.pluginTabs.Items[i].Content = ui
						}
					}
				}
			}
		}
		a.pluginTabs.Refresh()
	}

	// Refresh event log
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
