package godot_ws

import (
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"mindpalace/pkg/eventsourcing"
	"mindpalace/pkg/logging"
)

type GodotServer struct {
	upgrader  websocket.Upgrader
	clients   map[*websocket.Conn]bool
	clientsMu sync.RWMutex
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

func (s *GodotServer) broadcast(env eventsourcing.DeltaEnvelope) {
	s.clientsMu.RLock()
	defer s.clientsMu.RUnlock()
	logging.Debug("Broadcasting delta envelope: type=%s, aggregate=%s, actions=%d", env.Type, env.Aggregate, len(env.Actions))
	for conn := range s.clients {
		err := conn.WriteJSON(env)
		if err != nil {
			logging.Error("Error broadcasting to Godot client: %v", err)
			// Optionally remove the client if error
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
	s.clients[conn] = true
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
			_, message, err := conn.ReadMessage()
			if err != nil {
				logging.Error("Error reading from Godot: %v", err)
				return
			}
			logging.Info("Received from Godot: %s", string(message))
			// TODO: Handle incoming messages if needed
		}
	}()

	// Send full 3D state to Godot
	if s.aggStore != nil {
		for _, agg := range s.aggStore.AllAggregates() {
			if broadcaster, ok := agg.(eventsourcing.ThreeDUIBroadcaster); ok {
				actions := broadcaster.GetFull3DState()
				logging.Debug("Sending full state for aggregate %s: %d actions", agg.ID(), len(actions))
				if len(actions) > 0 {
					env := eventsourcing.DeltaEnvelope{
						Type:      "delta",
						Aggregate: agg.ID(),
						EventID:   "full_state",
						Timestamp: eventsourcing.ISOTimestamp(),
						Actions:   actions,
					}
					err := conn.WriteJSON(env)
					if err != nil {
						logging.Error("Error sending full state to Godot: %v", err)
					}
				}
			}
		}
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
	logging.Info("Starting WebSocket server on :8081")
	err := http.ListenAndServe(":8081", nil)
	if err != nil {
		logging.Error("Server error: %v", err)
	}
}
