package sessiontrace

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type ReportPaths struct {
	JSON     string
	Markdown string
}

func AnalyzeFile(path string, options AnalysisOptions) (Analysis, error) {
	trace, err := ReadFile(path)
	if err != nil {
		return Analysis{}, err
	}
	return Analyze([]Trace{trace}, options), nil
}

func WriteReports(tracePath string, analysis Analysis) (ReportPaths, error) {
	base := strings.TrimSuffix(tracePath, filepath.Ext(tracePath))
	paths := ReportPaths{JSON: base + ".analysis.json", Markdown: base + ".analysis.md"}
	jsonBytes, err := json.MarshalIndent(analysis, "", "  ")
	if err != nil {
		return ReportPaths{}, fmt.Errorf("encode JSON analysis: %w", err)
	}
	jsonBytes = append(jsonBytes, '\n')
	markdown := renderMarkdown(analysis)
	if err := writeFileAtomically(paths.JSON, jsonBytes); err != nil {
		return ReportPaths{}, err
	}
	if err := writeFileAtomically(paths.Markdown, []byte(markdown)); err != nil {
		return paths, err
	}
	return paths, nil
}

func renderMarkdown(analysis Analysis) string {
	var output strings.Builder
	output.WriteString("# Session trace analysis\n\n")
	fmt.Fprintf(&output, "- Status: `%s`\n", analysis.Status)
	fmt.Fprintf(&output, "- Sessions: %d\n", analysis.SessionCount)
	fmt.Fprintf(&output, "- Events: %d\n", analysis.EventCount)
	fmt.Fprintf(&output, "- Findings: %d\n", len(analysis.Findings))
	if analysis.ProbableFailure != nil {
		finding := analysis.ProbableFailure
		output.WriteString("\n## Probable failure\n\n")
		fmt.Fprintf(&output, "- Severity: `%s`\n", finding.Severity)
		if finding.Event.Module != "" {
			fmt.Fprintf(&output, "- Service: `%s`\n", markdownText(finding.Event.Module))
		}
		if finding.Event.CorrelationID != "" {
			fmt.Fprintf(&output, "- Trace: `%s`\n", markdownText(finding.Event.CorrelationID))
		}
		if finding.Event.OpcodeHex != "" {
			fmt.Fprintf(&output, "- Opcode: `%s`\n", markdownText(finding.Event.OpcodeHex))
		}
		if finding.Event.Handler != "" {
			fmt.Fprintf(&output, "- Handler: `%s`\n", markdownText(ShortHandlerName(finding.Event.Handler)))
		}
		fmt.Fprintf(&output, "- Reason: %s\n", markdownText(finding.Message))
	}
	if len(analysis.Findings) > 0 {
		output.WriteString("\n## Findings\n\n")
		output.WriteString("| Severity | Kind | Service | Opcode | Detail |\n")
		output.WriteString("| --- | --- | --- | --- | --- |\n")
		for _, finding := range analysis.Findings {
			fmt.Fprintf(&output, "| %s | %s | %s | %s | %s |\n",
				markdownText(finding.Severity),
				markdownText(finding.Kind),
				markdownText(finding.Event.Module),
				markdownText(finding.Event.OpcodeHex),
				markdownText(finding.Message),
			)
		}
	}
	return output.String()
}

func markdownText(value string) string {
	value = strings.ReplaceAll(value, "\r", " ")
	value = strings.ReplaceAll(value, "\n", " ")
	value = strings.ReplaceAll(value, "|", "\\|")
	value = strings.ReplaceAll(value, "`", "'")
	return value
}

func writeFileAtomically(path string, content []byte) error {
	directory := filepath.Dir(path)
	temporary, err := os.CreateTemp(directory, ".session-analysis-*")
	if err != nil {
		return fmt.Errorf("create analysis file: %w", err)
	}
	temporaryPath := temporary.Name()
	closed := false
	defer func() {
		if !closed {
			_ = temporary.Close()
		}
		_ = os.Remove(temporaryPath)
	}()
	if err := temporary.Chmod(0o600); err != nil {
		return fmt.Errorf("protect analysis file: %w", err)
	}
	_, writeErr := temporary.Write(content)
	closeErr := temporary.Close()
	closed = true
	if err := errors.Join(writeErr, closeErr); err != nil {
		return fmt.Errorf("write analysis file: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("publish analysis file: %w", err)
	}
	return nil
}
