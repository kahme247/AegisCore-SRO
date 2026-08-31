package sessiontrace

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const testTraceJSONL = `{"type":"session","version":1,"started_at":"2026-07-21T10:00:00Z","session":{"session_id":"agent-session","correlation_id":"gateway-session","module":"AgentServer","remote":"127.0.0.1:1234"},"capacity":256}
{"type":"event","sequence":1,"timestamp":"2026-07-21T10:00:00.001Z","elapsed_ms":1,"direction":"inbound","stage":"dispatch","opcode":28724,"opcode_hex":"0x7034","payload_size":12,"authenticated":true,"handler":"github.com/Harvi8/AegisCore/agent-server/handler/inventory.(*Handler).Handle-fm","duration_us":150000,"outcome":"error","error":"inventory transaction failed"}
{"type":"summary","ended_at":"2026-07-21T10:00:01Z","recorded":1,"dropped":2,"last_sequence":3}
`

func TestReadAnalyzeAndWriteReports(t *testing.T) {
	directory := t.TempDir()
	tracePath := filepath.Join(directory, "agent.jsonl")
	if err := os.WriteFile(tracePath, []byte(testTraceJSONL), 0o600); err != nil {
		t.Fatal(err)
	}
	trace, err := ReadFile(tracePath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	analysis := Analyze([]Trace{trace}, DefaultAnalysisOptions())
	if analysis.Status != StatusAttention || analysis.ProbableFailure == nil || analysis.ProbableFailure.Kind != "operation_error" {
		t.Fatalf("analysis = %+v", analysis)
	}
	if len(analysis.Findings) != 3 {
		t.Fatalf("findings = %+v, want error, slow handler, and overwrite", analysis.Findings)
	}

	paths, err := WriteReports(tracePath, analysis)
	if err != nil {
		t.Fatalf("WriteReports() error = %v", err)
	}
	jsonBytes, err := os.ReadFile(paths.JSON)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Analysis
	if err := json.Unmarshal(jsonBytes, &decoded); err != nil {
		t.Fatalf("decode JSON report: %v", err)
	}
	if decoded.ProbableFailure == nil || decoded.ProbableFailure.Event.CorrelationID != "gateway-session" {
		t.Fatalf("decoded analysis = %+v", decoded)
	}
	markdown, err := os.ReadFile(paths.Markdown)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"# Session trace analysis", "## Probable failure", "0x7034", "inventory transaction failed"} {
		if !strings.Contains(string(markdown), expected) {
			t.Fatalf("Markdown report missing %q:\n%s", expected, markdown)
		}
	}
}

func TestAnalyzeDetectsRepeatedPacketBurstOnce(t *testing.T) {
	startedAt := time.Date(2026, time.July, 21, 10, 0, 0, 0, time.UTC)
	trace := Trace{Header: Header{Session: Session{SessionID: "agent", CorrelationID: "gateway", Module: "AgentServer"}}}
	for index := 0; index < 6; index++ {
		trace.Events = append(trace.Events, Event{
			Timestamp:     startedAt.Add(time.Duration(index) * 100 * time.Millisecond),
			Direction:     "inbound",
			Stage:         "decoded",
			OpcodeHex:     "0x7034",
			CorrelationID: "gateway",
			Module:        "AgentServer",
		})
	}
	analysis := Analyze([]Trace{trace}, DefaultAnalysisOptions())
	if len(analysis.Findings) != 1 || analysis.Findings[0].Kind != "packet_loop" {
		t.Fatalf("findings = %+v", analysis.Findings)
	}
}

func TestAnalyzeCleanTraceReportsNoAnomaly(t *testing.T) {
	analysis := Analyze([]Trace{{Events: []Event{{Stage: "dispatch", Outcome: "handled", DurationUS: 10}}}}, DefaultAnalysisOptions())
	if analysis.Status != StatusNoAnomaly || analysis.ProbableFailure != nil || len(analysis.Findings) != 0 {
		t.Fatalf("analysis = %+v", analysis)
	}
}
