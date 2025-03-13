package eventsourcing

import (
	"bufio"
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
		event := &GenericEvent{}
		if err := event.Unmarshal(raw); err != nil {
			return err
		}
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
