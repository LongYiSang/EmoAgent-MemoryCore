package eval

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/longyisang/emoagent-memorycore/internal/app/memorycore"
)

func TestBuildQueryAnalysisReportIncludesReturnedStructAndFallback(t *testing.T) {
	report := MatrixReport{
		TestPlanVersion: matrixTestPlanVersion,
		CaseID:          "qa_report_case",
		Profiles: []ProfileMatrixReport{{
			Profile: ProfileSemanticFullCurrent,
			Report: Report{Steps: []StepReport{{
				ID:        "q019",
				Action:    "retrieve",
				QueryText: "为什么会出错",
				Retrieval: &memorycore.MemoryContext{
					QueryAnalysis: &memorycore.QueryAnalysis{
						Source: memorycore.QueryAnalysisSourceSemanticFallback,
						Diagnostics: &memorycore.QueryAnalysisDiagnostics{
							SemanticStatus:    "failed",
							SemanticProvider:  "deepseek",
							SemanticModel:     "deepseek-v4-flash",
							SemanticLatencyMs: 8123,
							FallbackReason:    "provider_error",
							SemanticAnalysis: &memorycore.SemanticQueryAnalysisDiagnostics{
								MemoryAbility: string(memorycore.MemoryAbilityGotcha),
								QueryRewrites: []memorycore.QueryRewrite{{Text: "出错原因", Purpose: "semantic_recall", Weight: 0.5}},
							},
						},
					},
				},
			}}},
		}},
	}

	got := BuildQueryAnalysisReport(nil, report)
	if len(got.Entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(got.Entries))
	}
	entry := got.Entries[0]
	if entry.QuestionID != "q019" || entry.Semantic.FallbackReason != "provider_error" || entry.Semantic.ReturnedStruct == nil {
		t.Fatalf("entry = %#v, want question id, fallback reason, and returned struct", entry)
	}
	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal query analysis report: %v", err)
	}
	jsonText := string(raw)
	for _, want := range []string{`"question_id":"q019"`, `"fallback_reason":"provider_error"`, `"returned_struct"`, `"memory_ability":"gotcha"`} {
		if !strings.Contains(jsonText, want) {
			t.Fatalf("query analysis report json = %s, want %s", jsonText, want)
		}
	}
}
