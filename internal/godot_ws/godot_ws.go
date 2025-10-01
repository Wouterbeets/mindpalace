package godot_ws

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"mindpalace/internal/audio"
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
}

type ClientState struct {
	conn      *websocket.Conn
	ready     bool
	lastReady time.Time
}

func NewGodotServer() *GodotServer {
	return &GodotServer{
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true }, // Allow all origins for testing
		},
		clients:   make(map[*websocket.Conn]*ClientState),
		deltaChan: make(chan eventsourcing.DeltaEnvelope, 100),
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

	keyString := r.URL.Query().Get("keys")
	if keyString == "" {
		http.Error(w, "Missing 'keys' query parameter", http.StatusBadRequest)
		return
	}

	logging.Info("Received keypress request: %s", keyString)
	s.SendKeypresses(keyString)
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Keypresses sent"))
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
