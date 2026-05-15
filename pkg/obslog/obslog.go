// Package obslog provides a structured logger for 5G procedure events.
//
// Every log entry carries:
//   - Timestamp
//   - Component name (AMF, gNB, SMF, ...)
//   - Message
//   - 3GPP spec reference (TS XX.XXX §Y.Z)
//   - Optional key-value pairs
//
// Output formats:
//   - Human-readable console (with colour)
//   - JSON (for log aggregation)
//   - JSONL file (persistent, one JSON object per line)
//
// Usage:
//
//	log := obslog.New("AMF")
//	log.Info("NGSetupRequest received", "TS 38.413 §9.2.6.1",
//	    "gnb", "gNB-001", "tac", "000001")
package obslog

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

)

// Level is the severity of a log entry.
type Level int

const (
	LevelDebug Level = iota
	LevelInfo
	LevelWarn
	LevelError
)

func (l Level) String() string {
	return [...]string{"DEBUG", "INFO ", "WARN ", "ERROR"}[l]
}

// colour codes for terminal output
func (l Level) colour() string {
	return [...]string{"\033[36m", "\033[32m", "\033[33m", "\033[31m"}[l]
}

const colourReset = "\033[0m"

// Entry is one structured log record.
type Entry struct {
	Timestamp time.Time         `json:"ts"`
	Component string            `json:"component"`
	Level     string            `json:"level"`
	Message   string            `json:"msg"`
	SpecRef   string            `json:"specRef,omitempty"`
	Fields    map[string]string `json:"fields,omitempty"`
}

// Logger emits structured log entries for a named component.
type Logger struct {
	component string
	mu        sync.Mutex
	sink      *Sink
}

// Sink is the shared output target for all loggers.
// Holds the file writer and sequence diagram recorder.
type Sink struct {
	mu       sync.Mutex
	jsonFile *os.File
	coloured bool
}

// globalSink is the process-wide output target.
var globalSink = &Sink{coloured: true}

// publishHook is called for each log entry when set (e.g. by pkg/obspub).
var publishHook func(Entry)

// SetPublishHook registers a callback invoked after each log entry is written.
func SetPublishHook(fn func(Entry)) {
	publishHook = fn
}

// InitFile opens a JSONL log file. Call once at startup.
// All Logger instances share this file.
func InitFile(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("open log file %s: %w", path, err)
	}
	globalSink.mu.Lock()
	globalSink.jsonFile = f
	globalSink.mu.Unlock()
	fmt.Printf("[obslog] JSON log: %s\n", path)
	return nil
}

// New creates a Logger for the named component.
// e.g. obslog.New("AMF"), obslog.New("gNB")
func New(component string) *Logger {
	return &Logger{component: component, sink: globalSink}
}

// Info logs an informational event with a spec reference.
// kvpairs are alternating key, value strings.
func (l *Logger) Info(msg, specRef string, kvpairs ...string) {
	l.log(LevelInfo, msg, specRef, kvpairs...)
}

// Debug logs a debug event.
func (l *Logger) Debug(msg, specRef string, kvpairs ...string) {
	l.log(LevelDebug, msg, specRef, kvpairs...)
}

// Warn logs a warning.
func (l *Logger) Warn(msg, specRef string, kvpairs ...string) {
	l.log(LevelWarn, msg, specRef, kvpairs...)
}

// Error logs an error.
func (l *Logger) Error(msg, specRef string, kvpairs ...string) {
	l.log(LevelError, msg, specRef, kvpairs...)
}

// log is the internal implementation.
func (l *Logger) log(level Level, msg, specRef string, kvpairs ...string) {
	fields := make(map[string]string)
	for i := 0; i+1 < len(kvpairs); i += 2 {
		fields[kvpairs[i]] = kvpairs[i+1]
	}

	entry := Entry{
		Timestamp: time.Now(),
		Component: l.component,
		Level:     level.String(),
		Message:   msg,
		SpecRef:   specRef,
		Fields:    fields,
	}

	l.sink.mu.Lock()
	defer l.sink.mu.Unlock()

	// Console output
	l.writeConsole(level, entry)

	// JSON file output
	if l.sink.jsonFile != nil {
		b, err := json.Marshal(entry)
		if err == nil {
			l.sink.jsonFile.Write(b)
			l.sink.jsonFile.WriteString("\n")
		}
	}

	if publishHook != nil {
		publishHook(entry)
	}
}

// writeConsole emits a coloured, human-readable log line.
func (l *Logger) writeConsole(level Level, e Entry) {
	ts := e.Timestamp.Format("15:04:05.000")

	// Build fields string
	var fieldParts []string
	for k, v := range e.Fields {
		fieldParts = append(fieldParts, fmt.Sprintf("%s=%s", k, v))
	}
	fieldStr := ""
	if len(fieldParts) > 0 {
		fieldStr = "  " + strings.Join(fieldParts, " ")
	}

	// Spec reference
	specStr := ""
	if e.SpecRef != "" {
		specStr = fmt.Sprintf("  \033[2m[%s]\033[0m", e.SpecRef)
	}

	colour := ""
	reset := ""
	if l.sink.coloured {
		colour = level.colour()
		reset = colourReset
	}

	fmt.Printf("%s%s [%s] %-4s %s%s%s%s\n",
		colour, ts, e.Component, level.String(),
		e.Message, fieldStr, specStr, reset)
}

// Close flushes and closes the JSON log file.
func Close() {
	globalSink.mu.Lock()
	defer globalSink.mu.Unlock()
	if globalSink.jsonFile != nil {
		globalSink.jsonFile.Close()
		globalSink.jsonFile = nil
	}
}
