package eval

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/longyisang/emoagent-memorycore/internal/app/memorycore"
)

func TestLoadFixtureBytesValidatesRequiredCaseID(t *testing.T) {
	_, err := LoadFixtureBytes([]byte(`
steps: []
`))
	if err == nil {
		t.Fatal("LoadFixtureBytes err is nil, want missing case_id error")
	}
	if !strings.Contains(err.Error(), "case_id") {
		t.Fatalf("error = %q, want case_id", err.Error())
	}
}

func TestLoadFixtureBytesRejectsUnknownStepAction(t *testing.T) {
	_, err := LoadFixtureBytes([]byte(`
case_id: BAD_STEP
steps:
  - id: unknown
    action: teleport
`))
	if err == nil {
		t.Fatal("LoadFixtureBytes err is nil, want unknown step action error")
	}
	if !strings.Contains(err.Error(), "BAD_STEP") || !strings.Contains(err.Error(), "teleport") {
		t.Fatalf("error = %q, want case id and action", err.Error())
	}
}

func TestLoadFixtureBytesRejectsUnknownAssertionType(t *testing.T) {
	_, err := LoadFixtureBytes([]byte(`
case_id: BAD_ASSERTION
steps:
  - id: retrieve
    action: retrieve
    retrieve:
      query_text: coffee
assertions:
  - type: unsupported_phase5f_assertion
    step: retrieve
`))
	if err == nil {
		t.Fatal("LoadFixtureBytes err is nil, want unknown assertion type error")
	}
	if !strings.Contains(err.Error(), "BAD_ASSERTION") || !strings.Contains(err.Error(), "unsupported_phase5f_assertion") {
		t.Fatalf("error = %q, want case id and assertion type", err.Error())
	}
}

func TestLoadFixtureBytesAcceptsPhase5FAssertionFields(t *testing.T) {
	fixture, err := LoadFixtureBytes([]byte(`
case_id: PHASE5F_ASSERTION_FIELDS
steps:
  - id: retrieve
    action: retrieve
    retrieve:
      query_text: coffee
assertions:
  - type: selected_recall_at_k
    step: retrieve
    relevant_node_ids: [fact_a, fact_b]
    at: 8
    min: 0.9
  - type: context_precision_at_k
    step: retrieve
    relevant_node_ids: [fact_a]
    at: 8
    min: 0.75
  - type: selected_chain_correct
    step: retrieve
    block_type: causal_context
    node_id: fact_a
    node_ids: [fact_b]
    link_type: CAUSED_BY
    direction: outbound
    historical_status: current
    source_ref_count: 1
  - type: suppression_event
    step: retrieve
    node_id: fact_b
    suppression_reason: mmr_duplicate
  - type: graph_activation_candidate
    step: retrieve
    node_id: fact_c
    source: graph_activation
    rank: 1
`))
	if err != nil {
		t.Fatalf("LoadFixtureBytes err = %v, want nil", err)
	}
	if len(fixture.Assertions) != 5 {
		t.Fatalf("assertion count = %d, want 5", len(fixture.Assertions))
	}
	if got := fixture.Assertions[0].RelevantNodeIDs; len(got) != 2 || got[0] != "fact_a" || fixture.Assertions[0].At != 8 || fixture.Assertions[0].Min != 0.9 {
		t.Fatalf("selected_recall assertion = %#v", fixture.Assertions[0])
	}
	if got := fixture.Assertions[2].NodeIDs; len(got) != 1 || got[0] != "fact_b" || fixture.Assertions[2].BlockType != "causal_context" || fixture.Assertions[2].SourceRefCount != 1 {
		t.Fatalf("selected_chain assertion = %#v", fixture.Assertions[2])
	}
}

func TestPhase5FMetricMath(t *testing.T) {
	selected := []string{"a", "b", "c"}
	relevant := []string{"a", "c", "d"}

	if got := recallAtK(selected, relevant, 2); got != float64(1)/float64(3) {
		t.Fatalf("recallAtK = %.3f, want %.3f", got, float64(1)/float64(3))
	}
	if got := recallAtK(selected, relevant, 0); got != float64(2)/float64(3) {
		t.Fatalf("recallAtK all = %.3f, want %.3f", got, float64(2)/float64(3))
	}
	if got := precisionAtK(selected, relevant, 2); got != 0.5 {
		t.Fatalf("precisionAtK = %.3f, want 0.500", got)
	}
}

func TestPhase5FMetricAssertionsUseOnlySelectedItems(t *testing.T) {
	fixture, err := LoadFixtureBytes([]byte(`
case_id: PHASE5F_METRIC_SELECTED_ONLY
seed:
  sessions:
    - id: s1
      channel: api
  entities:
    - id: user
      canonical_name: Long
      entity_type: user
  episodes:
    - id: ep1
      session_id: s1
      content: 早会导致焦虑，咖啡帮助恢复。
      occurred_at: "2026-04-28T09:00:00+08:00"
steps:
  - id: selected
    action: consolidate
    consolidate:
      candidate:
        subject_entity_id: user
        predicate: dislikes
        object_literal: 早会
        content_summary: 用户不喜欢早会。
        source_episode_ids: [ep1]
        confidence: explicit
        importance: 1.0
  - id: related
    action: consolidate
    consolidate:
      candidate:
        subject_entity_id: user
        predicate: likes
        object_literal: 咖啡
        content_summary: 用户喝咖啡恢复精力。
        source_episode_ids: [ep1]
        confidence: explicit
        importance: 0.1
  - id: retrieve
    action: retrieve
    retrieve:
      session_id: s1
      query_text: 早会
      policy:
        final_memory_count: 1
assertions:
  - type: selected_recall_at_k
    step: retrieve
    relevant_node_ids: [$selected.fact_id, $related.fact_id]
    at: 8
    min: 0.5
  - type: context_precision_at_k
    step: retrieve
    relevant_node_ids: [$selected.fact_id]
    at: 8
    min: 1.0
  - type: forbidden_recall_zero
    step: retrieve
    forbidden_node_ids: [$related.fact_id]
`))
	if err != nil {
		t.Fatalf("load fixture: %v", err)
	}

	report := NewRunner(RunnerOptions{TempDir: t.TempDir()}).Run(context.Background(), fixture)
	if report.Failed() {
		t.Fatalf("run fixture: %s", report.Error())
	}
}

func TestPhase5FMetricAssertionFailureMessage(t *testing.T) {
	fixture, err := LoadFixtureBytes([]byte(`
case_id: PHASE5F_METRIC_FAILURE
seed:
  sessions:
    - id: s1
      channel: api
  entities:
    - id: user
      canonical_name: Long
      entity_type: user
  episodes:
    - id: ep1
      session_id: s1
      content: 早会和咖啡。
      occurred_at: "2026-04-28T09:00:00+08:00"
steps:
  - id: selected
    action: consolidate
    consolidate:
      candidate:
        subject_entity_id: user
        predicate: dislikes
        object_literal: 早会
        content_summary: 用户不喜欢早会。
        source_episode_ids: [ep1]
        confidence: explicit
        importance: 1.0
  - id: missing
    action: consolidate
    consolidate:
      candidate:
        subject_entity_id: user
        predicate: likes
        object_literal: 咖啡
        content_summary: 用户喜欢咖啡。
        source_episode_ids: [ep1]
        confidence: explicit
        importance: 0.1
  - id: retrieve
    action: retrieve
    retrieve:
      session_id: s1
      query_text: 早会
      policy:
        final_memory_count: 1
assertions:
  - type: selected_recall_at_k
    step: retrieve
    relevant_node_ids: [$selected.fact_id, $missing.fact_id]
    at: 8
    min: 1.0
`))
	if err != nil {
		t.Fatalf("load fixture: %v", err)
	}

	report := NewRunner(RunnerOptions{TempDir: t.TempDir()}).Run(context.Background(), fixture)
	if !report.Failed() {
		t.Fatal("report passed, want recall assertion failure")
	}
	errText := report.Error()
	if !strings.Contains(errText, "PHASE5F_METRIC_FAILURE") ||
		!strings.Contains(errText, "recall@1 >= 1.000") ||
		!strings.Contains(errText, "selected=") ||
		!strings.Contains(errText, "relevant=") {
		t.Fatalf("report error = %q, want case id, recall threshold, selected and relevant ids", errText)
	}
}

func TestForbiddenRecallZeroChecksSourceRefs(t *testing.T) {
	fixture, err := LoadFixtureBytes([]byte(`
case_id: FORBIDDEN_SOURCE_REF
seed:
  sessions:
    - id: s1
      channel: api
  entities:
    - id: user
      canonical_name: Long
      entity_type: user
  episodes:
    - id: ep_forbidden
      session_id: s1
      content: 用户喜欢咖啡。
      occurred_at: "2026-04-28T09:00:00+08:00"
steps:
  - id: fact
    action: consolidate
    consolidate:
      candidate:
        subject_entity_id: user
        predicate: likes
        object_literal: 咖啡
        content_summary: 用户喜欢咖啡。
        source_episode_ids: [ep_forbidden]
        confidence: explicit
        importance: 1.0
  - id: retrieve
    action: retrieve
    retrieve:
      session_id: s1
      query_text: 咖啡
      policy:
        final_memory_count: 1
assertions:
  - type: forbidden_recall_zero
    step: retrieve
    forbidden_node_ids: [ep_forbidden]
`))
	if err != nil {
		t.Fatalf("load fixture: %v", err)
	}

	report := NewRunner(RunnerOptions{TempDir: t.TempDir()}).Run(context.Background(), fixture)
	if !report.Failed() {
		t.Fatal("report passed, want source ref forbidden recall failure")
	}
	if !strings.Contains(report.Error(), "source_ref:ep_forbidden") {
		t.Fatalf("report error = %q, want forbidden source ref id", report.Error())
	}
}

func TestEvalRunnerGraphActivationStubFeedsRetrieveDiagnostics(t *testing.T) {
	fixture, err := LoadFixtureBytes([]byte(`
case_id: GRAPH_ACTIVATION_STUB
seed:
  sessions:
    - id: s1
      channel: api
  entities:
    - id: user
      canonical_name: Long
      entity_type: user
  episodes:
    - id: ep1
      session_id: s1
      content: 我喜欢咖啡，早会会让我焦虑。
      occurred_at: "2026-04-28T09:00:00+08:00"
steps:
  - id: seed_fact
    action: consolidate
    consolidate:
      candidate:
        subject_entity_id: user
        predicate: likes
        object_literal: 咖啡
        content_summary: 用户喜欢咖啡。
        source_episode_ids: [ep1]
        confidence: explicit
        importance: 0.8
  - id: graph_fact
    action: consolidate
    consolidate:
      candidate:
        subject_entity_id: user
        predicate: dislikes
        object_literal: 早会
        content_summary: 用户不喜欢早会。
        source_episode_ids: [ep1]
        confidence: explicit
        importance: 0.7
  - id: retrieve
    action: retrieve
    mirror_stub:
      index_mapped_nodes:
        - node_id: $seed_fact.fact_id
          node_type: fact
        - node_id: $graph_fact.fact_id
          node_type: fact
      candidates:
        - node_id: $seed_fact.fact_id
          node_type: fact
          score: 0.95
    graph_activation_stub:
      candidates:
        - node_id: $graph_fact.fact_id
          node_type: fact
          score: 0.88
          source: graph_activation
          rank: 1
          path_node_ids: [$seed_fact.fact_id, $graph_fact.fact_id]
          path_link_types: [CAUSED_BY]
    retrieve:
      session_id: s1
      query_text: 早会
      policy:
        use_mirror: true
        final_memory_count: 2
assertions:
  - type: graph_activation_candidate
    step: retrieve
    node_id: $graph_fact.fact_id
    source: graph_activation
    rank: 1
  - type: memory_contains
    step: retrieve
    node_id: $graph_fact.fact_id
`))
	if err != nil {
		t.Fatalf("load fixture: %v", err)
	}

	report := NewRunner(RunnerOptions{TempDir: t.TempDir()}).Run(context.Background(), fixture)
	if report.Failed() {
		t.Fatalf("run fixture: %s", report.Error())
	}
}

func TestEvalRunnerGraphActivationStubFallbackStatuses(t *testing.T) {
	fixture, err := LoadFixtureBytes([]byte(`
case_id: GRAPH_ACTIVATION_STUB_FALLBACKS
seed:
  sessions:
    - id: s1
      channel: api
  entities:
    - id: user
      canonical_name: Long
      entity_type: user
  episodes:
    - id: ep1
      session_id: s1
      content: 我喜欢咖啡。
      occurred_at: "2026-04-28T09:00:00+08:00"
steps:
  - id: seed_fact
    action: consolidate
    consolidate:
      candidate:
        subject_entity_id: user
        predicate: likes
        object_literal: 咖啡
        content_summary: 用户喜欢咖啡。
        source_episode_ids: [ep1]
        confidence: explicit
        importance: 0.8
  - id: unavailable
    action: retrieve
    mirror_stub:
      index_mapped_nodes:
        - node_id: $seed_fact.fact_id
          node_type: fact
      candidates:
        - node_id: $seed_fact.fact_id
          node_type: fact
          score: 0.95
    graph_activation_stub:
      unavailable: true
    retrieve:
      query_text: nosqlitematch
      policy:
        use_mirror: true
  - id: degraded
    action: retrieve
    mirror_stub:
      index_mapped_nodes:
        - node_id: $seed_fact.fact_id
          node_type: fact
      candidates:
        - node_id: $seed_fact.fact_id
          node_type: fact
          score: 0.95
    graph_activation_stub:
      degraded: true
      fallback_reason: eval forced degradation
      candidates:
        - node_id: $seed_fact.fact_id
          node_type: fact
          score: 0.50
    retrieve:
      query_text: nosqlitematch
      policy:
        use_mirror: true
assertions:
  - type: graph_activation_candidate
    step: unavailable
    status: sidecar_error
  - type: memory_contains
    step: unavailable
    node_id: $seed_fact.fact_id
  - type: graph_activation_candidate
    step: degraded
    status: sidecar_degraded
  - type: memory_contains
    step: degraded
    node_id: $seed_fact.fact_id
`))
	if err != nil {
		t.Fatalf("load fixture: %v", err)
	}

	report := NewRunner(RunnerOptions{TempDir: t.TempDir()}).Run(context.Background(), fixture)
	if report.Failed() {
		t.Fatalf("run fixture: %s", report.Error())
	}
}

func TestRunnerReportsBadReferenceWithCaseID(t *testing.T) {
	fixture, err := LoadFixtureBytes([]byte(`
case_id: BAD_REF
steps:
  - id: retrieve
    action: retrieve
    retrieve:
      query_text: coffee
assertions:
  - type: memory_not_contains
    step: retrieve
    node_id: $missing.fact_id
`))
	if err != nil {
		t.Fatalf("load fixture: %v", err)
	}

	report := NewRunner(RunnerOptions{TempDir: t.TempDir()}).Run(context.Background(), fixture)
	if !report.Failed() {
		t.Fatal("report passed, want bad reference failure")
	}
	if !strings.Contains(report.Error(), "BAD_REF") || !strings.Contains(report.Error(), "$missing.fact_id") {
		t.Fatalf("report error = %q, want case id and missing ref", report.Error())
	}
}

func TestEvalRunnerMirrorStubMatchesSchema(t *testing.T) {
	tempDir := t.TempDir()
	fixture, err := LoadFixtureBytes([]byte(`
case_id: MIRROR_STUB_SCHEMA
steps:
  - id: retrieve
    action: retrieve
    mirror_stub:
      index_mapped_node_id: fact_mirror_stub
      index_mapped_type: fact
    retrieve:
      query_text: coffee
`))
	if err != nil {
		t.Fatalf("load fixture: %v", err)
	}

	report := NewRunner(RunnerOptions{TempDir: tempDir}).Run(context.Background(), fixture)
	if report.Failed() {
		t.Fatalf("run fixture: %v", report.Err)
	}

	db, err := sql.Open("sqlite", filepath.Join(tempDir, "MIRROR_STUB_SCHEMA.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	var status string
	var triviumType string
	if err := db.QueryRow(`
SELECT index_status, typeof(trivium_node_id)
FROM memory_index_map
WHERE node_id = ?`, "fact_mirror_stub").Scan(&status, &triviumType); err != nil {
		t.Fatalf("query mirror stub: %v", err)
	}
	if status != "indexed" {
		t.Fatalf("index_status = %q, want indexed", status)
	}
	if triviumType != "integer" {
		t.Fatalf("typeof(trivium_node_id) = %q, want integer", triviumType)
	}
}

func TestAssertionFailureIncludesExpectedAndActual(t *testing.T) {
	err := AssertionFailure{
		CaseID:    "ASSERT_FORMAT",
		Assertion: "memory_contains",
		Expected:  "node fact_01 present",
		Actual:    "no memory items",
	}

	message := err.Error()
	for _, want := range []string{"ASSERT_FORMAT", "memory_contains", "expected=node fact_01 present", "actual=no memory items"} {
		if !strings.Contains(message, want) {
			t.Fatalf("error = %q, want %q", message, want)
		}
	}
}

func TestAssertionsAcceptLegacySemanticBlockAliases(t *testing.T) {
	state := &runState{
		caseID: "BLOCK_ALIAS",
		steps: map[string]stepResult{
			"retrieve": {
				Retrieval: &memorycore.MemoryContext{
					Blocks: []memorycore.MemoryBlock{{
						BlockType: memorycore.MemoryBlockTypeRelevantCausalMemory,
						Items: []memorycore.MemoryContextItem{{
							NodeType: "fact",
							NodeID:   "fact_cause",
							Summary:  "用户因为早会而焦虑。",
						}},
					}},
				},
			},
		},
	}

	err := state.assertBlockContains(Assertion{
		Type:      "block_contains",
		Step:      "retrieve",
		BlockType: "causal_context",
		NodeID:    "fact_cause",
	}, true)
	if err != nil {
		t.Fatalf("assertBlockContains with legacy block alias: %v", err)
	}

	err = state.assertSelectedChainCorrect(Assertion{
		Type:      "selected_chain_correct",
		Step:      "retrieve",
		BlockType: "causal_context",
		NodeID:    "fact_cause",
	})
	if err != nil {
		t.Fatalf("assertSelectedChainCorrect with legacy block alias: %v", err)
	}
}

func TestReportDebugStringIncludesRetrievalDetails(t *testing.T) {
	occurredAt := time.Date(2026, 4, 28, 9, 0, 0, 0, time.FixedZone("CST", 8*60*60))
	report := Report{
		CaseID: "DEBUG_CASE",
		Steps: []StepReport{
			{
				ID:        "retrieve",
				Action:    "retrieve",
				QueryText: "为什么上午会回避重要任务",
				Retrieval: &memorycore.MemoryContext{
					QueryAnalysis: &memorycore.QueryAnalysis{
						TimeMode:      memorycore.QueryTimeModeCurrent,
						MemoryDomain:  memorycore.MemoryDomainUserProfile,
						MemoryAbility: memorycore.MemoryAbilityCausalExplain,
						EvidenceNeed:  memorycore.EvidenceNeedExactObservation,
						Diagnostics: &memorycore.QueryAnalysisDiagnostics{
							SemanticStatus:    "failed",
							FallbackReason:    "invalid_json",
							SemanticLatencyMs: 17,
						},
					},
					Blocks: []memorycore.MemoryBlock{
						{
							BlockType: "causal_context",
							Items: []memorycore.MemoryContextItem{
								{
									NodeType:         "fact",
									NodeID:           "fact_morning_task_avoidance",
									Summary:          "用户上午会回避重要任务。",
									HistoricalStatus: "current",
									SourceRefs: []memorycore.MemorySourceRef{
										{
											EpisodeID:     "ep_early_meeting_pressure",
											SessionID:     "s_seed",
											OccurredAt:    occurredAt,
											SourceStatus:  "visible",
											EvidenceCount: 1,
										},
									},
									RelatedFacts: []memorycore.MemoryRelatedFactRef{
										{
											NodeType:         "fact",
											NodeID:           "fact_dislikes_8am_meeting",
											LinkType:         "CAUSED_BY",
											Direction:        "outbound",
											HistoricalStatus: "current",
											Summary:          "用户不喜欢8点早会。",
										},
									},
								},
							},
						},
					},
				},
			},
		},
		Results: []AssertionResult{
			{Name: "causal link selected", Type: "selected_chain_correct"},
		},
	}

	debug := report.DebugString()
	for _, want := range []string{
		"case_id=DEBUG_CASE",
		"step=retrieve action=retrieve query=\"为什么上午会回避重要任务\"",
		"analysis time_mode=current domain=user_profile_memory ability=causal_explain evidence=exact_observation",
		"semantic_status=failed fallback=invalid_json semantic_latency_ms=17 query_analysis_invalid_json_count=1",
		"block=causal_context",
		"fact:fact_morning_task_avoidance status=current summary=\"用户上午会回避重要任务。\"",
		"source episode=ep_early_meeting_pressure session=s_seed",
		"related fact:fact_dislikes_8am_meeting link=CAUSED_BY direction=outbound status=current",
		"PASS causal link selected (selected_chain_correct)",
	} {
		if !strings.Contains(debug, want) {
			t.Fatalf("DebugString() =\n%s\nwant substring %q", debug, want)
		}
	}
}
