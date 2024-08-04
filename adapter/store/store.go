// events.go
package events

import (
	"database/sql"
	"fmt"

	_ "github.com/mattn/go-sqlite3"
)

// Event represents an event in the events table.
type Event struct {
	AggregateID string
	EventID     string
	EventType   string
	Data        string
}

// ConnectDB opens a connection to the SQLite database.
func ConnectDB(dbPath string) (*sql.DB, error) {
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("unable to connect to database: %w", err)
	}
	return db, nil
}

// WriteEvent inserts a new event into the events table.
func WriteEvent(db *sql.DB, event Event) error {
	_, err := db.Exec(
		`INSERT INTO events (aggregate_id, event_id, event_type, data) VALUES (?, ?, ?, ?)`,
		event.AggregateID, event.EventID, event.EventType, event.Data,
	)
	if err != nil {
		return fmt.Errorf("unable to insert event: %w", err)
	}
	return nil
}

// ReadEventByID retrieves an event by its event ID.
func ReadEventByID(db *sql.DB, eventID string) (*Event, error) {
	row := db.QueryRow(`SELECT aggregate_id, event_id, event_type, data FROM events WHERE event_id = ?`, eventID)

	var aggregateID, eventIDOut, eventType, data string
	if err := row.Scan(&aggregateID, &eventIDOut, &eventType, &data); err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("no event found with event_id: %s", eventID)
		}
		return nil, fmt.Errorf("unable to query event: %w", err)
	}

	return &Event{
		AggregateID: aggregateID,
		EventID:     eventIDOut,
		EventType:   eventType,
		Data:        data,
	}, nil
}

// ListEventsByAggregateID retrieves all events for a given aggregate ID.
func ListEventsByAggregateID(db *sql.DB, aggregateID string) ([]Event, error) {
	rows, err := db.Query(`SELECT aggregate_id, event_id, event_type, data FROM events WHERE aggregate_id = ?`, aggregateID)
	if err != nil {
		return nil, fmt.Errorf("unable to query events: %w", err)
	}
	defer rows.Close()

	var events []Event
	for rows.Next() {
		var aggID, eventID, eventType, data string
		if err := rows.Scan(&aggID, &eventID, &eventType, &data); err != nil {
			return nil, fmt.Errorf("unable to scan event: %w", err)
		}

		events = append(events, Event{
			AggregateID: aggID,
			EventID:     eventID,
			EventType:   eventType,
			Data:        data,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating over rows: %w", err)
	}

	return events, nil
}
