package eventsourcing

import (
	"database/sql"
	"fmt"
	"sync"

	_ "github.com/mattn/go-sqlite3"
)

// SQLiteEventStore manages the persistence and retrieval of events using SQLite
type SQLiteEventStore struct {
	mu       sync.Mutex
	events   []Event
	db       *sql.DB
	dbPath   string
}

func NewSQLiteEventStore(dbPath string) (*SQLiteEventStore, error) {
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, err
	}

	// Create table if not exists
	createTableSQL := `CREATE TABLE IF NOT EXISTS events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		event_type TEXT NOT NULL,
		data TEXT NOT NULL,
		timestamp DATETIME DEFAULT CURRENT_TIMESTAMP
	);`
	if _, err := db.Exec(createTableSQL); err != nil {
		return nil, fmt.Errorf("failed to create table: %v", err)
	}

	return &SQLiteEventStore{
		db:     db,
		dbPath: dbPath,
	}, nil
}

func (es *SQLiteEventStore) Load() error {
	es.mu.Lock()
	defer es.mu.Unlock()

	rows, err := es.db.Query("SELECT data FROM events ORDER BY id")
	if err != nil {
		return err
	}
	defer rows.Close()

	es.events = []Event{} // Reset
	for rows.Next() {
		var data []byte
		if err := rows.Scan(&data); err != nil {
			return err
		}
		event, err := UnmarshalEvent(data)
		if err != nil {
			return fmt.Errorf("failed to load event: %v", err)
		}
		es.events = append(es.events, event)
	}
	return rows.Err()
}

func (es *SQLiteEventStore) Append(events ...Event) error {
	es.mu.Lock()
	defer es.mu.Unlock()

	tx, err := es.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, event := range events {
		data, err := event.Marshal()
		if err != nil {
			return err
		}
		_, err = tx.Exec("INSERT INTO events (event_type, data) VALUES (?, ?)", event.Type(), string(data))
		if err != nil {
			return err
		}
		es.events = append(es.events, event)
	}
	return tx.Commit()
}

func (es *SQLiteEventStore) GetEvents() []Event {
	es.mu.Lock()
	defer es.mu.Unlock()
	return append([]Event{}, es.events...)
}

func (es *SQLiteEventStore) Close() error {
	return es.db.Close()
}
