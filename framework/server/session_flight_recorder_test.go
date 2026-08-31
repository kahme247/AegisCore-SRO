package server

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
)

func TestSessionFlightRecorderKeepsNewestBoundedEvents(t *testing.T) {
	startedAt := time.Date(2026, time.July, 21, 10, 0, 0, 0, time.UTC)
	recorder := newSessionFlightRecorder(
		flightRecorderConfig{enabled: true, directory: t.TempDir(), capacity: 2},
		flightRecorderMetadata{SessionID: "session-1", Module: "GatewayServer"},
		startedAt,
	)
	for index := 0; index < 3; index++ {
		recorder.record(flightRecorderEvent{Type: "event", Stage: "test"}, startedAt.Add(time.Duration(index)*time.Millisecond))
	}

	_, events, summary, ok := recorder.snapshotAndMarkFlushed()
	if !ok {
		t.Fatal("snapshot unexpectedly reported an already-flushed recorder")
	}
	if len(events) != 2 || events[0].Sequence != 2 || events[1].Sequence != 3 {
		t.Fatalf("events = %+v, want retained sequences 2 and 3", events)
	}
	if summary.Recorded != 2 || summary.Dropped != 1 || summary.LastSequence != 3 {
		t.Fatalf("summary = %+v", summary)
	}
}

func TestSessionFlightRecorderUpdatesGatewayCorrelationBeforeFlush(t *testing.T) {
	recorder := newSessionFlightRecorder(
		flightRecorderConfig{enabled: true, directory: t.TempDir(), capacity: 4},
		flightRecorderMetadata{SessionID: "agent-session", CorrelationID: "agent-session", Module: "AgentServer"},
		time.Now(),
	)
	recorder.setCorrelationID("gateway-session", HandshakeEstablished, true)

	header, events, _, ok := recorder.snapshotAndMarkFlushed()
	if !ok || header.Session.CorrelationID != "gateway-session" {
		t.Fatalf("header = %+v, ok = %t", header, ok)
	}
	if len(events) != 1 || events[0].Stage != "correlated" || !events[0].Authenticated {
		t.Fatalf("events = %+v", events)
	}
}

func TestSessionFlightRecorderRedactsCredentialsAndBoundsOptInPrefix(t *testing.T) {
	recorder := newSessionFlightRecorder(
		flightRecorderConfig{enabled: true, directory: t.TempDir(), capacity: 4, prefixBytes: 3},
		flightRecorderMetadata{SessionID: "session-2", Module: "GatewayServer"},
		time.Now(),
	)

	login := network.EmptyClientPacket()
	login.MessageID = opcode.LoginRequest
	if err := login.WriteBytes([]byte("username-password")); err != nil {
		t.Fatal(err)
	}
	recorder.recordPacket("inbound", "decoded", login, "", "accepted", nil, HandshakeEstablished, false)

	ordinary := network.EmptyClientPacket()
	ordinary.MessageID = opcode.ShardlistRequest
	if err := ordinary.WriteBytes([]byte{0x01, 0x02, 0x03, 0x04}); err != nil {
		t.Fatal(err)
	}
	recorder.recordPacket("inbound", "decoded", ordinary, "", "accepted", nil, HandshakeEstablished, false)

	_, events, _, _ := recorder.snapshotAndMarkFlushed()
	if !events[0].PayloadRedacted || events[0].PayloadSHA256 != "" || events[0].PayloadPrefixHex != "" {
		t.Fatalf("credential event was not fully redacted: %+v", events[0])
	}
	if events[1].PayloadRedacted || events[1].PayloadSHA256 == "" || events[1].PayloadPrefixHex != "010203" {
		t.Fatalf("ordinary event payload metadata = %+v", events[1])
	}
}

func TestSessionFlightRecorderFlushesJSONLArtifactOnce(t *testing.T) {
	directory := t.TempDir()
	startedAt := time.Date(2026, time.July, 21, 10, 30, 0, 0, time.UTC)
	recorder := newSessionFlightRecorder(
		flightRecorderConfig{enabled: true, directory: directory, capacity: 4},
		flightRecorderMetadata{SessionID: "session/3", Module: "Agent Server", Remote: "127.0.0.1:1234"},
		startedAt,
	)
	recorder.recordLifecycle("connected", "accepted", nil, HandshakeNew, false)
	recorder.recordLifecycle("closed", "drained", errors.New("test disconnect"), HandshakeEstablished, true)

	path, err := recorder.flush()
	if err != nil {
		t.Fatalf("flush() error = %v", err)
	}
	if filepath.Dir(path) != directory {
		t.Fatalf("flush path = %q, want directory %q", path, directory)
	}
	if filepath.Base(path) != "Agent_Server-20260721T103000.000000000Z-session_3.jsonl" {
		t.Fatalf("unexpected trace filename %q", filepath.Base(path))
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("trace permissions = %o, want 600", info.Mode().Perm())
	}

	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	var records []map[string]any
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var record map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			t.Fatalf("decode trace line: %v", err)
		}
		records = append(records, record)
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if len(records) != 4 || records[0]["type"] != "session" || records[3]["type"] != "summary" {
		t.Fatalf("trace records = %+v", records)
	}

	again, err := recorder.flush()
	if err != nil || again != path {
		t.Fatalf("second flush() = %q, %v; want %q, nil", again, err, path)
	}
}

func TestCompletedSessionTraceAutomaticallyProducesAnalysisReports(t *testing.T) {
	directory := t.TempDir()
	recorder := newSessionFlightRecorder(
		flightRecorderConfig{enabled: true, directory: directory, capacity: 4},
		flightRecorderMetadata{SessionID: "agent-session", CorrelationID: "gateway-session", Module: "AgentServer"},
		time.Now(),
	)
	packet := network.EmptyClientPacket()
	packet.MessageID = opcode.ItemOperationRequest
	if err := packet.WriteByte(1); err != nil {
		t.Fatal(err)
	}
	recorder.recordDispatch(packet, "inventory.Handler", true, errors.New("inventory transaction failed"), time.Millisecond, HandshakeEstablished, true)
	tracePath, err := recorder.flush()
	if err != nil {
		t.Fatal(err)
	}

	analysis, err := analyzeCompletedSessionTrace(tracePath)
	if err != nil {
		t.Fatalf("analyzeCompletedSessionTrace() error = %v", err)
	}
	if analysis.Probable == nil || analysis.Probable.Kind != "operation_error" || analysis.Probable.Event.CorrelationID != "gateway-session" {
		t.Fatalf("analysis = %+v", analysis)
	}
	for _, path := range []string{analysis.Reports.JSON, analysis.Reports.Markdown} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("analysis report %q: %v", path, err)
		}
	}
}

func TestFlightRecorderConfigFromEnvironmentIsOptInAndCapped(t *testing.T) {
	t.Setenv("SRO_SESSION_TRACE", "1")
	t.Setenv("SRO_SESSION_TRACE_DIR", "custom-traces")
	t.Setenv("SRO_SESSION_TRACE_CAPACITY", "99999")
	t.Setenv("SRO_SESSION_TRACE_PREFIX_BYTES", "99999")

	config := flightRecorderConfigFromEnvironment()
	if !config.enabled || config.directory != "custom-traces" || config.capacity != maxFlightRecorderCapacity || config.prefixBytes != maxFlightRecorderPrefixBytes {
		t.Fatalf("config = %+v", config)
	}
}
