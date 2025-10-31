package godot_ws

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"mindpalace/internal/audio"
	"mindpalace/pkg/eventsourcing"
	"mindpalace/pkg/logging"
	"mindpalace/pkg/ui3d"
)

type EventProcessorInterface interface {
	RegisterCommand(name string, handler eventsourcing.CommandHandler)
	ExecuteCommand(name string, data interface{}) error
}

type GodotServer struct {
	upgrader   websocket.Upgrader
	clients    map[*websocket.Conn]*ClientState
	clientsMu  sync.RWMutex
	deltaChan  chan eventsourcing.DeltaEnvelope
	sequenceMu sync.RWMutex

	settingsVisible   bool
	eventBus          eventsourcing.EventBus
	aggStore          eventsourcing.AggregateStore
	eventStore        eventsourcing.EventStore
	eventProcessor    eventsourcing.EventProcessorInterface // Added for direct command execution
	pendingKeypresses map[string]chan map[string]interface{}
	pendingMu         sync.RWMutex
	transcriber       *audio.VoiceTranscriber

	ackChan    chan int
	ackTimeout time.Duration

	lastBroadcastSequence int

	commandChan chan eventsourcing.CommandData
	controlChan chan string
}

func (s *GodotServer) SetEventProcessor(ep eventsourcing.EventProcessorInterface) {
	s.eventProcessor = ep
}

func (s *GodotServer) SetAckChan(ch chan int) {
	s.ackChan = ch
}

func (s *GodotServer) getLastBroadcastSequence() int {
	s.sequenceMu.RLock()
	defer s.sequenceMu.RUnlock()
	return s.lastBroadcastSequence
}

func (s *GodotServer) setLastBroadcastSequence(seq int) {
	s.sequenceMu.Lock()
	s.lastBroadcastSequence = seq
	s.sequenceMu.Unlock()
}

type ClientState struct {
	conn           *websocket.Conn
	ready          bool
	lastReady      time.Time
	lastSequenceID int
	synced         bool
	writeMu        sync.Mutex
}

func NewGodotServer() *GodotServer {
	return &GodotServer{
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true }, // Allow all origins for testing
		},
		clients:           make(map[*websocket.Conn]*ClientState),
		deltaChan:         make(chan eventsourcing.DeltaEnvelope, 1000),
		pendingKeypresses: make(map[string]chan map[string]interface{}),
		ackTimeout:        5 * time.Second,
		commandChan:       make(chan eventsourcing.CommandData, 100),
	}
}

func (s *GodotServer) SetDeltaChan(ch chan eventsourcing.DeltaEnvelope) {
	s.deltaChan = ch
}

func (s *GodotServer) SetTranscriber(vt *audio.VoiceTranscriber) {
	s.transcriber = vt
}

func (s *GodotServer) SetCommandChan(ch chan eventsourcing.CommandData) {
	s.commandChan = ch
}

func (s *GodotServer) SetControlChan(ch chan string) {
	s.controlChan = ch
}

func (s *GodotServer) SetAggStore(store eventsourcing.AggregateStore) {
	s.aggStore = store
}

func (s *GodotServer) SetEventStore(store eventsourcing.EventStore) {
	s.eventStore = store
}

func (s *GodotServer) CommandChan() <-chan eventsourcing.CommandData {
	return s.commandChan
}

func (s *GodotServer) SendTranscription(text string) {
	logging.Info("AUDIO: Sending transcription to Godot: '%s'", text)
	env := eventsourcing.DeltaEnvelope{
		Type:      "transcription_update",
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
	logging.Info("AUDIO: Broadcasting transcription update")
	s.broadcast(env)
}

func (s *GodotServer) SendDelta(env eventsourcing.DeltaEnvelope) {
	logging.Debug("BACKEND: Sending delta envelope: type=%s, aggregate=%s, sequence=%d, actions=%d", env.Type, env.Aggregate, env.SequenceID, len(env.Actions))
	if env.SequenceID > 0 {
		s.setLastBroadcastSequence(env.SequenceID)
	}
	s.clientsMu.Lock()
	for _, client := range s.clients {
		if env.SequenceID > 0 && client.lastSequenceID < env.SequenceID {
			client.synced = false
		}
		client.writeMu.Lock()
		logging.Debug("BACKEND: Writing delta to client")
		if err := client.conn.WriteJSON(env); err != nil {
			logging.Error("Error sending delta: %v", err)
		} else {
			logging.Debug("BACKEND: Delta sent successfully to client")
		}
		client.writeMu.Unlock()
	}
	s.clientsMu.Unlock()
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
	case "ui_action":
		s.handleUIAction(msg)
	case "keypress_ack":
		s.handleKeypressAck(msg)
	case "toggle_mic":
		s.handleToggleMic(msg)
	case "debug_info":
		s.handleDebugInfo(msg)
	case "delta_ack":
		s.handleDeltaAck(conn, msg)

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

	command := eventsourcing.CommandData{
		Name: "ProcessUserRequest",
		Data: map[string]interface{}{
			"requestText": text,
			"requestID":   fmt.Sprintf("godot_req_%d", time.Now().UnixNano()),
		},
	}

	select {
	case s.commandChan <- command:
	default:
		logging.Info("eventsourcing.CommandData channel full, dropping request")
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

func (s *GodotServer) handleToggleMic(msg map[string]interface{}) {
	logging.Info("Handling toggle mic from Godot")
	if s.transcriber == nil {
		logging.Error("Transcriber not set")
		return
	}

	// Check if capture is currently running
	if s.transcriber.IsCaptureRunning() {
		logging.Info("Stopping microphone capture")
		s.transcriber.StopCapture()
	} else {
		logging.Info("Starting microphone capture")
		if err := s.transcriber.StartCapture(context.Background()); err != nil {
			logging.Error("Failed to start microphone capture: %v", err)
		}
	}
}

func (s *GodotServer) handleUIAction(msg map[string]interface{}) {
	logging.Debug("Handling UI action from Godot: %v", msg)
	actionData, ok := msg["action"].(map[string]interface{})
	if !ok {
		logging.Error("Invalid ui_action: missing action data")
		return
	}

	cmd, ok1 := actionData["command"].(string)
	data, ok2 := actionData["data"].(map[string]interface{})
	if !ok1 || !ok2 {
		logging.Error("Invalid ui_action: missing command or data")
		return
	}

	if s.eventProcessor == nil {
		logging.Error("EventProcessor not set")
		return
	}

	err := s.eventProcessor.ExecuteCommand(cmd, data)
	if err != nil {
		logging.Error("Failed to execute command '%s': %v", cmd, err)
	} else {
		logging.Info("Successfully executed UI action for command '%s'", cmd)
	}
}

func (s *GodotServer) handleDebugInfo(msg map[string]interface{}) {
	logging.Debug("Handling debug info from Godot: %v", msg)
	// For now, just log it. Could store in a field for the debug endpoint if needed.
}

func (s *GodotServer) handleDeltaAck(conn *websocket.Conn, msg map[string]interface{}) {
	seqID, ok := msg["sequence_id"].(float64)
	if !ok {
		logging.Error("Invalid sequence_id in delta_ack")
		return
	}
	if conn != nil {
		s.clientsMu.Lock()
		if client, exists := s.clients[conn]; exists {
			if client.lastSequenceID < int(seqID) {
				client.lastSequenceID = int(seqID)
			}
			client.synced = true
		}
		s.clientsMu.Unlock()
	}
	select {
	case s.ackChan <- int(seqID):
	default:
		logging.Info("ACK channel full, dropping ACK for %d", int(seqID))
	}
}

func (s *GodotServer) handleReadyMessage(conn *websocket.Conn, msg map[string]interface{}) {
	logging.Info("BACKEND: Received ready signal from Godot client")

	var clientSeq int
	if info, ok := msg["client_info"].(map[string]interface{}); ok {
		if seqVal, ok := info["last_sequence_id"].(float64); ok {
			clientSeq = int(seqVal)
		}
	}
	currentSeq := s.getLastBroadcastSequence()

	var needsRebuild bool
	s.clientsMu.Lock()
	if client, exists := s.clients[conn]; exists {
		client.ready = true
		client.lastReady = time.Now()
		if client.lastSequenceID < clientSeq {
			client.lastSequenceID = clientSeq
		}

		switch {
		case currentSeq == 0 && !client.synced:
			needsRebuild = true
		case clientSeq == 0 && currentSeq > 0:
			needsRebuild = true
			client.synced = false
		case clientSeq < currentSeq:
			needsRebuild = true
			client.synced = false
		case clientSeq == currentSeq && currentSeq != 0:
			client.synced = true
			needsRebuild = false
		default:
			needsRebuild = !client.synced
		}
	}
	s.clientsMu.Unlock()

	// Send zones to Godot
	zones := ui3d.GetGlobalZones()
	setupZonesPayload := map[string]interface{}{
		"type":  "setup_zones",
		"zones": zones,
	}
	payloadBytes, err := json.Marshal(setupZonesPayload)
	if err != nil {
		logging.Info("Marshal setup_zones: %v", err)
	} else {
		setupMsg := map[string]interface{}{
			"type":    "setup_zones",
			"payload": string(payloadBytes),
		}
		conn.WriteJSON(setupMsg)
		logging.Info("Sent setup_zones with %d zones", len(zones))
	}

	// Trigger rebuild after ready
	if s.controlChan != nil && needsRebuild {
		logging.Info("Triggering state rebuild for client (client_seq=%d, current_seq=%d)", clientSeq, currentSeq)
		select {
		case s.controlChan <- "rebuild_state":
		default:
			logging.Info("Control channel full, skipping rebuild trigger")
		}
	} else if !needsRebuild {
		logging.Info("Client already in sync (client_seq=%d, current_seq=%d); skipping rebuild", clientSeq, currentSeq)
	}
}

func (s *GodotServer) broadcast(env eventsourcing.DeltaEnvelope) {
	s.clientsMu.RLock()
	defer s.clientsMu.RUnlock()
	logging.Trace("Broadcasting delta envelope: type=%s, aggregate=%s, actions=%d", env.Type, env.Aggregate, len(env.Actions))
	for _, client := range s.clients {
		client.writeMu.Lock()
		err := client.conn.WriteJSON(env)
		client.writeMu.Unlock()
		if err != nil {
			logging.Error("Error broadcasting to Godot client: %v", err)
			// Optionally remove the client if error
		}
	}
	if env.SequenceID > 0 {
		s.setLastBroadcastSequence(env.SequenceID)
	}
}

func (s *GodotServer) broadcastJSON(msg interface{}) {
	s.clientsMu.RLock()
	defer s.clientsMu.RUnlock()
	logging.Trace("Broadcasting JSON message: %v", msg)
	for _, client := range s.clients {
		client.writeMu.Lock()
		err := client.conn.WriteJSON(msg)
		client.writeMu.Unlock()
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
		conn:           conn,
		ready:          false,
		lastSequenceID: 0,
		synced:         false,
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
	// Start forwarding deltas
	go func() {
		for env := range s.deltaChan {
			logging.Debug("GODOT_SERVER: Received delta from channel: type=%s, aggregate=%s, sequence=%d", env.Type, env.Aggregate, env.SequenceID)
			s.SendDelta(env)
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
