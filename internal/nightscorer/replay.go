package nightscorer

import (
    "crypto/sha1"
    "encoding/hex"
    "encoding/json"
    "fmt"
    "context"

    "mindpalace/internal/llmprocessor"
    "mindpalace/pkg/aggregate"
    "mindpalace/pkg/eventsourcing"
    "mindpalace/pkg/logging"
)

// RunScoringReplay performs a silent state rebuild and then iterates through all
// events, prompting an LLM to evaluate each event and call a scoring tool. Any
// resulting score is recorded into the target aggregate if it implements
// eventsourcing.EventScorable.
func RunScoringReplay(mgr *aggregate.AggregateManager, events []eventsourcing.Event, targetAggregate string, llm llmprocessor.LLMClient, scoreName, label string) error {
    if mgr == nil || llm == nil {
        return fmt.Errorf("nightscorer: missing manager or llm client")
    }
    // Rebuild silently to ensure in-memory state is consistent without UI acks
    if err := mgr.RebuildStateSilent(events); err != nil {
        return err
    }

    agg, err := mgr.AggregateByName(targetAggregate)
    if err != nil {
        return fmt.Errorf("nightscorer: target aggregate not found: %w", err)
    }
    sink, ok := agg.(eventsourcing.EventScorable)
    if !ok {
        return fmt.Errorf("nightscorer: aggregate %s is not scorable", agg.ID())
    }

    // Prepare static tool definition for scoring
    tool := llmprocessor.Tool{
        Type: "function",
        Function: llmprocessor.FunctionDef{
            Name:        "record_score",
            Description: "Record a score for a given event. Always call exactly once with the provided event_id.",
            Parameters:  []byte(`{"type":"object","properties":{"event_id":{"type":"string"},"score_name":{"type":"string"},"label":{"type":"string"},"score":{"type":"number","minimum":0,"maximum":1},"rationale":{"type":"string"}},"required":["event_id","score_name","score"]}`),
        },
    }

    // If available, pull current open tasks to give the LLM context for task relevance scoring.
    var openTasks []eventsourcing.TaskSummary
    if prov, ok := agg.(eventsourcing.TaskSummaryProvider); ok && scoreName == "task_relevance" {
        openTasks = prov.OpenTaskSummaries()
    }

    for i, ev := range events {
        raw, _ := ev.Marshal()
        eventID := hashEvent(raw)

        // Build a context-aware system prompt.
        sysContent := fmt.Sprintf(
            "You are an event scoring assistant. You receive a single event (JSON).\n"+
                "Score it for the criterion named '%s' (label: '%s') on a 0..1 scale.\n"+
                "You MUST call the tool 'record_score' exactly once with: event_id='%s', score_name, label, score.\n"+
                "Score semantics:\n"+
                "- 0.0 = irrelevant; 1.0 = maximally relevant.\n",
            scoreName, label, eventID,
        )

        if scoreName == "task_relevance" && len(openTasks) > 0 {
            // Provide a compact view of open tasks.
            tasksJSON, _ := json.Marshal(struct {
                Tasks []eventsourcing.TaskSummary `json:"tasks"`
            }{Tasks: openTasks})
            sysContent += "\nOpenTasks JSON follows. Consider titles, descriptions, tags, and near-term deadlines when scoring.\n"
            sysContent += string(tasksJSON)
        }

        sys := llmprocessor.Message{Role: "system", Content: sysContent}

        usr := llmprocessor.Message{Role: "user", Content: string(raw)}

        resp, err := llm.ChatCompletion(
            context.Background(),
            []llmprocessor.Message{sys, usr},
            []llmprocessor.Tool{tool},
            false,
        )
        if err != nil {
            logging.Error("nightscorer: LLM failed on event %d (%s): %v", i+1, ev.Type(), err)
            continue
        }
        if len(resp.Choices) == 0 {
            continue
        }
        choice := resp.Choices[0]
        if len(choice.ToolCalls) == 0 {
            logging.Debug("nightscorer: no tool call for event %s", ev.Type())
            continue
        }
        for _, call := range choice.ToolCalls {
            if call.Function.Name != "record_score" {
                continue
            }
            var args struct {
                EventID   string   `json:"event_id"`
                ScoreName string   `json:"score_name"`
                Label     string   `json:"label"`
                Score     float64  `json:"score"`
                Rationale *string  `json:"rationale,omitempty"`
            }
            if err := json.Unmarshal(call.Function.Arguments, &args); err != nil {
                logging.Error("nightscorer: bad tool args: %v", err)
                continue
            }
            if args.EventID == "" {
                args.EventID = eventID
            }
            if args.ScoreName == "" {
                args.ScoreName = scoreName
            }
            if args.Label == "" {
                args.Label = label
            }
            sink.RecordEventScore(args.EventID, eventsourcing.EventScore{
                Name:  args.ScoreName,
                Label: args.Label,
                Value: args.Score,
            })
        }
    }

    return nil
}

func hashEvent(b []byte) string {
    h := sha1.Sum(b)
    return hex.EncodeToString(h[:])
}
