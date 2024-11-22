package eventstore

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"mindpalace/internal/eventsourcing/interfaces"
	"reflect"

	_ "github.com/mattn/go-sqlite3"
)

type SQLiteEventStorer struct {
	db         *sql.DB
	eventTypes map[string]reflect.Type // Maps event type names to concrete types
}

// NewSQLiteEventStorer initializes the SQLiteEventStorer and sets up the database
func NewSQLiteEventStorer(dataSourceName string) (*SQLiteEventStorer, error) {
	db, err := sql.Open("sqlite3", dataSourceName)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Initialize the events table if it doesn't exist
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS events (
			id TEXT PRIMARY KEY,
			aggregate_id TEXT NOT NULL,
			type TEXT NOT NULL,
			data TEXT NOT NULL,
			occurred_at DATETIME NOT NULL,
			version INTEGER NOT NULL
		)
	`); err != nil {
		return nil, fmt.Errorf("failed to create events table: %w", err)
	}

	return &SQLiteEventStorer{
		db:         db,
		eventTypes: make(map[string]reflect.Type),
	}, nil
}

// RegisterEventType registers an event type for deserialization by name
func (s *SQLiteEventStorer) RegisterEventType(name string, eventType reflect.Type) {
	s.eventTypes[name] = eventType
}

func (s *SQLiteEventStorer) Append(aggregateID string, e interfaces.Event) error {
	if e == nil {
		return fmt.Errorf("event can not be nil %+v", e)
	}
	eventType := e.EventName() // Get event type name
	data, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	_, err = s.db.Exec(`
		INSERT INTO events (id, aggregate_id, type, data, occurred_at, version) 
		VALUES (?, ?, ?, ?, ?, ?)`,
		e.EventID(), aggregateID, eventType, string(data), e.OccurredAt(), e.Version(),
	)
	if err != nil {
		return fmt.Errorf("failed to append event to database: %w", err)
	}
	return nil
}

func (s *SQLiteEventStorer) Load(aggregateID string) ([]interfaces.Event, error) {
	rows, err := s.db.Query(`
		SELECT type, data FROM events WHERE aggregate_id = ? ORDER BY occurred_at ASC`, aggregateID)
	if err != nil {
		return nil, fmt.Errorf("failed to load events: %w", err)
	}
	defer rows.Close()

	var events []interfaces.Event
	for rows.Next() {
		var eventTypeName, data string
		if err := rows.Scan(&eventTypeName, &data); err != nil {
			return nil, fmt.Errorf("failed to scan event data: %w", err)
		}

		// Lookup the correct event type
		eventType, ok := s.eventTypes[eventTypeName]
		if !ok {
			return nil, fmt.Errorf("unknown event type: %s", eventTypeName)
		}

		// Create a new instance of the event type
		eventPtr := reflect.New(eventType).Interface()

		// Unmarshal JSON into the concrete event
		if err := json.Unmarshal([]byte(data), eventPtr); err != nil {
			return nil, fmt.Errorf("failed to unmarshal event data: %w", err)
		}

		events = append(events, eventPtr.(interfaces.Event))
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}

	return events, nil
}
