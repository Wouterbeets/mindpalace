package godot_ws

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"mindpalace/internal/orchestration"
	"mindpalace/pkg/eventsourcing"
	"mindpalace/pkg/logging"
)

type GodotServer struct {
	upgrader   websocket.Upgrader
	clients    map[*websocket.Conn]*ClientState
	clientsMu  sync.RWMutex
	deltaChan  chan eventsourcing.DeltaEnvelope
	aggStore   eventsourcing.AggregateStore
	eventStore eventsourcing.EventStore

	settingsVisible   bool
	eventBus          eventsourcing.EventBus
	pendingKeypresses map[string]chan map[string]interface{}
	pendingMu         sync.RWMutex
}

type ClientState struct {
	conn      *websocket.Conn
	ready     bool
	lastReady time.Time
}

type TaskPositionUpdatedEvent struct {
	EventType string  `json:"event_type"`
	TaskID    string  `json:"task_id"`
	PositionX float64 `json:"position_x"`
	PositionY float64 `json:"position_y"`
	PositionZ float64 `json:"position_z"`
}

func (e *TaskPositionUpdatedEvent) Type() string {
	return "taskmanager_TaskPositionUpdated"
}

func (e *TaskPositionUpdatedEvent) Marshal() ([]byte, error) {
	e.EventType = e.Type()
	return json.Marshal(e)
}

func (e *TaskPositionUpdatedEvent) Unmarshal(data []byte) error {
	return json.Unmarshal(data, e)
}

func NewGodotServer() *GodotServer {
	return &GodotServer{
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true }, // Allow all origins for testing
		},
		clients:           make(map[*websocket.Conn]*ClientState),
		deltaChan:         make(chan eventsourcing.DeltaEnvelope, 1000),
		pendingKeypresses: make(map[string]chan map[string]interface{}),
	}
}

func (s *GodotServer) SetDeltaChan(ch chan eventsourcing.DeltaEnvelope) {
	s.deltaChan = ch
}

func (s *GodotServer) SetAggStore(aggStore eventsourcing.AggregateStore) {
	s.aggStore = aggStore
}

func (s *GodotServer) SetEventBus(eb eventsourcing.EventBus) {
	s.eventBus = eb
}

func (s *GodotServer) SetEventStore(es eventsourcing.EventStore) {
	s.eventStore = es
}

func (s *GodotServer) SendTranscription(text string) {
	logging.Debug("AUDIO: Sending transcription to Godot: %s", text)
	env := eventsourcing.DeltaEnvelope{
		Type:      "delta",
		Aggregate: "transcription",
		EventID:   fmt.Sprintf("transcription-%d", time.Now().UnixNano()),
		Timestamp: eventsourcing.ISOTimestamp(),
		Actions: []eventsourcing.DeltaAction{
			{
				Type:     "update",
				NodeID:   "transcription_display",
				NodeType: "Label3D",
				Properties: map[string]interface{}{
					"text": text,
				},
			},
		},
	}
	s.broadcast(env)
}

func (s *GodotServer) SendKeypresses(keyString string) {
	logging.Debug("Sending keypresses to Godot: %s", keyString)
	msg := map[string]interface{}{
		"type": "keypresses",
		"keys": keyString,
	}
	s.broadcastJSON(msg)
}

func (s *GodotServer) SendKeypressesWithID(keyString, correlationID string) {
	logging.Debug("Sending keypresses to Godot: keys='%s', correlation_id='%s'", keyString, correlationID)
	msg := map[string]interface{}{
		"type":           "keypresses",
		"keys":           keyString,
		"correlation_id": correlationID,
	}
	s.broadcastJSON(msg)
}

func (s *GodotServer) handleTextMessage(conn *websocket.Conn, message []byte) {
	logging.Trace("Handling text message from Godot")
	var msg map[string]interface{}
	if err := json.Unmarshal(message, &msg); err != nil {
		logging.Error("Failed to parse JSON message from Godot: %v", err)
		return
	}

	msgType, ok := msg["type"].(string)
	if !ok {
		logging.Error("Message missing 'type' field")
		return
	}

	logging.Trace("Parsed message type: %s", msgType)
	switch msgType {
	case "ready":
		s.handleReadyMessage(conn, msg)

	case "state_update":
		s.handleStateUpdate(msg)
	case "request":
		s.handleRequestMessage(msg)
	case "delta":
		s.handleDeltaMessage(msg)
	case "ui_Position3DObject":
		s.handlePosition3DObjectEvent(msg)
	case "keypress_ack":
		s.handleKeypressAck(msg)

	default:
		logging.Info("Unknown message type from Godot: %s", msgType)
	}
}

func (s *GodotServer) handleStateUpdate(msg map[string]interface{}) {
	logging.Debug("Handling state update from Godot: %v", msg)
	if visible, ok := msg["settings_visible"].(bool); ok {
		s.settingsVisible = visible
	}

}

func (s *GodotServer) handleRequestMessage(msg map[string]interface{}) {
	logging.Debug("Handling request from Godot: %v", msg)
	text, ok := msg["text"].(string)
	if !ok {
		logging.Error("Request message missing text")
		return
	}

	event := &orchestration.UserRequestReceivedEvent{
		RequestID:   fmt.Sprintf("godot_req_%d", time.Now().UnixNano()),
		RequestText: text,
		Timestamp:   eventsourcing.ISOTimestamp(),
	}

	if s.eventBus != nil {
		s.eventBus.Publish(event)
	} else {
		logging.Error("EventBus not set")
	}
}

func (s *GodotServer) handleDeltaMessage(msg map[string]interface{}) {
	logging.Debug("Handling delta from Godot: %v", msg)
	actions, ok := msg["actions"].([]interface{})
	if !ok {
		logging.Error("Delta message missing actions")
		return
	}

	for _, a := range actions {
		action, ok := a.(map[string]interface{})
		if !ok {
			continue
		}
		if action["type"] == "update" {
			if props, ok := action["properties"].(map[string]interface{}); ok {
				if pos, ok := props["position"].([]interface{}); ok && len(pos) >= 3 {
					x, _ := pos[0].(float64)
					y, _ := pos[1].(float64)
					z, _ := pos[2].(float64)
					if nodeID, ok := action["node_id"].(string); ok {
						event := &eventsourcing.Position3DObjectEvent{
							ObjectID: nodeID,
							Position: []float64{x, y, z},
						}
						if s.eventBus != nil {
							s.eventBus.Publish(event)
						} else {
							logging.Error("EventBus not set")
						}
					}
				}
			}
		}
	}
}

func (s *GodotServer) handlePosition3DObjectEvent(msg map[string]interface{}) {
	logging.Debug("Handling Position3DObjectEvent from Godot: %v", msg)
	objectID, ok := msg["object_id"].(string)
	if !ok {
		logging.Error("Position3DObjectEvent missing object_id")
		return
	}
	pos, ok := msg["position"].([]interface{})
	if !ok || len(pos) < 3 {
		logging.Error("Position3DObjectEvent missing or invalid position")
		return
	}
	x, _ := pos[0].(float64)
	y, _ := pos[1].(float64)
	z, _ := pos[2].(float64)

	event := &eventsourcing.Position3DObjectEvent{
		ObjectID: objectID,
		Position: []float64{x, y, z},
	}
	if s.eventBus != nil {
		s.eventBus.Publish(event)
	} else {
		logging.Error("EventBus not set")
	}
}

func (s *GodotServer) handleKeypressAck(msg map[string]interface{}) {
	logging.Debug("Handling keypress ACK from Godot: %v", msg)
	correlationID, ok := msg["correlation_id"].(string)
	if !ok || correlationID == "" {
		logging.Error("Keypress ACK missing correlation_id")
		return
	}

	s.pendingMu.RLock()
	ch, exists := s.pendingKeypresses[correlationID]
	s.pendingMu.RUnlock()

	if !exists {
		logging.Info("Received ACK for unknown correlation_id: %s", correlationID)
		return
	}

	// Send the result back through the channel
	select {
	case ch <- msg:
		logging.Debug("Sent keypress ACK result for correlation_id: %s", correlationID)
	default:
		logging.Info("Channel full for correlation_id: %s", correlationID)
	}

	// Clean up the pending request
	s.pendingMu.Lock()
	delete(s.pendingKeypresses, correlationID)
	s.pendingMu.Unlock()
}

func (s *GodotServer) handleReadyMessage(conn *websocket.Conn, msg map[string]interface{}) {
	logging.Info("Received ready signal from Godot client")

	s.clientsMu.Lock()
	if client, exists := s.clients[conn]; exists {
		client.ready = true
		client.lastReady = time.Now()
	}
	s.clientsMu.Unlock()

	// Send full state immediately now that client is ready
	go s.sendFullState(conn)
}

func (s *GodotServer) sendFullState(conn *websocket.Conn) {
	if s.aggStore == nil || s.eventStore == nil {
		logging.Error("AggStore or EventStore is nil, cannot send full state")
		return
	}

	logging.Info("Replaying event history to Godot client")
	events := s.eventStore.GetEvents()
	totalActions := 0

	// Send zones first
	zonesMsg := map[string]interface{}{
		"type": "zones",
		"zones": map[string][]float64{
			"task":     {0, 0, 20},
			"note":     {-20, 0, 0},
			"calendar": {20, 0, 0},
			"default":  {0, 5, 0},
		},
	}
	conn.WriteJSON(zonesMsg)

	// Group aggregates by ID for replay
	replayAggs := make(map[string]eventsourcing.ThreeDUIBroadcaster)
	for _, agg := range s.aggStore.AllAggregates() {
		if broadcaster, ok := agg.(eventsourcing.ThreeDUIBroadcaster); ok {
			replayAggs[agg.ID()] = broadcaster
		}
	}

	// For each aggregate, create a clone and replay events
	for aggID, originalBroadcaster := range replayAggs {
		replayAgg := originalBroadcaster.Clone()
		if replayAgg == nil {
			logging.Error("Clone returned nil for aggregate %s", aggID)
			continue
		}
		replayBroadcaster, ok := replayAgg.(eventsourcing.ThreeDUIBroadcaster)
		if !ok {
			logging.Error("Cloned aggregate %s does not implement ThreeDUIBroadcaster", aggID)
			continue
		}

		actions := []eventsourcing.DeltaAction{}
		for _, event := range events {
			eventActions := replayBroadcaster.Broadcast3DDelta(event)
			actions = append(actions, eventActions...)
		}

		logging.Info("Aggregate %s replayed %d events, sending %d actions", aggID, len(events), len(actions))
		totalActions += len(actions)
		if len(actions) > 0 {
			// Send actions one by one with delay to see the process unfold
			for i, action := range actions {
				env := eventsourcing.DeltaEnvelope{
					Type:      "delta",
					Aggregate: aggID,
					EventID:   fmt.Sprintf("replay-%d", i+1),
					Timestamp: eventsourcing.ISOTimestamp(),
					Actions:   []eventsourcing.DeltaAction{action},
				}
				err := conn.WriteJSON(env)
				if err != nil {
					logging.Error("Error sending replay action to Godot: %v", err)
					return
				}
				// Delay to allow Godot to process each action
				time.Sleep(40 * time.Millisecond)
			}
		}
	}
	logging.Info("Total actions sent to Godot: %d", totalActions)
}

func (s *GodotServer) broadcast(env eventsourcing.DeltaEnvelope) {
	s.clientsMu.RLock()
	defer s.clientsMu.RUnlock()
	logging.Trace("Broadcasting delta envelope: type=%s, aggregate=%s, actions=%d", env.Type, env.Aggregate, len(env.Actions))
	for conn := range s.clients {
		err := conn.WriteJSON(env)
		if err != nil {
			logging.Error("Error broadcasting to Godot client: %v", err)
			// Optionally remove the client if error
		}
	}
}

func (s *GodotServer) broadcastJSON(msg interface{}) {
	s.clientsMu.RLock()
	defer s.clientsMu.RUnlock()
	logging.Trace("Broadcasting JSON message: %v", msg)
	for conn := range s.clients {
		err := conn.WriteJSON(msg)
		if err != nil {
			logging.Error("Error broadcasting JSON to Godot client: %v", err)
		}
	}
}

func (s *GodotServer) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		logging.Error("WebSocket upgrade error: %v", err)
		return
	}
	s.clientsMu.Lock()
	s.clients[conn] = &ClientState{
		conn:  conn,
		ready: false,
	}
	s.clientsMu.Unlock()
	logging.Info("Godot client connected")

	const pongWait = 60 * time.Second
	conn.SetReadDeadline(time.Now().Add(pongWait))
	conn.SetPongHandler(func(string) error { conn.SetReadDeadline(time.Now().Add(pongWait)); return nil })

	// Start listening for messages
	go func() {
		defer conn.Close()
		defer func() {
			s.clientsMu.Lock()
			delete(s.clients, conn)
			s.clientsMu.Unlock()
		}()
		for {
			messageType, message, err := conn.ReadMessage()
			if err != nil {
				logging.Error("Error reading from Godot: %v", err)
				return
			}

			if messageType == websocket.TextMessage {
				logging.Trace("Received text from Godot: %s", string(message))
				s.handleTextMessage(conn, message)

			} else {
				logging.Info("Received unknown message type from Godot: %d", messageType)
			}
		}
	}()

	// Wait for ready signal before sending state
	// State will be sent when client sends "ready" message
}

func (s *GodotServer) HandleKeypresses(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Keys          string `json:"keys"`
		CorrelationID string `json:"correlation_id,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON body", http.StatusBadRequest)
		return
	}

	if req.Keys == "" {
		http.Error(w, "Missing 'keys' field in request body", http.StatusBadRequest)
		return
	}

	// Generate correlation ID if not provided
	if req.CorrelationID == "" {
		req.CorrelationID = fmt.Sprintf("keypress_%d", time.Now().UnixNano())
	}

	logging.Info("Received keypress request: keys='%s', correlation_id='%s'", req.Keys, req.CorrelationID)

	// Create a channel to wait for ACK
	ch := make(chan map[string]interface{}, 1)

	s.pendingMu.Lock()
	s.pendingKeypresses[req.CorrelationID] = ch
	s.pendingMu.Unlock()

	// Send keypresses with correlation ID
	s.SendKeypressesWithID(req.Keys, req.CorrelationID)

	// Wait for ACK with timeout
	timeout := time.After(5 * time.Second)
	select {
	case result := <-ch:
		// Got ACK
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	case <-timeout:
		// Timeout - clean up
		s.pendingMu.Lock()
		delete(s.pendingKeypresses, req.CorrelationID)
		s.pendingMu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusGatewayTimeout)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error":          "timeout",
			"correlation_id": req.CorrelationID,
			"message":        "No response from Godot frontend within 5 seconds",
		})
	}
}

func (s *GodotServer) Start() {
	// Start broadcasting deltas
	go func() {
		for env := range s.deltaChan {
			s.broadcast(env)
		}
	}()

	http.HandleFunc("/godot", s.HandleWebSocket)
	http.HandleFunc("/keypresses", s.HandleKeypresses)
	logging.Info("Starting WebSocket server on :8081")
	err := http.ListenAndServe(":8081", nil)
	if err != nil {
		logging.Error("Server error: %v", err)
	}
}
