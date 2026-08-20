package main

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestJSONLAuditRecorderPersistsOneEventPerLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit", "events.jsonl")
	recorder, err := OpenJSONLAuditRecorder(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := recorder.Record(AuditEvent{Action: "operator.login", Outcome: "success", Operator: "admin"}); err != nil {
		t.Fatal(err)
	}
	if err := recorder.Close(); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	if !scanner.Scan() {
		t.Fatal("missing audit line")
	}
	var event AuditEvent
	if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
		t.Fatal(err)
	}
	if event.Action != "operator.login" || event.Operator != "admin" || event.Timestamp.IsZero() {
		t.Fatalf("event = %#v", event)
	}
	if scanner.Scan() {
		t.Fatal("unexpected second audit line")
	}
}

func TestJSONLAuditRecorderReturnsBoundedNewestEvents(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	recorder, err := OpenJSONLAuditRecorder(path)
	if err != nil {
		t.Fatal(err)
	}
	defer recorder.Close()
	for _, event := range []AuditEvent{
		{Action: "operator.login", Outcome: "success"},
		{Action: "world.broadcast", Outcome: "attempt"},
		{Action: "world.broadcast", Outcome: "success"},
	} {
		if err := recorder.Record(event); err != nil {
			t.Fatal(err)
		}
	}
	events, err := recorder.Recent(context.Background(), AuditQuery{Action: "world.broadcast", Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Action != "world.broadcast" || events[0].Outcome != "success" {
		t.Fatalf("events = %#v", events)
	}
}
