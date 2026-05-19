package eval

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestQueryAnalysisFixtureAcceptsControlledSemanticStubFields(t *testing.T) {
	fixture, err := LoadFixtureBytes([]byte(`
schema_version: memory_eval.v0.2
suite: controlled_phase6
allow_stub: true
case_id: QA_SEMANTIC_STUB_FIELDS
steps:
  - id: retrieve
    action: retrieve
    semantic_query_analysis_stub:
      status: ok
      provider: eval_semantic_stub
      model: stub-v1
      prompt_version: phase6-test
      analysis:
        time_mode: current
        memory_domain: relationship_memory
        memory_ability: provenance
        evidence_need: provenance_source
        confidence: 0.92
        query_rewrites:
          - text: semantic rewrite target
            purpose: provenance_dense
            weight: 0.8
        context_block_hints: [provenance_memory]
    retrieve:
      query_text: where did I say this
      fusion_mode: weighted_rrf_support
assertions:
  - type: query_analysis
    step: retrieve
    source: merged
    status: ok
    query_rewrites: [semantic rewrite target]
    context_block_hints: [provenance_memory]
`))
	if err != nil {
		t.Fatalf("LoadFixtureBytes err = %v, want nil", err)
	}
	if !fixture.UsesEvalStub() {
		t.Fatal("UsesEvalStub = false, want true for semantic_query_analysis_stub")
	}
	if err := fixture.ValidateStubPolicy(FixtureStubPolicyForbid); err == nil || !strings.Contains(err.Error(), "semantic_query_analysis_stub") {
		t.Fatalf("ValidateStubPolicy forbid err = %v, want semantic stub policy error", err)
	}
	if got := fixture.Steps[0].Retrieve.FusionMode; got != "weighted_rrf_support" {
		t.Fatalf("fusion_mode = %q, want weighted_rrf_support", got)
	}
}

func TestQueryAnalysisEvalRunnerSemanticStubFallbackDiagnostics(t *testing.T) {
	fixture, err := LoadFixtureBytes([]byte(`
case_id: QA_SEMANTIC_FALLBACK
allow_stub: true
steps:
  - id: retrieve
    action: retrieve
    semantic_query_analysis_stub:
      status: error
      fallback_reason: semantic_unavailable
      provider: eval_semantic_stub
      model: stub-v1
      prompt_version: phase6-test
    retrieve:
      query_text: coffee
assertions:
  - type: query_analysis
    name: semantic fallback is diagnostic only
    step: retrieve
    source: semantic_failed_rule_fallback
    status: failed
    fallback_reason: semantic_unavailable
`))
	if err != nil {
		t.Fatalf("load fixture: %v", err)
	}

	report := NewRunner(RunnerOptions{TempDir: t.TempDir()}).Run(context.Background(), fixture)
	if report.Failed() {
		t.Fatalf("run fixture:\n%s", report.DebugString())
	}
	debug := report.DebugString()
	for _, want := range []string{
		"source=semantic_failed_rule_fallback",
		"semantic_status=failed",
		"fallback=semantic_unavailable",
	} {
		if !strings.Contains(debug, want) {
			t.Fatalf("DebugString() =\n%s\nwant substring %q", debug, want)
		}
	}
}

func TestQueryAnalysisEvalRunnerSemanticRewritesFlowIntoMirrorDiagnostics(t *testing.T) {
	fixture, err := LoadFixtureBytes([]byte(`
case_id: QA_SEMANTIC_REWRITE_MIRROR
allow_stub: true
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
      content: User said the migration checklist should use sqlite first.
      occurred_at: "2026-04-28T09:00:00+08:00"
steps:
  - id: fact
    action: fact
    fact:
      id: fact_sqlite_checklist
      subject_entity_id: user
      predicate: prefers
      object_literal: sqlite first migration checklist
      content_summary: User prefers sqlite-first migration checklists.
      source_episode_ids: [ep1]
      confidence: explicit
      importance: 0.9
  - id: retrieve
    action: retrieve
    semantic_query_analysis_stub:
      status: ok
      provider: eval_semantic_stub
      model: stub-v1
      prompt_version: phase6-test
      analysis:
        time_mode: current
        memory_domain: relationship_memory
        memory_ability: provenance
        evidence_need: provenance_source
        confidence: 0.95
        query_rewrites:
          - text: sqlite first migration checklist
            purpose: provenance_dense
            weight: 0.9
        context_block_hints: [provenance_memory]
    mirror_stub:
      index_mapped_nodes:
        - node_id: $fact.fact_id
          node_type: fact
      candidates:
        - node_id: $fact.fact_id
          node_type: fact
          score: 0.98
          source: eval_dense
          rank: 1
    retrieve:
      session_id: s1
      query_text: where did I mention the migration checklist
      policy:
        use_mirror: true
        use_fts: false
        final_memory_count: 1
assertions:
  - type: query_analysis
    step: retrieve
    source: merged
    status: ok
    memory_ability: provenance
    evidence_need: provenance_source
    query_rewrites: [sqlite first migration checklist]
    context_block_hints: [provenance_memory]
  - type: mirror_candidate
    step: retrieve
    status: used
    query_count: 2
    raw_query_count: 1
    rewrite_query_count: 1
  - type: memory_contains
    step: retrieve
    node_id: $fact.fact_id
`))
	if err != nil {
		t.Fatalf("load fixture: %v", err)
	}

	report := NewRunner(RunnerOptions{TempDir: t.TempDir()}).Run(context.Background(), fixture)
	if report.Failed() {
		t.Fatalf("run fixture:\n%s", report.DebugString())
	}
	debug := report.DebugString()
	for _, want := range []string{
		"rewrites=sqlite first migration checklist",
		"hints=provenance_memory",
		"mirror status=used candidates=1 query_count=2 raw=1 rewrites=1",
	} {
		if !strings.Contains(debug, want) {
			t.Fatalf("DebugString() =\n%s\nwant substring %q", debug, want)
		}
	}
}

func TestQueryAnalysisEvalRunnerSemanticStubIsStepScoped(t *testing.T) {
	fixture, err := LoadFixtureBytes([]byte(`
case_id: QA_SEMANTIC_STEP_SCOPED
allow_stub: true
steps:
  - id: stubbed
    action: retrieve
    semantic_query_analysis_stub:
      status: error
      fallback_reason: semantic_unavailable
      provider: eval_semantic_stub
    retrieve:
      query_text: coffee fallback
  - id: rule_only
    action: retrieve
    retrieve:
      query_text: coffee rule only
assertions:
  - type: query_analysis
    step: stubbed
    source: semantic_failed_rule_fallback
    status: failed
    fallback_reason: semantic_unavailable
  - type: query_analysis
    step: rule_only
    source: rule_only
`))
	if err != nil {
		t.Fatalf("load fixture: %v", err)
	}

	report := NewRunner(RunnerOptions{TempDir: t.TempDir()}).Run(context.Background(), fixture)
	if report.Failed() {
		t.Fatalf("run fixture:\n%s", report.DebugString())
	}
}

func TestQueryAnalysisEvalFusionModeChangesControlledMirrorBehavior(t *testing.T) {
	fixture, err := LoadFixtureBytes([]byte(`
case_id: QA_FUSION_MODE_BEHAVIOR
allow_stub: true
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
      content: User likes latte and keeps a latte calibration checklist.
      occurred_at: "2026-04-28T09:00:00+08:00"
steps:
  - id: preference
    action: fact
    fact:
      id: fact_latte_preference
      subject_entity_id: user
      predicate: likes
      object_literal: latte
      content_summary: User likes latte.
      source_episode_ids: [ep1]
      confidence: explicit
      importance: 0.8
  - id: calibration
    action: fact
    fact:
      id: fact_latte_calibration
      subject_entity_id: user
      predicate: keeps
      object_literal: latte calibration checklist
      content_summary: User keeps a latte calibration checklist.
      source_episode_ids: [ep1]
      confidence: explicit
      importance: 0.6
  - id: rebuild_search
    action: rebuild_search
    rebuild_search: {}
  - id: max_only
    action: retrieve
    semantic_query_analysis_stub:
      status: ok
      provider: eval_semantic_stub
      analysis:
        time_mode: current
        memory_domain: relationship_memory
        memory_ability: direct_fact
        evidence_need: exact_observation
        confidence: 0.9
        query_rewrites:
          - text: latte calibration checklist
            purpose: dense_recall
            weight: 0.8
    mirror_stub:
      index_mapped_nodes:
        - node_id: $calibration.fact_id
          node_type: fact
      candidates:
        - node_id: $calibration.fact_id
          node_type: fact
          score: 0.97
          source: eval_dense
          rank: 1
    retrieve:
      session_id: s1
      query_text: latte calibration
      fusion_mode: max_only
      policy:
        use_mirror: true
        use_fts: false
        final_memory_count: 1
  - id: weighted_rrf
    action: retrieve
    semantic_query_analysis_stub:
      status: ok
      provider: eval_semantic_stub
      analysis:
        time_mode: current
        memory_domain: relationship_memory
        memory_ability: direct_fact
        evidence_need: exact_observation
        confidence: 0.9
        query_rewrites:
          - text: latte calibration checklist
            purpose: dense_recall
            weight: 0.8
    mirror_stub:
      index_mapped_nodes:
        - node_id: $calibration.fact_id
          node_type: fact
      candidates:
        - node_id: $calibration.fact_id
          node_type: fact
          score: 0.97
          source: eval_dense
          rank: 1
    retrieve:
      session_id: s1
      query_text: latte calibration
      fusion_mode: weighted_rrf_support
      policy:
        use_mirror: true
        use_fts: false
        final_memory_count: 1
assertions:
  - type: memory_contains
    step: max_only
    node_id: $preference.fact_id
  - type: memory_not_contains
    step: max_only
    node_id: $calibration.fact_id
  - type: mirror_candidate
    step: max_only
    status: no_candidates
    query_count: 2
    raw_query_count: 1
    rewrite_query_count: 1
  - type: memory_contains
    step: weighted_rrf
    node_id: $calibration.fact_id
  - type: mirror_candidate
    step: weighted_rrf
    status: used
    query_count: 2
    raw_query_count: 1
    rewrite_query_count: 1
  - type: ablation_improves
    step: weighted_rrf
    compare_step: max_only
    relevant_node_ids: [$calibration.fact_id]
    at: 1
`))
	if err != nil {
		t.Fatalf("load fixture: %v", err)
	}

	report := NewRunner(RunnerOptions{TempDir: t.TempDir()}).Run(context.Background(), fixture)
	if report.Failed() {
		t.Fatalf("run fixture:\n%s", report.DebugString())
	}
}

func TestQueryAnalysisEvalRunnerAssertsDroppedEnglishRewriteDiagnostics(t *testing.T) {
	fixture, err := LoadFixtureBytes([]byte(`
case_id: QA_DROP_ENGLISH_REWRITE_ASSERTIONS
allow_stub: true
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
      content: 用户喜欢手冲咖啡。
      occurred_at: "2026-04-28T09:00:00+08:00"
steps:
  - id: coffee_fact
    action: fact
    fact:
      id: fact_pour_over
      subject_entity_id: user
      predicate: likes
      object_literal: 手冲咖啡
      content_summary: 用户喜欢手冲咖啡。
      source_episode_ids: [ep1]
      confidence: explicit
      importance: 0.8
  - id: retrieve
    action: retrieve
    semantic_query_analysis_stub:
      status: ok
      provider: eval_semantic_stub
      analysis:
        time_mode: current
        memory_domain: user_profile_memory
        memory_ability: direct_fact
        evidence_need: exact_observation
        confidence: 0.95
        query_rewrites:
          - text: when did the user say they enjoy pour over coffee in the morning
            purpose: semantic_recall
            weight: 0.8
          - text: 用户喜欢手冲咖啡
            purpose: semantic_recall
            weight: 0.7
    mirror_stub:
      index_mapped_nodes:
        - node_id: $coffee_fact.fact_id
          node_type: fact
      candidates:
        - node_id: $coffee_fact.fact_id
          node_type: fact
          score: 0.99
          source: semantic_rewrite_dense
          rank: 1
    retrieve:
      session_id: s1
      query_text: 我是不是喜欢手冲咖啡
      policy:
        use_mirror: true
        use_fts: false
        final_memory_count: 1
assertions:
  - type: query_analysis
    step: retrieve
    source: merged
    status: ok
    query_rewrites: [用户喜欢手冲咖啡]
    dropped_rewrite_count: 1
    dropped_rewrite_reasons: [rewrite_language_mismatch]
    english_rewrite_count: 1
  - type: mirror_candidate
    step: retrieve
    status: used
    query_count: 2
    raw_query_count: 1
    rewrite_query_count: 1
  - type: memory_contains
    step: retrieve
    node_id: $coffee_fact.fact_id
`))
	if err != nil {
		t.Fatalf("load fixture: %v", err)
	}

	report := NewRunner(RunnerOptions{TempDir: t.TempDir()}).Run(context.Background(), fixture)
	if report.Failed() {
		t.Fatalf("run fixture:\n%s", report.DebugString())
	}
	debug := report.DebugString()
	for _, want := range []string{
		"english_rewrite_count=1",
		"dropped_rewrite_count=1",
		"dropped_rewrite_reasons=rewrite_language_mismatch",
	} {
		if !strings.Contains(debug, want) {
			t.Fatalf("DebugString() =\n%s\nwant substring %q", debug, want)
		}
	}
	if strings.Contains(debug, "when did the user say they enjoy pour over coffee") {
		t.Fatalf("DebugString leaked dropped English rewrite:\n%s", debug)
	}
}

func TestControlledPhase6CompletionPremiseNarrativeChainFixtures(t *testing.T) {
	for _, name := range []string{
		"RET_historical_supersedes_completion_q004_style.yaml",
		"RET_causal_completion_q015_style.yaml",
		"RET_premise_counterexample_q011_style.yaml",
		"RET_relationship_narrative_source_completion_q020_style.yaml",
	} {
		name := name
		t.Run(name, func(t *testing.T) {
			runFixtureFile(t, filepath.Join("controlled", "phase6", name))
		})
	}
}

func TestControlledPhase6QueryAnalysisFixtures(t *testing.T) {
	for _, name := range []string{
		"QA_drop_english_rewrite_for_chinese_query.yaml",
		"QA_state_transition_clamp_q009_style.yaml",
	} {
		name := name
		t.Run(name, func(t *testing.T) {
			runFixtureFile(t, filepath.Join("controlled", "phase6", name))
		})
	}
}
