package godot_ws

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"mindpalace/internal/audio"
	"mindpalace/internal/orchestration"
	"mindpalace/pkg/eventsourcing"
	"mindpalace/pkg/logging"
)

type GodotServer struct {
	upgrader          websocket.Upgrader
	clients           map[*websocket.Conn]*ClientState
	clientsMu         sync.RWMutex
	deltaChan         chan eventsourcing.DeltaEnvelope
	aggStore          eventsourcing.AggregateStore
	audioCallback     func([]byte) // Callback for processing audio chunks
	transcriber       *audio.VoiceTranscriber
	settingsVisible   bool
	selectedMicDevice string
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
		deltaChan:         make(chan eventsourcing.DeltaEnvelope, 100),
		pendingKeypresses: make(map[string]chan map[string]interface{}),
	}
}

func (s *GodotServer) SetDeltaChan(ch chan eventsourcing.DeltaEnvelope) {
	s.deltaChan = ch
}

func (s *GodotServer) SetAggStore(aggStore eventsourcing.AggregateStore) {
	s.aggStore = aggStore
}

func (s *GodotServer) SetAudioCallback(callback func([]byte)) {
	s.audioCallback = callback
}

func (s *GodotServer) SetTranscriber(t *audio.VoiceTranscriber) {
	s.transcriber = t
}

func (s *GodotServer) SetEventBus(eb eventsourcing.EventBus) {
	s.eventBus = eb
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
	case "audio_chunk":
		s.handleAudioChunk(msg)
	case "state_update":
		s.handleStateUpdate(msg)
	case "request":
		s.handleRequestMessage(msg)
	case "delta":
		s.handleDeltaMessage(msg)
	case "keypress_ack":
		s.handleKeypressAck(msg)
		// case "start_audio_capture":
		// 	logging.Info("Received start_audio_capture signal from Godot")
		// 	if s.transcriber != nil {
		// 		if err := s.transcriber.StartCapture(context.Background()); err != nil {
		// 			logging.Error("Failed to start audio capture: %v", err)
		// 		} else {
		// 			logging.Info("Started audio capture from backend microphone")
		// 		}
		// 	} else {
		// 		logging.Info("Transcriber not set for audio capture")
		// 	}
	default:
		logging.Info("Unknown message type from Godot: %s", msgType)
	}
}

func (s *GodotServer) handleBinaryMessage(message []byte) {
	logging.Debug("AUDIO: Handling binary message from Godot: %d bytes", len(message))
	// Binary messages are audio data
	if s.audioCallback != nil {
		logging.Debug("AUDIO: Calling audio callback with binary data (%d bytes)", len(message))
		s.audioCallback(message)
	} else {
		logging.Info("AUDIO: Audio callback not set, ignoring binary message")
	}
}

func (s *GodotServer) handleAudioChunk(msg map[string]interface{}) {
	logging.Debug("AUDIO: Handling audio chunk from Godot")
	dataStr, ok := msg["data"].(string)
	if !ok {
		logging.Error("AUDIO: Audio chunk missing 'data' field")
		return
	}

	logging.Debug("AUDIO: Decoding base64 audio data, length: %d", len(dataStr))
	audioData, err := base64.StdEncoding.DecodeString(dataStr)
	if err != nil {
		logging.Error("AUDIO: Failed to decode base64 audio data: %v", err)
		return
	}

	logging.Debug("AUDIO: Decoded audio data: %d bytes", len(audioData))
	if s.audioCallback != nil {
		logging.Debug("AUDIO: Calling audio callback with decoded data (%d bytes)", len(audioData))
		s.audioCallback(audioData)
	} else {
		logging.Info("AUDIO: Audio callback not set, ignoring audio chunk")
	}
}

func (s *GodotServer) handleStateUpdate(msg map[string]interface{}) {
	logging.Debug("Handling state update from Godot: %v", msg)
	if visible, ok := msg["settings_visible"].(bool); ok {
		s.settingsVisible = visible
	}
	if mic, ok := msg["selected_mic_device"].(string); ok {
		s.selectedMicDevice = mic
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
					if nodeID, ok := action["node_id"].(string); ok && strings.HasPrefix(nodeID, "task_") {
						event := &TaskPositionUpdatedEvent{
							TaskID:    nodeID,
							PositionX: x,
							PositionY: y,
							PositionZ: z,
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
	if s.aggStore == nil {
		logging.Error("AggStore is nil, cannot send full state")
		return
	}

	logging.Info("Sending full 3D state to Godot client")
	totalActions := 0
	for _, agg := range s.aggStore.AllAggregates() {
		if broadcaster, ok := agg.(eventsourcing.ThreeDUIBroadcaster); ok {
			actions := broadcaster.GetFull3DState()
			logging.Info("Aggregate %s implements ThreeDUIBroadcaster, sending %d actions", agg.ID(), len(actions))
			totalActions += len(actions)
			if len(actions) > 0 {
				env := eventsourcing.DeltaEnvelope{
					Type:      "delta",
					Aggregate: agg.ID(),
					EventID:   "full_state",
					Timestamp: eventsourcing.ISOTimestamp(),
					Actions:   actions,
				}
				logging.Info("Sending JSON to Godot")
				err := conn.WriteJSON(env)
				if err != nil {
					logging.Error("Error sending full state to Godot: %v", err)
					return
				}
			} else {
				logging.Info("Aggregate %s has no actions to send", agg.ID())
			}
		} else {
			logging.Info("Aggregate %s does not implement ThreeDUIBroadcaster", agg.ID())
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
			} else if messageType == websocket.BinaryMessage {
				s.handleBinaryMessage(message)
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
