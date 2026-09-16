package server

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
)

const (
	defaultFlightRecorderCapacity = 256
	maxFlightRecorderCapacity     = 4096
	maxFlightRecorderPrefixBytes  = 64
)

type flightRecorderConfig struct {
	enabled     bool
	directory   string
	capacity    int
	prefixBytes int
}

type flightRecorderMetadata struct {
	SessionID     string `json:"session_id"`
	CorrelationID string `json:"correlation_id"`
	Module        string `json:"module"`
	Remote        string `json:"remote,omitempty"`
}

type flightRecorderEvent struct {
	Type             string    `json:"type"`
	Sequence         uint64    `json:"sequence"`
	Timestamp        time.Time `json:"timestamp"`
	ElapsedMS        int64     `json:"elapsed_ms"`
	Direction        string    `json:"direction,omitempty"`
	Stage            string    `json:"stage"`
	Opcode           uint16    `json:"opcode,omitempty"`
	OpcodeHex        string    `json:"opcode_hex,omitempty"`
	PayloadSize      int       `json:"payload_size,omitempty"`
	PayloadSHA256    string    `json:"payload_sha256,omitempty"`
	PayloadPrefixHex string    `json:"payload_prefix_hex,omitempty"`
	PayloadRedacted  bool      `json:"payload_redacted,omitempty"`
	Encrypted        bool      `json:"encrypted,omitempty"`
	HandshakePhase   string    `json:"handshake_phase,omitempty"`
	Authenticated    bool      `json:"authenticated"`
	Handler          string    `json:"handler,omitempty"`
	DurationUS       int64     `json:"duration_us,omitempty"`
	Outcome          string    `json:"outcome,omitempty"`
	Error            string    `json:"error,omitempty"`
}

type flightRecorderFileHeader struct {
	Type      string                 `json:"type"`
	Version   int                    `json:"version"`
	StartedAt time.Time              `json:"started_at"`
	Session   flightRecorderMetadata `json:"session"`
	Capacity  int                    `json:"capacity"`
}

type flightRecorderFileSummary struct {
	Type         string    `json:"type"`
	EndedAt      time.Time `json:"ended_at"`
	Recorded     int       `json:"recorded"`
	Dropped      uint64    `json:"dropped"`
	LastSequence uint64    `json:"last_sequence"`
	FlushError   string    `json:"flush_error,omitempty"`
}

// sessionFlightRecorder retains only the newest events for one connection.
// It performs no filesystem I/O on packet paths; the terminal session drain
// writes one JSONL artifact after inbound and outbound workers have stopped.
type sessionFlightRecorder struct {
	mu          sync.Mutex
	config      flightRecorderConfig
	metadata    flightRecorderMetadata
	startedAt   time.Time
	events      []flightRecorderEvent
	next        int
	count       int
	sequence    uint64
	dropped     uint64
	flushed     bool
	flushResult string
}

func flightRecorderConfigFromEnvironment() flightRecorderConfig {
	config := flightRecorderConfig{
		enabled:   sessionTraceEnabled(),
		directory: strings.TrimSpace(os.Getenv("SRO_SESSION_TRACE_DIR")),
		capacity:  defaultFlightRecorderCapacity,
	}
	if config.directory == "" {
		config.directory = filepath.Join(".local", "session-traces")
	}
	if value, err := strconv.Atoi(strings.TrimSpace(os.Getenv("SRO_SESSION_TRACE_CAPACITY"))); err == nil && value > 0 {
		config.capacity = min(value, maxFlightRecorderCapacity)
	}
	if value, err := strconv.Atoi(strings.TrimSpace(os.Getenv("SRO_SESSION_TRACE_PREFIX_BYTES"))); err == nil && value > 0 {
		config.prefixBytes = min(value, maxFlightRecorderPrefixBytes)
	}
	return config
}

func newSessionFlightRecorder(config flightRecorderConfig, metadata flightRecorderMetadata, now time.Time) *sessionFlightRecorder {
	if !config.enabled {
		return nil
	}
	if config.capacity <= 0 {
		config.capacity = defaultFlightRecorderCapacity
	}
	config.capacity = min(config.capacity, maxFlightRecorderCapacity)
	config.prefixBytes = max(0, min(config.prefixBytes, maxFlightRecorderPrefixBytes))
	return &sessionFlightRecorder{
		config:    config,
		metadata:  metadata,
		startedAt: now.UTC(),
		events:    make([]flightRecorderEvent, config.capacity),
	}
}

func (r *sessionFlightRecorder) recordLifecycle(stage, outcome string, err error, phase HandshakePhase, authenticated bool) {
	if r == nil {
		return
	}
	event := flightRecorderEvent{
		Type:           "event",
		Direction:      "lifecycle",
		Stage:          stage,
		Outcome:        outcome,
		HandshakePhase: phase.String(),
		Authenticated:  authenticated,
	}
	if err != nil {
		event.Error = err.Error()
	}
	r.record(event, time.Now().UTC())
}

func (r *sessionFlightRecorder) setCorrelationID(correlationID string, phase HandshakePhase, authenticated bool) {
	if r == nil || strings.TrimSpace(correlationID) == "" {
		return
	}
	r.mu.Lock()
	r.metadata.CorrelationID = correlationID
	r.mu.Unlock()
	r.recordLifecycle("correlated", "accepted", nil, phase, authenticated)
}

func (r *sessionFlightRecorder) recordPacket(direction, stage string, packet network.Packet, handler, outcome string, err error, phase HandshakePhase, authenticated bool) {
	if r == nil {
		return
	}
	event := r.packetEvent(direction, stage, packet, handler, outcome, err, phase, authenticated)
	r.record(event, time.Now().UTC())
}

func (r *sessionFlightRecorder) packetEvent(direction, stage string, packet network.Packet, handler, outcome string, err error, phase HandshakePhase, authenticated bool) flightRecorderEvent {
	payload := packet.Payload()
	event := flightRecorderEvent{
		Type:           "event",
		Direction:      direction,
		Stage:          stage,
		Opcode:         packet.MessageID,
		OpcodeHex:      fmt.Sprintf("0x%04X", packet.MessageID),
		PayloadSize:    len(payload),
		Encrypted:      packet.Encrypted,
		HandshakePhase: phase.String(),
		Authenticated:  authenticated,
		Handler:        handler,
		Outcome:        outcome,
	}
	if err != nil {
		event.Error = err.Error()
	}
	if isSensitiveTraceOpcode(packet.MessageID) {
		event.PayloadRedacted = true
	} else if len(payload) > 0 {
		hash := sha256.Sum256(payload)
		event.PayloadSHA256 = hex.EncodeToString(hash[:])
		if r.config.prefixBytes > 0 {
			length := min(len(payload), r.config.prefixBytes)
			event.PayloadPrefixHex = hex.EncodeToString(payload[:length])
		}
	}
	return event
}

func (r *sessionFlightRecorder) recordDispatch(packet network.Packet, handler string, handled bool, err error, duration time.Duration, phase HandshakePhase, authenticated bool) {
	if r == nil {
		return
	}
	outcome := "handled"
	if err != nil {
		outcome = "error"
	} else if !handled {
		outcome = "unhandled"
	}
	event := r.packetEvent("inbound", "dispatch", packet, handler, outcome, err, phase, authenticated)
	event.DurationUS = duration.Microseconds()
	r.record(event, time.Now().UTC())
}

func (r *sessionFlightRecorder) record(event flightRecorderEvent, now time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.flushed {
		return
	}
	r.sequence++
	event.Sequence = r.sequence
	event.Timestamp = now
	event.ElapsedMS = now.Sub(r.startedAt).Milliseconds()
	if r.count == len(r.events) {
		r.dropped++
	} else {
		r.count++
	}
	r.events[r.next] = event
	r.next = (r.next + 1) % len(r.events)
}

func (r *sessionFlightRecorder) snapshotAndMarkFlushed() (flightRecorderFileHeader, []flightRecorderEvent, flightRecorderFileSummary, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.flushed {
		return flightRecorderFileHeader{}, nil, flightRecorderFileSummary{}, false
	}
	r.flushed = true
	header := flightRecorderFileHeader{Type: "session", Version: 1, StartedAt: r.startedAt, Session: r.metadata, Capacity: len(r.events)}
	events := make([]flightRecorderEvent, 0, r.count)
	start := 0
	if r.count == len(r.events) {
		start = r.next
	}
	for index := 0; index < r.count; index++ {
		events = append(events, r.events[(start+index)%len(r.events)])
	}
	summary := flightRecorderFileSummary{
		Type:         "summary",
		EndedAt:      time.Now().UTC(),
		Recorded:     len(events),
		Dropped:      r.dropped,
		LastSequence: r.sequence,
	}
	return header, events, summary, true
}

func (r *sessionFlightRecorder) flush() (string, error) {
	if r == nil {
		return "", nil
	}
	header, events, summary, ok := r.snapshotAndMarkFlushed()
	if !ok {
		r.mu.Lock()
		result := r.flushResult
		r.mu.Unlock()
		return result, nil
	}
	if err := os.MkdirAll(r.config.directory, 0o750); err != nil {
		return "", fmt.Errorf("create session trace directory: %w", err)
	}
	filename := fmt.Sprintf("%s-%s-%s.jsonl", sanitizeTraceFilename(r.metadata.Module), header.StartedAt.Format("20060102T150405.000000000Z"), sanitizeTraceFilename(r.metadata.SessionID))
	path := filepath.Join(r.config.directory, filename)
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", fmt.Errorf("create session trace: %w", err)
	}
	writer := bufio.NewWriter(file)
	encoder := json.NewEncoder(writer)
	writeErr := encoder.Encode(header)
	for _, event := range events {
		if writeErr != nil {
			break
		}
		writeErr = encoder.Encode(event)
	}
	if writeErr == nil {
		writeErr = encoder.Encode(summary)
	}
	writeErr = errors.Join(writeErr, writer.Flush(), file.Close())
	if writeErr != nil {
		return path, fmt.Errorf("write session trace: %w", writeErr)
	}
	r.mu.Lock()
	r.flushResult = path
	r.mu.Unlock()
	return path, nil
}

func sanitizeTraceFilename(value string) string {
	if value == "" {
		return "unknown"
	}
	var builder strings.Builder
	for _, character := range value {
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || character == '-' || character == '_' {
			builder.WriteRune(character)
		} else {
			builder.WriteByte('_')
		}
	}
	return builder.String()
}

func isSensitiveTraceOpcode(messageID uint16) bool {
	switch messageID {
	case 0x5000, 0x9000,
		opcode.PatchRequest,
		opcode.LoginRequest,
		opcode.LoginResponse,
		opcode.LauncherTokenRequest,
		opcode.LauncherTokenResponse,
		opcode.AuthRequest,
		opcode.AuthResponse,
		opcode.AgentLoginTokenRequest,
		opcode.AgentGameserverForPlayerRequest,
		opcode.AgentPlayerJoinRequest,
		opcode.AgentGameserverForPlayerResponse,
		opcode.AgentPlayerJoinResponse:
		return true
	default:
		return false
	}
}

func (phase HandshakePhase) String() string {
	switch phase {
	case HandshakeNew:
		return "new"
	case HandshakeChallengeSent:
		return "challenge_sent"
	case HandshakeAwaitingCompletion:
		return "awaiting_completion"
	case HandshakeEstablished:
		return "established"
	default:
		return fmt.Sprintf("unknown_%d", phase)
	}
}
