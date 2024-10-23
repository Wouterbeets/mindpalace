package store

import (
	"database/sql"
	"fmt"

	_ "github.com/mattn/go-sqlite3"
)

type Event struct {
	AggregateID string
	EventID     string
	EventType   string
	Data        string
}

type Store struct {
	db *sql.DB
}

func NewStore(dbPath string) (*Store, error) {
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("unable to connect to database: %w", err)
	}
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("unable to ping database: %w", err)
	}
	return &Store{db: db}, nil
}

func (s *Store) WriteEvent(event Event) error {
	_, err := s.db.Exec(
		`INSERT INTO events (aggregate_id, event_id, event_type, data) VALUES (?, ?, ?, ?)`,
		event.AggregateID, event.EventID, event.EventType, event.Data,
	)
	if err != nil {
		return fmt.Errorf("unable to insert event: %w", err)
	}
	return nil
}

func (s *Store) ReadEventByID(eventID string) (*Event, error) {
	row := s.db.QueryRow(`SELECT aggregate_id, event_id, event_type, data FROM events WHERE event_id = ?`, eventID)

	var event Event
	if err := row.Scan(&event.AggregateID, &event.EventID, &event.EventType, &event.Data); err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("no event found with event_id: %s", eventID)
		}
		return nil, fmt.Errorf("unable to query event: %w", err)
	}

	return &event, nil
}

func (s *Store) ListEventsByAggregateID(aggregateID string) ([]Event, error) {
	rows, err := s.db.Query(`SELECT aggregate_id, event_id, event_type, data FROM events WHERE aggregate_id = ?`, aggregateID)
	if err != nil {
		return nil, fmt.Errorf("unable to query events: %w", err)
	}
	defer rows.Close()

	var events []Event
	for rows.Next() {
		var event Event
		if err := rows.Scan(&event.AggregateID, &event.EventID, &event.EventType, &event.Data); err != nil {
			return nil, fmt.Errorf("unable to scan event: %w", err)
		}
		events = append(events, event)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating over rows: %w", err)
	}

	return events, nil
}
