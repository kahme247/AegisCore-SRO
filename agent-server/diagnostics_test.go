package main

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestStartAgentDiagnosticsDisabled(t *testing.T) {
	diagnostics, err := startAgentDiagnostics(false, 6060)
	if err != nil {
		t.Fatalf("startAgentDiagnostics() error = %v", err)
	}
	if diagnostics != nil {
		t.Fatal("disabled diagnostics returned a server")
	}
}

func TestAgentDiagnosticsServesLoopbackRuntimeMetricsAndPprof(t *testing.T) {
	diagnostics, err := startAgentDiagnostics(true, 0)
	if err != nil {
		t.Fatalf("startAgentDiagnostics() error = %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := diagnostics.Close(ctx); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})

	tcpAddress, ok := diagnostics.Addr().(*net.TCPAddr)
	if !ok || !tcpAddress.IP.IsLoopback() {
		t.Fatalf("diagnostics address = %v, want TCP loopback", diagnostics.Addr())
	}
	baseURL := "http://" + diagnostics.Addr().String()

	response, err := http.Get(baseURL + "/debug/runtime/metrics")
	if err != nil {
		t.Fatalf("GET runtime metrics: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("runtime metrics status = %d, want 200", response.StatusCode)
	}
	var values map[string]any
	if err := json.NewDecoder(response.Body).Decode(&values); err != nil {
		t.Fatalf("decode runtime metrics: %v", err)
	}
	if _, exists := values["/sched/goroutines:goroutines"]; !exists {
		t.Fatal("runtime metrics omitted goroutine count")
	}

	response, err = http.Get(baseURL + "/debug/pprof/heap?debug=1")
	if err != nil {
		t.Fatalf("GET heap profile: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("heap profile status = %d, want 200", response.StatusCode)
	}
}

func TestStartAgentDiagnosticsRejectsInvalidPort(t *testing.T) {
	if _, err := startAgentDiagnostics(true, 65536); err == nil {
		t.Fatal("startAgentDiagnostics() accepted an invalid port")
	}
}
