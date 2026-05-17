package eval

import (
	"strings"
	"testing"

	"github.com/longyisang/emoagent-memorycore/internal/app/memorycore"
)

func TestQualityReportFullShowsQuestionExpectationAndResult(t *testing.T) {
	fixture := &Fixture{
		CaseID: "quality_case",
		Steps: []Step{
			{
				ID:     "f_target",
				Action: "fact",
				Fact: &FactStep{
					ContentSummary: "用户晚上九点后更适合深度工作。",
				},
			},
			{
				ID:     "q001_fact",
				Action: "retrieve",
				Retrieve: &RetrieveStep{
					QueryText: "晚上九点 深度工作",
				},
			},
		},
		Assertions: []Assertion{
			{
				Type:            "selected_recall_at_k",
				Name:            "q001 recalls target",
				Step:            "q001_fact",
				RelevantNodeIDs: []string{"$f_target.fact_id"},
				At:              4,
				Min:             1,
			},
		},
	}
	report := Report{
		CaseID: fixture.CaseID,
		Steps: []StepReport{
			{
				ID:        "q001_fact",
				Action:    "retrieve",
				QueryText: "晚上九点 深度工作",
				Retrieval: &memorycore.MemoryContext{
					Blocks: []memorycore.MemoryBlock{
						{
							BlockType: "experience_context",
							Items: []memorycore.MemoryContextItem{
								{
									NodeType:         "fact",
									NodeID:           "f_target",
									Summary:          "用户晚上九点后更适合深度工作。",
									HistoricalStatus: "current",
								},
							},
						},
					},
				},
			},
		},
		Results: []AssertionResult{
			{Name: "q001 recalls target", Type: "selected_recall_at_k"},
		},
	}

	out := FormatQualityBenchmarkReport([]QualityBenchmarkCase{{Fixture: fixture, Report: report}}, QualityBenchmarkReportOptions{Mode: QualityBenchmarkModeFull})
	for _, want := range []string{
		"question_id: q001_fact",
		"问题: 晚上九点 深度工作",
		"期望:",
		"relevant_node_ids=f_target",
		"fact:f_target 用户晚上九点后更适合深度工作。",
		"结果:",
		"PASS [selected_recall_at_k] q001 recalls target",
		"experience_context fact:f_target current 用户晚上九点后更适合深度工作。",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("FormatQualityBenchmarkReport() =\n%s\nwant substring %q", out, want)
		}
	}
}

func TestQualityReportBriefOnlyShowsFailures(t *testing.T) {
	fixture := &Fixture{
		CaseID: "quality_case",
		Steps: []Step{
			{
				ID:     "q_pass",
				Action: "retrieve",
				Retrieve: &RetrieveStep{
					QueryText: "passing question",
				},
			},
			{
				ID:     "q_fail",
				Action: "retrieve",
				Retrieve: &RetrieveStep{
					QueryText: "failing question",
				},
			},
		},
		Assertions: []Assertion{
			{Type: "query_analysis", Name: "passing assertion", Step: "q_pass"},
			{Type: "block_contains", Name: "failing assertion", Step: "q_fail", NodeID: "missing_fact"},
		},
	}
	report := Report{
		CaseID: fixture.CaseID,
		Steps: []StepReport{
			{ID: "q_pass", Action: "retrieve", QueryText: "passing question", Retrieval: &memorycore.MemoryContext{}},
			{ID: "q_fail", Action: "retrieve", QueryText: "failing question", Retrieval: &memorycore.MemoryContext{}},
		},
		Results: []AssertionResult{
			{Name: "passing assertion", Type: "query_analysis"},
			{
				Name: "failing assertion",
				Type: "block_contains",
				Err: AssertionFailure{
					CaseID:    "quality_case",
					Assertion: "block_contains",
					Expected:  "node missing_fact present",
					Actual:    "empty",
				},
			},
		},
	}

	out := FormatQualityBenchmarkReport([]QualityBenchmarkCase{{Fixture: fixture, Report: report}}, QualityBenchmarkReportOptions{Mode: QualityBenchmarkModeBrief})
	if strings.Contains(out, "passing question") {
		t.Fatalf("brief report includes passing question:\n%s", out)
	}
	for _, want := range []string{
		"question_id: q_fail",
		"问题: failing question",
		"FAIL [block_contains] failing assertion",
		"expected=node missing_fact present actual=empty",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("brief report =\n%s\nwant substring %q", out, want)
		}
	}
}
