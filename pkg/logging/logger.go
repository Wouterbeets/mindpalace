package logging

import (
	"fmt"
	"io"
	"log"
	"os"
	"sync"
)

// LogLevel defines the logging level
type LogLevel int

const (
	// LogLevelError is for error messages only
	LogLevelError LogLevel = iota
	// LogLevelInfo is for important but normal messages
	LogLevelInfo
	// LogLevelDebug is for detailed messages useful for debugging
	LogLevelDebug
	// LogLevelTrace is for extremely detailed messages
	LogLevelTrace
)

// Logger provides a simple logging interface with verbosity controls
type Logger struct {
	mu      sync.Mutex
	level   LogLevel
	logger  *log.Logger
	handler io.Writer
}

var (
	// Global instance of the logger
	globalLogger *Logger
	once         sync.Once
)

// GetLogger returns the global logger instance
func GetLogger() *Logger {
	once.Do(func() {
		globalLogger = &Logger{
			level:   LogLevelInfo, // Default level
			handler: os.Stdout,
			logger:  log.New(os.Stdout, "", log.LstdFlags),
		}
	})
	return globalLogger
}

// SetVerbosity sets the global log level
func SetVerbosity(level LogLevel) {
	GetLogger().SetLevel(level)
}

// SetOutput sets the log output destination
func SetOutput(w io.Writer) {
	GetLogger().SetOutput(w)
}

// SetLevel sets the log level for this logger
func (l *Logger) SetLevel(level LogLevel) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.level = level
}

// SetOutput sets the output destination for this logger
func (l *Logger) SetOutput(w io.Writer) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.handler = w
	l.logger = log.New(w, "", log.LstdFlags)
}

// Error logs an error message regardless of verbosity level
func (l *Logger) Error(format string, args ...interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()
	msg := fmt.Sprintf("[ERROR] "+format, args...)
	l.logger.Println(msg)
}

// Info logs information that should always be shown unless errors only
func (l *Logger) Info(format string, args ...interface{}) {
	if l.level >= LogLevelInfo {
		l.mu.Lock()
		defer l.mu.Unlock()
		msg := fmt.Sprintf("[INFO] "+format, args...)
		l.logger.Println(msg)
	}
}

// Debug logs detailed information for debugging purposes
func (l *Logger) Debug(format string, args ...interface{}) {
	if l.level >= LogLevelDebug {
		l.mu.Lock()
		defer l.mu.Unlock()
		msg := fmt.Sprintf("[DEBUG] "+format, args...)
		l.logger.Println(msg)
	}
}

// Trace logs extremely detailed information
func (l *Logger) Trace(format string, args ...interface{}) {
	if l.level >= LogLevelTrace {
		l.mu.Lock()
		defer l.mu.Unlock()
		msg := fmt.Sprintf("[TRACE] "+format, args...)
		l.logger.Println(msg)
	}
}

// Command logs information about commands being executed
func (l *Logger) Command(commandName string, data any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	msg := fmt.Sprintf("[COMMAND] %s", commandName)
	l.logger.Println(msg)

	// Log command details at debug level
	if l.level >= LogLevelDebug {
		if data != nil {
			dataMsg := fmt.Sprintf("[DEBUG] Command data: %v", data)
			l.logger.Println(dataMsg)
		}
	}
}

// Event logs information about events being processed
func (l *Logger) Event(eventType string, data map[string]interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()
	msg := fmt.Sprintf("[EVENT] %s", eventType)
	l.logger.Println(msg)

	// Log event details at debug level
	if l.level >= LogLevelDebug {
		if data != nil {
			dataMsg := fmt.Sprintf("[DEBUG] Event data: %v", data)
			l.logger.Println(dataMsg)
		}
	}
}

// Global convenience functions

// Error logs an error message
func Error(format string, args ...interface{}) {
	GetLogger().Error(format, args...)
}

// Info logs an info message
func Info(format string, args ...interface{}) {
	GetLogger().Info(format, args...)
}

// Debug logs a debug message
func Debug(format string, args ...interface{}) {
	GetLogger().Debug(format, args...)
}

// Trace logs a trace message
func Trace(format string, args ...interface{}) {
	GetLogger().Trace(format, args...)
}

// Command logs a command execution
func Command(commandName string, data any) {
	GetLogger().Command(commandName, data)
}

// Event logs an event
func Event(eventType string, data map[string]interface{}) {
	GetLogger().Event(eventType, data)
}
