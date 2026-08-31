package main

import (
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	traceanalysis "github.com/Harvi8/AegisCore/framework/diagnostics/sessiontrace"
)

type traceAnomaly = traceanalysis.Finding

func runAnalyze(arguments []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("session-trace analyze", flag.ContinueOnError)
	flags.SetOutput(stderr)
	slowThreshold := flags.Duration("slow", 100*time.Millisecond, "report dispatches at or above this duration")
	repeatCount := flags.Int("repeat", 5, "report this many repeated inbound packets within one second")
	sessionFilter := flags.String("session", "", "analyze sessions whose ID or trace ID contains this value")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if *slowThreshold <= 0 {
		fmt.Fprintln(stderr, "session-trace analyze: -slow must be positive")
		return 2
	}
	if *repeatCount < 2 {
		fmt.Fprintln(stderr, "session-trace analyze: -repeat must be at least 2")
		return 2
	}
	paths := flags.Args()
	if len(paths) == 0 {
		paths = []string{filepath.Join(".local", "session-traces")}
	}
	traces, err := loadTraces(paths)
	if err != nil {
		fmt.Fprintf(stderr, "session-trace analyze: %v\n", err)
		return 1
	}
	if *sessionFilter != "" {
		filtered := traces[:0]
		for _, trace := range traces {
			if strings.Contains(trace.Header.Session.SessionID, *sessionFilter) || strings.Contains(trace.Header.Session.CorrelationID, *sessionFilter) {
				filtered = append(filtered, trace)
			}
		}
		traces = filtered
	}

	options := traceanalysis.DefaultAnalysisOptions()
	options.SlowThreshold = *slowThreshold
	options.RepeatCount = *repeatCount
	analysis := traceanalysis.Analyze(traces, options)
	anomalies := analysis.Findings
	if len(anomalies) == 0 {
		fmt.Fprintf(stdout, "No anomalies found in %d session(s).\n", len(traces))
		return 0
	}

	probable := anomalies[0]
	fmt.Fprintln(stdout, "Probable failure:")
	printAnomalySummary(stdout, probable)
	fmt.Fprintln(stdout)

	writer := tabwriter.NewWriter(stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(writer, "SEVERITY\tTIME\tMODULE\tTRACE\tOPCODE\tKIND\tDETAIL")
	for _, anomaly := range anomalies {
		timestamp := "-"
		if !anomaly.Event.Timestamp.IsZero() {
			timestamp = anomaly.Event.Timestamp.Local().Format("15:04:05.000")
		}
		fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			anomaly.Severity,
			timestamp,
			anomaly.Event.Module,
			shortSessionID(anomaly.Event.CorrelationID),
			anomaly.Event.OpcodeHex,
			anomaly.Kind,
			anomaly.Message,
		)
	}
	if err := writer.Flush(); err != nil {
		fmt.Fprintf(stderr, "session-trace analyze: write output: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "\n%d anomaly/anomalies across %d session(s).\n", len(anomalies), len(traces))
	return 0
}

func printAnomalySummary(output io.Writer, anomaly traceAnomaly) {
	fmt.Fprintf(output, "  severity: %s\n", anomaly.Severity)
	if anomaly.Event.Module != "" {
		fmt.Fprintf(output, "  service: %s\n", anomaly.Event.Module)
	}
	if anomaly.Event.CorrelationID != "" {
		fmt.Fprintf(output, "  trace: %s\n", anomaly.Event.CorrelationID)
	}
	if anomaly.Event.OpcodeHex != "" {
		fmt.Fprintf(output, "  opcode: %s\n", anomaly.Event.OpcodeHex)
	}
	if anomaly.Event.Handler != "" {
		fmt.Fprintf(output, "  handler: %s\n", shortHandlerName(anomaly.Event.Handler))
	}
	fmt.Fprintf(output, "  reason: %s\n", anomaly.Message)
}
