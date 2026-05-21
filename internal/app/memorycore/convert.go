package memorycore

import (
	"github.com/longyisang/emoagent-memorycore/internal/core"
	memsqlite "github.com/longyisang/emoagent-memorycore/internal/store/sqlite"
	"time"
)

func retentionResultFromStore(result memsqlite.RetentionResult) *RunRetentionResult {
	return &RunRetentionResult{
		EvaluatedFacts:        result.EvaluatedFacts,
		ExpiredFacts:          result.ExpiredFacts,
		ArchivedFacts:         result.ArchivedFacts,
		DeepArchivedFacts:     result.DeepArchivedFacts,
		SearchDocumentsSynced: result.SearchDocumentsSynced,
		MirrorUpdatesEnqueued: result.MirrorUpdatesEnqueued,
	}
}

func sessionFromCore(session core.Session) *Session {
	return &Session{
		ID:        session.ID,
		PersonaID: session.PersonaID,
		Channel:   string(session.Channel),
		Title:     session.Title,
		Summary:   session.Summary,
		StartedAt: session.StartedAt,
		EndedAt:   session.EndedAt,
	}
}

func episodeFromCore(episode core.Episode) *Episode {
	return &Episode{
		ID:               episode.ID,
		PersonaID:        episode.PersonaID,
		SessionID:        episode.SessionID,
		Role:             string(episode.Role),
		Content:          episode.Content,
		ContentHash:      episode.ContentHash,
		OccurredAt:       episode.OccurredAt,
		SourceType:       string(episode.SourceType),
		SourceRef:        episode.SourceRef,
		PrevEpisodeID:    episode.PrevEpisodeID,
		NextEpisodeID:    episode.NextEpisodeID,
		VisibilityStatus: string(episode.VisibilityStatus),
		SensitivityLevel: string(episode.SensitivityLevel),
		Searchable:       episode.Searchable,
	}
}

func entityFromCore(entity core.Entity, aliases []core.EntityAlias) *Entity {
	result := &Entity{
		ID:               entity.ID,
		PersonaID:        entity.PersonaID,
		CanonicalName:    entity.CanonicalName,
		EntityType:       string(entity.EntityType),
		Description:      entity.Description,
		VisibilityStatus: string(entity.VisibilityStatus),
		SensitivityLevel: string(entity.SensitivityLevel),
		Searchable:       entity.Searchable,
		Aliases:          make([]EntityAlias, 0, len(aliases)),
	}
	for _, alias := range aliases {
		result.Aliases = append(result.Aliases, *entityAliasFromCore(alias))
	}
	return result
}

func entityAliasFromCore(alias core.EntityAlias) *EntityAlias {
	return &EntityAlias{
		ID:              alias.ID,
		PersonaID:       alias.PersonaID,
		EntityID:        alias.EntityID,
		Alias:           alias.Alias,
		AliasType:       string(alias.AliasType),
		Confidence:      alias.Confidence,
		SourceEpisodeID: alias.SourceEpisodeID,
	}
}

func consolidationResultFromCore(result memsqlite.ConsolidationResult) *ConsolidationResult {
	return &ConsolidationResult{
		Action:            result.Action,
		Status:            result.Status,
		Fact:              factFromCore(result.Fact),
		ExistingFact:      factFromCore(result.ExistingFact),
		SupersededFactIDs: append([]string(nil), result.SupersededFactIDs...),
		LinkIDs:           append([]string(nil), result.LinkIDs...),
		RejectedReason:    result.RejectedReason,
		NeedsReviewReason: result.NeedsReviewReason,
	}
}

func factFromCore(fact *core.Fact) *Fact {
	if fact == nil {
		return nil
	}
	return &Fact{
		ID:                 fact.ID,
		PersonaID:          fact.PersonaID,
		SubjectEntityID:    fact.SubjectEntityID,
		Predicate:          fact.Predicate,
		ObjectEntityID:     fact.ObjectEntityID,
		ObjectLiteral:      fact.ObjectLiteral,
		ContentSummary:     fact.ContentSummary,
		FactType:           string(fact.FactType),
		ValidFrom:          fact.ValidFrom,
		ValidTo:            fact.ValidTo,
		Confidence:         string(fact.ExtractionConfidence),
		ConfidenceScore:    fact.ExtractionConfidenceScore,
		Importance:         fact.Importance,
		Valence:            fact.Valence,
		Arousal:            fact.Arousal,
		Sensitivity:        string(fact.SensitivityLevel),
		ValidityStatus:     string(fact.ValidityStatus),
		VisibilityStatus:   string(fact.VisibilityStatus),
		LifecycleStatus:    string(fact.LifecycleStatus),
		Pinned:             fact.Pinned,
		ReinforcementCount: fact.ReinforcementCount,
		Searchable:         fact.Searchable,
		CreatedAt:          fact.CreatedAt,
		UpdatedAt:          fact.UpdatedAt,
	}
}

func memoryContextFromStore(context memsqlite.MemoryContext) *MemoryContext {
	result := &MemoryContext{
		Blocks:              make([]MemoryBlock, 0, len(context.Blocks)),
		DoNotMention:        make([]MemorySuppression, 0, len(context.DoNotMention)),
		TokenEstimate:       context.TokenEstimate,
		Mirror:              mirrorDiagnosticsFromStore(context.Mirror),
		GraphActivation:     graphActivationDiagnosticsFromStore(context.GraphActivation),
		Rerank:              rerankDiagnosticsFromStore(context.Rerank),
		QueryAnalysis:       queryAnalysisFromStore(context.QueryAnalysis),
		AnchorFusion:        anchorFusionDiagnosticsFromStore(context.AnchorFusion),
		RetrievalConfidence: retrievalConfidenceFromStore(context.RetrievalConfidence),
	}
	for _, block := range context.Blocks {
		out := MemoryBlock{
			BlockType: block.BlockType,
			Items:     make([]MemoryContextItem, 0, len(block.Items)),
		}
		for _, item := range block.Items {
			out.Items = append(out.Items, memoryContextItemFromStore(item))
		}
		result.Blocks = append(result.Blocks, out)
	}
	for _, suppression := range context.DoNotMention {
		result.DoNotMention = append(result.DoNotMention, MemorySuppression{
			NodeType: suppression.NodeType,
			NodeID:   suppression.NodeID,
			Reason:   suppression.Reason,
		})
	}
	return result
}

func retrievalConfidenceFromStore(value *memsqlite.RetrievalConfidence) *RetrievalConfidence {
	if value == nil {
		return nil
	}
	return &RetrievalConfidence{
		CandidateRecallProxy:  value.CandidateRecallProxy,
		SourceDiversity:       value.SourceDiversity,
		AnchorCoverage:        value.AnchorCoverage,
		TopRankMargin:         value.TopRankMargin,
		AuthorityPassRatio:    value.AuthorityPassRatio,
		TemporalConsistency:   value.TemporalConsistency,
		RequiredChainCoverage: value.RequiredChainCoverage,
		MMRDiversity:          value.MMRDiversity,
		SensitivitySafety:     value.SensitivitySafety,
		Overall:               value.Overall,
		CorrectiveAction:      value.CorrectiveAction,
		HardFailureReason:     value.HardFailureReason,
	}
}

func memoryContextItemFromStore(item memsqlite.MemoryContextItem) MemoryContextItem {
	result := MemoryContextItem{
		NodeType:         item.NodeType,
		NodeID:           item.NodeID,
		Summary:          item.Summary,
		Confidence:       item.Confidence,
		UsageGuidance:    item.UsageGuidance,
		HistoricalStatus: item.HistoricalStatus,
		ValidFrom:        cloneTimePtr(item.ValidFrom),
		ValidTo:          cloneTimePtr(item.ValidTo),
		SourceRefs:       make([]MemorySourceRef, 0, len(item.SourceRefs)),
		RelatedFacts:     make([]MemoryRelatedFactRef, 0, len(item.RelatedFacts)),
		DoNotOverstate:   item.DoNotOverstate,
	}
	for _, source := range item.SourceRefs {
		result.SourceRefs = append(result.SourceRefs, MemorySourceRef{
			EpisodeID:     source.EpisodeID,
			SessionID:     source.SessionID,
			SessionTitle:  source.SessionTitle,
			OccurredAt:    source.OccurredAt,
			SourceStatus:  source.SourceStatus,
			EvidenceCount: source.EvidenceCount,
			QuoteAllowed:  source.QuoteAllowed,
		})
	}
	for _, related := range item.RelatedFacts {
		result.RelatedFacts = append(result.RelatedFacts, MemoryRelatedFactRef{
			NodeType:         related.NodeType,
			NodeID:           related.NodeID,
			Summary:          related.Summary,
			LinkType:         related.LinkType,
			Direction:        related.Direction,
			HistoricalStatus: related.HistoricalStatus,
		})
	}
	return result
}

func cloneTimePtr(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func queryAnalysisFromStore(value *memsqlite.QueryAnalysis) *QueryAnalysis {
	if value == nil {
		return nil
	}
	result := &QueryAnalysis{
		Raw:               value.Raw,
		Normalized:        value.Normalized,
		Terms:             append([]string(nil), value.Terms...),
		EntityMentions:    make([]QueryEntityMention, 0, len(value.EntityMentions)),
		TimeMode:          QueryTimeMode(value.TimeMode),
		Signals:           make([]QuerySignal, 0, len(value.Signals)),
		MemoryDomain:      MemoryDomain(value.MemoryDomain),
		MemoryAbility:     MemoryAbility(value.MemoryAbility),
		EvidenceNeed:      EvidenceNeed(value.EvidenceNeed),
		Source:            QueryAnalysisSource(value.Source),
		Confidence:        value.Confidence,
		FieldConfidence:   queryAnalysisConfidenceFromStore(value.FieldConfidence),
		Scores:            queryAnalysisScoresFromStore(value.Scores),
		Probes:            queryAnchorProbeFromStore(value.Probes),
		Decision:          queryAnalysisDecisionFromStore(value.Decision),
		Evidence:          queryAnalysisEvidenceFromStore(value.Evidence),
		Alternatives:      queryAnalysisAlternativesFromStore(value.Alternatives),
		QueryRewrites:     queryRewritesFromStore(value.QueryRewrites),
		SemanticAnchors:   semanticAnchorsFromStore(value.SemanticAnchors),
		ContextBlockHints: append([]string(nil), value.ContextBlockHints...),
		PolicyHints:       queryPolicyHintsFromStore(value.PolicyHints),
		Diagnostics:       queryAnalysisDiagnosticsFromStore(value.Diagnostics),
	}
	for _, mention := range value.EntityMentions {
		result.EntityMentions = append(result.EntityMentions, QueryEntityMention{
			EntityID:      mention.EntityID,
			CanonicalName: mention.CanonicalName,
			Alias:         mention.Alias,
			MatchText:     mention.MatchText,
			MatchKind:     QueryEntityMentionKind(mention.MatchKind),
		})
	}
	for _, signal := range value.Signals {
		result.Signals = append(result.Signals, QuerySignal(signal))
	}
	return result
}

func queryRewritesFromStore(values []memsqlite.QueryRewrite) []QueryRewrite {
	if len(values) == 0 {
		return nil
	}
	result := make([]QueryRewrite, 0, len(values))
	for _, rewrite := range values {
		result = append(result, QueryRewrite{
			Text:    rewrite.Text,
			Purpose: rewrite.Purpose,
			Weight:  rewrite.Weight,
		})
	}
	return result
}

func semanticAnchorsFromStore(values []memsqlite.SemanticAnchor) []SemanticAnchor {
	if len(values) == 0 {
		return nil
	}
	result := make([]SemanticAnchor, 0, len(values))
	for _, anchor := range values {
		result = append(result, SemanticAnchor{
			Text:       anchor.Text,
			AnchorType: anchor.AnchorType,
			EntityID:   anchor.EntityID,
			Weight:     anchor.Weight,
			Confidence: anchor.Confidence,
		})
	}
	return result
}

func queryAnalysisConfidenceFromStore(value memsqlite.QueryAnalysisConfidence) QueryAnalysisConfidence {
	return QueryAnalysisConfidence{
		Overall:          value.Overall,
		TimeMode:         value.TimeMode,
		MemoryAbility:    value.MemoryAbility,
		MemoryDomain:     value.MemoryDomain,
		EvidenceNeed:     value.EvidenceNeed,
		EntityResolution: value.EntityResolution,
	}
}

func queryAnalysisScoresFromStore(value memsqlite.QueryAnalysisScores) QueryAnalysisScores {
	return QueryAnalysisScores{
		RuleFit:                     value.RuleFit,
		AnchorReadiness:             value.AnchorReadiness,
		ExpectedRetrievalConfidence: value.ExpectedRetrievalConfidence,
		SemanticNeed:                value.SemanticNeed,
		Complexity:                  value.Complexity,
		Ambiguity:                   value.Ambiguity,
		Specificity:                 value.Specificity,
		SafetyRisk:                  value.SafetyRisk,
		IntentEvidence:              value.IntentEvidence,
		TimeEvidence:                value.TimeEvidence,
		DomainEvidence:              value.DomainEvidence,
		EvidenceNeedEvidence:        value.EvidenceNeedEvidence,
		EntityResolution:            value.EntityResolution,
		FieldConsistency:            value.FieldConsistency,
		DefaultFallbackPenalty:      value.DefaultFallbackPenalty,
		MultiIntentConflictPenalty:  value.MultiIntentConflictPenalty,
		SensitivityPenalty:          value.SensitivityPenalty,
	}
}

func queryAnchorProbeFromStore(value memsqlite.QueryAnchorProbe) QueryAnchorProbe {
	return QueryAnchorProbe{
		EntityExactConf:        value.EntityExactConf,
		EntityAmbiguity:        value.EntityAmbiguity,
		SparseProbeConf:        value.SparseProbeConf,
		PredicateProbeConf:     value.PredicateProbeConf,
		RecentProbeConf:        value.RecentProbeConf,
		PinnedCoreProbeConf:    value.PinnedCoreProbeConf,
		NarrativeProbeConf:     value.NarrativeProbeConf,
		FallbackSearchHitCount: value.FallbackSearchHitCount,
		Top1Score:              value.Top1Score,
		Top2Score:              value.Top2Score,
		Top1Margin:             value.Top1Margin,
		Breakdown:              queryAnchorProbeBreakdownFromStore(value.Breakdown),
	}
}

func queryAnchorProbeBreakdownFromStore(values []memsqlite.QueryAnchorProbeBreakdown) []QueryAnchorProbeBreakdown {
	if len(values) == 0 {
		return nil
	}
	out := make([]QueryAnchorProbeBreakdown, 0, len(values))
	for _, value := range values {
		out = append(out, QueryAnchorProbeBreakdown{
			Source:      value.Source,
			Status:      value.Status,
			Confidence:  value.Confidence,
			HitCount:    value.HitCount,
			TopScore:    value.TopScore,
			SecondScore: value.SecondScore,
			Reason:      value.Reason,
			Error:       value.Error,
		})
	}
	return out
}

func queryAnalysisDecisionFromStore(value memsqlite.QueryAnalysisDecision) QueryAnalysisDecision {
	return QueryAnalysisDecision{
		UseSemantic:      value.UseSemantic,
		SemanticMode:     value.SemanticMode,
		RetrievalMode:    value.RetrievalMode,
		ReasonCodes:      append([]string(nil), value.ReasonCodes...),
		ThresholdVersion: value.ThresholdVersion,
		ScorerVersion:    value.ScorerVersion,
	}
}

func queryAnalysisEvidenceFromStore(values []memsqlite.QueryAnalysisEvidence) []QueryAnalysisEvidence {
	if len(values) == 0 {
		return nil
	}
	result := make([]QueryAnalysisEvidence, 0, len(values))
	for _, value := range values {
		result = append(result, QueryAnalysisEvidence{
			Field:     value.Field,
			Signal:    value.Signal,
			MatchText: value.MatchText,
			SpanStart: value.SpanStart,
			SpanEnd:   value.SpanEnd,
			Weight:    value.Weight,
			Detector:  value.Detector,
		})
	}
	return result
}

func queryAnalysisAlternativesFromStore(values []memsqlite.QueryAnalysisAlternative) []QueryAnalysisAlternative {
	if len(values) == 0 {
		return nil
	}
	result := make([]QueryAnalysisAlternative, 0, len(values))
	for _, value := range values {
		result = append(result, QueryAnalysisAlternative{
			Field:       value.Field,
			Value:       value.Value,
			Confidence:  value.Confidence,
			ReasonCodes: append([]string(nil), value.ReasonCodes...),
			Detector:    value.Detector,
		})
	}
	return result
}

func queryPolicyHintsFromStore(value memsqlite.QueryPolicyHints) QueryPolicyHints {
	return QueryPolicyHints{
		PreferEvidencedByLinks: value.PreferEvidencedByLinks,
		PreferSupersedesLinks:  value.PreferSupersedesLinks,
		PreferCausalLinks:      value.PreferCausalLinks,
		PreferCounterexamples:  value.PreferCounterexamples,
		PreferNarratives:       value.PreferNarratives,
		MaxHopsHint:            value.MaxHopsHint,
	}
}

func queryAnalysisDiagnosticsFromStore(value *memsqlite.QueryAnalysisDiagnostics) *QueryAnalysisDiagnostics {
	if value == nil {
		return nil
	}
	return &QueryAnalysisDiagnostics{
		ScorerVersion:           value.ScorerVersion,
		RuleConfidenceLegacy:    value.RuleConfidenceLegacy,
		RuleConfidenceReason:    value.RuleConfidenceReason,
		SemanticDecisionLegacy:  value.SemanticDecisionLegacy,
		MinConfidenceToOverride: value.MinConfidenceToOverride,
		Signals:                 append([]string(nil), value.Signals...),
		EntityMentionCount:      value.EntityMentionCount,
		Scores:                  queryAnalysisScoresFromStore(value.Scores),
		FieldConfidence:         queryAnalysisConfidenceFromStore(value.FieldConfidence),
		RuleDecision:            queryAnalysisDecisionFromStore(value.RuleDecision),
		AdaptiveDecision:        queryAnalysisDecisionFromStore(value.AdaptiveDecision),
		RuleEvidence:            queryAnalysisEvidenceFromStore(value.RuleEvidence),
		RuleAlternatives:        queryAnalysisAlternativesFromStore(value.RuleAlternatives),
		SemanticStatus:          value.SemanticStatus,
		SemanticProvider:        value.SemanticProvider,
		SemanticModel:           value.SemanticModel,
		PromptVersion:           value.PromptVersion,
		SemanticLatencyMs:       value.SemanticLatencyMs,
		FallbackReason:          value.FallbackReason,
		RewriteCount:            value.RewriteCount,
		SemanticAnchorCount:     value.SemanticAnchorCount,
		DroppedRewriteCount:     value.DroppedRewriteCount,
		DroppedRewriteReasons:   append([]string(nil), value.DroppedRewriteReasons...),
		EnglishRewriteCount:     value.EnglishRewriteCount,
		SemanticDriftCount:      value.SemanticDriftCount,
		SemanticAnalysis:        semanticQueryAnalysisDiagnosticsFromStore(value.SemanticAnalysis),
	}
}

func semanticQueryAnalysisDiagnosticsFromStore(value *memsqlite.SemanticQueryAnalysisDiagnostics) *SemanticQueryAnalysisDiagnostics {
	if value == nil {
		return nil
	}
	out := &SemanticQueryAnalysisDiagnostics{
		TimeMode:          value.TimeMode,
		Signals:           append([]string(nil), value.Signals...),
		MemoryDomain:      value.MemoryDomain,
		MemoryAbility:     value.MemoryAbility,
		EvidenceNeed:      value.EvidenceNeed,
		Confidence:        value.Confidence,
		FieldConfidence:   queryAnalysisConfidenceFromStore(value.FieldConfidence),
		Scores:            queryAnalysisScoresFromStore(value.Scores),
		Probes:            queryAnchorProbeFromStore(value.Probes),
		Decision:          queryAnalysisDecisionFromStore(value.Decision),
		Evidence:          queryAnalysisEvidenceFromStore(value.Evidence),
		Alternatives:      queryAnalysisAlternativesFromStore(value.Alternatives),
		QueryRewrites:     queryRewritesFromStore(value.QueryRewrites),
		SemanticAnchors:   semanticAnchorsFromStore(value.SemanticAnchors),
		ContextBlockHints: append([]string(nil), value.ContextBlockHints...),
		PolicyHints:       queryPolicyHintsFromStore(value.PolicyHints),
	}
	for _, mention := range value.EntityMentions {
		out.EntityMentions = append(out.EntityMentions, SemanticQueryEntityMentionDiagnostics{
			EntityID:      mention.EntityID,
			CanonicalName: mention.CanonicalName,
			Alias:         mention.Alias,
			MatchText:     mention.MatchText,
			MatchKind:     mention.MatchKind,
			Confidence:    mention.Confidence,
		})
	}
	return out
}

func mirrorDiagnosticsFromStore(value *memsqlite.MirrorDiagnostics) *MirrorRetrievalDiagnostics {
	if value == nil {
		return nil
	}
	result := &MirrorRetrievalDiagnostics{
		Status:                       value.Status,
		Degraded:                     value.Degraded,
		FallbackReason:               value.FallbackReason,
		LatencyMs:                    value.LatencyMs,
		SidecarCandidateCount:        value.SidecarCandidateCount,
		MappedCandidateCount:         value.MappedCandidateCount,
		DroppedCandidateCount:        value.DroppedCandidateCount,
		EmbeddingCacheHits:           value.EmbeddingCacheHits,
		EmbeddingCacheMisses:         value.EmbeddingCacheMisses,
		EmbeddingLiveCallCount:       value.EmbeddingLiveCallCount,
		QueryCount:                   value.QueryCount,
		RawQueryCount:                value.RawQueryCount,
		RewriteQueryCount:            value.RewriteQueryCount,
		AnchorQueryCount:             value.AnchorQueryCount,
		MergedCandidateCount:         value.MergedCandidateCount,
		QueryTrimCount:               value.QueryTrimCount,
		DenseEmbeddingWallLatencyMs:  value.DenseEmbeddingWallLatencyMs,
		DenseEmbeddingBatchLatencyMs: value.DenseEmbeddingBatchLatencyMs,
		DenseSearchTotalLatencyMs:    value.DenseSearchTotalLatencyMs,
		QueryCountTrimmedByBudget:    value.QueryCountTrimmedByBudget,
		PerQuery:                     mirrorCandidatePerQueryDiagnosticsFromStore(value.PerQuery),
		Candidates:                   make([]MirrorCandidateDiagnostics, 0, len(value.Candidates)),
	}
	for _, item := range value.Candidates {
		sqliteFactID := item.SQLiteFactID
		if item.DropReason == "dropped_by_authority_filter" {
			sqliteFactID = ""
		}
		result.Candidates = append(result.Candidates, MirrorCandidateDiagnostics{
			TriviumNodeID:  item.TriviumNodeID,
			SQLiteFactID:   sqliteFactID,
			Score:          item.Score,
			Source:         item.Source,
			PrimaryPurpose: item.PrimaryPurpose,
			Rank:           item.Rank,
			HitCount:       item.HitCount,
			DropReason:     item.DropReason,
		})
	}
	return result
}

func mirrorCandidatePerQueryDiagnosticsFromStore(values []memsqlite.MirrorCandidatePerQueryDiagnostic) []MirrorCandidatePerQueryDiagnostics {
	result := make([]MirrorCandidatePerQueryDiagnostics, 0, len(values))
	for _, value := range values {
		result = append(result, MirrorCandidatePerQueryDiagnostics{
			Source:    value.Source,
			Purpose:   value.Purpose,
			Count:     value.Count,
			LatencyMs: value.LatencyMs,
		})
	}
	return result
}

func graphActivationDiagnosticsFromStore(value *memsqlite.GraphActivationDiagnostics) *GraphActivationDiagnostics {
	if value == nil {
		return nil
	}
	result := &GraphActivationDiagnostics{
		Status:                value.Status,
		Degraded:              value.Degraded,
		FallbackReason:        value.FallbackReason,
		LatencyMs:             value.LatencyMs,
		SidecarCandidateCount: value.SidecarCandidateCount,
		MappedCandidateCount:  value.MappedCandidateCount,
		DroppedCandidateCount: value.DroppedCandidateCount,
		Candidates:            make([]GraphActivationCandidateDiagnostics, 0, len(value.Candidates)),
	}
	for _, item := range value.Candidates {
		sqliteNodeID := item.SQLiteNodeID
		if item.DropReason != "" {
			sqliteNodeID = ""
		}
		result.Candidates = append(result.Candidates, GraphActivationCandidateDiagnostics{
			TriviumNodeID: item.TriviumNodeID,
			SQLiteNodeID:  sqliteNodeID,
			NodeType:      item.NodeType,
			Score:         item.Score,
			Source:        item.Source,
			Rank:          item.Rank,
			DropReason:    item.DropReason,
			Paths:         graphActivationPathsFromStore(item.Paths),
		})
	}
	return result
}

func graphActivationPathsFromStore(paths []memsqlite.GraphActivationPath) []GraphActivationPath {
	result := make([]GraphActivationPath, 0, len(paths))
	for _, path := range paths {
		result = append(result, GraphActivationPath{
			TriviumNodeIDs: append([]int64(nil), path.TriviumNodeIDs...),
			LinkTypes:      append([]string(nil), path.LinkTypes...),
		})
	}
	return result
}

func rerankDiagnosticsFromStore(value *memsqlite.RerankDiagnostics) *RerankDiagnostics {
	if value == nil {
		return nil
	}
	return &RerankDiagnostics{
		Status:             value.Status,
		SkippedReason:      value.SkippedReason,
		InputCount:         value.InputCount,
		SafeCandidateCount: value.SafeCandidateCount,
		ResultCount:        value.ResultCount,
		Degraded:           value.Degraded,
		FallbackReason:     value.FallbackReason,
		LatencyMs:          value.LatencyMs,
	}
}

func anchorFusionDiagnosticsFromStore(value *memsqlite.AnchorFusionDiagnostics) *AnchorFusionDiagnostics {
	if value == nil {
		return nil
	}
	result := &AnchorFusionDiagnostics{
		Seeds: make([]FusedAnchor, 0, len(value.Seeds)),
	}
	for _, seed := range value.Seeds {
		out := FusedAnchor{
			NodeID:           seed.NodeID,
			NodeType:         string(seed.NodeType),
			FusedAnchorScore: seed.FusedAnchorScore,
			SeedEnergy:       seed.SeedEnergy,
			SourceBreakdown:  make([]AnchorSourceBreakdown, 0, len(seed.SourceBreakdown)),
		}
		for _, breakdown := range seed.SourceBreakdown {
			out.SourceBreakdown = append(out.SourceBreakdown, AnchorSourceBreakdown{
				Source:          breakdown.Source,
				Rank:            breakdown.Rank,
				RawScore:        breakdown.RawScore,
				Weight:          breakdown.Weight,
				RRFContribution: breakdown.RRFContribution,
				DebugReason:     breakdown.DebugReason,
			})
		}
		result.Seeds = append(result.Seeds, out)
	}
	return result
}

func narrativeDraftToStore(draft *NarrativeDraft) *memsqlite.NarrativeDraft {
	if draft == nil {
		return nil
	}
	return &memsqlite.NarrativeDraft{
		ID:               draft.ID,
		Scope:            draft.Scope,
		ScopeRef:         draft.ScopeRef,
		Summary:          draft.Summary,
		EmotionalTone:    draft.EmotionalTone,
		ValenceAvg:       draft.ValenceAvg,
		ArousalAvg:       draft.ArousalAvg,
		Importance:       draft.Importance,
		ValidFrom:        draft.ValidFrom,
		ValidTo:          draft.ValidTo,
		SensitivityLevel: draft.SensitivityLevel,
	}
}

func insightDraftsToStore(drafts []InsightDraft) []memsqlite.InsightDraft {
	if len(drafts) == 0 {
		return nil
	}
	result := make([]memsqlite.InsightDraft, 0, len(drafts))
	for _, draft := range drafts {
		result = append(result, memsqlite.InsightDraft{
			ID:               draft.ID,
			InsightType:      draft.InsightType,
			Content:          draft.Content,
			Confidence:       draft.Confidence,
			Importance:       draft.Importance,
			Valence:          draft.Valence,
			Arousal:          draft.Arousal,
			SensitivityLevel: draft.SensitivityLevel,
		})
	}
	return result
}

func compressionResultFromStore(result memsqlite.CompressionResult) *ApplyCompressionResult {
	return &ApplyCompressionResult{
		NarrativeID:             result.NarrativeID,
		InsightIDs:              append([]string(nil), result.InsightIDs...),
		SourceFactsConsolidated: result.SourceFactsConsolidated,
		DerivedLinkIDs:          append([]string(nil), result.DerivedLinkIDs...),
		SearchDocumentsSynced:   result.SearchDocumentsSynced,
		MirrorUpdatesEnqueued:   result.MirrorUpdatesEnqueued,
		DryRun:                  result.DryRun,
	}
}

func forgetResultFromStore(result memsqlite.ForgetResult) *ForgetResult {
	return &ForgetResult{
		DeletionEventID:        result.DeletionEventID,
		TargetNodeType:         string(result.TargetNodeType),
		TargetNodeID:           result.TargetNodeID,
		SearchDocumentsDeleted: result.SearchDocumentsDeleted,
		FTSRowsDeleted:         result.FTSRowsDeleted,
		MirrorDeletesEnqueued:  result.MirrorDeletesEnqueued,
		LinksScrubbed:          result.LinksScrubbed,
	}
}
