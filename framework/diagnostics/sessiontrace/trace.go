package sessiontrace

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"time"
)

type Session struct {
	SessionID     string `json:"session_id"`
	CorrelationID string `json:"correlation_id"`
	Module        string `json:"module"`
	Remote        string `json:"remote"`
}

type Header struct {
	Type      string    `json:"type"`
	Version   int       `json:"version"`
	StartedAt time.Time `json:"started_at"`
	Session   Session   `json:"session"`
	Capacity  int       `json:"capacity"`
}

type Event struct {
	Type             string    `json:"type"`
	Sequence         uint64    `json:"sequence"`
	Timestamp        time.Time `json:"timestamp"`
	ElapsedMS        int64     `json:"elapsed_ms"`
	Direction        string    `json:"direction"`
	Stage            string    `json:"stage"`
	Opcode           uint16    `json:"opcode"`
	OpcodeHex        string    `json:"opcode_hex"`
	PayloadSize      int       `json:"payload_size"`
	PayloadSHA256    string    `json:"payload_sha256"`
	PayloadPrefixHex string    `json:"payload_prefix_hex"`
	PayloadRedacted  bool      `json:"payload_redacted"`
	Encrypted        bool      `json:"encrypted"`
	HandshakePhase   string    `json:"handshake_phase"`
	Authenticated    bool      `json:"authenticated"`
	Handler          string    `json:"handler"`
	DurationUS       int64     `json:"duration_us"`
	Outcome          string    `json:"outcome"`
	Error            string    `json:"error"`
	SessionID        string    `json:"session_id,omitempty"`
	CorrelationID    string    `json:"correlation_id,omitempty"`
	Module           string    `json:"module,omitempty"`
	Source           string    `json:"-"`
}

type Summary struct {
	Type         string    `json:"type"`
	EndedAt      time.Time `json:"ended_at"`
	Recorded     int       `json:"recorded"`
	Dropped      uint64    `json:"dropped"`
	LastSequence uint64    `json:"last_sequence"`
}

type Trace struct {
	Path    string
	Header  Header
	Events  []Event
	Summary Summary
}

func ReadFile(path string) (Trace, error) {
	file, err := os.Open(path)
	if err != nil {
		return Trace{}, fmt.Errorf("open %q: %w", path, err)
	}
	defer file.Close()

	trace := Trace{Path: path}
	scanner := bufio.NewScanner(file)
	buffer := make([]byte, 64*1024)
	scanner.Buffer(buffer, 1024*1024)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := scanner.Bytes()
		var envelope struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(line, &envelope); err != nil {
			return Trace{}, fmt.Errorf("decode %q line %d: %w", path, lineNumber, err)
		}
		switch envelope.Type {
		case "session":
			if err := json.Unmarshal(line, &trace.Header); err != nil {
				return Trace{}, fmt.Errorf("decode %q header: %w", path, err)
			}
		case "event":
			var event Event
			if err := json.Unmarshal(line, &event); err != nil {
				return Trace{}, fmt.Errorf("decode %q event: %w", path, err)
			}
			trace.Events = append(trace.Events, event)
		case "summary":
			if err := json.Unmarshal(line, &trace.Summary); err != nil {
				return Trace{}, fmt.Errorf("decode %q summary: %w", path, err)
			}
		default:
			return Trace{}, fmt.Errorf("decode %q line %d: unknown record type %q", path, lineNumber, envelope.Type)
		}
	}
	if err := scanner.Err(); err != nil {
		return Trace{}, fmt.Errorf("read %q: %w", path, err)
	}
	if trace.Header.Type != "session" {
		return Trace{}, fmt.Errorf("decode %q: missing session header", path)
	}
	if trace.Summary.Type != "summary" {
		return Trace{}, fmt.Errorf("decode %q: missing session summary", path)
	}
	for index := range trace.Events {
		trace.Events[index].SessionID = trace.Header.Session.SessionID
		trace.Events[index].CorrelationID = trace.Header.Session.CorrelationID
		trace.Events[index].Module = trace.Header.Session.Module
		trace.Events[index].Source = path
	}
	return trace, nil
}
