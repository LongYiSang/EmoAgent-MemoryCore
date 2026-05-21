package memorycore

import (
	"encoding/json"
	"strings"
	"testing"

	memsqlite "github.com/longyisang/emoagent-memorycore/internal/store/sqlite"
)

func TestQueryAnalysisFromStoreOmitsEmptySemanticFields(t *testing.T) {
	analysis := queryAnalysisFromStore(&memsqlite.QueryAnalysis{
		Raw:           "secret query",
		Normalized:    "secret query",
		TimeMode:      memsqlite.QueryTimeModeCurrent,
		MemoryDomain:  memsqlite.MemoryDomainRelationship,
		MemoryAbility: memsqlite.MemoryAbilityDirectFact,
		EvidenceNeed:  memsqlite.EvidenceNeedExactObservation,
		Source:        memsqlite.QueryAnalysisSourceRuleOnly,
	})
	if analysis == nil {
		t.Fatal("analysis = nil")
	}
	if analysis.QueryRewrites != nil || analysis.SemanticAnchors != nil || analysis.ContextBlockHints != nil || analysis.Diagnostics != nil {
		t.Fatalf("semantic fields = rewrites:%#v anchors:%#v hints:%#v diagnostics:%#v, want nil", analysis.QueryRewrites, analysis.SemanticAnchors, analysis.ContextBlockHints, analysis.Diagnostics)
	}

	raw, err := json.Marshal(analysis)
	if err != nil {
		t.Fatalf("marshal query analysis: %v", err)
	}
	jsonText := string(raw)
	for _, field := range []string{"QueryRewrites", "SemanticAnchors", "ContextBlockHints", "Diagnostics"} {
		if strings.Contains(jsonText, field) {
			t.Fatalf("json = %s, want %s omitted", jsonText, field)
		}
	}
	if strings.Contains(jsonText, "semantic-only-sensitive-text") {
		t.Fatalf("json = %s, unexpected manufactured semantic text", jsonText)
	}
}

func TestQueryAnalysisRewriteDropDiagnosticsMapToPublicDTO(t *testing.T) {
	analysis := queryAnalysisFromStore(&memsqlite.QueryAnalysis{
		Raw:           "我喜欢Laufey这件事是从哪里知道的？",
		Normalized:    "我喜欢laufey这件事是从哪里知道的？",
		TimeMode:      memsqlite.QueryTimeModeCurrent,
		MemoryDomain:  memsqlite.MemoryDomainUserProfile,
		MemoryAbility: memsqlite.MemoryAbilityProvenance,
		EvidenceNeed:  memsqlite.EvidenceNeedProvenanceSource,
		Source:        memsqlite.QueryAnalysisSourceMerged,
		QueryRewrites: []memsqlite.QueryRewrite{{
			Text:    "用户喜欢Laufey的来源",
			Purpose: "semantic_recall",
			Weight:  0.7,
		}},
		Diagnostics: &memsqlite.QueryAnalysisDiagnostics{
			SemanticStatus:          "ok",
			RewriteCount:            1,
			DroppedRewriteCount:     1,
			DroppedRewriteReasons:   []string{"rewrite_language_mismatch"},
			EnglishRewriteCount:     2,
			ScorerVersion:           "rule_confidence_legacy.v0",
			RuleConfidenceLegacy:    0.42,
			RuleConfidenceReason:    "exact_fact_only",
			SemanticDecisionLegacy:  true,
			MinConfidenceToOverride: 0.72,
			Signals:                 []string{string(memsqlite.QuerySignalExactFact)},
			EntityMentionCount:      1,
		},
	})
	if analysis == nil || analysis.Diagnostics == nil {
		t.Fatalf("analysis diagnostics = %#v, want populated diagnostics", analysis)
	}
	if analysis.Diagnostics.DroppedRewriteCount != 1 {
		t.Fatalf("dropped rewrite count = %d, want 1", analysis.Diagnostics.DroppedRewriteCount)
	}
	if analysis.Diagnostics.EnglishRewriteCount != 2 {
		t.Fatalf("English rewrite count = %d, want 2", analysis.Diagnostics.EnglishRewriteCount)
	}
	if len(analysis.Diagnostics.DroppedRewriteReasons) != 1 || analysis.Diagnostics.DroppedRewriteReasons[0] != "rewrite_language_mismatch" {
		t.Fatalf("dropped rewrite reasons = %#v, want rewrite_language_mismatch", analysis.Diagnostics.DroppedRewriteReasons)
	}
	if analysis.Diagnostics.ScorerVersion != "rule_confidence_legacy.v0" ||
		analysis.Diagnostics.RuleConfidenceLegacy != 0.42 ||
		analysis.Diagnostics.RuleConfidenceReason != "exact_fact_only" ||
		!analysis.Diagnostics.SemanticDecisionLegacy ||
		analysis.Diagnostics.MinConfidenceToOverride != 0.72 ||
		analysis.Diagnostics.EntityMentionCount != 1 {
		t.Fatalf("legacy diagnostics = %#v, want mapped legacy fields", analysis.Diagnostics)
	}
	if len(analysis.Diagnostics.Signals) != 1 || analysis.Diagnostics.Signals[0] != string(memsqlite.QuerySignalExactFact) {
		t.Fatalf("diagnostic signals = %#v, want exact_fact", analysis.Diagnostics.Signals)
	}

	raw, err := json.Marshal(analysis)
	if err != nil {
		t.Fatalf("marshal query analysis: %v", err)
	}
	jsonText := string(raw)
	if strings.Contains(jsonText, "when did the user say they like Laufey") {
		t.Fatalf("json leaked dropped rewrite text: %s", jsonText)
	}
	if !strings.Contains(jsonText, "rewrite_language_mismatch") {
		t.Fatalf("json = %s, want public drop reason", jsonText)
	}
}
