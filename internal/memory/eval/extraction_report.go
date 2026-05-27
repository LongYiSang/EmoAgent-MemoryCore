package eval

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

type extractionLatestReport struct {
	Suite        string                  `json:"suite"`
	FixtureCount int                     `json:"fixture_count"`
	Passed       int                     `json:"passed"`
	Failed       int                     `json:"failed"`
	Metrics      extractionLatestMetrics `json:"metrics"`
	Cases        []extractionLatestCase  `json:"cases"`
}

type extractionLatestMetrics struct {
	DuplicateFactCount           int `json:"duplicate_fact_count"`
	ForbiddenRecall              int `json:"forbidden_recall"`
	AuditLeak                    int `json:"audit_leak"`
	DeletedSourceRevival         int `json:"deleted_source_revival"`
	AgentAffectBoundaryViolation int `json:"agent_affect_boundary_violation"`
}

type extractionLatestCase struct {
	CaseID string `json:"case_id"`
	Path   string `json:"path,omitempty"`
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

func writeExtractionLatestReports(jsonPath string, markdownPath string, cases []QualityBenchmarkCase) error {
	report := extractionLatestReport{
		Suite: "extraction_consolidation",
		Cases: make([]extractionLatestCase, 0, len(cases)),
	}
	for _, item := range cases {
		caseID := item.Report.CaseID
		if item.Fixture != nil && item.Fixture.CaseID != "" {
			caseID = item.Fixture.CaseID
		}
		out := extractionLatestCase{CaseID: caseID, Path: item.Path, Status: "pass"}
		if item.Report.Failed() {
			out.Status = "fail"
			out.Error = item.Report.Error()
			report.Failed++
		} else {
			report.Passed++
		}
		report.Metrics = mergeExtractionLatestMetrics(report.Metrics, extractionLatestMetricsFromReport(item.Report))
		report.Cases = append(report.Cases, out)
	}
	report.FixtureCount = len(report.Cases)
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(jsonPath, append(data, '\n'), 0o644); err != nil {
		return err
	}
	return os.WriteFile(markdownPath, []byte(formatExtractionLatestMarkdown(report)), 0o644)
}

func mergeExtractionLatestMetrics(left extractionLatestMetrics, right extractionLatestMetrics) extractionLatestMetrics {
	left.DuplicateFactCount += right.DuplicateFactCount
	left.ForbiddenRecall += right.ForbiddenRecall
	left.AuditLeak += right.AuditLeak
	left.DeletedSourceRevival += right.DeletedSourceRevival
	left.AgentAffectBoundaryViolation += right.AgentAffectBoundaryViolation
	return left
}

func extractionLatestMetricsFromReport(report Report) extractionLatestMetrics {
	var metrics extractionLatestMetrics
	for _, result := range report.Results {
		if result.Err == nil {
			continue
		}
		key := strings.ToLower(strings.Join([]string{result.Name, result.Type, result.Err.Error()}, " "))
		switch {
		case result.Type == "no_duplicate_by_key" || strings.Contains(key, "duplicate_fact"):
			metrics.DuplicateFactCount++
		case result.Type == "forbidden_recall_zero" || strings.Contains(key, "forbidden_recall"):
			metrics.ForbiddenRecall++
		case strings.Contains(key, "audit_leak"):
			metrics.AuditLeak++
		case strings.Contains(key, "deleted_source_revival"):
			metrics.DeletedSourceRevival++
		case strings.Contains(key, "agent_affect_boundary"):
			metrics.AgentAffectBoundaryViolation++
		}
	}
	if report.Err != nil {
		key := strings.ToLower(report.Err.Error())
		switch {
		case strings.Contains(key, "duplicate_fact"):
			metrics.DuplicateFactCount++
		case strings.Contains(key, "forbidden_recall"):
			metrics.ForbiddenRecall++
		case strings.Contains(key, "audit_leak"):
			metrics.AuditLeak++
		case strings.Contains(key, "deleted_source_revival"):
			metrics.DeletedSourceRevival++
		case strings.Contains(key, "agent_affect_boundary"):
			metrics.AgentAffectBoundaryViolation++
		}
	}
	return metrics
}

func formatExtractionLatestMarkdown(report extractionLatestReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Memory Eval Latest\n\n")
	fmt.Fprintf(&b, "- suite: %s\n", report.Suite)
	fmt.Fprintf(&b, "- fixtures: %d\n", report.FixtureCount)
	fmt.Fprintf(&b, "- passed: %d\n", report.Passed)
	fmt.Fprintf(&b, "- failed: %d\n", report.Failed)
	fmt.Fprintf(&b, "- forbidden_recall: %d\n", report.Metrics.ForbiddenRecall)
	fmt.Fprintf(&b, "- audit_leak: %d\n", report.Metrics.AuditLeak)
	fmt.Fprintf(&b, "- deleted_source_revival: %d\n", report.Metrics.DeletedSourceRevival)
	fmt.Fprintf(&b, "- agent_affect_boundary_violation: %d\n", report.Metrics.AgentAffectBoundaryViolation)
	fmt.Fprintf(&b, "- duplicate_fact_count: %d\n\n", report.Metrics.DuplicateFactCount)
	b.WriteString("| case | status |\n|---|---|\n")
	for _, item := range report.Cases {
		fmt.Fprintf(&b, "| %s | %s |\n", item.CaseID, item.Status)
	}
	return b.String()
}
