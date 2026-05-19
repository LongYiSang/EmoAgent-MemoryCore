package eval

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/longyisang/emoagent-memorycore/internal/app/memorycore"
)

type QualityBenchmarkMode string

const (
	QualityBenchmarkModeBrief QualityBenchmarkMode = "brief"
	QualityBenchmarkModeFull  QualityBenchmarkMode = "full"
)

type QualityBenchmarkReportOptions struct {
	Mode QualityBenchmarkMode
}

type QualityBenchmarkCase struct {
	Path    string
	Fixture *Fixture
	Report  Report
}

type qualityAssertionResult struct {
	assertion Assertion
	result    AssertionResult
}

type qualityNode struct {
	nodeType string
	nodeID   string
	content  string
}

func FormatQualityBenchmarkReport(cases []QualityBenchmarkCase, opts QualityBenchmarkReportOptions) string {
	mode := opts.Mode
	if mode == "" {
		mode = QualityBenchmarkModeBrief
	}

	var b strings.Builder
	stats := qualityStats(cases)
	fmt.Fprintf(&b, "quality_benchmark_report\n")
	fmt.Fprintf(&b, "mode: %s\n", mode)
	fmt.Fprintf(
		&b,
		"summary: fixtures=%d questions=%d assertions=%d failures=%d\n",
		stats.fixtures,
		stats.questions,
		stats.assertions,
		stats.failures,
	)

	wroteQuestion := false
	for _, item := range cases {
		fixture := item.Fixture
		report := item.Report
		caseID := report.CaseID
		if fixture != nil && fixture.CaseID != "" {
			caseID = fixture.CaseID
		}
		if report.Err != nil {
			if b.Len() > 0 {
				b.WriteString("\n")
			}
			fmt.Fprintf(&b, "case_id: %s\n", caseID)
			if item.Path != "" {
				fmt.Fprintf(&b, "path: %s\n", item.Path)
			}
			b.WriteString("结果:\n")
			fmt.Fprintf(&b, "  FAIL case_error: %v\n", report.Err)
			wroteQuestion = true
			continue
		}
		if fixture == nil {
			continue
		}
		catalog := newQualityNodeCatalog(fixture)
		stepReports := qualityStepReports(report)
		assertions := qualityAssertionsByStep(fixture, report)
		for _, step := range fixture.Steps {
			if step.Action != "retrieve" || step.Retrieve == nil {
				continue
			}
			stepAssertions := assertions[step.ID]
			if mode == QualityBenchmarkModeBrief && !qualityQuestionFailed(stepAssertions) {
				continue
			}
			if b.Len() > 0 {
				b.WriteString("\n")
			}
			fmt.Fprintf(&b, "case_id: %s\n", fixture.CaseID)
			if item.Path != "" {
				fmt.Fprintf(&b, "path: %s\n", item.Path)
			}
			fmt.Fprintf(&b, "question_id: %s\n", step.ID)
			fmt.Fprintf(&b, "问题: %s\n", step.Retrieve.QueryText)
			b.WriteString("期望:\n")
			if len(stepAssertions) == 0 {
				b.WriteString("  - (no assertions)\n")
			}
			for _, assertion := range stepAssertions {
				writeQualityExpectation(&b, assertion.assertion, catalog)
			}
			b.WriteString("结果:\n")
			for _, assertion := range stepAssertions {
				writeQualityAssertionResult(&b, assertion.result)
			}
			stepReport, ok := stepReports[step.ID]
			if ok {
				writeQualityRetrievalResult(&b, stepReport)
			} else {
				b.WriteString("  selected: (missing retrieve step report)\n")
			}
			wroteQuestion = true
		}
	}
	if !wroteQuestion && mode == QualityBenchmarkModeBrief {
		b.WriteString("\n未发现失败结果。使用 --mode full 查看全部问题、期望和结果。\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

type benchmarkStats struct {
	fixtures   int
	questions  int
	assertions int
	failures   int
}

func qualityStats(cases []QualityBenchmarkCase) benchmarkStats {
	stats := benchmarkStats{fixtures: len(cases)}
	for _, item := range cases {
		if item.Fixture != nil {
			for _, step := range item.Fixture.Steps {
				if step.Action == "retrieve" && step.Retrieve != nil {
					stats.questions++
				}
			}
			stats.assertions += len(item.Fixture.Assertions)
		} else {
			stats.assertions += len(item.Report.Results)
		}
		if item.Report.Err != nil {
			stats.failures++
		}
		for _, result := range item.Report.Results {
			if result.Err != nil {
				stats.failures++
			}
		}
	}
	return stats
}

func newQualityNodeCatalog(fixture *Fixture) map[string]qualityNode {
	catalog := map[string]qualityNode{}
	add := func(keys []string, node qualityNode) {
		for _, key := range keys {
			key = strings.TrimSpace(strings.TrimPrefix(key, "$"))
			if key == "" {
				continue
			}
			catalog[key] = node
		}
	}
	for _, episode := range fixture.Seed.Episodes {
		node := qualityNode{nodeType: "episode", nodeID: episode.ID, content: episode.Content}
		add([]string{episode.ID, "episode." + episode.ID + ".id"}, node)
	}
	for _, entity := range fixture.Seed.Entities {
		node := qualityNode{nodeType: "entity", nodeID: entity.ID, content: entity.CanonicalName}
		add([]string{entity.ID, "entity." + entity.ID + ".id"}, node)
	}
	for _, step := range fixture.Steps {
		if step.Fact == nil {
			continue
		}
		factID := strings.TrimSpace(step.Fact.ID)
		if factID == "" {
			factID = step.ID
		}
		node := qualityNode{nodeType: "fact", nodeID: factID, content: step.Fact.ContentSummary}
		add([]string{factID, step.ID, step.ID + ".fact_id", "fact." + factID + ".id"}, node)
	}
	return catalog
}

func qualityStepReports(report Report) map[string]StepReport {
	out := map[string]StepReport{}
	for _, step := range report.Steps {
		out[step.ID] = step
	}
	return out
}

func qualityAssertionsByStep(fixture *Fixture, report Report) map[string][]qualityAssertionResult {
	out := map[string][]qualityAssertionResult{}
	for index, assertion := range fixture.Assertions {
		result := AssertionResult{Name: assertion.Name, Type: assertion.Type}
		if index < len(report.Results) {
			result = report.Results[index]
		} else {
			result.Err = fmt.Errorf("missing assertion result")
		}
		out[assertion.Step] = append(out[assertion.Step], qualityAssertionResult{
			assertion: assertion,
			result:    result,
		})
	}
	return out
}

func qualityQuestionFailed(assertions []qualityAssertionResult) bool {
	for _, assertion := range assertions {
		if assertion.result.Err != nil {
			return true
		}
	}
	return false
}

func writeQualityExpectation(b *strings.Builder, assertion Assertion, catalog map[string]qualityNode) {
	name := assertion.Name
	if name == "" {
		name = assertion.Type
	}
	fmt.Fprintf(b, "  - [%s] %s", assertion.Type, name)
	if details := qualityExpectationDetails(assertion); details != "" {
		fmt.Fprintf(b, ": %s", details)
	}
	b.WriteString("\n")
	nodes := qualityAssertionNodes(assertion, catalog)
	if len(nodes) == 0 {
		return
	}
	b.WriteString("    content:\n")
	for _, node := range nodes {
		fmt.Fprintf(b, "      %s:%s %s\n", node.nodeType, node.nodeID, node.content)
	}
}

func qualityExpectationDetails(assertion Assertion) string {
	var details []string
	appendIDs := func(label string, values []string) {
		values = qualityCleanRefs(values)
		if len(values) > 0 {
			details = append(details, fmt.Sprintf("%s=%s", label, strings.Join(values, ",")))
		}
	}
	appendString := func(label string, value string) {
		if strings.TrimSpace(value) != "" {
			details = append(details, fmt.Sprintf("%s=%s", label, value))
		}
	}
	appendInt := func(label string, value int) {
		if value != 0 {
			details = append(details, fmt.Sprintf("%s=%d", label, value))
		}
	}
	appendFloat := func(label string, value float64) {
		if value != 0 {
			details = append(details, fmt.Sprintf("%s=%g", label, value))
		}
	}

	appendIDs("relevant_node_ids", assertion.RelevantNodeIDs)
	appendString("node_id", qualityCleanRef(assertion.NodeID))
	appendIDs("node_ids", assertion.NodeIDs)
	appendIDs("forbidden_node_ids", assertion.ForbiddenNodeIDs)
	appendString("block_type", assertion.BlockType)
	appendString("summary", assertion.Summary)
	appendString("time_mode", assertion.TimeMode)
	appendString("memory_domain", assertion.MemoryDomain)
	appendString("memory_ability", assertion.MemoryAbility)
	appendString("evidence_need", assertion.EvidenceNeed)
	appendString("link_type", assertion.LinkType)
	appendString("direction", assertion.Direction)
	appendString("historical_status", assertion.HistoricalStatus)
	appendString("related_historical_status", assertion.RelatedHistoricalStatus)
	appendInt("source_ref_count", assertion.SourceRefCount)
	appendString("status", assertion.Status)
	appendString("source", assertion.Source)
	appendString("fallback_reason", assertion.FallbackReason)
	appendString("action", assertion.Action)
	appendString("equals", assertion.Equals)
	appendInt("at", assertion.At)
	appendFloat("min", assertion.Min)
	appendFloat("max", assertion.Max)
	if len(assertion.ForbiddenContains) > 0 {
		details = append(details, "forbidden_contains="+strings.Join(assertion.ForbiddenContains, ","))
	}
	if len(assertion.QueryRewrites) > 0 {
		details = append(details, "query_rewrites="+strings.Join(assertion.QueryRewrites, ","))
	}
	if len(assertion.ContextBlockHints) > 0 {
		details = append(details, "context_block_hints="+strings.Join(assertion.ContextBlockHints, ","))
	}
	appendInt("query_count", assertion.QueryCount)
	appendInt("raw_query_count", assertion.RawQueryCount)
	appendInt("rewrite_query_count", assertion.RewriteQueryCount)
	appendInt("anchor_query_count", assertion.AnchorQueryCount)
	return strings.Join(details, " ")
}

func qualityAssertionNodes(assertion Assertion, catalog map[string]qualityNode) []qualityNode {
	var refs []string
	refs = append(refs, assertion.NodeID)
	refs = append(refs, assertion.NodeIDs...)
	refs = append(refs, assertion.RelevantNodeIDs...)
	refs = append(refs, assertion.ForbiddenNodeIDs...)
	refs = append(refs, assertion.FromNodeID, assertion.ToNodeID, assertion.EpisodeID)

	seen := map[string]struct{}{}
	var out []qualityNode
	for _, ref := range refs {
		ref = strings.TrimSpace(strings.TrimPrefix(ref, "$"))
		if ref == "" {
			continue
		}
		node, ok := catalog[ref]
		if !ok {
			node = qualityNode{nodeType: qualityNodeTypeFromRef(ref), nodeID: qualityCleanRef(ref)}
		}
		key := node.nodeType + ":" + node.nodeID
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, node)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].nodeType+":"+out[i].nodeID < out[j].nodeType+":"+out[j].nodeID
	})
	return out
}

func qualityNodeTypeFromRef(ref string) string {
	if strings.HasPrefix(ref, "episode.") {
		return "episode"
	}
	if strings.HasPrefix(ref, "entity.") {
		return "entity"
	}
	return "node"
}

func writeQualityAssertionResult(b *strings.Builder, result AssertionResult) {
	name := result.Name
	if name == "" {
		name = result.Type
	}
	if result.Err == nil {
		fmt.Fprintf(b, "  PASS [%s] %s\n", result.Type, name)
		return
	}
	fmt.Fprintf(b, "  FAIL [%s] %s", result.Type, name)
	var failure AssertionFailure
	if errors.As(result.Err, &failure) {
		fmt.Fprintf(b, ": expected=%s actual=%s\n", failure.Expected, failure.Actual)
		return
	}
	fmt.Fprintf(b, ": error=%v\n", result.Err)
}

func writeQualityRetrievalResult(b *strings.Builder, step StepReport) {
	retrieval := step.Retrieval
	if retrieval == nil {
		b.WriteString("  selected: (nil retrieval)\n")
		return
	}
	if step.FusionMode != "" {
		fmt.Fprintf(b, "  fusion_mode: %s\n", step.FusionMode)
	}
	if retrieval.QueryAnalysis != nil {
		writeQualityAnalysis(b, retrieval.QueryAnalysis)
	}
	if retrieval.Mirror != nil {
		fmt.Fprintf(
			b,
			"  mirror: status=%s query_count=%d raw_query_count=%d rewrite_query_count=%d anchor_query_count=%d candidates=%d\n",
			retrieval.Mirror.Status,
			retrieval.Mirror.QueryCount,
			retrieval.Mirror.RawQueryCount,
			retrieval.Mirror.RewriteQueryCount,
			retrieval.Mirror.AnchorQueryCount,
			len(retrieval.Mirror.Candidates),
		)
	}
	selected := qualitySelectedItems(retrieval)
	if len(selected) == 0 {
		b.WriteString("  selected: (empty)\n")
		return
	}
	b.WriteString("  selected:\n")
	for _, item := range selected {
		fmt.Fprintf(
			b,
			"    - %s %s:%s %s %s\n",
			item.blockType,
			item.nodeType,
			item.nodeID,
			item.historicalStatus,
			item.summary,
		)
	}
}

func writeQualityAnalysis(b *strings.Builder, analysis *memorycore.QueryAnalysis) {
	var parts []string
	if analysis.TimeMode != "" {
		parts = append(parts, "time_mode="+string(analysis.TimeMode))
	}
	if analysis.MemoryDomain != "" {
		parts = append(parts, "domain="+string(analysis.MemoryDomain))
	}
	if analysis.MemoryAbility != "" {
		parts = append(parts, "ability="+string(analysis.MemoryAbility))
	}
	if analysis.EvidenceNeed != "" {
		parts = append(parts, "evidence="+string(analysis.EvidenceNeed))
	}
	if analysis.Source != "" {
		parts = append(parts, "source="+string(analysis.Source))
	}
	if analysis.Diagnostics != nil && analysis.Diagnostics.SemanticStatus != "" {
		parts = append(parts, "semantic_status="+analysis.Diagnostics.SemanticStatus)
	}
	if analysis.Diagnostics != nil && analysis.Diagnostics.FallbackReason != "" {
		parts = append(parts, "fallback="+analysis.Diagnostics.FallbackReason)
	}
	if len(analysis.QueryRewrites) > 0 {
		parts = append(parts, "rewrites="+strings.Join(queryRewritesToStrings(analysis.QueryRewrites), ","))
	}
	if len(analysis.ContextBlockHints) > 0 {
		parts = append(parts, "hints="+strings.Join(analysis.ContextBlockHints, ","))
	}
	if len(parts) == 0 {
		return
	}
	fmt.Fprintf(b, "  analysis: %s\n", strings.Join(parts, " "))
}

type qualitySelectedItem struct {
	blockType        string
	nodeType         string
	nodeID           string
	historicalStatus string
	summary          string
}

func qualitySelectedItems(retrieval *memorycore.MemoryContext) []qualitySelectedItem {
	var out []qualitySelectedItem
	for _, block := range retrieval.Blocks {
		for _, item := range block.Items {
			status := item.HistoricalStatus
			if status == "" {
				status = "current"
			}
			out = append(out, qualitySelectedItem{
				blockType:        block.BlockType,
				nodeType:         item.NodeType,
				nodeID:           item.NodeID,
				historicalStatus: status,
				summary:          item.Summary,
			})
		}
	}
	return out
}

func qualityCleanRefs(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = qualityCleanRef(value)
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}

func qualityCleanRef(value string) string {
	value = strings.TrimSpace(strings.TrimPrefix(value, "$"))
	if value == "" {
		return ""
	}
	if strings.HasSuffix(value, ".fact_id") {
		return strings.TrimSuffix(value, ".fact_id")
	}
	if strings.HasPrefix(value, "episode.") && strings.HasSuffix(value, ".id") {
		return strings.TrimSuffix(strings.TrimPrefix(value, "episode."), ".id")
	}
	if strings.HasPrefix(value, "entity.") && strings.HasSuffix(value, ".id") {
		return strings.TrimSuffix(strings.TrimPrefix(value, "entity."), ".id")
	}
	return value
}
