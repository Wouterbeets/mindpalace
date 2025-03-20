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

// We could create a custom logger, but Fyne v2 doesn't seem to support custom loggers easily
// Let's keep this for future reference in case we upgrade Fyne

// App represents the UI application
type App struct {
	eventProcessor *eventsourcing.EventProcessor
	globalAgg      *aggregate.AppAggregate
	eventChan      chan eventsourcing.Event
	commands       map[string]eventsourcing.CommandHandler
	ui             fyne.App
	stateDisplay   *widget.Entry
	eventLog       *widget.List
	eventDetail    *widget.Entry
	transcriber    *audio.VoiceTranscriber
	transcribing   bool
	transcriptBox  *widget.Entry
	chatHistory    *fyne.Container
	chatScroll     *container.Scroll
	tasksContainer *fyne.Container
	orchestrator   *orchestration.RequestOrchestrator
}

func NewApp(ep *eventsourcing.EventProcessor, agg *aggregate.AppAggregate, orch *orchestration.RequestOrchestrator) *App {
	chatHistory := container.NewVBox()
	tasksContainer := container.NewVBox()
	fyneApp := app.NewWithID("com.mindpalace.app")

	a := &App{
		globalAgg:      agg,
		orchestrator:   orch,
		eventProcessor: ep,
		commands:       make(map[string]eventsourcing.CommandHandler),
		ui:             fyneApp,
		transcriber:    audio.NewVoiceTranscriber(),
		transcribing:   false,
		transcriptBox:  widget.NewMultiLineEntry(),
		chatHistory:    chatHistory,
		chatScroll:     container.NewScroll(chatHistory),
		tasksContainer: tasksContainer,
		eventLog: widget.NewList(
			func() int { return len(ep.GetEvents()) }, // Use GetEvents directly
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
} // InitUI initializes the UI components
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
		for _, e := range events {
			logging.Trace("onselect: all events: %s, %T ", e.Type(), e)
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
	stateText.SetText(fmt.Sprintf("%v", a.globalAgg.GetState()))
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

	// Create a container for tasks with a title
	tasksScroll := container.NewScroll(a.tasksContainer)
	// Don't set a min size for tasks - let it expand to fill available space

	tasksSectionHeader := widget.NewLabel("📋 Tasks")
	tasksSectionHeader.TextStyle = fyne.TextStyle{Bold: true}

	tasksSection := container.NewBorder(
		tasksSectionHeader,
		nil, nil, nil,
		tasksScroll,
	)

	// Create a more stylish audio control section
	startStopButton := widget.NewButton("Start Audio", nil)
	startStopButton.Importance = widget.MediumImportance

	logging.Trace("Run running on goroutine: %d", runtime.NumGoroutine())
	startStopButton.OnTapped = func() {
		logging.Trace("Button tapped on goroutine: %d", runtime.NumGoroutine())
		if !a.transcribing {
			// Clear the transcript box on the main thread
			fyne.CurrentApp().Driver().DoFromGoroutine(func() {
				a.transcriptBox.SetText("")
				logging.Trace("Cleared transcript box")
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
							logging.Trace("Added text to transcript: %s", text)
						}, false)
					})
				}
			})
			if err != nil {
				logging.Error("Failed to start audio: %v", err)

				// Show an error dialog
				fyne.CurrentApp().Driver().DoFromGoroutine(func() {
					// Create a simple info dialog
					message := fmt.Sprintf("Audio error: %v\n\nPlease type your request instead.", err)
					errorDialog := dialog.NewInformation("Audio Unavailable", message, fyne.CurrentApp().Driver().AllWindows()[0])
					errorDialog.Show()

					// Update button appearance to indicate disabled state
					startStopButton.Importance = widget.WarningImportance
					startStopButton.SetText("Audio Unavailable")
					startStopButton.Disable()
				}, false)

				return
			}

			// Update button text on the main thread
			fyne.CurrentApp().Driver().DoFromGoroutine(func() {
				startStopButton.SetText("Stop Audio")
				startStopButton.Importance = widget.DangerImportance
			}, false)
			a.transcribing = true
		} else {
			// Stop transcription with panic protection
			eventsourcing.SafeGo("StopTranscription", nil, func() {
				a.transcriber.Stop()
			})

			// Update button text on the main thread
			fyne.CurrentApp().Driver().DoFromGoroutine(func() {
				startStopButton.SetText("Start Audio")
				startStopButton.Importance = widget.MediumImportance
			}, false)
			a.transcribing = false
		}
	}
	a.transcriptBox.SetPlaceHolder("Type your request or speak using the 'Start Audio' button...")
	// Set transcript box to have more visible rows for better text editing
	a.transcriptBox.SetMinRowsVisible(5)         // Increased from 3 to give more space for editing
	a.transcriptBox.Wrapping = fyne.TextWrapWord // Enable word wrapping for better readability

	// Create a spinner for indicating processing state
	processingSpinner := widget.NewProgressBarInfinite()
	processingSpinner.Hide()

	// Define submitButton in advance so we can reference it inside the closure
	var submitButton *widget.Button

	submitButton = widget.NewButton("Submit", func() {
		// Wrap submit action in SafeGo for panic recovery
		eventsourcing.SafeGo("SubmitTranscription", nil, func() {
			logging.Trace("Submit button pressed")
			transcriptionText := a.transcriptBox.Text
			if transcriptionText == "" {
				logging.Trace("No transcription text to submit")
				return
			}

			// Show processing spinner in transcript box
			fyne.CurrentApp().Driver().DoFromGoroutine(func() {
				a.transcriptBox.SetText("Processing request...")
				a.transcriptBox.Disable() // Disable editing while processing
				submitButton.Disable()    // Disable submit button while processing
				processingSpinner.Show()
			}, false)

			err := a.orchestrator.ProcessRequest(transcriptionText, "")
			if err != nil {
				logging.Error(err.Error())
			}
			// Clear the input and reset UI state after command is executed
			fyne.CurrentApp().Driver().DoFromGoroutine(func() {
				a.transcriptBox.SetText("")
				a.transcriptBox.Enable()
				submitButton.Enable()
				processingSpinner.Hide()
			}, false)
		})
	})
	submitButton.Importance = widget.HighImportance

	// Wrap transcript box in scroll container to allow scrolling for long inputs
	transcriptScroll := container.NewScroll(a.transcriptBox)
	transcriptScroll.SetMinSize(fyne.NewSize(0, 100)) // Set reasonable minimum height

	// Create a container for input area with progress bar at bottom
	inputWithProgress := container.NewBorder(
		nil,
		processingSpinner, // Place progress bar at bottom
		nil, nil,
		transcriptScroll,
	)

	// Create the input area with buttons
	inputArea := container.NewBorder(
		nil, nil,
		startStopButton, submitButton,
		inputWithProgress,
	)

	// Main chat interface with BorderLayout to keep input at bottom
	chatScrollAndTasksSplit := container.NewHSplit(
		a.chatScroll,
		tasksSection,
	)
	chatScrollAndTasksSplit.Offset = 0.7 // 70% for chat, 30% for tasks

	// Use a BorderLayout to fix the input area at the bottom
	chatInterface := container.NewBorder(
		// Top
		container.NewVBox(
			appHeader,
			widget.NewSeparator(),
		),
		// Bottom
		container.NewVBox(
			widget.NewSeparator(),
			inputArea,
		),
		// Left
		nil,
		// Right
		nil,
		// Center (takes all remaining space)
		chatScrollAndTasksSplit,
	)

	// Debug panels
	stateScrollable := container.NewScroll(a.stateDisplay)
	split := container.NewHSplit(a.eventLog, a.eventDetail)
	split.SetOffset(0.3)

	tabs := container.NewAppTabs(
		container.NewTabItem("MindPalace", chatInterface),
		container.NewTabItem("Plugin Explorer", a.buildPluginExplorer()),
		container.NewTabItem("Command Execution", a.buildCommandExecution()),
		container.NewTabItem("State Display", stateScrollable),
		container.NewTabItem("Event Log", split),
	)

	window.SetContent(tabs)
	window.Resize(fyne.NewSize(1000, 700)) // Larger default window size
	window.ShowAndRun()
}

// buildPluginExplorer builds a plugin explorer UI
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

// refreshUI updates the UI based on the current state
// refreshUI updates the UI based on the current state
func (a *App) refreshUI() {
	// Only log this at trace level to avoid spamming logs
	logging.Trace("Refreshing UI in real-time")
	a.chatHistory.Objects = nil
	a.tasksContainer.Objects = nil

	// Update chat history (unchanged)
	for _, msg := range a.globalAgg.ChatHistory {
		var content fyne.CanvasObject

		if strings.Contains(msg.Role, "[think]") {
			// Create collapsible container for think content
			detailsContent := widget.NewLabel(msg.Content)
			detailsContent.Wrapping = fyne.TextWrapWord

			detailsItem := widget.NewAccordionItem("Thinking details...", detailsContent)
			details := widget.NewAccordion(detailsItem)
			details.MultiOpen = false

			header := widget.NewLabel("🧠 Assistant thinking process...")
			header.TextStyle = fyne.TextStyle{Italic: true}

			content = container.NewVBox(
				header,
				details,
			)
		} else {
			// Regular message
			messageContainer := container.NewVBox()

			var roleLabel *widget.Label
			if msg.Role == "You" {
				roleLabel = widget.NewLabel("You")
				roleLabel.TextStyle = fyne.TextStyle{Bold: true}
			} else {
				roleLabel = widget.NewLabel("MindPalace")
				roleLabel.TextStyle = fyne.TextStyle{Bold: true}
			}

			messageLabel := widget.NewLabel(msg.Content)
			messageLabel.Wrapping = fyne.TextWrapWord

			messageContainer.Add(roleLabel)
			messageContainer.Add(messageLabel)

			content = container.NewPadded(messageContainer)
		}

		if len(a.chatHistory.Objects) > 0 {
			a.chatHistory.Add(widget.NewSeparator())
		}
		a.chatHistory.Add(content)
	}

	// Update task list using aggregate state
	if tasks, ok := a.globalAgg.State["tasks"].(map[string]map[string]interface{}); ok {
		for _, taskData := range tasks {
			// Extract task fields
			taskTitle, _ := taskData["Title"].(string)
			taskDescription, _ := taskData["Description"].(string)
			taskStatus, _ := taskData["Status"].(string)
			taskPriority, _ := taskData["Priority"].(string)
			taskDeadline, _ := taskData["Deadline"].(string)
			taskCompletedAt, _ := taskData["CompletedAt"].(string)
			taskCompletionNotes, _ := taskData["CompletionNotes"].(string)

			// Set defaults for missing fields
			if taskStatus == "" {
				taskStatus = "Pending"
			}
			if taskPriority == "" {
				taskPriority = "Medium"
			}

			// Create task card
			taskCard := container.NewVBox()

			// Status indicator
			var statusEmoji string
			switch taskStatus {
			case "Completed":
				statusEmoji = "✅"
			case "In Progress":
				statusEmoji = "🔄"
			case "Blocked":
				statusEmoji = "🚫"
			default:
				statusEmoji = "⏳"
			}

			// Priority indicator
			var priorityIndicator string
			switch taskPriority {
			case "Critical":
				priorityIndicator = "‼️"
			case "High":
				priorityIndicator = "❗"
			case "Medium":
				priorityIndicator = "📌"
			case "Low":
				priorityIndicator = "📎"
			default:
				priorityIndicator = ""
			}

			// Task header with title, status, and priority
			headerText := fmt.Sprintf("%s %s %s", statusEmoji, priorityIndicator, taskTitle)
			titleLabel := widget.NewLabel(headerText)
			titleLabel.TextStyle = fyne.TextStyle{Bold: true}

			// Create task details accordion
			detailsContent := container.NewVBox()

			// Description section (if available)
			if taskDescription != "" {
				descLabel := widget.NewLabel(taskDescription)
				descLabel.Wrapping = fyne.TextWrapWord
				detailsContent.Add(descLabel)
				detailsContent.Add(widget.NewSeparator())
			}

			// Metadata section
			metadataContent := container.NewVBox()

			// Status row
			statusRow := container.NewHBox(
				widget.NewLabel("Status:"),
				widget.NewLabel(taskStatus),
			)
			metadataContent.Add(statusRow)

			// Priority row
			priorityRow := container.NewHBox(
				widget.NewLabel("Priority:"),
				widget.NewLabel(taskPriority),
			)
			metadataContent.Add(priorityRow)

			// Deadline row (if available)
			if taskDeadline != "" {
				deadlineRow := container.NewHBox(
					widget.NewLabel("Deadline:"),
					widget.NewLabel(taskDeadline),
				)
				metadataContent.Add(deadlineRow)
			}

			// Completion info (if applicable)
			if taskStatus == "Completed" && taskCompletedAt != "" {
				completedRow := container.NewHBox(
					widget.NewLabel("Completed:"),
					widget.NewLabel(taskCompletedAt),
				)
				metadataContent.Add(completedRow)

				if taskCompletionNotes != "" {
					notesRow := container.NewVBox(
						widget.NewLabel("Notes:"),
						widget.NewLabel(taskCompletionNotes),
					)
					metadataContent.Add(notesRow)
				}
			}

			detailsContent.Add(metadataContent)

			// Tags section (if available)
			if tags, ok := taskData["Tags"].([]interface{}); ok && len(tags) > 0 {
				tagsContent := container.NewHBox(widget.NewLabel("Tags:"))
				for _, tag := range tags {
					if tagStr, ok := tag.(string); ok {
						tagLabel := widget.NewLabel(fmt.Sprintf("#%s", tagStr))
						tagLabel.TextStyle = fyne.TextStyle{Italic: true}
						tagsContent.Add(tagLabel)
					}
				}
				detailsContent.Add(tagsContent)
			}

			// Dependencies section (if available)
			if deps, ok := taskData["Dependencies"].([]interface{}); ok && len(deps) > 0 {
				depsContent := container.NewVBox(widget.NewLabel("Dependencies:"))
				for _, dep := range deps {
					if depStr, ok := dep.(string); ok {
						depLabel := widget.NewLabel(fmt.Sprintf("• Depends on: %s", depStr))
						depsContent.Add(depLabel)
					}
				}
				detailsContent.Add(depsContent)
			}

			// Create the accordion
			details := widget.NewAccordion(
				widget.NewAccordionItem("Details", detailsContent),
			)

			taskCard.Add(titleLabel)
			taskCard.Add(details)

			// Add to tasks container with separator
			if len(a.tasksContainer.Objects) > 0 {
				a.tasksContainer.Add(widget.NewSeparator())
			}
			a.tasksContainer.Add(taskCard)
		}
	}

	// Refresh UI elements
	a.chatHistory.Refresh()
	a.tasksContainer.Refresh()

	// Scroll to bottom
	if len(a.chatHistory.Objects) > 0 {
		a.chatScroll.ScrollToBottom()
	}

	// Update state display
	a.stateDisplay.SetText(fmt.Sprintf("%v", a.globalAgg.GetState()))

	// Update event log with latest events
	events := a.eventProcessor.GetEvents()
	a.eventLog.Length = func() int {
		return len(events)
	}
	a.eventLog.UpdateItem = func(id widget.ListItemID, obj fyne.CanvasObject) {
		obj.(*widget.Label).SetText(events[id].Type())
	}
	a.eventLog.Refresh()
}

// RebuildState rebuilds the state from events
func (a *App) RebuildState() {
	a.globalAgg.State = make(map[string]interface{})
	a.globalAgg.ChatHistory = nil
	events := a.eventProcessor.GetEvents() // Re-fetch all events to ensure consistency
	eventsCopy := make([]eventsourcing.Event, len(events))
	copy(eventsCopy, events)

	for _, event := range eventsCopy {
		if err := a.globalAgg.ApplyEvent(event); err != nil {
			logging.Error("Failed to apply event during rebuild: %v", err)
		}
	}
	a.refreshUI()
}

// parseStreamingContent extracts think tags and regular text from streaming content
func parseStreamingContent(content string) (thinks []string, regular string) {
	re := regexp.MustCompile(`(?s)<think>(.*?)</think>`)
	matches := re.FindAllStringSubmatch(content, -1)
	for _, match := range matches {
		thinks = append(thinks, match[1])
	}
	regular = re.ReplaceAllString(content, "")
	return thinks, strings.TrimSpace(regular)
}

// handleStreamingUpdate processes streaming updates from the LLM and updates the UI
func (a *App) handleStreamingUpdate(data map[string]interface{}) {
	requestID, _ := data["RequestID"].(string)
	partialContent, _ := data["PartialContent"].(string)
	isFinal, _ := data["IsFinal"].(bool)

	// Parse think tags from the partialContent
	thinks, regularContent := parseStreamingContent(partialContent)

	// Find and update thinking message if we have new think content
	if len(thinks) > 0 {
		thinkContent := strings.Join(thinks, "\n\n")

		// Check if we already have a thinking message for this request
		thinkMessageFound := false
		for i, msg := range a.globalAgg.ChatHistory {
			if msg.RequestID == requestID && msg.Role == "Assistant [think]" {
				// Update existing thinking message
				a.globalAgg.ChatHistory[i].Content = thinkContent
				thinkMessageFound = true
				break
			}
		}

		// If no thinking message found, create a new one
		if !thinkMessageFound {
			thinkMessage := chat.ChatMessage{
				Role:              "Assistant [think]",
				Content:           thinkContent,
				RequestID:         requestID,
				StreamingComplete: true, // Thinking is always considered complete
			}
			a.globalAgg.ChatHistory = append(a.globalAgg.ChatHistory, thinkMessage)
		}
	}

	// Find the existing assistant message or create a new one for regular content
	var assistantMessageFound bool
	for i, msg := range a.globalAgg.ChatHistory {
		// Skip non-assistant messages and those from other requests
		// Removed StreamingComplete check to ensure we update existing messages
		if msg.RequestID != requestID || msg.Role != "MindPalace" {
			continue
		}

		// Found the assistant message for this request
		assistantMessageFound = true

		// Update the content with regular content (without think tags)
		a.globalAgg.ChatHistory[i].Content = regularContent

		// Mark as complete if this is the final chunk
		if isFinal {
			a.globalAgg.ChatHistory[i].StreamingComplete = true
		}

		break
	}

	// If no existing message found, create a new one
	if !assistantMessageFound && regularContent != "" {
		newMessage := chat.ChatMessage{
			Role:              "MindPalace",
			Content:           regularContent,
			RequestID:         requestID,
			StreamingComplete: isFinal,
		}
		a.globalAgg.ChatHistory = append(a.globalAgg.ChatHistory, newMessage)
	}

	// Update UI
	a.refreshUI()

	// Scroll to bottom to follow new content
	a.chatScroll.ScrollToBottom()
}
