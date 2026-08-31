package sessiontrace

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

const (
	StatusNoAnomaly = "no_anomaly_detected"
	StatusAttention = "attention_required"
)

type AnalysisOptions struct {
	SlowThreshold time.Duration `json:"slow_threshold"`
	RepeatCount   int           `json:"repeat_count"`
	RepeatWindow  time.Duration `json:"repeat_window"`
}

func DefaultAnalysisOptions() AnalysisOptions {
	return AnalysisOptions{SlowThreshold: 100 * time.Millisecond, RepeatCount: 5, RepeatWindow: time.Second}
}

type Finding struct {
	Severity string `json:"severity"`
	Kind     string `json:"kind"`
	Message  string `json:"message"`
	Score    int    `json:"score"`
	Event    Event  `json:"event"`
}

type Analysis struct {
	Version         int             `json:"version"`
	GeneratedAt     time.Time       `json:"generated_at"`
	Status          string          `json:"status"`
	Options         AnalysisOptions `json:"options"`
	SessionCount    int             `json:"session_count"`
	EventCount      int             `json:"event_count"`
	ProbableFailure *Finding        `json:"probable_failure,omitempty"`
	Findings        []Finding       `json:"findings"`
}

func Analyze(traces []Trace, options AnalysisOptions) Analysis {
	options = normalizeOptions(options)
	analysis := Analysis{
		Version:      1,
		GeneratedAt:  time.Now().UTC(),
		Status:       StatusNoAnomaly,
		Options:      options,
		SessionCount: len(traces),
	}
	var decoded []Event
	for _, trace := range traces {
		analysis.EventCount += len(trace.Events)
		if trace.Summary.Dropped > 0 {
			analysis.Findings = append(analysis.Findings, Finding{
				Severity: "warning",
				Kind:     "trace_overwrite",
				Message:  fmt.Sprintf("%d older event(s) were overwritten; evidence is incomplete", trace.Summary.Dropped),
				Score:    40,
				Event: Event{
					Module:        trace.Header.Session.Module,
					SessionID:     trace.Header.Session.SessionID,
					CorrelationID: trace.Header.Session.CorrelationID,
					Source:        trace.Path,
				},
			})
		}
		for _, event := range trace.Events {
			switch event.Outcome {
			case "error":
				message := event.Error
				if message == "" {
					message = "operation returned an error"
				}
				analysis.Findings = append(analysis.Findings, Finding{Severity: "critical", Kind: "operation_error", Message: message, Score: 100, Event: event})
			case "rejected":
				message := event.Error
				if message == "" {
					message = "packet was rejected"
				}
				analysis.Findings = append(analysis.Findings, Finding{Severity: "high", Kind: "packet_rejected", Message: message, Score: 90, Event: event})
			case "unhandled":
				if event.Stage == "dispatch" {
					analysis.Findings = append(analysis.Findings, Finding{Severity: "high", Kind: "unhandled_opcode", Message: "no registered handler accepted this opcode", Score: 80, Event: event})
				}
			}
			if event.Stage == "dispatch" && event.DurationUS >= options.SlowThreshold.Microseconds() {
				analysis.Findings = append(analysis.Findings, Finding{
					Severity: "warning",
					Kind:     "slow_handler",
					Message:  fmt.Sprintf("%s took %s", ShortHandlerName(event.Handler), time.Duration(event.DurationUS)*time.Microsecond),
					Score:    60,
					Event:    event,
				})
			}
			if event.Direction == "inbound" && event.Stage == "decoded" {
				decoded = append(decoded, event)
			}
		}
	}
	analysis.Findings = append(analysis.Findings, repeatedPacketFindings(decoded, options.RepeatCount, options.RepeatWindow)...)
	sort.SliceStable(analysis.Findings, func(left, right int) bool {
		if analysis.Findings[left].Score != analysis.Findings[right].Score {
			return analysis.Findings[left].Score > analysis.Findings[right].Score
		}
		return analysis.Findings[left].Event.Timestamp.After(analysis.Findings[right].Event.Timestamp)
	})
	if len(analysis.Findings) > 0 {
		analysis.Status = StatusAttention
		probable := analysis.Findings[0]
		analysis.ProbableFailure = &probable
	}
	return analysis
}

func normalizeOptions(options AnalysisOptions) AnalysisOptions {
	defaults := DefaultAnalysisOptions()
	if options.SlowThreshold <= 0 {
		options.SlowThreshold = defaults.SlowThreshold
	}
	if options.RepeatCount < 2 {
		options.RepeatCount = defaults.RepeatCount
	}
	if options.RepeatWindow <= 0 {
		options.RepeatWindow = defaults.RepeatWindow
	}
	return options
}

func repeatedPacketFindings(events []Event, threshold int, window time.Duration) []Finding {
	sort.SliceStable(events, func(left, right int) bool { return events[left].Timestamp.Before(events[right].Timestamp) })
	type burst struct {
		first    time.Time
		count    int
		reported bool
	}
	bursts := make(map[string]burst)
	var findings []Finding
	for _, event := range events {
		key := event.CorrelationID + "\x00" + event.Module + "\x00" + event.OpcodeHex
		current := bursts[key]
		if current.count == 0 || event.Timestamp.Sub(current.first) > window {
			current = burst{first: event.Timestamp, count: 1}
		} else {
			current.count++
		}
		if current.count >= threshold && !current.reported {
			current.reported = true
			findings = append(findings, Finding{
				Severity: "warning",
				Kind:     "packet_loop",
				Message:  fmt.Sprintf("opcode repeated at least %d times within %s", threshold, window),
				Score:    50,
				Event:    event,
			})
		}
		bursts[key] = current
	}
	return findings
}

func ShortHandlerName(value string) string {
	if index := strings.LastIndexByte(value, '/'); index >= 0 {
		value = value[index+1:]
	}
	return strings.TrimSuffix(value, "-fm")
}
