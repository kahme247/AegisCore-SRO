package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/pprof"
	"runtime/metrics"
	"strconv"
	"time"

	log "github.com/sirupsen/logrus"
)

type agentDiagnostics struct {
	server   *http.Server
	listener net.Listener
}

func startAgentDiagnostics(enabled bool, port int) (*agentDiagnostics, error) {
	if !enabled {
		return nil, nil
	}
	if port < 0 || port > 65535 {
		return nil, fmt.Errorf("diagnostics port %d is outside 0..65535", port)
	}

	listener, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		return nil, fmt.Errorf("bind Agent diagnostics listener: %w", err)
	}

	mux := http.NewServeMux()
	registerPprofHandlers(mux)
	mux.HandleFunc("GET /debug/runtime/metrics", runtimeMetricsHandler)
	server := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       30 * time.Second,
	}
	diagnostics := &agentDiagnostics{server: server, listener: listener}
	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) && !errors.Is(err, net.ErrClosed) {
			log.WithError(err).Error("Agent diagnostics listener stopped unexpectedly")
		}
	}()
	log.Infof("Agent diagnostics listening on http://%s", listener.Addr())
	return diagnostics, nil
}

func registerPprofHandlers(mux *http.ServeMux) {
	mux.HandleFunc("GET /debug/pprof/", pprof.Index)
	mux.HandleFunc("GET /debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("GET /debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("GET /debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("POST /debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("GET /debug/pprof/trace", pprof.Trace)
	for _, name := range []string{"allocs", "block", "goroutine", "heap", "mutex", "threadcreate"} {
		mux.Handle("GET /debug/pprof/"+name, pprof.Handler(name))
	}
}

func runtimeMetricsHandler(response http.ResponseWriter, _ *http.Request) {
	descriptions := metrics.All()
	samples := make([]metrics.Sample, len(descriptions))
	for i := range descriptions {
		samples[i].Name = descriptions[i].Name
	}
	metrics.Read(samples)

	values := make(map[string]any, len(samples))
	for _, sample := range samples {
		switch sample.Value.Kind() {
		case metrics.KindUint64:
			values[sample.Name] = sample.Value.Uint64()
		case metrics.KindFloat64:
			values[sample.Name] = sample.Value.Float64()
		}
	}

	response.Header().Set("Cache-Control", "no-store")
	response.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(response).Encode(values); err != nil {
		log.WithError(err).Debug("encode Agent runtime metrics")
	}
}

func (d *agentDiagnostics) Close(ctx context.Context) error {
	if d == nil || d.server == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return d.server.Shutdown(ctx)
}

func (d *agentDiagnostics) Addr() net.Addr {
	if d == nil || d.listener == nil {
		return nil
	}
	return d.listener.Addr()
}
