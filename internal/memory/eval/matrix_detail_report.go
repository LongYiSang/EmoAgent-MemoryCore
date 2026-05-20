package eval

import (
	"fmt"
	"strings"
)

func FormatMatrixDetailReport(fixture *Fixture, report MatrixReport) string {
	var b strings.Builder
	caseID := report.CaseID
	if fixture != nil && fixture.CaseID != "" {
		caseID = fixture.CaseID
	}
	fmt.Fprintf(&b, "matrix_detail_report\n")
	fmt.Fprintf(&b, "test_plan_version: %s\n", matrixReportTestPlanVersion(report))
	fmt.Fprintf(&b, "case_id: %s\n", caseID)
	if len(report.Profiles) > 0 {
		writeMatrixProfileSummary(&b, report.Profiles)
	}
	if fixture == nil {
		return strings.TrimRight(b.String(), "\n")
	}

	catalog := newQualityNodeCatalog(fixture)
	expectedByStep := qualityAssertionsByStep(fixture, Report{})
	writeMatrixFailureIndex(&b, fixture, report.Profiles)
	for _, step := range fixture.Steps {
		if step.Action != "retrieve" || step.Retrieve == nil {
			continue
		}
		fmt.Fprintf(&b, "\nquestion_id: %s\n", step.ID)
		fmt.Fprintf(&b, "问题: %s\n", step.Retrieve.QueryText)
		b.WriteString("期望:\n")
		expectations := expectedByStep[step.ID]
		if len(expectations) == 0 {
			b.WriteString("  - (no assertions)\n")
		}
		for _, assertion := range expectations {
			writeQualityExpectation(&b, assertion.assertion, catalog)
		}
		writeMatrixQuestionResultComparison(&b, fixture, report.Profiles, step.ID)
		b.WriteString("实际结果:\n")
		for _, profile := range report.Profiles {
			writeMatrixProfileQuestionDetail(&b, fixture, profile, step.ID)
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func writeMatrixProfileSummary(b *strings.Builder, profiles []ProfileMatrixReport) {
	b.WriteString("profile_summary:\n")
	b.WriteString("| profile | status | capability | assertion_failures | selected_recall_at_8 | precision_at_8 | fallback_count | query_analysis_used_count | query_analysis_fallback_count | query_analysis_invalid_json_count | query_analysis_validation_failed_count | query_analysis_latency_p50 | query_analysis_latency_p95 | english_rewrite_count | dropped_rewrite_count | semantic_rewrite_dense_count | candidate_query_count | graph_activation_used_count | rerank_live_call_count | provider_timeout_count | query_trim_count | raw_exact_survival_count |\n")
	b.WriteString("|---|---|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|\n")
	var profileErrors []profileErrorSummary
	for _, profile := range profiles {
		assertionFailures := countAssertionFailures(profile.Report)
		fmt.Fprintf(
			b,
			"| %s | %s | %s | %d | %.3f | %.3f | %d | %d | %d | %d | %d | %d | %d | %d | %d | %d | %d | %d | %d | %d | %d | %d |\n",
			profile.Profile,
			profile.Status,
			profile.Capability.Status,
			assertionFailures,
			profile.Metrics.SelectedRecallAt8,
			profile.Metrics.PrecisionAt8,
			profile.Metrics.FallbackCount,
			profile.Metrics.QueryAnalysisUsedCount,
			profile.Metrics.QueryAnalysisFallbackCount,
			profile.Metrics.QueryAnalysisInvalidJSONCount,
			profile.Metrics.QueryAnalysisValidationFailedCount,
			profile.Metrics.QueryAnalysisLatencyP50,
			profile.Metrics.QueryAnalysisLatencyP95,
			profile.Metrics.EnglishRewriteCount,
			profile.Metrics.DroppedRewriteCount,
			profile.Metrics.SemanticRewriteDenseCount,
			profile.Metrics.CandidateQueryCount,
			profile.Metrics.GraphActivationUsedCount,
			profile.Metrics.RerankLiveCallCount,
			profile.Metrics.ProviderTimeoutCount,
			profile.Metrics.QueryTrimCount,
			profile.Metrics.RawExactSurvivalCount,
		)
		if errSummary := nonAssertionProfileError(profile, assertionFailures); errSummary != "" {
			profileErrors = append(profileErrors, profileErrorSummary{profile: profile.Profile, err: errSummary})
		}
	}
	if len(profileErrors) > 0 {
		b.WriteString("profile_errors:\n")
		for _, profileError := range profileErrors {
			fmt.Fprintf(b, "  - %s: %s\n", profileError.profile, profileError.err)
		}
	}
}

func writeMatrixFailureIndex(b *strings.Builder, fixture *Fixture, profiles []ProfileMatrixReport) {
	if fixture == nil || len(profiles) == 0 {
		return
	}
	b.WriteString("\nfailure_index:\n")
	b.WriteString("| question_id |")
	for _, profile := range profiles {
		fmt.Fprintf(b, " %s |", profile.Profile)
	}
	b.WriteString("\n|---|")
	for range profiles {
		b.WriteString("---|")
	}
	b.WriteString("\n")
	for _, step := range fixture.Steps {
		if step.Action != "retrieve" || step.Retrieve == nil {
			continue
		}
		fmt.Fprintf(b, "| %s |", step.ID)
		for _, profile := range profiles {
			status, failures := matrixStepAssertionSummary(fixture, profile, step.ID)
			cell := status
			if failures != "" {
				cell += " " + failures
			}
			fmt.Fprintf(b, " %s |", cell)
		}
		b.WriteString("\n")
	}
}

func writeMatrixQuestionResultComparison(b *strings.Builder, fixture *Fixture, profiles []ProfileMatrixReport, stepID string) {
	b.WriteString("结果对比:\n")
	b.WriteString("| profile | result | failed_assertions |\n")
	b.WriteString("|---|---|---|\n")
	for _, profile := range profiles {
		status, failures := matrixStepAssertionSummary(fixture, profile, stepID)
		if failures == "" {
			failures = "-"
		}
		fmt.Fprintf(b, "| %s | %s | %s |\n", profile.Profile, status, failures)
	}
}

func writeMatrixProfileQuestionDetail(b *strings.Builder, fixture *Fixture, profile ProfileMatrixReport, stepID string) {
	fmt.Fprintf(b, "\nprofile: %s\n", profile.Profile)

	b.WriteString("结果:\n")
	assertions := qualityAssertionsByStep(fixture, profile.Report)[stepID]
	if len(assertions) == 0 {
		b.WriteString("  - (no assertions)\n")
	}
	for _, assertion := range assertions {
		writeQualityAssertionResult(b, assertion.result)
	}

	b.WriteString("实际结果:\n")
	stepReport, ok := qualityStepReports(profile.Report)[stepID]
	if !ok {
		b.WriteString("  selected: (missing retrieve step report)\n")
		return
	}
	writeQualityRetrievalResult(b, stepReport)
}

func matrixStepAssertionSummary(fixture *Fixture, profile ProfileMatrixReport, stepID string) (string, string) {
	if profile.Report.Err != nil {
		return "ERROR", "run_error"
	}
	assertions := qualityAssertionsByStep(fixture, profile.Report)[stepID]
	if len(assertions) == 0 {
		return "-", ""
	}
	var failures []string
	seen := map[string]struct{}{}
	for _, assertion := range assertions {
		if assertion.result.Err == nil {
			continue
		}
		label := assertion.result.Type
		if strings.TrimSpace(label) == "" {
			label = assertion.assertion.Type
		}
		if strings.TrimSpace(label) == "" {
			label = "assertion"
		}
		if _, ok := seen[label]; ok {
			continue
		}
		seen[label] = struct{}{}
		failures = append(failures, label)
	}
	if len(failures) > 0 {
		return "FAIL", strings.Join(failures, ",")
	}
	return "PASS", ""
}

type profileErrorSummary struct {
	profile Profile
	err     string
}

func countAssertionFailures(report Report) int {
	failures := 0
	for _, result := range report.Results {
		if result.Err != nil {
			failures++
		}
	}
	return failures
}

func nonAssertionProfileError(profile ProfileMatrixReport, assertionFailures int) string {
	if profile.Report.Err != nil {
		return firstProfileErrorLine(profile.Report.Err.Error())
	}
	if assertionFailures == 0 && strings.TrimSpace(profile.Error) != "" {
		return firstProfileErrorLine(profile.Error)
	}
	return ""
}

func firstProfileErrorLine(value string) string {
	for _, line := range strings.Split(value, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return line
		}
	}
	return ""
}
