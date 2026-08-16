package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	downloadconfig "github.com/Harvi8/AegisCore/download-server/config"
)

func TestReadinessTracksServiceLifecycle(t *testing.T) {
	downloadconfig.Initialize()
	download, err := newDownloadServer(nil)
	if err != nil {
		t.Fatal(err)
	}

	assertStatus := func(want int) {
		t.Helper()
		request := httptest.NewRequest(http.MethodGet, "/readyz", nil)
		response := httptest.NewRecorder()
		download.healthServer.Handler.ServeHTTP(response, request)
		if response.Code != want {
			t.Fatalf("readiness status = %d, want %d", response.Code, want)
		}
	}

	assertStatus(http.StatusServiceUnavailable)
	download.ready.Store(true)
	assertStatus(http.StatusOK)
	if err := download.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
	assertStatus(http.StatusServiceUnavailable)
}
