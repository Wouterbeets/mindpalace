package orchestration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"text/template"
	"time"

	"mindpalace/internal/llmprocessor"
	"mindpalace/pkg/aggregate"
	"mindpalace/pkg/eventsourcing"
	"mindpalace/pkg/llmmodels"
	"mindpalace/pkg/logging"
)

// Interfaces for testability
type LLMClientInterface interface {
	ChatCompletion(ctx context.Context, messages []llmprocessor.Message, tools []llmprocessor.Tool, stream bool) (*llmprocessor.ChatResponse, error)
	Close() error
}

type PluginManagerInterface interface {
	GetLLMPlugins() []eventsourcing.Plugin
	GetPlugin(name string) (eventsourcing.Plugin, error)
	GetPluginByCommand(cmd string) (eventsourcing.Plugin, error)
	PluginMetadataSnapshot(name string) (eventsourcing.PluginMetadataSnapshot, bool)
	PluginSnapshots() []eventsourcing.PluginSnapshot
	PluginDefaultTimeout(name string, fallback time.Duration) time.Duration
	RecordInvocation(name string, result eventsourcing.PluginInvocationResult) eventsourcing.PluginTelemetry
}

type SyncPluginCatalogInput struct{}

type EventProcessorInterface interface {
	RegisterCommand(name string, handler eventsourcing.CommandHandler)
	ExecuteCommand(name string, data interface{}) error
}

type EventBusInterface interface {
	Subscribe(eventType string, handler eventsourcing.EventHandler)
	Publish(event eventsourcing.Event)
	SubscribeAll(handler eventsourcing.EventHandler)
}

const systemPromptTemplate = `You are MindPalace, a local-first steward of MemoryCrystals. Stay curious, truthful, and kind.

Principles:
- Decentralize divinity: run everything on-device and refuse hidden clouds.
- Honor dreamwork: dawn baselines and goals are the source of truth—reuse them before inventing.
- Lead with truth: verify facts, admit uncertainty, prefer surgical interventions over rewrites.
- Keep humans in the loop: narrate plans, ask clarifying questions, invite collaboration.

{{if .Agents}}
Agents online (call only when they clearly advance the goal):
{{range .Agents}}- {{.Name}} → {{.UsageHint}}
{{end}}
{{else}}
No specialist agents are online—answer directly.
{{end}}

Always ground replies in the latest dawn baseline + goals. If context is missing, ask the user for guidance.`

type agentPromptDescriptor struct {
	Name           string
	Summary        string
	UsageHint      string
	Tags           string
	Reliability    string
	SuccessRate    string
	AverageLatency string
	score          float64
}

// ContextPreview captures the exact LLM context MindPalace will feed into the router
// before deciding how to serve the next user request.
type ContextPreview struct {
	ActiveAgents  []string            `json:"active_agents"`
	SystemPrompt  string              `json:"system_prompt"`
	PluginPrompts map[string]string   `json:"plugin_prompts"`
	Messages      []llmmodels.Message `json:"messages"`
}

const defaultToolTimeout = 45 * time.Second

func (ro *RequestOrchestrator) buildAgentPromptDescriptors() []agentPromptDescriptor {
	snapshots := ro.pluginManager.PluginSnapshots()
	descriptors := make([]agentPromptDescriptor, 0, len(snapshots))

	for _, snapshot := range snapshots {
		meta := snapshot.Metadata
		tele := snapshot.Telemetry
		if meta.Name == "" {
			continue
		}

		summary := meta.Summary
		if summary == "" {
			summary = "No summary supplied—lean on explicit mentions."
		}
		usage := meta.UsageHint
		if usage == "" {
			usage = summary
		}

		successLine, successScore := renderSuccessRate(tele)
		reliabilityLine := renderReliability(meta, tele)
		tagsLine := strings.Join(meta.Tags, ", ")
		avgLatency := renderLatency(tele.AverageLatencyMillis)

		descriptors = append(descriptors, agentPromptDescriptor{
			Name:           meta.Name,
			Summary:        summary,
			UsageHint:      usage,
			Tags:           tagsLine,
			Reliability:    reliabilityLine,
			SuccessRate:    successLine,
			AverageLatency: avgLatency,
			score:          successScore,
		})
	}

	sort.Slice(descriptors, func(i, j int) bool {
		if descriptors[i].score == descriptors[j].score {
			return descriptors[i].Name < descriptors[j].Name
		}
		return descriptors[i].score > descriptors[j].score
	})

	return descriptors
}

func renderSuccessRate(tele eventsourcing.PluginTelemetry) (string, float64) {
	if tele.Invocations == 0 {
		return "", 0
	}
	rate := tele.SuccessRate()
	percentage := math.Round(rate * 100)
	return fmt.Sprintf("%d/%d successful (~%.0f%%)", tele.Successes, tele.Invocations, percentage), rate
}

func renderReliability(meta eventsourcing.PluginMetadataSnapshot, tele eventsourcing.PluginTelemetry) string {
	label := meta.Reliability
	if label == "" {
		if tele.Invocations >= 10 && tele.SuccessRate() >= 0.85 {
			label = "trusted"
		} else if tele.Invocations >= 3 {
			label = "learning"
		} else {
			label = "unknown"
		}
	}
	label = titleCase(label)
	if meta.Safety != "" {
		label = fmt.Sprintf("%s • safety: %s", label, meta.Safety)
	}
	if meta.Lifecycle != "" {
		label = fmt.Sprintf("%s • %s", label, meta.Lifecycle)
	}
	return label
}

func renderLatency(avgMillis float64) string {
	if avgMillis <= 0 {
		return ""
	}
	duration := time.Duration(avgMillis * float64(time.Millisecond))
	if duration < time.Millisecond {
		return fmt.Sprintf("~%dµs", duration/time.Microsecond)
	}
	if duration < time.Second {
		return fmt.Sprintf("~%dms", duration/time.Millisecond)
	}
	if duration < time.Minute {
		return fmt.Sprintf("~%s", duration.Round(10*time.Millisecond))
	}
	return fmt.Sprintf("~%s", duration.Round(time.Second))
}

func titleCase(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return value
	}
	lower := strings.ToLower(value)
	return strings.ToUpper(lower[:1]) + lower[1:]
}

func renderToolResultSummary(functionName, agentName string, emitted []map[string]interface{}) string {
	base := fmt.Sprintf("Tool '%s'", functionName)
	if agentName != "" {
		base = fmt.Sprintf("%s via %s", base, agentName)
	}
	if len(emitted) == 0 {
		return base + "."
	}

	primary := emitted[0]
	eventType := strings.TrimSpace(stringValue(primary["event_type"]))

	switch eventType {
	case "taskmanager_TaskCreated":
		title := stringValue(primary["title"])
		if title == "" {
			return base + " created a task."
		}
		status := strings.ToLower(stringValue(primary["status"]))
		priority := stringValue(primary["priority"])
		deadline := stringValue(primary["deadline"])

		var builder strings.Builder
		builder.WriteString(fmt.Sprintf("Created task \"%s\"", title))
		if priority != "" {
			builder.WriteString(fmt.Sprintf(" [%s]", priority))
		}
		if deadline != "" {
			builder.WriteString(fmt.Sprintf(" due %s", deadline))
		}
		if status != "" {
			builder.WriteString(fmt.Sprintf(" (%s)", status))
		}
		builder.WriteString(".")
		return builder.String()

	case "taskmanager_TaskUpdated":
		title := stringValue(primary["title"])
		if title == "" {
			title = stringValue(primary["task_id"])
		}
		if title == "" {
			return base + " updated a task."
		}
		status := stringValue(primary["status"])
		if status != "" {
			return fmt.Sprintf("Updated task \"%s\" to status %s.", title, status)
		}
		return fmt.Sprintf("Updated task \"%s\".", title)

	case "taskmanager_TaskCompleted":
		taskID := stringValue(primary["task_id"])
		if taskID == "" {
			return base + " completed a task."
		}
		return fmt.Sprintf("Marked task %s as completed.", taskID)
	}

	labels := make([]string, 0, len(emitted))
	for _, item := range emitted {
		etype := prettifyEventLabel(stringValue(item["event_type"]))
		if etype != "" {
			labels = append(labels, etype)
		}
		if len(labels) >= 2 {
			break
		}
	}
	if len(labels) > 0 {
		return fmt.Sprintf("%s emitted %s.", base, strings.Join(labels, " & "))
	}
	return base + "."
}

func stringValue(v interface{}) string {
	switch val := v.(type) {
	case string:
		return val
	case fmt.Stringer:
		return val.String()
	case []byte:
		return string(val)
	default:
		if val == nil {
			return ""
		}
		return fmt.Sprintf("%v", val)
	}
}

func toolCallSignature(call *agentSagaToolCall) string {
	if call == nil {
		return ""
	}
	payload, err := json.Marshal(call.Arguments)
	if err != nil {
		return strings.TrimSpace(call.Name)
	}
	return fmt.Sprintf("%s|%s", strings.TrimSpace(call.Name), payload)
}

type RequestOrchestrator struct {
	llmClient        LLMClientInterface
	pluginManager    PluginManagerInterface
	agg              *OrchestrationAggregate
	eventProcessor   EventProcessorInterface
	eventBus         EventBusInterface
	systemPromptTmpl *template.Template // Base template, no plugin specifics here
	commandChan      <-chan eventsourcing.CommandData
	controlChan      <-chan string
	aggStore         eventsourcing.AggregateStore
	events           []eventsourcing.Event
}

type agentSagaStatus string

const (
	sagaStatusNeedTool agentSagaStatus = "need_tool"
	sagaStatusSuccess  agentSagaStatus = "success"
	sagaStatusBlocked  agentSagaStatus = "blocked"
	maxSagaIterations                  = 6
)

type agentSagaOutcome struct {
	Goal     string             `json:"goal,omitempty"`
	Status   agentSagaStatus    `json:"status"`
	Summary  string             `json:"summary,omitempty"`
	Notes    []string           `json:"notes,omitempty"`
	ToolCall *agentSagaToolCall `json:"tool_call,omitempty"`
}

type agentSagaToolCall struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments"`
}

type agentSagaResult struct {
	Events     []eventsourcing.Event
	Summary    string
	RawSummary string
	Goal       string
	Success    bool
}

type agentSagaContext struct {
	RequestID   string
	AgentName   string
	UserRequest string
	PluginState string
	Goal        string
	Facts       []string
	LastSummary string
	Iteration   int
	ToolCounter int
	ParseErrors int
	LastToolSig string
	RepeatCalls int
}

const agentSagaInstruction = `You are collaborating with MindPalace in a compact goal-driven loop.

- Reaffirm or refine the working goal.
- Decide the next action.
- If a tool must run, reply with status "need_tool" and include {"tool_call":{"name":"...","arguments":{...}}}.
- After MindPalace returns tool results, reconsider and choose the next move.
- When the goal is achieved, reply with status "success" and provide a concise summary.
- If progress is blocked, reply with status "blocked" and describe what is missing.

Always respond using ONLY a JSON object with the schema:
{
  "goal": "string (optional)",
  "status": "need_tool | success | blocked",
  "summary": "string (optional)",
  "notes": ["optional additional bullet points"],
  "tool_call": {"name": "function name", "arguments": {...}} // required when status == "need_tool"
}

Write no extra prose outside the JSON.`

func (ctx *agentSagaContext) addFact(fact string) {
	fact = strings.TrimSpace(fact)
	if fact == "" {
		return
	}
	ctx.Facts = append(ctx.Facts, fact)
	if len(ctx.Facts) > 8 {
		ctx.Facts = ctx.Facts[len(ctx.Facts)-8:]
	}
}

func (ctx *agentSagaContext) buildUserMessage() string {
	var builder strings.Builder
	builder.WriteString("User request:\n")
	if ctx.UserRequest != "" {
		builder.WriteString(ctx.UserRequest)
		builder.WriteString("\n\n")
	} else {
		builder.WriteString("No explicit request text supplied.\n\n")
	}

	if ctx.Goal != "" {
		builder.WriteString("Working goal: ")
		builder.WriteString(ctx.Goal)
		builder.WriteString("\n\n")
	}

	if len(ctx.Facts) > 0 {
		builder.WriteString("Progress facts:\n")
		for _, fact := range ctx.Facts {
			builder.WriteString("- ")
			builder.WriteString(fact)
			builder.WriteString("\n")
		}
		builder.WriteString("\n")
	}

	if ctx.LastSummary != "" {
		builder.WriteString("Last agent summary: ")
		builder.WriteString(ctx.LastSummary)
		builder.WriteString("\n\n")
	}

	builder.WriteString("Iteration: ")
	builder.WriteString(fmt.Sprintf("%d\n", ctx.Iteration))
	builder.WriteString("Respond with the JSON schema described above.")
	return builder.String()
}

func (ro *RequestOrchestrator) buildAgentSagaMessages(plugin eventsourcing.Plugin, ctx *agentSagaContext) []llmprocessor.Message {
	var systemBuilder strings.Builder
	systemPrompt := strings.TrimSpace(plugin.SystemPrompt())
	if systemPrompt != "" {
		systemBuilder.WriteString(systemPrompt)
		systemBuilder.WriteString("\n\n")
	}
	systemBuilder.WriteString(agentSagaInstruction)
	if ctx.PluginState != "" {
		systemBuilder.WriteString("\n\nPlugin state JSON:\n")
		systemBuilder.WriteString(ctx.PluginState)
	}

	return []llmprocessor.Message{
		{Role: "system", Content: systemBuilder.String()},
		{Role: "user", Content: ctx.buildUserMessage()},
	}
}

func (ro *RequestOrchestrator) newAgentResponseEvent(requestID, agentName, stage, responseText, raw string) *AgentResponseEvent {
	return &AgentResponseEvent{
		RequestID:    requestID,
		AgentName:    agentName,
		ResponseText: responseText,
		RawResponse:  raw,
		Stage:        stage,
		Timestamp:    eventsourcing.ISOTimestamp(),
	}
}

func parseAgentSagaOutcome(raw string) (*agentSagaOutcome, error) {
	candidate := strings.TrimSpace(raw)
	if candidate == "" {
		return nil, fmt.Errorf("empty response")
	}
	if outcome, err := tryUnmarshalSagaOutcome(candidate); err == nil {
		return outcome, nil
	}
	if body, ok := extractJSONBlock(candidate); ok {
		if outcome, err := tryUnmarshalSagaOutcome(body); err == nil {
			return outcome, nil
		}
	}
	return nil, fmt.Errorf("unable to parse saga outcome")
}

func tryUnmarshalSagaOutcome(body string) (*agentSagaOutcome, error) {
	var outcome agentSagaOutcome
	if err := json.Unmarshal([]byte(body), &outcome); err != nil {
		return nil, err
	}
	if outcome.Status == "" {
		return nil, fmt.Errorf("missing status field")
	}
	switch outcome.Status {
	case sagaStatusNeedTool, sagaStatusSuccess, sagaStatusBlocked:
	default:
		return nil, fmt.Errorf("unrecognised status %q", outcome.Status)
	}
	if outcome.Status == sagaStatusNeedTool {
		if outcome.ToolCall == nil || strings.TrimSpace(outcome.ToolCall.Name) == "" {
			return nil, fmt.Errorf("tool_call missing for need_tool status")
		}
	}
	return &outcome, nil
}

func extractJSONBlock(text string) (string, bool) {
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start == -1 || end == -1 || end <= start {
		return "", false
	}
	return text[start : end+1], true
}

func summarizeToolEvents(toolEvents []eventsourcing.Event) (string, bool) {
	for _, evt := range toolEvents {
		switch e := evt.(type) {
		case *ToolCallCompleted:
			if summary, ok := e.Results["summary"].(string); ok && strings.TrimSpace(summary) != "" {
				return summary, true
			}
			base := fmt.Sprintf("Tool '%s' completed successfully.", e.Function)
			if e.AgentName != "" {
				base = fmt.Sprintf("Tool '%s' completed successfully via %s.", e.Function, e.AgentName)
			}
			return base, true
		case *ToolCallFailedEvent:
			msg := e.ErrorMsg
			if strings.TrimSpace(msg) == "" {
				msg = "tool call failed"
			}
			prefix := fmt.Sprintf("Tool '%s' failed", e.Function)
			if e.AgentName != "" {
				prefix = fmt.Sprintf("Tool '%s' failed via %s", e.Function, e.AgentName)
			}
			return fmt.Sprintf("%s: %s", prefix, msg), false
		}
	}
	return "", true
}

func renderStageSummary(outcome *agentSagaOutcome) string {
	if outcome == nil {
		return ""
	}
	if strings.TrimSpace(outcome.Summary) != "" {
		return strings.TrimSpace(outcome.Summary)
	}
	switch outcome.Status {
	case sagaStatusNeedTool:
		if outcome.ToolCall != nil && outcome.ToolCall.Name != "" {
			return fmt.Sprintf("Preparing to call %s.", outcome.ToolCall.Name)
		}
		return "Preparing next tool call."
	case sagaStatusSuccess:
		return "Goal achieved."
	case sagaStatusBlocked:
		return "Agent reports it is blocked."
	default:
		return "Agent response recorded."
	}
}

func NewRequestOrchestrator(llmClient LLMClientInterface, pm PluginManagerInterface, agg *OrchestrationAggregate, ep EventProcessorInterface, eb EventBusInterface, commandChan <-chan eventsourcing.CommandData, controlChan <-chan string, aggStore eventsourcing.AggregateStore, events []eventsourcing.Event) *RequestOrchestrator {
	tmpl, err := template.New("systemPrompt").Parse(systemPromptTemplate)
	if err != nil {
		logging.Error("Failed to parse system prompt template: %v", err)
		panic(err.Error())
	}
	ro := &RequestOrchestrator{
		llmClient:        llmClient,
		pluginManager:    pm,
		agg:              agg,
		eventProcessor:   ep,
		eventBus:         eb,
		systemPromptTmpl: tmpl,
		commandChan:      commandChan,
		controlChan:      controlChan,
		aggStore:         aggStore,
		events:           events,
	}
	ro.initializeCommandsAndSubscriptions()
	return ro
}

// DecideAgentCallCommand now dynamically fetches plugin prompts per call
func (ro *RequestOrchestrator) prepareLLMContext() ([]llmmodels.Message, []string, error) {
	cm := ro.agg.chatState.GetChatManager()
	if cm == nil {
		return nil, nil, fmt.Errorf("chat manager unavailable")
	}

	plugins := ro.pluginManager.GetLLMPlugins()
	pluginNames := make([]string, len(plugins))
	for i, plugin := range plugins {
		pluginNames[i] = plugin.Name()
	}

	cm.ResetPluginPrompts()
	for _, plugin := range plugins {
		prompt := strings.TrimSpace(plugin.Description())
		if prompt == "" {
			prompt = strings.TrimSpace(plugin.SystemPrompt())
		}
		if prompt != "" {
			cm.SetPluginPrompt(plugin.Name(), prompt)
		}
	}

	descriptors := ro.buildAgentPromptDescriptors()
	var promptBuffer bytes.Buffer
	if err := ro.systemPromptTmpl.Execute(&promptBuffer, map[string]interface{}{"Agents": descriptors}); err != nil {
		logging.Error("Failed to render system prompt: %v", err)
	} else {
		cm.SetSystemPrompt(promptBuffer.String())
	}

	llmMessages := cm.GetLLMContext(pluginNames)
	return llmMessages, pluginNames, nil
}

func (ro *RequestOrchestrator) ContextPreview() (*ContextPreview, error) {
	llmMessages, activeAgents, err := ro.prepareLLMContext()
	if err != nil {
		return nil, err
	}

	cm := ro.agg.chatState.GetChatManager()
	if cm == nil {
		return nil, fmt.Errorf("chat manager unavailable")
	}

	return &ContextPreview{
		ActiveAgents:  activeAgents,
		SystemPrompt:  cm.SystemPrompt(),
		PluginPrompts: cm.PluginPrompts(),
		Messages:      llmMessages,
	}, nil
}

func (ro *RequestOrchestrator) DecideAgentCallCommand(event *UserRequestReceivedEvent) ([]eventsourcing.Event, error) {
	preferredAgent := strings.TrimSpace(event.TargetAgent)
	if preferredAgent != "" {
		if plugin := ro.findPluginByName(preferredAgent); plugin != nil {
			now := eventsourcing.ISOTimestamp()
			logging.Info("Honoring targeted agent request %s -> %s", event.RequestID, plugin.Name())
			return []eventsourcing.Event{
				&AgentCallDecidedEvent{
					RequestID:   event.RequestID,
					AgentName:   plugin.Name(),
					CallAgent:   true,
					Timestamp:   now,
					Model:       plugin.AgentModel(),
					Query:       event.RequestText,
					RawResponse: fmt.Sprintf("Direct conversation routed to %s", plugin.Name()),
				},
			}, nil
		}
		logging.Info("Target agent %s not found for request %s; falling back to orchestrator routing", preferredAgent, event.RequestID)
	}

	// Get LLM context with fresh plugin data
	llmMessages, _, err := ro.prepareLLMContext()
	if err != nil {
		return nil, err
	}
	// Convert messages
	messages := make([]llmprocessor.Message, len(llmMessages))
	for i, m := range llmMessages {
		messages[i] = llmprocessor.Message{
			Role:    m.Role,
			Content: m.Content,
		}
	}
	// Convert tools
	llmTools := ro.gatherAgentTools()
	tools := make([]llmprocessor.Tool, len(llmTools))
	for i, t := range llmTools {
		funcMap := t.Function
		params, _ := json.Marshal(funcMap["parameters"])
		tools[i] = llmprocessor.Tool{
			Type: t.Type,
			Function: llmprocessor.FunctionDef{
				Name:        funcMap["name"].(string),
				Description: funcMap["description"].(string),
				Parameters:  params,
			},
		}
	}
	ctx := context.WithValue(context.Background(), "request_id", event.RequestID)
	resp, err := ro.llmClient.ChatCompletion(ctx, messages, tools, false)
	if err != nil {
		return nil, fmt.Errorf("LLM call failed: %v", err)
	}

	choice := resp.Choices[0]
	var events []eventsourcing.Event

	if len(choice.ToolCalls) == 0 {
		now := eventsourcing.ISOTimestamp()
		events = append(events, &AgentCallDecidedEvent{
			RequestID:    event.RequestID,
			AgentName:    "",
			CallAgent:    false,
			Timestamp:    now,
			ResponseText: choice.Message.Content,
			RawResponse:  choice.Message.Content,
		})
		events = append(events, &RequestCompletedEvent{
			RequestID:    event.RequestID,
			ResponseText: choice.Message.Content,
			CompletedAt:  now,
		})
		return events, nil
	}

	for _, call := range choice.ToolCalls {
		plug, err := ro.pluginManager.GetPlugin(call.Function.Name)
		if err != nil {
			return nil, fmt.Errorf("requested plugin does not exist: %w", err)
		}
		var args map[string]interface{}
		if err := json.Unmarshal(call.Function.Arguments, &args); err != nil {
			return nil, fmt.Errorf("failed to unmarshal arguments for plugin %s: %w", plug.Name(), err)
		}
		queryBytes, _ := json.Marshal(args)
		query := string(queryBytes)
		agentCallEvent := &AgentCallDecidedEvent{
			RequestID:   event.RequestID,
			AgentName:   plug.Name(),
			CallAgent:   true,
			Timestamp:   eventsourcing.ISOTimestamp(),
			Model:       plug.AgentModel(),
			Query:       query,
			RawResponse: choice.Message.Content,
		}
		logging.Debug("agent call event: %v", agentCallEvent)
		events = append(events, agentCallEvent)
	}
	logging.Info("DecideAgentCallCommand generated %d events", len(events))
	return events, nil
}

// gatherAgentTools gathers tools for available LLM plugins using their descriptions
func (ro *RequestOrchestrator) gatherAgentTools() []llmmodels.Tool {
	var tools []llmmodels.Tool
	for _, plugin := range ro.pluginManager.GetLLMPlugins() {
		tools = append(tools, llmmodels.Tool{
			Type: "function",
			Function: map[string]interface{}{
				"name":        plugin.Name(),
				"description": plugin.Description(),
				"parameters": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"query": map[string]interface{}{
							"type":        "string",
							"description": "query for the agent",
						},
					},
					"required": []string{"query"},
				},
			},
		})
	}
	return tools
}

// gatherAgentToolsFor gathers tools only for a given subset of plugins
// (reverted) shortlist functions removed per user request

// commandHandler defines the structure for command registration
type commandHandler struct {
	name    string
	handler eventsourcing.CommandHandler
}

// eventSubscription defines the structure for event subscriptions
type eventSubscription struct {
	eventType string
	handler   func(eventsourcing.Event) error
}

func (ro *RequestOrchestrator) Start() {
	go func() {
		for cmd := range ro.commandChan {
			err := ro.eventProcessor.ExecuteCommand(cmd.Name, cmd.Data)
			if err != nil {
				logging.Error("Failed to execute command %s: %v", cmd.Name, err)
			}
		}
	}()

	go func() {
		for msg := range ro.controlChan {
			switch msg {
			case "rebuild_state":
				logging.Info("Rebuilding aggregate state after ready signal")
				if aggManager, ok := ro.aggStore.(*aggregate.AggregateManager); ok {
					logging.Info("ORCHESTRATOR: Starting rebuild with %d events", len(ro.events))
					err := aggManager.RebuildState(ro.events)
					if err != nil {
						logging.Error("Failed to rebuild state: %v", err)
					} else {
						logging.Info("ORCHESTRATOR: Rebuild completed successfully")
						if err := ro.eventProcessor.ExecuteCommand("SyncPluginCatalog", &SyncPluginCatalogInput{}); err != nil {
							logging.Error("Failed to sync plugin catalog after rebuild: %v", err)
						}
					}
				} else {
					logging.Error("AggregateStore is not *AggregateManager")
				}
			default:
				logging.Info("Unknown control message: %s", msg)
			}
		}
	}()
}

func (ro *RequestOrchestrator) initializeCommandsAndSubscriptions() {
	// Define all command handlers
	commands := []commandHandler{
		{
			name:    "ProcessUserRequest",
			handler: eventsourcing.NewCommand(ro.ProcessUserRequestCommand),
		},
		{
			name:    "SyncPluginCatalog",
			handler: eventsourcing.NewCommand(ro.SyncPluginCatalogCommand),
		},
		{
			name:    "DecideAgentCall",
			handler: eventsourcing.NewCommand(ro.DecideAgentCallCommand),
		},
		{
			name:    "ExecuteAgentCall",
			handler: eventsourcing.NewCommand(ro.ExecuteAgentCall),
		},
	}

	// Define all event subscriptions
	subscriptions := []eventSubscription{
		{
			eventType: "orchestration_UserRequestReceived",
			handler: func(event eventsourcing.Event) error {
				return ro.eventProcessor.ExecuteCommand("DecideAgentCall", event)
			},
		},
		{
			eventType: "orchestration_AgentCallDecided",
			handler: func(event eventsourcing.Event) error {
				if e, ok := event.(*AgentCallDecidedEvent); ok {
					if !e.CallAgent {
						logging.Debug("Agent decision for request %s resolved without downstream agent call", e.RequestID)
						return nil
					}
					return ro.eventProcessor.ExecuteCommand("ExecuteAgentCall", e)
				}
				return nil
			},
		},
	}

	// Register all commands
	for _, cmd := range commands {
		ro.eventProcessor.RegisterCommand(cmd.name, cmd.handler)
	}

	// Register all subscriptions
	for _, sub := range subscriptions {
		ro.eventBus.Subscribe(sub.eventType, sub.handler)
		logging.Debug("Subscribed to event: %s", sub.eventType)
	}
}

func (ro *RequestOrchestrator) SyncPluginCatalogCommand(_ *SyncPluginCatalogInput) ([]eventsourcing.Event, error) {
	snapshots := ro.pluginManager.PluginSnapshots()
	events := make([]eventsourcing.Event, 0, len(snapshots))
	for _, snapshot := range snapshots {
		events = append(events, &PluginSnapshotUpsertedEvent{
			Snapshot:  snapshot,
			Timestamp: eventsourcing.ISOTimestamp(),
		})
	}
	return events, nil
}

func (ro *RequestOrchestrator) findPluginByName(name string) eventsourcing.Plugin {
	if name == "" {
		return nil
	}
	if plugin, err := ro.pluginManager.GetPlugin(name); err == nil && plugin != nil {
		return plugin
	}
	lower := strings.ToLower(name)
	for _, plugin := range ro.pluginManager.GetLLMPlugins() {
		if strings.ToLower(plugin.Name()) == lower {
			return plugin
		}
	}
	return nil
}

func (ro *RequestOrchestrator) ProcessUserRequestCommand(data map[string]interface{}) ([]eventsourcing.Event, error) {
	requestText, ok := data["requestText"].(string)
	if !ok {
		return nil, fmt.Errorf("requestText must be a string")
	}
	requestID, _ := data["requestID"].(string)
	if requestID == "" {
		requestID = fmt.Sprintf("req-%d", time.Now().UnixNano())
	}
	targetAgent, _ := data["targetAgent"].(string)
	targetAgent = strings.TrimSpace(targetAgent)

	if targetAgent != "" {
		logging.Info("Processing user request. Request ID: %s (target agent: %s)", requestID, targetAgent)
	} else {
		logging.Info("Processing user request. Request ID: %s", requestID)
	}

	return []eventsourcing.Event{
		&UserRequestReceivedEvent{
			EventType:   "orchestration_UserRequestReceived",
			RequestID:   requestID,
			RequestText: requestText,
			Timestamp:   eventsourcing.ISOTimestamp(),
			TargetAgent: targetAgent,
		},
	}, nil
}

func (ro *RequestOrchestrator) ExecuteToolCallCommand(event *ToolCallRequestPlaced) ([]eventsourcing.Event, error) {
	var events []eventsourcing.Event

	startTimestamp := eventsourcing.ISOTimestamp()
	events = append(events, &ToolCallStarted{
		RequestID:  event.RequestID,
		ToolCallID: event.ToolCallID,
		Function:   event.Function,
		AgentName:  event.AgentName,
		Timestamp:  startTimestamp,
	})

	plugin, err := ro.pluginManager.GetPluginByCommand(event.Function)
	if err != nil {
		errorMsg := fmt.Sprintf("no plugin found for command %s", event.Function)
		logging.Error(errorMsg)
		events = append(events, &ToolCallFailedEvent{
			EventType:  "orchestration_ToolCallFailed",
			RequestID:  event.RequestID,
			ToolCallID: event.ToolCallID,
			Function:   event.Function,
			AgentName:  event.AgentName,
			ErrorMsg:   errorMsg,
			Timestamp:  eventsourcing.ISOTimestamp(),
		})
		return events, nil
	}

	pluginName := plugin.Name()
	startTime := time.Now()
	appendSnapshot := func(telemetry eventsourcing.PluginTelemetry) {
		if snapshot, ok := ro.pluginManager.PluginMetadataSnapshot(pluginName); ok {
			events = append(events, &PluginSnapshotUpsertedEvent{
				Snapshot: eventsourcing.PluginSnapshot{
					Metadata:  snapshot,
					Telemetry: telemetry,
				},
				Timestamp: eventsourcing.ISOTimestamp(),
			})
		}
	}
	fail := func(errorMsg string, timedOut bool, panicked bool) ([]eventsourcing.Event, error) {
		logging.Error(errorMsg)
		events = append(events, &ToolCallFailedEvent{
			EventType:  "orchestration_ToolCallFailed",
			RequestID:  event.RequestID,
			ToolCallID: event.ToolCallID,
			Function:   event.Function,
			AgentName:  event.AgentName,
			ErrorMsg:   errorMsg,
			Timestamp:  eventsourcing.ISOTimestamp(),
		})
		telemetry := ro.pluginManager.RecordInvocation(pluginName, eventsourcing.PluginInvocationResult{
			Timestamp: time.Now(),
			Duration:  time.Since(startTime),
			Success:   false,
			Error:     errorMsg,
			TimedOut:  timedOut,
			Panicked:  panicked,
		})
		appendSnapshot(telemetry)
		return events, nil
	}

	schemas := plugin.Schemas()
	inputSchema, exists := schemas[event.Function]
	if !exists {
		return fail(fmt.Sprintf("no schema found for command %s", event.Function), false, false)
	}

	input := inputSchema.New()
	inputJSON, err := json.Marshal(event.Arguments)
	if err != nil {
		return fail(fmt.Sprintf("failed to marshal arguments: %v", err), false, false)
	}

	if err := json.Unmarshal(inputJSON, input); err != nil {
		return fail(fmt.Sprintf("failed to unmarshal arguments into %T: %v", input, err), false, false)
	}

	handler, exists := plugin.Commands()[event.Function]
	if !exists {
		return fail(fmt.Sprintf("no handler for command %s", event.Function), false, false)
	}

	type toolCallResult struct {
		events    []eventsourcing.Event
		err       error
		panicWith interface{}
	}

	resultChan := make(chan toolCallResult, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				resultChan <- toolCallResult{panicWith: r}
			}
		}()
		emitEvents, execErr := handler.Execute(input)
		resultChan <- toolCallResult{events: emitEvents, err: execErr}
	}()

	timeout := ro.pluginManager.PluginDefaultTimeout(pluginName, defaultToolTimeout)
	if timeout <= 0 {
		timeout = defaultToolTimeout
	}

	select {
	case outcome := <-resultChan:
		if outcome.panicWith != nil {
			return fail(fmt.Sprintf("command %s panicked: %v", event.Function, outcome.panicWith), false, true)
		}
		if outcome.err != nil {
			return fail(fmt.Sprintf("command %s failed: %v", event.Function, outcome.err), false, false)
		}
		// Collect emitted events for inclusion in tool results so the LLM
		// can see actual outputs rather than just telemetry.
		emitted := make([]map[string]interface{}, 0, len(outcome.events))
		for _, toolEvent := range outcome.events {
			logging.Debug("tool call returned event: %v", toolEvent)
			// Best-effort JSON round-trip into a map for readability.
			if raw, err := toolEvent.Marshal(); err == nil {
				var m map[string]interface{}
				if uerr := json.Unmarshal(raw, &m); uerr == nil {
					// Ensure an event_type field exists for the LLM to identify.
					if _, ok := m["event_type"]; !ok {
						m["event_type"] = toolEvent.Type()
					}
					emitted = append(emitted, m)
				} else {
					// Fall back to a minimal map if unmarshal fails.
					emitted = append(emitted, map[string]interface{}{
						"event_type": toolEvent.Type(),
						"raw":        string(raw),
					})
				}
			} else {
				emitted = append(emitted, map[string]interface{}{
					"event_type": toolEvent.Type(),
					"error":      fmt.Sprintf("marshal failed: %v", err),
				})
			}
		}
		events = append(events, outcome.events...)
		duration := time.Since(startTime)
		telemetry := ro.pluginManager.RecordInvocation(pluginName, eventsourcing.PluginInvocationResult{
			Timestamp: time.Now(),
			Duration:  duration,
			Success:   true,
		})
		resultSummary := map[string]interface{}{
			"success":        true,
			"events_emitted": len(outcome.events),
			"latency_ms":     float64(duration) / float64(time.Millisecond),
			"telemetry":      telemetry,
			"function":       event.Function,
			"function_args":  event.Arguments,
			"emitted_events": emitted, // structured outputs for downstream LLM context
		}
		if summary := renderToolResultSummary(event.Function, event.AgentName, emitted); summary != "" {
			resultSummary["summary"] = summary
		}
		events = append(events, &ToolCallCompleted{
			RequestID:  event.RequestID,
			ToolCallID: event.ToolCallID,
			Function:   event.Function,
			AgentName:  event.AgentName,
			Results:    resultSummary,
			Timestamp:  eventsourcing.ISOTimestamp(),
		})
		appendSnapshot(telemetry)
		logging.Info("added tool call completed event")
		return events, nil
	case <-time.After(timeout):
		timedOutMsg := fmt.Sprintf("command %s timed out after %s", event.Function, timeout)
		return fail(timedOutMsg, true, false)
	}

	return events, nil
}

// gatherPluginTools gathers tools specific to a given plugin
func (ro *RequestOrchestrator) gatherPluginTools(plugin eventsourcing.Plugin) []llmmodels.Tool {
	var tools []llmmodels.Tool
	for name, schema := range plugin.Schemas() {
		tools = append(tools, llmmodels.Tool{
			Type: "function",
			Function: map[string]interface{}{
				"name":        name,
				"description": schema.Schema()["description"],
				"parameters":  schema.Schema()["parameters"],
			},
		})
	}
	return tools
}

func (ro *RequestOrchestrator) RunAgentSaga(plugin eventsourcing.Plugin, event *AgentCallDecidedEvent) (*agentSagaResult, error) {
	stateJSON, err := json.Marshal(plugin.Aggregate())
	if err != nil {
		return nil, fmt.Errorf("failed to marshal plugin state: %v", err)
	}

	ctx := &agentSagaContext{
		RequestID:   event.RequestID,
		AgentName:   plugin.Name(),
		UserRequest: strings.TrimSpace(event.Query),
		PluginState: string(stateJSON),
		Facts:       make([]string, 0),
	}
	if ctx.UserRequest == "" {
		ctx.UserRequest = "Proceed with the outstanding objective."
	}

	result := &agentSagaResult{
		Events: make([]eventsourcing.Event, 0),
	}

	llmTools := ro.gatherPluginTools(plugin)
	tools := make([]llmprocessor.Tool, len(llmTools))
	for i, t := range llmTools {
		funcMap := t.Function
		params, _ := json.Marshal(funcMap["parameters"])
		name, _ := funcMap["name"].(string)
		description, _ := funcMap["description"].(string)
		tools[i] = llmprocessor.Tool{
			Type: t.Type,
			Function: llmprocessor.FunctionDef{
				Name:        name,
				Description: description,
				Parameters:  params,
			},
		}
	}

	for iteration := 1; iteration <= maxSagaIterations; iteration++ {
		ctx.Iteration = iteration
		messages := ro.buildAgentSagaMessages(plugin, ctx)

		llmCtx := context.WithValue(context.Background(), "request_id", event.RequestID)
		resp, err := ro.llmClient.ChatCompletion(llmCtx, messages, tools, false)
		if err != nil {
			return nil, fmt.Errorf("agent saga LLM call failed: %w", err)
		}
		if len(resp.Choices) == 0 {
			return nil, fmt.Errorf("agent saga returned no choices")
		}

		rawContent := strings.TrimSpace(resp.Choices[0].Message.Content)
		outcome, parseErr := parseAgentSagaOutcome(rawContent)
		if parseErr != nil {
			ctx.ParseErrors++
			warning := fmt.Sprintf("Agent response could not be parsed: %v", parseErr)
			ctx.LastSummary = warning + " Please reply with the JSON schema."
			ctx.addFact("Previous agent reply was not valid JSON. Respond using only the specified schema.")
			result.Events = append(result.Events, ro.newAgentResponseEvent(event.RequestID, plugin.Name(), "clarify", ctx.LastSummary, rawContent))
			if ctx.ParseErrors >= 2 {
				result.Success = false
				result.Summary = warning
				result.RawSummary = rawContent
				return result, nil
			}
			continue
		}

		if strings.TrimSpace(outcome.Goal) != "" {
			ctx.Goal = strings.TrimSpace(outcome.Goal)
		}
		for _, note := range outcome.Notes {
			ctx.addFact(note)
		}

		stageSummary := renderStageSummary(outcome)
		stage := "progress"
		switch outcome.Status {
		case sagaStatusNeedTool:
			stage = "planning"
		case sagaStatusSuccess:
			stage = "final"
		case sagaStatusBlocked:
			stage = "blocked"
		}
		ctx.LastSummary = stageSummary
		result.Events = append(result.Events, ro.newAgentResponseEvent(event.RequestID, plugin.Name(), stage, stageSummary, rawContent))

		switch outcome.Status {
		case sagaStatusNeedTool:
			signature := toolCallSignature(outcome.ToolCall)
			if signature != "" {
				if signature == ctx.LastToolSig {
					ctx.RepeatCalls++
					if ctx.RepeatCalls == 2 {
						reminder := "This tool call matches the previous step. Verify whether the goal is already satisfied and respond with status \"success\" when appropriate."
						ctx.addFact(reminder)
						ctx.LastSummary = reminder
						continue
					}
					if ctx.RepeatCalls >= 3 {
						blockMsg := fmt.Sprintf("Agent repeated identical tool call %s without progress.", outcome.ToolCall.Name)
						result.Events = append(result.Events, ro.newAgentResponseEvent(event.RequestID, plugin.Name(), "blocked", blockMsg, rawContent))
						result.Success = false
						result.Summary = blockMsg
						result.RawSummary = rawContent
						return result, nil
					}
				} else {
					ctx.LastToolSig = signature
					ctx.RepeatCalls = 1
				}
			}
			toolCallID := fmt.Sprintf("%s-toolcall-%d", event.RequestID, ctx.ToolCounter)
			ctx.ToolCounter++
			toolEvent := &ToolCallRequestPlaced{
				RequestID:  event.RequestID,
				ToolCallID: toolCallID,
				Function:   outcome.ToolCall.Name,
				AgentName:  plugin.Name(),
				Arguments:  outcome.ToolCall.Arguments,
				Timestamp:  eventsourcing.ISOTimestamp(),
			}
			result.Events = append(result.Events, toolEvent)

			toolEvents, toolErr := ro.ExecuteToolCallCommand(toolEvent)
			result.Events = append(result.Events, toolEvents...)

			summary, success := summarizeToolEvents(toolEvents)
			if summary != "" {
				ctx.addFact(summary)
				ctx.LastSummary = summary + " If this completes the goal, reply with status \"success\" and share a concise summary."
			}
			ctx.LastToolSig = ""
			ctx.RepeatCalls = 0
			if stateJSON, err := json.Marshal(plugin.Aggregate()); err == nil {
				ctx.PluginState = string(stateJSON)
			}

			if toolErr != nil {
				result.Success = false
				if summary == "" {
					summary = fmt.Sprintf("Tool %s execution hit an error: %v", outcome.ToolCall.Name, toolErr)
				}
				result.Summary = summary
				result.RawSummary = rawContent
				return result, nil
			}
			if !success {
				if summary == "" {
					summary = fmt.Sprintf("Tool %s failed.", outcome.ToolCall.Name)
				}
				result.Success = false
				result.Summary = summary
				result.RawSummary = rawContent
				return result, nil
			}

			continue

		case sagaStatusSuccess:
			finalSummary := stageSummary
			if finalSummary == "" {
				switch {
				case ctx.LastSummary != "":
					finalSummary = ctx.LastSummary
				case len(ctx.Facts) > 0:
					finalSummary = ctx.Facts[len(ctx.Facts)-1]
				default:
					finalSummary = "Goal achieved."
				}
			}
			result.Success = true
			result.Summary = finalSummary
			result.RawSummary = rawContent
			return result, nil

		case sagaStatusBlocked:
			blockSummary := stageSummary
			if blockSummary == "" {
				if ctx.LastSummary != "" {
					blockSummary = ctx.LastSummary
				} else {
					blockSummary = "Agent reported it is blocked."
				}
			}
			result.Success = false
			result.Summary = blockSummary
			result.RawSummary = rawContent
			return result, nil
		}
	}

	result.Success = false
	result.Summary = "Agent saga exceeded iteration limit."
	result.RawSummary = result.Summary
	return result, nil
}

func (ro *RequestOrchestrator) ExecuteAgentCall(event *AgentCallDecidedEvent) ([]eventsourcing.Event, error) {
	if !event.CallAgent {
		logging.Debug("ExecuteAgentCall skipped for request %s: decision resolved without agent", event.RequestID)
		return nil, nil
	}

	plugin, err := ro.pluginManager.GetPlugin(event.AgentName)
	if err != nil {
		errorMsg := fmt.Sprintf("agent call failed: %v", err)
		return []eventsourcing.Event{&AgentExecutionFailedEvent{
			EventType:   "orchestration_AgentExecutionFailed",
			RequestID:   event.RequestID,
			AgentName:   event.AgentName,
			ErrorMsg:    errorMsg,
			Timestamp:   eventsourcing.ISOTimestamp(),
			Recoverable: false,
		}}, nil
	}

	sagaResult, sagaErr := ro.RunAgentSaga(plugin, event)
	events := make([]eventsourcing.Event, 0)
	if sagaResult != nil {
		events = append(events, sagaResult.Events...)
	}
	if sagaErr != nil {
		errorMsg := fmt.Sprintf("agent saga failed: %v", sagaErr)
		events = append(events, &AgentExecutionFailedEvent{
			EventType:   "orchestration_AgentExecutionFailed",
			RequestID:   event.RequestID,
			AgentName:   event.AgentName,
			ErrorMsg:    errorMsg,
			Timestamp:   eventsourcing.ISOTimestamp(),
			Recoverable: false,
		})
		events = append(events, &RequestCompletedEvent{
			EventType:    "orchestration_RequestCompleted",
			RequestID:    event.RequestID,
			ResponseText: fmt.Sprintf("I hit an internal error coordinating %s: %s", event.AgentName, errorMsg),
			CompletedAt:  eventsourcing.ISOTimestamp(),
		})
		return events, nil
	}

	if sagaResult == nil {
		errorMsg := "agent saga returned no result"
		events = append(events, &AgentExecutionFailedEvent{
			EventType:   "orchestration_AgentExecutionFailed",
			RequestID:   event.RequestID,
			AgentName:   event.AgentName,
			ErrorMsg:    errorMsg,
			Timestamp:   eventsourcing.ISOTimestamp(),
			Recoverable: false,
		})
		events = append(events, &RequestCompletedEvent{
			EventType:    "orchestration_RequestCompleted",
			RequestID:    event.RequestID,
			ResponseText: fmt.Sprintf("I could not complete the request because %s.", errorMsg),
			CompletedAt:  eventsourcing.ISOTimestamp(),
		})
		return events, nil
	}

	if sagaResult.Success {
		finalText := sagaResult.Summary
		if strings.TrimSpace(finalText) == "" {
			finalText = fmt.Sprintf("%s completed the requested goal.", event.AgentName)
		}
		events = append(events, &RequestCompletedEvent{
			EventType:    "orchestration_RequestCompleted",
			RequestID:    event.RequestID,
			ResponseText: finalText,
			CompletedAt:  eventsourcing.ISOTimestamp(),
		})
		return events, nil
	}

	failureSummary := sagaResult.Summary
	if strings.TrimSpace(failureSummary) == "" {
		failureSummary = fmt.Sprintf("%s was unable to complete the task.", event.AgentName)
	}
	events = append(events, &AgentExecutionFailedEvent{
		EventType:   "orchestration_AgentExecutionFailed",
		RequestID:   event.RequestID,
		AgentName:   event.AgentName,
		ErrorMsg:    failureSummary,
		Timestamp:   eventsourcing.ISOTimestamp(),
		Recoverable: false,
	})
	events = append(events, &RequestCompletedEvent{
		EventType:    "orchestration_RequestCompleted",
		RequestID:    event.RequestID,
		ResponseText: fmt.Sprintf("I could not finish because %s.", failureSummary),
		CompletedAt:  eventsourcing.ISOTimestamp(),
	})
	return events, nil
}
