package memorycore

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/longyisang/emoagent-memorycore/internal/core"
	memsqlite "github.com/longyisang/emoagent-memorycore/internal/store/sqlite"
)

func (s *service) RunNaturalMemoryCycle(ctx context.Context, req RunNaturalMemoryCycleRequest) (*RunNaturalMemoryCycleResult, error) {
	personaID := defaultString(req.PersonaID, s.persona)
	opts := s.naturalOptionsForRequest(req.Options)
	now := req.Now
	if now.IsZero() {
		now = s.now()
	}
	runKind := req.RunKind
	if runKind == "" {
		runKind = NaturalMemoryRunManual
	}
	if !opts.Enabled {
		return skippedNaturalResult(personaID, runKind, opts, req.DryRun, "natural memory disabled"), nil
	}
	if runKind == NaturalMemoryRunManual {
		if !opts.ManualTrigger.Enabled {
			return skippedNaturalResult(personaID, runKind, opts, req.DryRun, "manual trigger disabled"), nil
		}
		if req.DryRun && !opts.ManualTrigger.AllowDryRun {
			return nil, fmt.Errorf("%w: natural_memory.manual_trigger.allow_dry_run is false", ErrInvalidRequest)
		}
		if req.Force && !opts.ManualTrigger.AllowForce {
			return nil, fmt.Errorf("%w: natural_memory.manual_trigger.allow_force is false", ErrInvalidRequest)
		}
		if !req.MarkSleepCycle {
			req.MarkSleepCycle = opts.ManualTrigger.MarkSleepCycleByDefault
		}
	}
	if runKind == NaturalMemoryRunSleepCycle && !req.Force {
		local := naturalScheduleFor(now, opts, req.LocalDate, req.LocalTime, req.Timezone)
		completed, err := s.natural.SleepCycleCompletedForDate(ctx, personaID, local.LocalDate)
		if err != nil {
			return nil, err
		}
		if completed {
			return skippedNaturalResult(personaID, runKind, opts, req.DryRun, "sleep cycle already completed"), nil
		}
		req.LocalDate = local.LocalDate
		req.LocalTime = local.LocalTime
		req.Timezone = local.Timezone
	}

	maxWrites := opts.Limits.MaxWritesPerRun
	if maxWrites <= 0 {
		maxWrites = opts.Limits.MaxCandidatesPerRun
	}
	candidates, skipped, err := s.natural.ListCandidates(ctx, personaID, opts.Limits.MaxCandidatesPerRun, now)
	if err != nil {
		return nil, err
	}
	runID := s.natural.NewID()
	result := &RunNaturalMemoryCycleResult{
		RunID:            runID,
		PersonaID:        personaID,
		RunKind:          runKind,
		AlgorithmVersion: opts.AlgorithmVersion,
		DryRun:           req.DryRun,
		Status:           NaturalMemoryRunStatusCompleted,
		SkippedNodes:     skipped,
	}

	scored := make([]naturalScoredNode, 0, len(candidates))
	writesApplied := 0
	for _, candidate := range candidates {
		node := naturalNodeFromStore(candidate)
		score := scoreNaturalMemoryNode(node, opts, now)
		result.EvaluatedNodes++
		result.ScoredNodes++
		if score.Protected {
			result.ProtectedNodes++
		}
		if score.Reactivated {
			result.ReactivatedNodes++
		}
		if score.FirstSleepConsolidated {
			result.FirstSleepConsolidatedNodes++
		}
		if searchTierRank(score.EffectiveSearchTier) > searchTierRank(node.CurrentSearchTier) {
			result.DecayedNodes++
		}
		writeSearchDocument := opts.SearchTier.Enabled && opts.SearchTier.WriteMemorySearchDocuments
		tierUpdateCandidate := writeSearchDocument && score.EffectiveSearchTier != node.CurrentSearchTier
		if req.DryRun && tierUpdateCandidate {
			result.SearchTierUpdates++
		}
		item := naturalExplainItem(node, score)
		if req.Explain && len(result.Explain) < opts.Limits.MaxExplainItems {
			result.Explain = append(result.Explain, item)
		}
		scored = append(scored, naturalScoredNode{Node: node, Score: score})
		if req.DryRun {
			continue
		}
		if writesApplied >= maxWrites {
			continue
		}
		state := naturalStateWrite(runID, node, score, opts, now)
		events := naturalEventWrites(runID, node, score)
		tierChanged, mirrorEnqueued, documentCreated, err := s.natural.ApplyNodeWrites(
			ctx,
			state,
			events,
			score.EffectiveSearchTier,
			writeSearchDocument,
			opts.SearchTier.EnqueueMirrorUpsertOnTierChange,
		)
		if err != nil {
			return nil, err
		}
		writesApplied++
		if documentCreated {
			result.SearchDocumentsCreated++
		}
		if tierChanged {
			result.SearchTierUpdates++
		}
		if mirrorEnqueued {
			result.MirrorUpdatesEnqueued++
		}
	}
	compressionCount, err := s.emitNaturalCompressionCandidates(ctx, runID, personaID, opts, scored, req.DryRun, maxWrites-writesApplied)
	if err != nil {
		return nil, err
	}
	result.CompressionCandidates = compressionCount
	if !req.DryRun {
		schedule := naturalScheduleFor(now, opts, req.LocalDate, req.LocalTime, req.Timezone)
		if err := s.natural.PersistRun(ctx, memsqlite.NaturalMemoryRunRow{
			ID:                          runID,
			PersonaID:                   personaID,
			RunKind:                     string(runKind),
			AlgorithmVersion:            opts.AlgorithmVersion,
			LocalDate:                   schedule.LocalDate,
			LocalTime:                   schedule.LocalTime,
			Timezone:                    schedule.Timezone,
			DryRun:                      req.DryRun,
			Force:                       req.Force,
			MarkSleepCycle:              req.MarkSleepCycle,
			Status:                      string(NaturalMemoryRunStatusCompleted),
			EvaluatedNodes:              result.EvaluatedNodes,
			ScoredNodes:                 result.ScoredNodes,
			ProtectedNodes:              result.ProtectedNodes,
			DecayedNodes:                result.DecayedNodes,
			ReactivatedNodes:            result.ReactivatedNodes,
			FirstSleepConsolidatedNodes: result.FirstSleepConsolidatedNodes,
			SearchTierUpdates:           result.SearchTierUpdates,
			SearchDocumentsCreated:      result.SearchDocumentsCreated,
			MirrorUpdatesEnqueued:       result.MirrorUpdatesEnqueued,
			CompressionCandidates:       result.CompressionCandidates,
			NarrativesCreated:           result.NarrativesCreated,
			InsightsCreated:             result.InsightsCreated,
		}); err != nil {
			return nil, err
		}
	}
	return result, nil
}

func (s *service) naturalOptionsForRequest(request NaturalMemoryOptions) NaturalMemoryOptions {
	opts := s.naturalOptions
	if request.AlgorithmVersion != "" ||
		request.Enabled ||
		request.Limits.MaxCandidatesPerRun != 0 ||
		request.Limits.MaxWritesPerRun != 0 {
		opts = request
	}
	opts = normalizeNaturalMemoryOptions(opts)
	if strings.TrimSpace(opts.SleepCycle.Timezone) == "" {
		opts.SleepCycle.Timezone = s.naturalOptions.SleepCycle.Timezone
	}
	return opts
}

func naturalNodeFromStore(row memsqlite.NaturalMemoryCandidateRow) naturalMemoryNode {
	return naturalMemoryNode{
		PersonaID:                  row.PersonaID,
		NodeType:                   row.NodeType,
		NodeID:                     row.NodeID,
		FactType:                   row.FactType,
		Importance:                 row.Importance,
		Confidence:                 row.Confidence,
		SensitivityLevel:           row.SensitivityLevel,
		ValidityStatus:             row.ValidityStatus,
		VisibilityStatus:           row.VisibilityStatus,
		LifecycleStatus:            row.LifecycleStatus,
		Pinned:                     row.Pinned,
		Searchable:                 row.Searchable,
		AccessCount:                row.AccessCount,
		ReinforcementCount:         row.ReinforcementCount,
		CongruentAccessCount:       row.CongruentAccessCount,
		RecentAccessEventCount:     row.RecentAccessEventCount,
		RecentPromptInjectedCount:  row.RecentPromptInjectedCount,
		RecentReinforcedEventCount: row.RecentReinforcedEventCount,
		StructuralAssociationCount: row.StructuralAssociationCount,
		CreatedAt:                  row.CreatedAt,
		UpdatedAt:                  row.UpdatedAt,
		IngestedAt:                 row.IngestedAt,
		LastAccessedAt:             row.LastAccessedAt,
		ValidTo:                    row.ValidTo,
		CurrentSearchTier:          row.CurrentSearchTier,
		PreviousNaturalState:       NaturalMemoryState(row.PreviousNaturalState),
		FirstSleepConsolidated:     row.FirstSleepConsolidated,
		EmotionSalienceHint:        row.EmotionSalienceHint,
		EmotionPersistenceHint:     row.EmotionPersistenceHint,
		ReactivationCount:          row.ReactivationCount,
		LastReactivatedAt:          row.LastReactivatedAt,
		LastStrengthenedAt:         row.LastStrengthenedAt,
		ClusterKey:                 row.ClusterKey,
	}
}

func naturalStateWrite(runID string, node naturalMemoryNode, score naturalMemoryScore, opts NaturalMemoryOptions, now time.Time) memsqlite.NaturalMemoryStateWrite {
	breakdown, _ := json.Marshal(map[string]any{
		"algorithm":                opts.AlgorithmVersion,
		"retrievability":           score.Retrievability,
		"reactivation_score":       score.ReactivationScore,
		"recent_access_events":     node.RecentAccessEventCount,
		"recent_prompt_injected":   node.RecentPromptInjectedCount,
		"recent_reinforced_events": node.RecentReinforcedEventCount,
		"structural_associations":  node.StructuralAssociationCount,
		"reason_codes":             score.ReasonCodes,
	})
	var reactivatedAt *time.Time
	if score.Reactivated {
		reactivatedAt = &now
	}
	return memsqlite.NaturalMemoryStateWrite{
		RunID:                      runID,
		PersonaID:                  node.PersonaID,
		NodeType:                   node.NodeType,
		NodeID:                     node.NodeID,
		AlgorithmVersion:           opts.AlgorithmVersion,
		NaturalStrength:            score.NaturalStrength,
		Retrievability:             score.Retrievability,
		StabilityDays:              score.StabilityDays,
		DecayExponent:              score.DecayExponent,
		NaturalState:               string(score.NaturalState),
		LastSimulatedAt:            now,
		LastReactivatedAt:          reactivatedAt,
		LastStrengthenedAt:         node.LastStrengthenedAt,
		FirstSleepConsolidated:     score.FirstSleepConsolidated,
		ReactivationCountIncrement: score.Reactivated,
		ProtectedReason:            score.ProtectedReason,
		EmotionSalienceHint:        node.EmotionSalienceHint,
		EmotionPersistenceHint:     node.EmotionPersistenceHint,
		ScoreBreakdownJSON:         string(breakdown),
	}
}

func naturalEventWrites(runID string, node naturalMemoryNode, score naturalMemoryScore) []memsqlite.NaturalMemoryEventWrite {
	events := []memsqlite.NaturalMemoryEventWrite{naturalEventWrite(runID, node, score, "scored")}
	if score.Protected {
		events = append(events, naturalEventWrite(runID, node, score, "protected"))
	} else if searchTierRank(score.EffectiveSearchTier) > searchTierRank(node.CurrentSearchTier) {
		events = append(events, naturalEventWrite(runID, node, score, "decayed"))
	}
	if score.Reactivated {
		events = append(events, naturalEventWrite(runID, node, score, "reactivated"))
	}
	if score.FirstSleepConsolidated {
		events = append(events, naturalEventWrite(runID, node, score, "first_sleep_consolidated"))
	}
	if naturalStorageRewarmCandidate(node, score) {
		events = append(events, naturalEventWrite(runID, node, score, "storage_rewarm_candidate"))
	}
	if string(node.PreviousNaturalState) != "" && node.PreviousNaturalState != score.NaturalState {
		events = append(events, naturalEventWrite(runID, node, score, "natural_state_changed"))
	}
	if node.CurrentSearchTier != score.EffectiveSearchTier {
		events = append(events, naturalEventWrite(runID, node, score, "search_tier_updated"))
	}
	return events
}

func naturalStorageRewarmCandidate(node naturalMemoryNode, score naturalMemoryScore) bool {
	if !score.Reactivated {
		return false
	}
	if node.LifecycleStatus != core.LifecycleArchived && node.LifecycleStatus != core.LifecycleDeepArchived {
		return false
	}
	return searchTierRank(score.NaturalTier) < searchTierRank(score.EffectiveSearchTier)
}

func naturalEventWrite(runID string, node naturalMemoryNode, score naturalMemoryScore, eventType string) memsqlite.NaturalMemoryEventWrite {
	return memsqlite.NaturalMemoryEventWrite{
		RunID:             runID,
		PersonaID:         node.PersonaID,
		NodeType:          node.NodeType,
		NodeID:            node.NodeID,
		EventType:         eventType,
		FromNaturalState:  string(node.PreviousNaturalState),
		ToNaturalState:    string(score.NaturalState),
		FromSearchTier:    node.CurrentSearchTier,
		ToSearchTier:      score.EffectiveSearchTier,
		NaturalStrength:   score.NaturalStrength,
		Retrievability:    score.Retrievability,
		StabilityDays:     score.StabilityDays,
		DecayExponent:     score.DecayExponent,
		ReactivationScore: score.ReactivationScore,
		ReasonCodes:       score.ReasonCodes,
		SafeReasonSummary: score.SafeReasonSummary,
	}
}

func naturalExplainItem(node naturalMemoryNode, score naturalMemoryScore) NaturalMemoryExplainItem {
	return NaturalMemoryExplainItem{
		NodeType:          string(node.NodeType),
		NodeID:            node.NodeID,
		FromNaturalState:  string(node.PreviousNaturalState),
		ToNaturalState:    string(score.NaturalState),
		FromSearchTier:    string(node.CurrentSearchTier),
		ToSearchTier:      string(score.EffectiveSearchTier),
		NaturalStrength:   score.NaturalStrength,
		Retrievability:    score.Retrievability,
		ReactivationScore: score.ReactivationScore,
		ReasonCodes:       append([]string(nil), score.ReasonCodes...),
		SafeReasonSummary: score.SafeReasonSummary,
	}
}

func skippedNaturalResult(personaID string, runKind NaturalMemoryRunKind, opts NaturalMemoryOptions, dryRun bool, reason string) *RunNaturalMemoryCycleResult {
	return &RunNaturalMemoryCycleResult{
		PersonaID:        personaID,
		RunKind:          runKind,
		AlgorithmVersion: opts.AlgorithmVersion,
		DryRun:           dryRun,
		Status:           NaturalMemoryRunStatusSkipped,
		Explain: []NaturalMemoryExplainItem{{
			ReasonCodes:       []string{"skipped"},
			SafeReasonSummary: reason,
		}},
	}
}

type naturalScoredNode struct {
	Node  naturalMemoryNode
	Score naturalMemoryScore
}

func (s *service) emitNaturalCompressionCandidates(ctx context.Context, runID string, personaID string, opts NaturalMemoryOptions, scored []naturalScoredNode, dryRun bool, remainingWrites int) (int, error) {
	if !opts.Compression.Enabled || !opts.Compression.EmitCandidates {
		return 0, nil
	}
	groups := map[string][]naturalScoredNode{}
	for _, item := range scored {
		if item.Score.Protected || item.Node.NodeType != core.NodeTypeFact {
			continue
		}
		if !naturalCompressionTierAllowed(item.Score.EffectiveSearchTier, opts.Compression.AllowedSourceTiers) {
			continue
		}
		if opts.Compression.RequireLowSensitivity && item.Node.SensitivityLevel != core.SensitivityNormal {
			continue
		}
		if item.Node.Confidence < opts.Compression.RequireMinConfidence {
			continue
		}
		if naturalFactTypeExcluded(item.Node.FactType, opts.Compression.ExcludeFactTypes) {
			continue
		}
		key := strings.TrimSpace(item.Node.ClusterKey)
		if key == "" {
			key = string(item.Node.FactType)
		}
		groups[key] = append(groups[key], item)
	}
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	count := 0
	for _, key := range keys {
		group := groups[key]
		if len(group) < opts.Compression.MinClusterSize {
			continue
		}
		if count >= opts.Compression.MaxCandidatesPerRun {
			break
		}
		if !dryRun && remainingWrites <= 0 {
			break
		}
		count++
		if dryRun {
			continue
		}
		var refs []string
		var totalR, totalI, minC float64
		minC = 1
		for _, item := range group {
			refs = append(refs, string(item.Node.NodeType)+":"+item.Node.NodeID)
			totalR += item.Score.Retrievability
			totalI += item.Node.Importance
			if item.Node.Confidence < minC {
				minC = item.Node.Confidence
			}
		}
		event := naturalEventWrite(runID, group[0].Node, group[0].Score, "compression_candidate_emitted")
		event.ReasonCodes = append(event.ReasonCodes, "low_retrievability_cluster")
		event.SafeReasonSummary = "compression candidate emitted"
		if err := s.natural.EmitCompressionCandidate(ctx, memsqlite.NaturalCompressionCandidateWrite{
			RunID:             runID,
			PersonaID:         personaID,
			ClusterKey:        key,
			TargetNodeType:    core.NodeTypeNarrative,
			SourceRefs:        refs,
			CandidateSummary:  "natural memory compression candidate",
			AvgRetrievability: totalR / float64(len(group)),
			AvgImportance:     totalI / float64(len(group)),
			MinConfidence:     minC,
			ReasonCodes:       []string{"low_retrievability_cluster"},
		}, []memsqlite.NaturalMemoryEventWrite{event}); err != nil {
			return 0, err
		}
		remainingWrites--
	}
	return count, nil
}

func naturalCompressionTierAllowed(tier core.SearchTier, allowed []core.SearchTier) bool {
	for _, candidate := range allowed {
		if tier == candidate {
			return true
		}
	}
	return false
}

func naturalFactTypeExcluded(factType core.FactType, excluded []core.FactType) bool {
	for _, candidate := range excluded {
		if factType == candidate {
			return true
		}
	}
	return false
}
