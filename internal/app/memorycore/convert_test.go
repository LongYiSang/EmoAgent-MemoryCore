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
