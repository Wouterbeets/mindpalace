package logging

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetLogger(t *testing.T) {
	logger1 := GetLogger()
	logger2 := GetLogger()
	assert.Equal(t, logger1, logger2, "GetLogger should return the same instance")
}

func TestSetVerbosity(t *testing.T) {
	logger := GetLogger()
	SetVerbosity(LogLevelDebug)
	assert.Equal(t, LogLevelDebug, logger.level)
}

func TestLoggerSetLevel(t *testing.T) {
	logger := &Logger{}
	logger.SetLevel(LogLevelTrace)
	assert.Equal(t, LogLevelTrace, logger.level)
}

func TestLoggerSetOutput(t *testing.T) {
	logger := &Logger{}
	buf := &bytes.Buffer{}
	logger.SetOutput(buf)
	assert.Equal(t, buf, logger.handler)
	assert.NotNil(t, logger.logger)
}

func TestLoggerError(t *testing.T) {
	oldOutput := globalLogger.handler
	defer func() { globalLogger.handler = oldOutput }()

	buf := &bytes.Buffer{}
	SetOutput(buf)
	Error("test error")
	assert.Contains(t, buf.String(), "[ERROR] test error")
}

func TestLoggerInfo(t *testing.T) {
	buf := &bytes.Buffer{}
	SetOutput(buf)
	SetVerbosity(LogLevelInfo)
	Info("test info")
	assert.Contains(t, buf.String(), "[INFO] test info")
}

func TestLoggerDebug(t *testing.T) {
	buf := &bytes.Buffer{}
	SetOutput(buf)
	SetVerbosity(LogLevelDebug)
	Debug("test debug")
	assert.Contains(t, buf.String(), "[DEBUG] test debug")
}

func TestLoggerTrace(t *testing.T) {
	buf := &bytes.Buffer{}
	SetOutput(buf)
	SetVerbosity(LogLevelTrace)
	Trace("test trace")
	assert.Contains(t, buf.String(), "[TRACE] test trace")
}

func TestLoggerCommand(t *testing.T) {
	buf := &bytes.Buffer{}
	SetOutput(buf)
	SetVerbosity(LogLevelDebug)
	Command("testCmd", map[string]string{"key": "value"})
	output := buf.String()
	assert.Contains(t, output, "[COMMAND] testCmd")
	assert.Contains(t, output, "[DEBUG] Command data:")
}

func TestLogLevels(t *testing.T) {
	assert.Equal(t, LogLevel(0), LogLevelError)
	assert.Equal(t, LogLevel(1), LogLevelInfo)
	assert.Equal(t, LogLevel(2), LogLevelDebug)
	assert.Equal(t, LogLevel(3), LogLevelTrace)
}
