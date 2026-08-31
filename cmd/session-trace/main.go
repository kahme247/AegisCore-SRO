package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"

	traceanalysis "github.com/Harvi8/AegisCore/framework/diagnostics/sessiontrace"
)

type traceEvent = traceanalysis.Event
type traceFile = traceanalysis.Trace

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(arguments []string, stdout, stderr io.Writer) int {
	if len(arguments) > 0 {
		switch arguments[0] {
		case "timeline":
			return runTimeline(arguments[1:], stdout, stderr)
		case "analyze":
			return runAnalyze(arguments[1:], stdout, stderr)
		case "diff":
			return runDiff(arguments[1:], stdout, stderr)
		case "help", "-h", "--help":
			printUsage(stdout)
			return 0
		}
	}
	return runTimeline(arguments, stdout, stderr)
}

func printUsage(output io.Writer) {
	fmt.Fprintln(output, "Usage:")
	fmt.Fprintln(output, "  session-trace [timeline] [options] [trace paths...]")
	fmt.Fprintln(output, "  session-trace analyze [options] [trace paths...]")
	fmt.Fprintln(output, "  session-trace diff [options] <healthy path> <failed path>")
}

func runTimeline(arguments []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("session-trace", flag.ContinueOnError)
	flags.SetOutput(stderr)
	errorsOnly := flags.Bool("errors-only", false, "show only rejected, error, and unhandled events")
	sessionFilter := flags.String("session", "", "show sessions whose ID contains this value")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	paths := flags.Args()
	if len(paths) == 0 {
		paths = []string{filepath.Join(".local", "session-traces")}
	}
	traces, err := loadTraces(paths)
	if err != nil {
		fmt.Fprintf(stderr, "session-trace: %v\n", err)
		return 1
	}
	filteredTraces := traces[:0]
	for _, trace := range traces {
		if *sessionFilter != "" && !strings.Contains(trace.Header.Session.SessionID, *sessionFilter) && !strings.Contains(trace.Header.Session.CorrelationID, *sessionFilter) {
			continue
		}
		filteredTraces = append(filteredTraces, trace)
	}
	traces = filteredTraces

	var events []traceEvent
	for _, trace := range traces {
		for _, event := range trace.Events {
			if *errorsOnly && event.Outcome != "error" && event.Outcome != "rejected" && event.Outcome != "unhandled" {
				continue
			}
			events = append(events, event)
		}
	}
	sort.SliceStable(events, func(left, right int) bool {
		if events[left].Timestamp.Equal(events[right].Timestamp) {
			return events[left].Sequence < events[right].Sequence
		}
		return events[left].Timestamp.Before(events[right].Timestamp)
	})

	writer := tabwriter.NewWriter(stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(writer, "TIME\tMODULE\tTRACE\tSESSION\tDIR\tSTAGE\tOPCODE\tSIZE\tDURATION\tOUTCOME\tHANDLER / ERROR")
	for _, event := range events {
		details := shortHandlerName(event.Handler)
		if event.Error != "" {
			if details != "" {
				details += ": "
			}
			details += event.Error
		}
		duration := ""
		if event.DurationUS != 0 {
			duration = fmt.Sprintf("%d us", event.DurationUS)
		}
		fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%d\t%s\t%s\t%s\n",
			event.Timestamp.Local().Format("2006-01-02 15:04:05.000"),
			event.Module,
			shortSessionID(event.CorrelationID),
			shortSessionID(event.SessionID),
			event.Direction,
			event.Stage,
			event.OpcodeHex,
			event.PayloadSize,
			duration,
			event.Outcome,
			details,
		)
	}
	if err := writer.Flush(); err != nil {
		fmt.Fprintf(stderr, "session-trace: write output: %v\n", err)
		return 1
	}

	var dropped uint64
	for _, trace := range traces {
		dropped += trace.Summary.Dropped
	}
	fmt.Fprintf(stdout, "\n%d session(s), %d event(s), %d overwritten event(s)\n", len(traces), len(events), dropped)
	return 0
}

func loadTraces(paths []string) ([]traceFile, error) {
	files, err := collectTraceFiles(paths)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, errors.New("no .jsonl session traces found")
	}
	traces := make([]traceFile, 0, len(files))
	for _, path := range files {
		trace, err := readTraceFile(path)
		if err != nil {
			return nil, err
		}
		traces = append(traces, trace)
	}
	return traces, nil
}

func collectTraceFiles(paths []string) ([]string, error) {
	seen := make(map[string]struct{})
	var files []string
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			return nil, fmt.Errorf("inspect %q: %w", path, err)
		}
		if !info.IsDir() {
			if strings.EqualFold(filepath.Ext(path), ".jsonl") {
				absolute, err := filepath.Abs(path)
				if err != nil {
					return nil, err
				}
				if _, exists := seen[absolute]; !exists {
					seen[absolute] = struct{}{}
					files = append(files, absolute)
				}
			}
			continue
		}
		err = filepath.WalkDir(path, func(entryPath string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() || !strings.EqualFold(filepath.Ext(entryPath), ".jsonl") {
				return nil
			}
			absolute, err := filepath.Abs(entryPath)
			if err != nil {
				return err
			}
			if _, exists := seen[absolute]; !exists {
				seen[absolute] = struct{}{}
				files = append(files, absolute)
			}
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("scan %q: %w", path, err)
		}
	}
	sort.Strings(files)
	return files, nil
}

func readTraceFile(path string) (traceFile, error) {
	return traceanalysis.ReadFile(path)
}

func shortSessionID(value string) string {
	if len(value) <= 8 {
		return value
	}
	return value[:8]
}

func shortHandlerName(value string) string {
	return traceanalysis.ShortHandlerName(value)
}
