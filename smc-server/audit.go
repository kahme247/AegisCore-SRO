package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type AuditEvent struct {
	Timestamp  time.Time      `json:"timestamp"`
	Action     string         `json:"action"`
	Outcome    string         `json:"outcome"`
	Operator   string         `json:"operator,omitempty"`
	RemoteAddr string         `json:"remoteAddr,omitempty"`
	Target     string         `json:"target,omitempty"`
	Details    map[string]any `json:"details,omitempty"`
}

type AuditRecorder interface {
	Record(AuditEvent) error
	Recent(context.Context, AuditQuery) ([]AuditEvent, error)
}

type AuditQuery struct {
	Action string
	Limit  int
}

type JSONLAuditRecorder struct {
	mu   sync.Mutex
	file *os.File
	path string
}

func OpenJSONLAuditRecorder(path string) (*JSONLAuditRecorder, error) {
	directory := filepath.Dir(path)
	if directory != "." {
		if err := os.MkdirAll(directory, 0o750); err != nil {
			return nil, fmt.Errorf("create SMC audit directory: %w", err)
		}
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open SMC audit log: %w", err)
	}
	return &JSONLAuditRecorder{file: file, path: path}, nil
}

func (r *JSONLAuditRecorder) Recent(ctx context.Context, query AuditQuery) ([]AuditEvent, error) {
	if r == nil || r.file == nil || r.path == "" {
		return nil, fmt.Errorf("SMC audit recorder is unavailable")
	}
	if query.Limit < 1 || query.Limit > 200 {
		return nil, fmt.Errorf("audit limit must be between 1 and 200")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	file, err := os.Open(r.path)
	if err != nil {
		return nil, fmt.Errorf("open SMC audit log for reading: %w", err)
	}
	defer file.Close()
	const readWindow int64 = 2 << 20
	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat SMC audit log: %w", err)
	}
	start := info.Size() - readWindow
	if start < 0 {
		start = 0
	}
	if _, err := file.Seek(start, io.SeekStart); err != nil {
		return nil, fmt.Errorf("seek SMC audit log: %w", err)
	}
	reader := bufio.NewReader(file)
	if start > 0 {
		if _, err := reader.ReadString('\n'); err != nil && err != io.EOF {
			return nil, fmt.Errorf("align SMC audit log read window: %w", err)
		}
	}
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64<<10), 256<<10)
	events := make([]AuditEvent, 0, query.Limit)
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var event AuditEvent
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			return nil, fmt.Errorf("decode SMC audit event: %w", err)
		}
		if query.Action != "" && event.Action != query.Action {
			continue
		}
		if len(events) == query.Limit {
			copy(events, events[1:])
			events[len(events)-1] = event
		} else {
			events = append(events, event)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan SMC audit log: %w", err)
	}
	for left, right := 0, len(events)-1; left < right; left, right = left+1, right-1 {
		events[left], events[right] = events[right], events[left]
	}
	return events, nil
}

func (r *JSONLAuditRecorder) Record(event AuditEvent) error {
	if r == nil || r.file == nil {
		return fmt.Errorf("SMC audit recorder is unavailable")
	}
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	}
	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("encode SMC audit event: %w", err)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, err := r.file.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("append SMC audit event: %w", err)
	}
	if err := r.file.Sync(); err != nil {
		return fmt.Errorf("sync SMC audit event: %w", err)
	}
	return nil
}

func (r *JSONLAuditRecorder) Close() error {
	if r == nil || r.file == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	err := r.file.Close()
	r.file = nil
	return err
}
