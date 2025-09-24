package godot_ws

import (
	"net/http"
	"time"

	"github.com/gorilla/websocket"
	"mindpalace/pkg/eventsourcing"
	"mindpalace/pkg/logging"
)

type GodotServer struct {
	upgrader  websocket.Upgrader
	clients   map[*websocket.Conn]bool
	deltaChan chan eventsourcing.DeltaEnvelope
	aggStore  eventsourcing.AggregateStore
}

func NewGodotServer() *GodotServer {
	return &GodotServer{
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true }, // Allow all origins for testing
		},
		clients:   make(map[*websocket.Conn]bool),
		deltaChan: make(chan eventsourcing.DeltaEnvelope, 100),
	}
}

func (s *GodotServer) SetDeltaChan(ch chan eventsourcing.DeltaEnvelope) {
	s.deltaChan = ch
}

func (s *GodotServer) SetAggStore(aggStore eventsourcing.AggregateStore) {
	s.aggStore = aggStore
}

func (s *GodotServer) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		logging.Error("WebSocket upgrade error: %v", err)
		return
	}
	s.clients[conn] = true
	logging.Info("Godot client connected")

	const pongWait = 60 * time.Second
	conn.SetReadDeadline(time.Now().Add(pongWait))
	conn.SetPongHandler(func(string) error { conn.SetReadDeadline(time.Now().Add(pongWait)); return nil })

	// Start listening for messages
	go func() {
		defer conn.Close()
		defer delete(s.clients, conn)
		for {
			_, message, err := conn.ReadMessage()
			if err != nil {
				logging.Error("Error reading from Godot: %v", err)
				return
			}
			logging.Info("Received from Godot: %s", string(message))
			// TODO: Handle incoming messages if needed
		}
	}()

	// Send a test message to Godot
	err = conn.WriteJSON(map[string]interface{}{
		"id":       "obj1",
		"position": []float64{10.0, 0.0, 5.0},
	})
	if err != nil {
		logging.Error("Error sending to Godot: %v", err)
	}
}

func (s *GodotServer) Start() {
	http.HandleFunc("/godot", s.HandleWebSocket)
	logging.Info("Starting WebSocket server on :8081")
	err := http.ListenAndServe(":8081", nil)
	if err != nil {
		logging.Error("Server error: %v", err)
	}
}
