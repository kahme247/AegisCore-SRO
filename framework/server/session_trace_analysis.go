package server

import traceanalysis "github.com/Harvi8/AegisCore/framework/diagnostics/sessiontrace"

type completedSessionTraceAnalysis struct {
	Reports  traceanalysis.ReportPaths
	Probable *traceanalysis.Finding
}

func analyzeCompletedSessionTrace(path string) (completedSessionTraceAnalysis, error) {
	analysis, err := traceanalysis.AnalyzeFile(path, traceanalysis.DefaultAnalysisOptions())
	if err != nil {
		return completedSessionTraceAnalysis{}, err
	}
	reports, err := traceanalysis.WriteReports(path, analysis)
	if err != nil {
		return completedSessionTraceAnalysis{}, err
	}
	return completedSessionTraceAnalysis{Reports: reports, Probable: analysis.ProbableFailure}, nil
}
