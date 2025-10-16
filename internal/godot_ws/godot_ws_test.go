package godot_ws

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"mindpalace/pkg/eventsourcing"
)

// MockAggregateStore for testing
type mockAggregateStore struct {
	aggregates []eventsourcing.Aggregate
}

func (m *mockAggregateStore) AllAggregates() []eventsourcing.Aggregate {
	return m.aggregates
}

// MockEventStore for testing
type mockEventStore struct {
	events []eventsourcing.Event
}

func (m *mockEventStore) Append(events ...eventsourcing.Event) error {
	m.events = append(m.events, events...)
	return nil
}

func (m *mockEventStore) GetEvents() []eventsourcing.Event {
	return m.events
}

func (m *mockEventStore) Load() error {
	return nil
}

// MockAggregate for testing
type mockAggregate struct {
	id string
}

func (m *mockAggregate) ID() string {
	return m.id
}

func (m *mockAggregate) ApplyEvent(event eventsourcing.Event) error {
	return nil
}

// MockThreeDUIBroadcaster for testing
type mockThreeDUIBroadcaster struct {
	mockAggregate
	deltas []eventsourcing.DeltaAction
}

func (m *mockThreeDUIBroadcaster) Broadcast3DDelta(event eventsourcing.Event) []eventsourcing.DeltaAction {
	return m.deltas
}

func (m *mockThreeDUIBroadcaster) Clone() eventsourcing.Aggregate {
	newMock := &mockThreeDUIBroadcaster{}
	newMock.id = m.id
	newMock.deltas = make([]eventsourcing.DeltaAction, len(m.deltas))
	copy(newMock.deltas, m.deltas)
	return newMock
}

func TestNewGodotServer(t *testing.T) {
	server := NewGodotServer()
	if server == nil {
		t.Fatal("NewGodotServer returned nil")
	}
	if server.clients == nil {
		t.Error("clients map not initialized")
	}
	if server.deltaChan == nil {
		t.Error("deltaChan not initialized")
	}
}

func TestGodotServer_SetDeltaChan(t *testing.T) {
	server := NewGodotServer()
	ch := make(chan eventsourcing.DeltaEnvelope, 10)
	server.SetDeltaChan(ch)
	if server.deltaChan != ch {
		t.Error("SetDeltaChan did not set the channel")
	}
}

func TestGodotServer_SetAggStore(t *testing.T) {
	server := NewGodotServer()
	aggStore := &mockAggregateStore{}
	server.SetAggStore(aggStore)
	if server.aggStore != aggStore {
		t.Error("SetAggStore did not set the aggregate store")
	}
}

func TestGodotServer_SendTranscription(t *testing.T) {
	server := NewGodotServer()
	// Just test that it doesn't panic
	server.SendTranscription("test text")
}

func TestGodotServer_handleTextMessage_UnknownType(t *testing.T) {
	server := NewGodotServer()

	msg := map[string]interface{}{
		"type": "unknown",
	}
	data, _ := json.Marshal(msg)

	// Should not panic or error
	server.handleTextMessage(nil, data)
}

func TestGodotServer_broadcast(t *testing.T) {
	server := NewGodotServer()

	// Mock websocket connection
	upgrader := websocket.Upgrader{}
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("Upgrade failed: %v", err)
		}
		server.clientsMu.Lock()
		server.clients[conn] = &ClientState{
			conn:  conn,
			ready: true,
		}
		server.clientsMu.Unlock()

		// Wait a bit for broadcast
		time.Sleep(100 * time.Millisecond)
		conn.Close()
	}))
	defer httpServer.Close()

	// Connect a client
	url := "ws" + strings.TrimPrefix(httpServer.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("Dial failed: %v", err)
	}
	defer conn.Close()

	env := eventsourcing.DeltaEnvelope{
		Type:      "delta",
		Aggregate: "test",
		Actions:   []eventsourcing.DeltaAction{{Type: "create", NodeID: "test"}},
	}

	server.broadcast(env)

	// Check if message was received
	conn.SetReadDeadline(time.Now().Add(1 * time.Second))
	var received eventsourcing.DeltaEnvelope
	err = conn.ReadJSON(&received)
	if err != nil {
		t.Errorf("ReadJSON failed: %v", err)
	}
	if received.Aggregate != "test" {
		t.Errorf("Received wrong aggregate")
	}
}

func TestGodotServer_HandleWebSocket_Replay(t *testing.T) {
	server := NewGodotServer()
	mockBroadcaster := &mockThreeDUIBroadcaster{
		mockAggregate: mockAggregate{id: "test"},
		deltas: []eventsourcing.DeltaAction{
			{Type: "create", NodeID: "node1"},
		},
	}
	aggStore := &mockAggregateStore{
		aggregates: []eventsourcing.Aggregate{mockBroadcaster},
	}
	// Mock event store with one event
	mockEventStore := &mockEventStore{
		events: []eventsourcing.Event{&eventsourcing.InitiatePluginCreationEvent{}},
	}
	server.SetAggStore(aggStore)
	server.SetEventStore(mockEventStore)

	httpServer := httptest.NewServer(http.HandlerFunc(server.HandleWebSocket))
	defer httpServer.Close()

	url := "ws" + strings.TrimPrefix(httpServer.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("Dial failed: %v", err)
	}
	defer conn.Close()

	// Send ready message
	readyMsg := map[string]interface{}{
		"type": "ready",
	}
	conn.WriteJSON(readyMsg)

	// Should receive replay
	conn.SetReadDeadline(time.Now().Add(1 * time.Second))
	var received eventsourcing.DeltaEnvelope
	err = conn.ReadJSON(&received)
	if err != nil {
		t.Errorf("ReadJSON failed: %v", err)
	}
	if received.Type != "delta" {
		t.Errorf("Expected type 'delta', got %s", received.Type)
	}
	if received.EventID != "replay" {
		t.Errorf("Expected event_id 'replay', got %s", received.EventID)
	}
	if len(received.Actions) != 1 {
		t.Errorf("Expected 1 action, got %d", len(received.Actions))
	}
}

func TestGodotServer_HandleKeypresses_InvalidMethod(t *testing.T) {
	server := NewGodotServer()

	req := httptest.NewRequest("GET", "/keypresses", nil)
	w := httptest.NewRecorder()

	server.HandleKeypresses(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected status 405, got %d", w.Code)
	}
}

func TestGodotServer_HandleKeypresses_InvalidJSON(t *testing.T) {
	server := NewGodotServer()

	req := httptest.NewRequest("POST", "/keypresses", strings.NewReader("invalid json"))
	w := httptest.NewRecorder()

	server.HandleKeypresses(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", w.Code)
	}
}

func TestGodotServer_HandleKeypresses_MissingKeys(t *testing.T) {
	server := NewGodotServer()

	reqBody := `{"correlation_id": "test123"}`
	req := httptest.NewRequest("POST", "/keypresses", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.HandleKeypresses(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", w.Code)
	}
}

func TestGodotServer_HandleKeypresses_Timeout(t *testing.T) {
	server := NewGodotServer()

	reqBody := `{"keys": "a b", "correlation_id": "test123"}`
	req := httptest.NewRequest("POST", "/keypresses", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.HandleKeypresses(w, req)

	if w.Code != http.StatusGatewayTimeout {
		t.Errorf("Expected status 504, got %d", w.Code)
	}

	var response map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if response["error"] != "timeout" {
		t.Errorf("Expected error 'timeout', got %v", response["error"])
	}
}

func TestGodotServer_SendKeypressesWithID(t *testing.T) {
	server := NewGodotServer()

	// Just test that it doesn't panic - the actual broadcasting is tested elsewhere
	server.SendKeypressesWithID("test keys", "test123")
}

func TestGodotServer_handleKeypressAck(t *testing.T) {
	server := NewGodotServer()

	// Create a channel and add it to pending requests
	ch := make(chan map[string]interface{}, 1)
	server.pendingMu.Lock()
	server.pendingKeypresses["test123"] = ch
	server.pendingMu.Unlock()

	// Send ACK message
	ackMsg := map[string]interface{}{
		"type":           "keypress_ack",
		"correlation_id": "test123",
		"success":        true,
		"processed_keys": "a b",
		"actions_taken":  []interface{}{"player_keypresses"},
	}

	data, _ := json.Marshal(ackMsg)
	server.handleTextMessage(nil, data)

	// Check if result was received
	select {
	case result := <-ch:
		if result["correlation_id"] != "test123" {
			t.Errorf("Expected correlation_id 'test123', got %v", result["correlation_id"])
		}
		if result["success"] != true {
			t.Errorf("Expected success true, got %v", result["success"])
		}
	default:
		t.Error("ACK result not received")
	}

	// Check if pending request was cleaned up
	server.pendingMu.RLock()
	_, exists := server.pendingKeypresses["test123"]
	server.pendingMu.RUnlock()
	if exists {
		t.Error("Pending request not cleaned up")
	}
}
