package eval

import "github.com/longyisang/emoagent-memorycore/internal/app/memorycore"

type QueryAnalysisReport struct {
	TestPlanVersion string                     `json:"test_plan_version"`
	CaseID          string                     `json:"case_id,omitempty"`
	Entries         []QueryAnalysisReportEntry `json:"entries"`
}

type QueryAnalysisReportEntry struct {
	CaseID        string                      `json:"case_id,omitempty"`
	Profile       Profile                     `json:"profile"`
	QuestionID    string                      `json:"question_id"`
	Action        string                      `json:"action,omitempty"`
	QueryText     string                      `json:"query_text,omitempty"`
	Source        string                      `json:"source,omitempty"`
	Semantic      QueryAnalysisSemanticReport `json:"semantic"`
	QueryAnalysis *memorycore.QueryAnalysis   `json:"query_analysis,omitempty"`
	Error         string                      `json:"error,omitempty"`
}

type QueryAnalysisSemanticReport struct {
	Status         string                                       `json:"status,omitempty"`
	Provider       string                                       `json:"provider,omitempty"`
	Model          string                                       `json:"model,omitempty"`
	PromptVersion  string                                       `json:"prompt_version,omitempty"`
	LatencyMS      int64                                        `json:"latency_ms,omitempty"`
	FallbackReason string                                       `json:"fallback_reason,omitempty"`
	ReturnedStruct *memorycore.SemanticQueryAnalysisDiagnostics `json:"returned_struct,omitempty"`
}

func BuildQueryAnalysisReport(fixture *Fixture, report MatrixReport) QueryAnalysisReport {
	caseID := report.CaseID
	if fixture != nil && fixture.CaseID != "" {
		caseID = fixture.CaseID
	}
	out := QueryAnalysisReport{
		TestPlanVersion: matrixReportTestPlanVersion(report),
		CaseID:          caseID,
	}
	for _, profile := range report.Profiles {
		for _, step := range profile.Report.Steps {
			if step.Retrieval == nil && step.QueryText == "" {
				continue
			}
			entry := QueryAnalysisReportEntry{
				CaseID:     caseID,
				Profile:    profile.Profile,
				QuestionID: step.ID,
				Action:     step.Action,
				QueryText:  step.QueryText,
			}
			if step.Retrieval == nil {
				entry.Error = appendQueryAnalysisReportError(entry.Error, "retrieval_missing")
				if profile.Report.Err != nil {
					entry.Error = appendQueryAnalysisReportError(entry.Error, profile.Report.Err.Error())
				}
				out.Entries = append(out.Entries, entry)
				continue
			}
			analysis := step.Retrieval.QueryAnalysis
			if analysis == nil {
				entry.Error = appendQueryAnalysisReportError(entry.Error, "query_analysis_missing")
				out.Entries = append(out.Entries, entry)
				continue
			}
			entry.Source = string(analysis.Source)
			entry.QueryAnalysis = analysis
			entry.Semantic = queryAnalysisSemanticReportFromAnalysis(analysis)
			out.Entries = append(out.Entries, entry)
		}
	}
	return out
}

func queryAnalysisSemanticReportFromAnalysis(analysis *memorycore.QueryAnalysis) QueryAnalysisSemanticReport {
	if analysis == nil {
		return QueryAnalysisSemanticReport{}
	}
	if analysis.Diagnostics == nil {
		if analysis.Source == memorycore.QueryAnalysisSourceRuleOnly {
			return QueryAnalysisSemanticReport{Status: "rule_only_no_semantic_call"}
		}
		return QueryAnalysisSemanticReport{}
	}
	diagnostics := analysis.Diagnostics
	status := diagnostics.SemanticStatus
	if status == "" && analysis.Source == memorycore.QueryAnalysisSourceRuleOnly {
		status = "rule_only_no_semantic_call"
	}
	return QueryAnalysisSemanticReport{
		Status:         status,
		Provider:       diagnostics.SemanticProvider,
		Model:          diagnostics.SemanticModel,
		PromptVersion:  diagnostics.PromptVersion,
		LatencyMS:      diagnostics.SemanticLatencyMs,
		FallbackReason: diagnostics.FallbackReason,
		ReturnedStruct: diagnostics.SemanticAnalysis,
	}
}

func appendQueryAnalysisReportError(existing string, next string) string {
	if next == "" {
		return existing
	}
	if existing == "" {
		return next
	}
	return existing + "\n" + next
}
