package testapi

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"mindpalace/internal/orchestration"
	"mindpalace/internal/plugins"
	"mindpalace/pkg/eventsourcing"
	"mindpalace/pkg/logging"
)

// Server exposes a minimal HTTP surface for end-to-end testing.
type Server struct {
	addr           string
	eventProcessor *eventsourcing.EventProcessor
	pluginManager  *plugins.PluginManager
	httpServer     *http.Server
	orchestrator   *orchestration.RequestOrchestrator
}

// NewServer constructs a Server bound to addr (e.g. ":8092").
func NewServer(addr string, ep *eventsourcing.EventProcessor, pm *plugins.PluginManager, ro *orchestration.RequestOrchestrator) *Server {
	return &Server{
		addr:           addr,
		eventProcessor: ep,
		pluginManager:  pm,
		orchestrator:   ro,
	}
}

// Start launches the HTTP listeners in a goroutine.
func (s *Server) Start() {
	if s.addr == "" {
		logging.Info("testapi: HTTP API disabled (empty addr)")
		return
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/user-requests", s.handleUserRequest)
	mux.HandleFunc("/api/night-cycle", s.handleNightCycle)
	mux.HandleFunc("/api/context-window", s.handleContextWindow)

	s.httpServer = &http.Server{
		Addr:              s.addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		logging.Info("testapi: listening on %s", s.addr)
		if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logging.Error("testapi: server error: %v", err)
		}
	}()
}

type userRequestPayload struct {
	Text        string `json:"text"`
	RequestID   string `json:"request_id,omitempty"`
	TargetAgent string `json:"target_agent,omitempty"`
}

func (s *Server) handleUserRequest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var payload userRequestPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}
	payload.Text = strings.TrimSpace(payload.Text)
	if payload.Text == "" {
		http.Error(w, "text is required", http.StatusBadRequest)
		return
	}
	if payload.RequestID == "" {
		payload.RequestID = fmt.Sprintf("req_api_%d", time.Now().UnixNano())
	}

	cmdPayload := map[string]interface{}{
		"requestText": payload.Text,
		"requestID":   payload.RequestID,
	}
	if strings.TrimSpace(payload.TargetAgent) != "" {
		cmdPayload["targetAgent"] = strings.TrimSpace(payload.TargetAgent)
	}

	if err := s.eventProcessor.ExecuteCommand("ProcessUserRequest", cmdPayload); err != nil {
		logging.Error("testapi: ProcessUserRequest failed: %v", err)
		http.Error(w, "failed to process request", http.StatusInternalServerError)
		return
	}

	s.respondJSON(w, map[string]interface{}{
		"status":     "queued",
		"request_id": payload.RequestID,
	})
}

type nightCyclePayload struct {
	Reason string `json:"reason,omitempty"`
}

func (s *Server) handleNightCycle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var payload nightCyclePayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil && err != io.EOF {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}

	plugin, err := s.pluginManager.GetPlugin("dreamer")
	if err != nil {
		http.Error(w, "dreamer plugin unavailable", http.StatusServiceUnavailable)
		return
	}
	schema, ok := plugin.Schemas()["RunDreamCycle"]
	if !ok {
		http.Error(w, "dreamer plugin missing RunDreamCycle schema", http.StatusInternalServerError)
		return
	}
	input := schema.New()
	args := struct {
		Reason string `json:"Reason,omitempty"`
	}{Reason: strings.TrimSpace(payload.Reason)}

	if data, err := json.Marshal(args); err == nil {
		if err := json.Unmarshal(data, input); err != nil {
			http.Error(w, "failed to build command payload", http.StatusInternalServerError)
			return
		}
	} else {
		http.Error(w, "failed to build command payload", http.StatusInternalServerError)
		return
	}

	if err := s.eventProcessor.ExecuteCommand("RunDreamCycle", input); err != nil {
		logging.Error("testapi: RunDreamCycle failed: %v", err)
		http.Error(w, "failed to trigger dream cycle", http.StatusInternalServerError)
		return
	}

	s.respondJSON(w, map[string]interface{}{
		"status": "queued",
		"reason": args.Reason,
	})
}

type dawnResetSnapshot struct {
	WindowID        string   `json:"window_id"`
	CrystalIDs      []string `json:"crystal_ids"`
	EventIDs        []string `json:"event_ids"`
	AnchorIDs       []string `json:"anchor_ids"`
	BaselineSummary string   `json:"baseline_summary"`
	NextGoals       []string `json:"next_goals"`
	GeneratedAt     string   `json:"generated_at"`
	SourceReason    string   `json:"source_reason"`
	FitnessScore    float64  `json:"fitness_score"`
	WindowTokens    int      `json:"window_tokens"`
	SystemPrompt    string   `json:"system_prompt"`
}

type contextWindowResponse struct {
	dawnResetSnapshot
	ContextPreview *orchestration.ContextPreview `json:"llm_context,omitempty"`
}

func (s *Server) handleContextWindow(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	events := s.eventProcessor.GetEvents()
	var snapshot *dawnResetSnapshot
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Type() != "dreamer_DawnReset" {
			continue
		}
		raw, err := events[i].Marshal()
		if err != nil {
			continue
		}
		var candidate dawnResetSnapshot
		if err := json.Unmarshal(raw, &candidate); err != nil {
			continue
		}
		snapshot = &candidate
		break
	}

	if snapshot == nil {
		http.Error(w, "no dawn window available", http.StatusNotFound)
		return
	}

	response := contextWindowResponse{
		dawnResetSnapshot: *snapshot,
	}

	if s.orchestrator != nil {
		if preview, err := s.orchestrator.ContextPreview(); err != nil {
			logging.Error("testapi: failed to build context preview: %v", err)
		} else {
			response.ContextPreview = preview
		}
	}

	s.respondJSON(w, response)
	return

}

func (s *Server) respondJSON(w http.ResponseWriter, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		logging.Error("testapi: failed to encode response: %v", err)
	}
}
