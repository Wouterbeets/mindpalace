package eventsourcing

import (
	"bufio"
	"fmt"
	"mindpalace/pkg/logging"
	"os"
	"sync"
)

// EventStore manages the persistence and retrieval of events
type FileEventStore struct {
	mu       sync.Mutex
	events   []Event
	filePath string
}

func NewFileEventStore(filePath string) *FileEventStore {
	return &FileEventStore{
		filePath: filePath,
	}
}

func (es *FileEventStore) Load() error {
	es.mu.Lock()
	defer es.mu.Unlock()

	file, err := os.Open(es.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // File doesn't exist yet, no events to load
		}
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		raw := scanner.Bytes()
		logging.Trace("read from events file %s", string(raw))
		event, err := UnmarshalEvent(raw)
		if err != nil {
			return fmt.Errorf("failed to load event: %v", err)
		}
		logging.Trace("loaded event from eventstore %+v", event)
		es.events = append(es.events, event)
	}
	return scanner.Err()
}

func (es *FileEventStore) Append(events ...Event) error {
	es.mu.Lock()
	defer es.mu.Unlock()

	for _, event := range events {
		data, err := event.Marshal()
		if err != nil {
			return err
		}
		f, err := os.OpenFile(es.filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			return err
		}
		if _, err := f.Write(data); err != nil {
			f.Close()
			return err
		}
		if _, err := f.WriteString("\n"); err != nil {
			f.Close()
			return err
		}
		f.Close()
		es.events = append(es.events, event)
	}
	return nil
}

func (es *FileEventStore) GetEvents() []Event {
	es.mu.Lock()
	defer es.mu.Unlock()
	return append([]Event{}, es.events...)
}

// MigrateFromFileToSQLite migrates events from JSON file to SQLite database
func MigrateFromFileToSQLite(fileStore *FileEventStore, sqliteStore *SQLiteEventStore) error {
	events := fileStore.GetEvents()
	if len(events) == 0 {
		return nil // Nothing to migrate
	}
	logging.Info("Migrating %d events from file to SQLite", len(events))
	return sqliteStore.Append(events...)
}
