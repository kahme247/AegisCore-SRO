package main

import (
	"flag"
	"fmt"
	"io"
	"sort"
	"strings"
)

type eventSignature struct {
	Module      string
	Direction   string
	Stage       string
	OpcodeHex   string
	PayloadSize int
	Handler     string
	Outcome     string
}

func runDiff(arguments []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("session-trace diff", flag.ContinueOnError)
	flags.SetOutput(stderr)
	contextLines := flags.Int("context", 3, "events to show before and after the first divergence")
	check := flags.Bool("check", false, "return a nonzero status when traces diverge")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if *contextLines < 0 || *contextLines > 20 {
		fmt.Fprintln(stderr, "session-trace diff: -context must be between 0 and 20")
		return 2
	}
	paths := flags.Args()
	if len(paths) != 2 {
		fmt.Fprintln(stderr, "session-trace diff: expected <healthy path> <failed path>")
		return 2
	}
	healthyTraces, err := loadTraces([]string{paths[0]})
	if err != nil {
		fmt.Fprintf(stderr, "session-trace diff: load healthy trace: %v\n", err)
		return 1
	}
	failedTraces, err := loadTraces([]string{paths[1]})
	if err != nil {
		fmt.Fprintf(stderr, "session-trace diff: load failed trace: %v\n", err)
		return 1
	}
	healthy := orderedTraceEvents(healthyTraces)
	failed := orderedTraceEvents(failedTraces)
	index := firstDivergence(healthy, failed)
	if index == len(healthy) && index == len(failed) {
		fmt.Fprintf(stdout, "No protocol divergence: %d normalized event(s) match.\n", len(healthy))
		return 0
	}

	kind := classifyDivergence(healthy, failed, index)
	fmt.Fprintf(stdout, "First divergence at normalized event %d: %s\n\n", index+1, kind)
	if index < len(healthy) {
		fmt.Fprintln(stdout, "Expected from healthy trace:")
		fmt.Fprintf(stdout, "  %s\n", formatCompactEvent(healthy[index]))
	} else {
		fmt.Fprintln(stdout, "Expected from healthy trace:")
		fmt.Fprintln(stdout, "  <end of trace>")
	}
	if index < len(failed) {
		fmt.Fprintln(stdout, "Observed in failed trace:")
		fmt.Fprintf(stdout, "  %s\n", formatCompactEvent(failed[index]))
	} else {
		fmt.Fprintln(stdout, "Observed in failed trace:")
		fmt.Fprintln(stdout, "  <end of trace>")
	}

	printDiffContext(stdout, "Healthy context", healthy, index, *contextLines)
	printDiffContext(stdout, "Failed context", failed, index, *contextLines)
	if *check {
		return 3
	}
	return 0
}

func orderedTraceEvents(traces []traceFile) []traceEvent {
	var events []traceEvent
	for _, trace := range traces {
		events = append(events, trace.Events...)
	}
	sort.SliceStable(events, func(left, right int) bool {
		if events[left].Timestamp.Equal(events[right].Timestamp) {
			if events[left].Module == events[right].Module {
				return events[left].Sequence < events[right].Sequence
			}
			return events[left].Module < events[right].Module
		}
		return events[left].Timestamp.Before(events[right].Timestamp)
	})
	return events
}

func firstDivergence(healthy, failed []traceEvent) int {
	limit := min(len(healthy), len(failed))
	for index := 0; index < limit; index++ {
		if signatureOf(healthy[index]) != signatureOf(failed[index]) {
			return index
		}
	}
	return limit
}

func classifyDivergence(healthy, failed []traceEvent, index int) string {
	if index >= len(healthy) {
		return "failed trace contains additional events"
	}
	if index >= len(failed) {
		return "failed trace ended early"
	}
	healthySignature := signatureOf(healthy[index])
	failedSignature := signatureOf(failed[index])
	if index+1 < len(healthy) && signatureOf(healthy[index+1]) == failedSignature {
		return "failed trace is missing an expected event"
	}
	if index+1 < len(failed) && signatureOf(failed[index+1]) == healthySignature {
		return "failed trace contains an unexpected event"
	}
	return "event behavior changed"
}

func signatureOf(event traceEvent) eventSignature {
	return eventSignature{
		Module:      event.Module,
		Direction:   event.Direction,
		Stage:       event.Stage,
		OpcodeHex:   event.OpcodeHex,
		PayloadSize: event.PayloadSize,
		Handler:     shortHandlerName(event.Handler),
		Outcome:     event.Outcome,
	}
}

func formatCompactEvent(event traceEvent) string {
	parts := []string{event.Module, event.Direction, event.Stage}
	if event.OpcodeHex != "" {
		parts = append(parts, event.OpcodeHex)
	}
	parts = append(parts, fmt.Sprintf("size=%d", event.PayloadSize))
	if event.Handler != "" {
		parts = append(parts, "handler="+shortHandlerName(event.Handler))
	}
	if event.Outcome != "" {
		parts = append(parts, "outcome="+event.Outcome)
	}
	if event.Error != "" {
		parts = append(parts, "error="+event.Error)
	}
	return strings.Join(parts, " ")
}

func printDiffContext(output io.Writer, title string, events []traceEvent, divergence, contextLines int) {
	fmt.Fprintf(output, "\n%s:\n", title)
	start := max(0, divergence-contextLines)
	end := min(len(events), divergence+contextLines+1)
	if start == end {
		fmt.Fprintln(output, "  <no events>")
		return
	}
	for index := start; index < end; index++ {
		marker := " "
		if index == divergence {
			marker = ">"
		}
		fmt.Fprintf(output, "%s %4d  %s\n", marker, index+1, formatCompactEvent(events[index]))
	}
}
