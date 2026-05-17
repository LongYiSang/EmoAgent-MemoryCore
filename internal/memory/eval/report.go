package eval

import (
	"fmt"
	"strings"

	"github.com/longyisang/emoagent-memorycore/internal/app/memorycore"
)

type Report struct {
	CaseID         string
	MirrorArtifact MirrorArtifactReport
	Steps          []StepReport
	Results        []AssertionResult
	Err            error
}

type StepReport struct {
	ID        string
	Action    string
	QueryText string
	Retrieval *memorycore.MemoryContext
}

type AssertionResult struct {
	Name string
	Type string
	Err  error
}

type AssertionFailure struct {
	CaseID    string
	Assertion string
	Expected  string
	Actual    string
}

func (e AssertionFailure) Error() string {
	return fmt.Sprintf("case_id=%s assertion=%s expected=%s actual=%s", e.CaseID, e.Assertion, e.Expected, e.Actual)
}

func (r Report) Failed() bool {
	if r.Err != nil {
		return true
	}
	for _, result := range r.Results {
		if result.Err != nil {
			return true
		}
	}
	return false
}

func (r Report) Error() string {
	var parts []string
	if r.Err != nil {
		parts = append(parts, fmt.Sprintf("case_id=%s error=%v", r.CaseID, r.Err))
	}
	for _, result := range r.Results {
		if result.Err == nil {
			continue
		}
		name := result.Name
		if name == "" {
			name = result.Type
		}
		parts = append(parts, fmt.Sprintf("case_id=%s assertion=%s error=%v", r.CaseID, name, result.Err))
	}
	return strings.Join(parts, "\n")
}

func (r Report) DebugString() string {
	var b strings.Builder
	fmt.Fprintf(&b, "case_id=%s\n", r.CaseID)
	if r.Err != nil {
		fmt.Fprintf(&b, "error=%v\n", r.Err)
	}
	if len(r.Steps) > 0 {
		b.WriteString("steps:\n")
		for _, step := range r.Steps {
			writeStepDebug(&b, step)
		}
	}
	if len(r.Results) > 0 {
		b.WriteString("assertions:\n")
		for _, result := range r.Results {
			name := result.Name
			if name == "" {
				name = result.Type
			}
			if result.Err != nil {
				fmt.Fprintf(&b, "  FAIL %s (%s): %v\n", name, result.Type, result.Err)
				continue
			}
			fmt.Fprintf(&b, "  PASS %s (%s)\n", name, result.Type)
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func writeStepDebug(b *strings.Builder, step StepReport) {
	fmt.Fprintf(b, "  step=%s action=%s", step.ID, step.Action)
	if step.QueryText != "" {
		fmt.Fprintf(b, " query=%q", step.QueryText)
	}
	b.WriteString("\n")
	if step.Retrieval == nil {
		return
	}
	retrieval := step.Retrieval
	if retrieval.QueryAnalysis != nil {
		analysis := retrieval.QueryAnalysis
		fmt.Fprintf(
			b,
			"    analysis time_mode=%s domain=%s ability=%s evidence=%s",
			analysis.TimeMode,
			analysis.MemoryDomain,
			analysis.MemoryAbility,
			analysis.EvidenceNeed,
		)
		if len(analysis.Signals) > 0 {
			fmt.Fprintf(b, " signals=%s", strings.Join(querySignalsToStrings(analysis.Signals), ","))
		}
		if len(analysis.EntityMentions) > 0 {
			fmt.Fprintf(b, " entities=%s", strings.Join(queryEntitiesToStrings(analysis.EntityMentions), ","))
		}
		b.WriteString("\n")
	}
	fmt.Fprintf(b, "    token_estimate=%d\n", retrieval.TokenEstimate)
	for _, block := range retrieval.Blocks {
		fmt.Fprintf(b, "    block=%s items=%d\n", block.BlockType, len(block.Items))
		for _, item := range block.Items {
			writeItemDebug(b, item)
		}
	}
	if len(retrieval.DoNotMention) > 0 {
		b.WriteString("    do_not_mention:\n")
		for _, item := range retrieval.DoNotMention {
			fmt.Fprintf(b, "      %s:%s reason=%s\n", item.NodeType, item.NodeID, item.Reason)
		}
	}
	if retrieval.Mirror != nil {
		fmt.Fprintf(b, "    mirror status=%s candidates=%d\n", retrieval.Mirror.Status, len(retrieval.Mirror.Candidates))
	}
	if retrieval.GraphActivation != nil {
		fmt.Fprintf(b, "    graph_activation status=%s candidates=%d\n", retrieval.GraphActivation.Status, len(retrieval.GraphActivation.Candidates))
	}
	if retrieval.Rerank != nil {
		fmt.Fprintf(b, "    rerank status=%s safe_candidates=%d results=%d\n", retrieval.Rerank.Status, retrieval.Rerank.SafeCandidateCount, retrieval.Rerank.ResultCount)
	}
	if retrieval.AnchorFusion != nil {
		fmt.Fprintf(b, "    anchor_fusion seeds=%d\n", len(retrieval.AnchorFusion.Seeds))
	}
}

func writeItemDebug(b *strings.Builder, item memorycore.MemoryContextItem) {
	fmt.Fprintf(
		b,
		"      %s:%s status=%s summary=%q",
		item.NodeType,
		item.NodeID,
		item.HistoricalStatus,
		item.Summary,
	)
	if item.UsageGuidance != "" {
		fmt.Fprintf(b, " usage=%q", item.UsageGuidance)
	}
	if item.DoNotOverstate {
		b.WriteString(" do_not_overstate=true")
	}
	b.WriteString("\n")
	for _, source := range item.SourceRefs {
		fmt.Fprintf(
			b,
			"        source episode=%s session=%s status=%s evidence_count=%d occurred_at=%s\n",
			source.EpisodeID,
			source.SessionID,
			source.SourceStatus,
			source.EvidenceCount,
			source.OccurredAt.Format("2006-01-02T15:04:05Z07:00"),
		)
	}
	for _, related := range item.RelatedFacts {
		fmt.Fprintf(
			b,
			"        related %s:%s link=%s direction=%s status=%s summary=%q\n",
			related.NodeType,
			related.NodeID,
			related.LinkType,
			related.Direction,
			related.HistoricalStatus,
			related.Summary,
		)
	}
}

func querySignalsToStrings(values []memorycore.QuerySignal) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, string(value))
	}
	return out
}

func queryEntitiesToStrings(values []memorycore.QueryEntityMention) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value.Alias != "" {
			out = append(out, value.EntityID+"("+string(value.MatchKind)+":"+value.Alias+")")
			continue
		}
		out = append(out, value.EntityID+"("+string(value.MatchKind)+":"+value.MatchText+")")
	}
	return out
}
