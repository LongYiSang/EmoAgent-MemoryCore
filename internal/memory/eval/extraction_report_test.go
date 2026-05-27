package eval

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteExtractionLatestReportsAggregatesFailureMetrics(t *testing.T) {
	dir := t.TempDir()
	jsonPath := filepath.Join(dir, "latest.json")
	markdownPath := filepath.Join(dir, "latest.md")
	cases := []QualityBenchmarkCase{
		reportWithFailure("dup", "no_duplicate_by_key"),
		reportWithFailure("forbidden", "forbidden_recall_zero"),
		reportWithNamedFailure("audit", "sql_count", "audit_leak_zero"),
		reportWithNamedFailure("revival", "sql_count", "deleted_source_revival_zero"),
		reportWithNamedFailure("agent", "apply_status", "agent_affect_boundary_violation_zero"),
	}

	if err := writeExtractionLatestReports(jsonPath, markdownPath, cases); err != nil {
		t.Fatalf("write report: %v", err)
	}
	data, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatalf("read latest report: %v", err)
	}
	var report extractionLatestReport
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatalf("decode latest report: %v", err)
	}
	if report.Metrics.DuplicateFactCount != 1 ||
		report.Metrics.ForbiddenRecall != 1 ||
		report.Metrics.AuditLeak != 1 ||
		report.Metrics.DeletedSourceRevival != 1 ||
		report.Metrics.AgentAffectBoundaryViolation != 1 {
		t.Fatalf("metrics = %#v, want all review counters aggregated from failures", report.Metrics)
	}
}

func reportWithFailure(caseID string, assertionType string) QualityBenchmarkCase {
	return reportWithNamedFailure(caseID, assertionType, "")
}

func reportWithNamedFailure(caseID string, assertionType string, name string) QualityBenchmarkCase {
	return QualityBenchmarkCase{Report: Report{
		CaseID: caseID,
		Results: []AssertionResult{{
			Name: name,
			Type: assertionType,
			Err:  AssertionFailure{CaseID: caseID, Assertion: assertionType, Expected: "pass", Actual: "fail"},
		}},
	}}
}
