package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testTrace = `{"type":"session","version":1,"started_at":"2026-07-21T10:00:00Z","session":{"session_id":"12345678-abcd","correlation_id":"trace-correlation","module":"GatewayServer","remote":"127.0.0.1:1234"},"capacity":256}
{"type":"event","sequence":1,"timestamp":"2026-07-21T10:00:00.001Z","elapsed_ms":1,"direction":"inbound","stage":"dispatch","opcode":24834,"opcode_hex":"0x6102","payload_size":20,"authenticated":false,"handler":"github.com/Harvi8/AegisCore/gateway-server/handler.(*LoginRequestHandler).Handle-fm","duration_us":42,"outcome":"error","error":"invalid login"}
{"type":"summary","ended_at":"2026-07-21T10:00:01Z","recorded":1,"dropped":2,"last_sequence":3}
`

func TestReadTraceFileAttachesSessionMetadata(t *testing.T) {
	path := writeTestTrace(t, "gateway.jsonl")
	trace, err := readTraceFile(path)
	if err != nil {
		t.Fatalf("readTraceFile() error = %v", err)
	}
	if trace.Header.Session.SessionID != "12345678-abcd" || len(trace.Events) != 1 || trace.Events[0].CorrelationID != "trace-correlation" || trace.Events[0].Module != "GatewayServer" || trace.Summary.Dropped != 2 {
		t.Fatalf("trace = %+v", trace)
	}
}

func TestRunPrintsMergedReadableTimeline(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "gateway.jsonl")
	if err := os.WriteFile(path, []byte(testTrace), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if exitCode := run([]string{"-errors-only", directory}, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("run() exit = %d, stderr = %s", exitCode, stderr.String())
	}
	output := stdout.String()
	for _, expected := range []string{"GatewayServer", "trace-co", "12345678", "0x6102", "42 us", "invalid login", "1 session(s), 1 event(s), 2 overwritten event(s)"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("output missing %q:\n%s", expected, output)
		}
	}
}

func TestRunAnalyzeRanksOperationErrorAsProbableFailure(t *testing.T) {
	path := writeTestTrace(t, "failed.jsonl")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if exitCode := run([]string{"analyze", "-slow", "1us", path}, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("run(analyze) exit = %d, stderr = %s", exitCode, stderr.String())
	}
	output := stdout.String()
	for _, expected := range []string{"Probable failure:", "severity: critical", "opcode: 0x6102", "invalid login", "operation_error", "slow_handler", "trace_overwrite"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("analyze output missing %q:\n%s", expected, output)
		}
	}
}

func TestRunDiffReportsFirstBehaviorChange(t *testing.T) {
	healthy := strings.Replace(testTrace, `"outcome":"error","error":"invalid login"`, `"outcome":"handled"`, 1)
	directory := t.TempDir()
	healthyPath := filepath.Join(directory, "healthy.jsonl")
	failedPath := filepath.Join(directory, "failed.jsonl")
	if err := os.WriteFile(healthyPath, []byte(healthy), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(failedPath, []byte(testTrace), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if exitCode := run([]string{"diff", healthyPath, failedPath}, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("run(diff) exit = %d, stderr = %s", exitCode, stderr.String())
	}
	output := stdout.String()
	for _, expected := range []string{"First divergence at normalized event 1", "event behavior changed", "Expected from healthy trace:", "outcome=handled", "Observed in failed trace:", "outcome=error"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("diff output missing %q:\n%s", expected, output)
		}
	}
}

func TestRunDiffReportsMatchingTraces(t *testing.T) {
	path := writeTestTrace(t, "same.jsonl")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if exitCode := run([]string{"diff", path, path}, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("run(diff same) exit = %d, stderr = %s", exitCode, stderr.String())
	}
	if !strings.Contains(stdout.String(), "No protocol divergence: 1 normalized event(s) match.") {
		t.Fatalf("unexpected matching diff output:\n%s", stdout.String())
	}
}

func TestCollectTraceFilesRecursesAndDeduplicates(t *testing.T) {
	directory := t.TempDir()
	nested := filepath.Join(directory, "agent")
	if err := os.MkdirAll(nested, 0o750); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(nested, "agent.jsonl")
	if err := os.WriteFile(path, []byte(testTrace), 0o600); err != nil {
		t.Fatal(err)
	}
	files, err := collectTraceFiles([]string{directory, path})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatalf("files = %v, want one deduplicated trace", files)
	}
}

func writeTestTrace(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(testTrace), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
