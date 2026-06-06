package eval

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/longyisang/emoagent-memorycore/internal/core"
	"github.com/longyisang/emoagent-memorycore/pkg/memorycore"
)

func (s *runState) assertNaturalMemory(ctx context.Context, assertion Assertion) error {
	if err := s.ensureNaturalEvalPersona(); err != nil {
		return err
	}
	switch assertion.Type {
	case "natural_power_law_monotonic":
		return s.assertNaturalPowerLawMonotonic(ctx, assertion)
	case "natural_reactivation":
		return s.assertNaturalReactivation(ctx, assertion)
	case "natural_first_sleep_once":
		return s.assertNaturalFirstSleepOnce(ctx, assertion)
	case "natural_protected_memory":
		return s.assertNaturalProtectedMemory(ctx, assertion)
	case "natural_search_document_write_scope":
		return s.assertNaturalSearchDocumentWriteScope(ctx, assertion)
	case "natural_skip_visibility":
		return s.assertNaturalSkipVisibility(ctx, assertion)
	case "natural_sleep_cycle_once_per_day":
		return s.assertNaturalSleepCycleOncePerDay(ctx, assertion)
	case "natural_manual_quota":
		return s.assertNaturalManualQuota(ctx, assertion)
	case "natural_compression_candidate":
		return s.assertNaturalCompressionCandidate(ctx, assertion)
	case "natural_lifecycle_cap":
		return s.assertNaturalLifecycleCap(ctx, assertion)
	default:
		return AssertionFailure{CaseID: s.caseID, Assertion: assertion.Type, Expected: "natural assertion type", Actual: assertion.Type}
	}
}

func (s *runState) assertNaturalPowerLawMonotonic(ctx context.Context, assertion Assertion) error {
	now := naturalEvalNow()
	factType := naturalEvalFactType(assertion.FactType, core.FactTypeStablePreference)
	earlier := s.naturalEvalNodeID("earlier")
	later := s.naturalEvalNodeID("later")
	if err := s.seedNaturalEvalFact(earlier, factType, naturalEvalFactOptions{Predicate: "natural_power_earlier", CreatedAt: now.AddDate(0, 0, -90)}); err != nil {
		return err
	}
	if err := s.seedNaturalEvalFact(later, factType, naturalEvalFactOptions{Predicate: "natural_power_later", CreatedAt: now.AddDate(0, 0, -90)}); err != nil {
		return err
	}
	if err := s.seedNaturalEvalState(earlier, now.AddDate(0, 0, -naturalEvalInt(assertion.EarlierDeltaDays, 3))); err != nil {
		return err
	}
	if err := s.seedNaturalEvalState(later, now.AddDate(0, 0, -naturalEvalInt(assertion.LaterDeltaDays, 30))); err != nil {
		return err
	}
	if _, err := s.runNaturalEvalCycle(ctx, now, memorycore.NaturalMemoryRunManual, false, memorycore.NaturalMemoryOptions{}); err != nil {
		return err
	}
	earlyScore, err := s.naturalEvalRetrievability(earlier)
	if err != nil {
		return err
	}
	lateScore, err := s.naturalEvalRetrievability(later)
	if err != nil {
		return err
	}
	if earlyScore <= lateScore {
		return AssertionFailure{CaseID: s.caseID, Assertion: assertion.Type, Expected: "earlier retrievability > later", Actual: fmt.Sprintf("earlier=%.6f later=%.6f", earlyScore, lateScore)}
	}
	return nil
}

func (s *runState) assertNaturalReactivation(ctx context.Context, assertion Assertion) error {
	now := naturalEvalNow()
	nodeID := s.naturalEvalNodeID("reactivated")
	if err := s.seedNaturalEvalFact(nodeID, naturalEvalFactType(assertion.FactType, core.FactTypeStablePreference), naturalEvalFactOptions{
		Predicate:          "natural_reactivation",
		CreatedAt:          now.AddDate(0, 0, -10),
		AccessCount:        assertion.AccessCount,
		ReinforcementCount: assertion.ReinforcementCount,
	}); err != nil {
		return err
	}
	if assertion.AccessCount > 0 {
		if err := s.insertNaturalEvalAccessEvent(nodeID, "prompt_injected", now.Add(-2*time.Hour)); err != nil {
			return err
		}
	}
	if assertion.ReinforcementCount > 0 {
		if err := s.insertNaturalEvalAccessEvent(nodeID, "reinforced", now.Add(-time.Hour)); err != nil {
			return err
		}
	}
	if _, err := s.runNaturalEvalCycle(ctx, now, memorycore.NaturalMemoryRunManual, false, memorycore.NaturalMemoryOptions{}); err != nil {
		return err
	}
	score, err := s.naturalEvalEventScore(nodeID, "reactivated")
	if err != nil {
		return err
	}
	minScore := assertion.MinReactivationScore
	if minScore == 0 {
		minScore = 0.55
	}
	if score < minScore {
		return AssertionFailure{CaseID: s.caseID, Assertion: assertion.Type, Expected: fmt.Sprintf("reactivation_score>=%.3f", minScore), Actual: fmt.Sprintf("reactivation_score=%.3f", score)}
	}
	return nil
}

func (s *runState) assertNaturalFirstSleepOnce(ctx context.Context, assertion Assertion) error {
	now := naturalEvalNow()
	nodeID := s.naturalEvalNodeID("first_sleep")
	ageHours := naturalEvalInt(assertion.AgeHours, 12)
	opts := memorycore.DefaultNaturalMemoryOptions()
	if assertion.FirstSleepWindowHours > 0 {
		opts.Scoring.FirstSleepWindow = time.Duration(assertion.FirstSleepWindowHours) * time.Hour
	}
	if err := s.seedNaturalEvalFact(nodeID, core.FactTypeStablePreference, naturalEvalFactOptions{
		Predicate:          "natural_first_sleep",
		CreatedAt:          now.Add(-time.Duration(ageHours) * time.Hour),
		AccessCount:        3,
		ReinforcementCount: 1,
	}); err != nil {
		return err
	}
	if err := s.insertNaturalEvalAccessEvent(nodeID, "prompt_injected", now.Add(-time.Hour)); err != nil {
		return err
	}
	if err := s.insertNaturalEvalAccessEvent(nodeID, "reinforced", now.Add(-30*time.Minute)); err != nil {
		return err
	}
	if _, err := s.runNaturalEvalCycle(ctx, now, memorycore.NaturalMemoryRunManual, false, opts); err != nil {
		return err
	}
	if _, err := s.runNaturalEvalCycle(ctx, now.Add(2*time.Hour), memorycore.NaturalMemoryRunManual, false, opts); err != nil {
		return err
	}
	count, err := s.naturalEvalEventCount(nodeID, "first_sleep_consolidated")
	if err != nil {
		return err
	}
	if count != 1 {
		return AssertionFailure{CaseID: s.caseID, Assertion: assertion.Type, Expected: "first_sleep_consolidated event count=1", Actual: fmt.Sprintf("count=%d", count)}
	}
	return nil
}

func (s *runState) assertNaturalProtectedMemory(ctx context.Context, assertion Assertion) error {
	now := naturalEvalNow()
	nodeID := s.naturalEvalNodeID("protected")
	opts := memorycore.DefaultNaturalMemoryOptions()
	if assertion.ProtectedMinTier != "" {
		opts.Protection.ProtectedMinTier = memorycore.SearchTier(assertion.ProtectedMinTier)
	}
	if err := s.seedNaturalEvalFact(nodeID, naturalEvalFactType(assertion.FactType, core.FactTypeCoreIdentity), naturalEvalFactOptions{
		Predicate:  "natural_protected",
		CreatedAt:  now.AddDate(0, 0, -90),
		Pinned:     assertion.Pinned,
		SearchTier: core.SearchTierDeepCold,
	}); err != nil {
		return err
	}
	if _, err := s.runNaturalEvalCycle(ctx, now, memorycore.NaturalMemoryRunManual, false, opts); err != nil {
		return err
	}
	count, err := s.naturalEvalEventCount(nodeID, "protected")
	if err != nil {
		return err
	}
	if count != 1 {
		return AssertionFailure{CaseID: s.caseID, Assertion: assertion.Type, Expected: "protected event count=1", Actual: fmt.Sprintf("count=%d", count)}
	}
	tier, err := s.naturalEvalSearchTier(nodeID)
	if err != nil {
		return err
	}
	if naturalEvalTierRank(tier) > naturalEvalTierRank(core.SearchTier(opts.Protection.ProtectedMinTier)) {
		return AssertionFailure{CaseID: s.caseID, Assertion: assertion.Type, Expected: "tier no colder than " + string(opts.Protection.ProtectedMinTier), Actual: "tier=" + string(tier)}
	}
	return nil
}

func (s *runState) assertNaturalSearchDocumentWriteScope(ctx context.Context, assertion Assertion) error {
	now := naturalEvalNow()
	nodeID := s.naturalEvalNodeID("write_scope")
	if err := s.seedNaturalEvalFact(nodeID, core.FactTypeTransientContext, naturalEvalFactOptions{
		Predicate:  "natural_write_scope",
		CreatedAt:  now.AddDate(0, 0, -90),
		SearchTier: core.SearchTierHot,
	}); err != nil {
		return err
	}
	before, err := s.naturalEvalSearchSnapshot(nodeID)
	if err != nil {
		return err
	}
	if _, err := s.runNaturalEvalCycle(ctx, now, memorycore.NaturalMemoryRunManual, false, memorycore.NaturalMemoryOptions{}); err != nil {
		return err
	}
	after, err := s.naturalEvalSearchSnapshot(nodeID)
	if err != nil {
		return err
	}
	changed := before.changedColumns(after)
	sort.Strings(changed)
	allowed := append([]string(nil), assertion.AllowedColumns...)
	sort.Strings(allowed)
	for _, column := range changed {
		if !containsString(allowed, column) {
			return AssertionFailure{CaseID: s.caseID, Assertion: assertion.Type, Expected: "changed columns within " + strings.Join(allowed, ","), Actual: "changed=" + strings.Join(changed, ",")}
		}
	}
	if !containsString(changed, "search_tier") || !containsString(changed, "updated_at") {
		return AssertionFailure{CaseID: s.caseID, Assertion: assertion.Type, Expected: "search_tier and updated_at changed", Actual: "changed=" + strings.Join(changed, ",")}
	}
	return nil
}

func (s *runState) assertNaturalSkipVisibility(ctx context.Context, assertion Assertion) error {
	now := naturalEvalNow()
	statuses := assertion.SkippedVisibilityStatuses
	if len(statuses) == 0 {
		statuses = []string{"hidden", "forgotten", "purged"}
	}
	for _, status := range statuses {
		if err := s.seedNaturalEvalFact(s.naturalEvalNodeID(status), core.FactTypeTransientContext, naturalEvalFactOptions{
			Predicate:  "natural_skip_" + status,
			CreatedAt:  now.AddDate(0, 0, -30),
			Visibility: core.VisibilityStatus(status),
			SearchTier: core.SearchTierHot,
		}); err != nil {
			return err
		}
	}
	visibleID := s.naturalEvalNodeID("visible")
	if err := s.seedNaturalEvalFact(visibleID, core.FactTypeTransientContext, naturalEvalFactOptions{Predicate: "natural_skip_visible", CreatedAt: now.AddDate(0, 0, -30)}); err != nil {
		return err
	}
	if _, err := s.runNaturalEvalCycle(ctx, now, memorycore.NaturalMemoryRunManual, false, memorycore.NaturalMemoryOptions{}); err != nil {
		return err
	}
	for _, status := range statuses {
		count, err := s.naturalEvalStateCount(s.naturalEvalNodeID(status))
		if err != nil {
			return err
		}
		if count != 0 {
			return AssertionFailure{CaseID: s.caseID, Assertion: assertion.Type, Expected: status + " state count=0", Actual: fmt.Sprintf("count=%d", count)}
		}
	}
	return nil
}

func (s *runState) assertNaturalSleepCycleOncePerDay(ctx context.Context, assertion Assertion) error {
	now := naturalEvalScheduledNow(assertion)
	if err := s.seedNaturalEvalFact(s.naturalEvalNodeID("sleep"), core.FactTypeTransientContext, naturalEvalFactOptions{Predicate: "natural_sleep", CreatedAt: now.AddDate(0, 0, -30)}); err != nil {
		return err
	}
	first, err := s.service.Ops().RunNaturalMemoryTick(ctx, memorycore.RunNaturalMemoryTickRequest{PersonaID: defaultPersonaID, Now: now})
	if err != nil {
		return err
	}
	second, err := s.service.Ops().RunNaturalMemoryTick(ctx, memorycore.RunNaturalMemoryTickRequest{PersonaID: defaultPersonaID, Now: now.Add(time.Hour)})
	if err != nil {
		return err
	}
	if first.Status != memorycore.NaturalMemoryRunStatusCompleted || second.Status != memorycore.NaturalMemoryRunStatusSkipped {
		return AssertionFailure{CaseID: s.caseID, Assertion: assertion.Type, Expected: "completed then skipped", Actual: fmt.Sprintf("%s then %s", first.Status, second.Status)}
	}
	return nil
}

func (s *runState) assertNaturalManualQuota(ctx context.Context, assertion Assertion) error {
	now := naturalEvalNow()
	opts := memorycore.DefaultNaturalMemoryOptions()
	opts.ManualTrigger.MarkSleepCycleByDefault = assertion.MarkSleepCycleByDefault
	if err := s.seedNaturalEvalFact(s.naturalEvalNodeID("manual_quota"), core.FactTypeTransientContext, naturalEvalFactOptions{Predicate: "natural_manual_quota", CreatedAt: now.AddDate(0, 0, -30)}); err != nil {
		return err
	}
	manual, err := s.runNaturalEvalCycle(ctx, now, memorycore.NaturalMemoryRunManual, false, opts)
	if err != nil {
		return err
	}
	sleep, err := s.service.Ops().RunNaturalMemoryTick(ctx, memorycore.RunNaturalMemoryTickRequest{PersonaID: defaultPersonaID, Now: now, Options: opts})
	if err != nil {
		return err
	}
	nextDay := now.AddDate(0, 0, 1)
	marked, err := s.service.Ops().RunNaturalMemoryCycle(ctx, memorycore.RunNaturalMemoryCycleRequest{PersonaID: defaultPersonaID, RunKind: memorycore.NaturalMemoryRunManual, Now: nextDay, MarkSleepCycle: true, Options: opts})
	if err != nil {
		return err
	}
	skipped, err := s.service.Ops().RunNaturalMemoryTick(ctx, memorycore.RunNaturalMemoryTickRequest{PersonaID: defaultPersonaID, Now: nextDay.Add(time.Hour), Options: opts})
	if err != nil {
		return err
	}
	if manual.Status != memorycore.NaturalMemoryRunStatusCompleted || sleep.Status != memorycore.NaturalMemoryRunStatusCompleted || marked.Status != memorycore.NaturalMemoryRunStatusCompleted || skipped.Status != memorycore.NaturalMemoryRunStatusSkipped {
		return AssertionFailure{CaseID: s.caseID, Assertion: assertion.Type, Expected: "manual completed, sleep completed, marked completed, tick skipped", Actual: fmt.Sprintf("%s,%s,%s,%s", manual.Status, sleep.Status, marked.Status, skipped.Status)}
	}
	return nil
}

func (s *runState) assertNaturalCompressionCandidate(ctx context.Context, assertion Assertion) error {
	now := naturalEvalNow()
	minCluster := naturalEvalInt(assertion.MinClusterSize, 3)
	for i := 0; i < minCluster; i++ {
		if err := s.seedNaturalEvalFact(s.naturalEvalNodeID(fmt.Sprintf("compression_%d", i)), core.FactTypeTransientContext, naturalEvalFactOptions{
			Predicate:  "natural_compression",
			CreatedAt:  now.AddDate(0, 0, -90),
			SearchTier: core.SearchTierHot,
		}); err != nil {
			return err
		}
	}
	result, err := s.runNaturalEvalCycle(ctx, now, memorycore.NaturalMemoryRunManual, false, memorycore.NaturalMemoryOptions{})
	if err != nil {
		return err
	}
	if result.CompressionCandidates != 1 {
		return AssertionFailure{CaseID: s.caseID, Assertion: assertion.Type, Expected: "compression_candidates=1", Actual: fmt.Sprintf("compression_candidates=%d", result.CompressionCandidates)}
	}
	count, err := s.naturalEvalEventTypeCount("compression_candidate_emitted")
	if err != nil {
		return err
	}
	if count != 1 {
		return AssertionFailure{CaseID: s.caseID, Assertion: assertion.Type, Expected: "compression_candidate_emitted count=1", Actual: fmt.Sprintf("count=%d", count)}
	}
	if assertion.SourceLifecycleStatusUnchanged {
		lifecycle, err := s.naturalEvalFactLifecycle(s.naturalEvalNodeID("compression_0"))
		if err != nil {
			return err
		}
		if lifecycle != core.LifecycleActive {
			return AssertionFailure{CaseID: s.caseID, Assertion: assertion.Type, Expected: "source lifecycle active", Actual: "lifecycle=" + string(lifecycle)}
		}
	}
	return nil
}

func (s *runState) assertNaturalLifecycleCap(ctx context.Context, assertion Assertion) error {
	now := naturalEvalNow()
	archivedID := s.naturalEvalNodeID("archived")
	deepID := s.naturalEvalNodeID("deep_archived")
	seedID := s.naturalEvalNodeID("rewarm_seed")
	for _, item := range []struct {
		id        string
		lifecycle core.LifecycleStatus
	}{
		{id: archivedID, lifecycle: core.LifecycleArchived},
		{id: deepID, lifecycle: core.LifecycleDeepArchived},
		{id: seedID, lifecycle: core.LifecycleActive},
	} {
		if err := s.seedNaturalEvalFact(item.id, core.FactTypeSignificantEvent, naturalEvalFactOptions{
			Predicate:  "natural_lifecycle_cap",
			CreatedAt:  now.AddDate(0, 0, -45),
			Lifecycle:  item.lifecycle,
			SearchTier: core.SearchTierHot,
		}); err != nil {
			return err
		}
	}
	if err := s.insertNaturalEvalAccessEvent(archivedID, "prompt_injected", now.Add(-2*time.Hour)); err != nil {
		return err
	}
	if err := s.insertNaturalEvalAccessEvent(deepID, "prompt_injected", now.Add(-2*time.Hour)); err != nil {
		return err
	}
	if err := s.insertNaturalEvalAccessEvent(seedID, "retrieved", now.Add(-time.Hour)); err != nil {
		return err
	}
	if _, err := s.runNaturalEvalCycle(ctx, now, memorycore.NaturalMemoryRunManual, false, memorycore.NaturalMemoryOptions{}); err != nil {
		return err
	}
	wantArchived := core.SearchTier(naturalEvalString(assertion.ArchivedMaxTier, string(core.SearchTierCold)))
	wantDeep := core.SearchTier(naturalEvalString(assertion.DeepArchivedMaxTier, string(core.SearchTierDeepCold)))
	for _, item := range []struct {
		id   string
		want core.SearchTier
	}{
		{id: archivedID, want: wantArchived},
	} {
		tier, err := s.naturalEvalSearchTier(item.id)
		if err != nil {
			return err
		}
		if tier != item.want {
			return AssertionFailure{CaseID: s.caseID, Assertion: assertion.Type, Expected: fmt.Sprintf("%s tier=%s", item.id, item.want), Actual: fmt.Sprintf("tier=%s", tier)}
		}
		count, err := s.naturalEvalEventCount(item.id, "storage_rewarm_candidate")
		if err != nil {
			return err
		}
		if count != 1 {
			return AssertionFailure{CaseID: s.caseID, Assertion: assertion.Type, Expected: item.id + " storage_rewarm_candidate count=1", Actual: fmt.Sprintf("count=%d", count)}
		}
	}
	deepTier, err := s.naturalEvalSearchTier(deepID)
	if err != nil {
		return err
	}
	deepCount, err := s.naturalEvalEventCount(deepID, "storage_rewarm_candidate")
	if err != nil {
		return err
	}
	if assertion.DeepArchivedSkipped {
		if deepTier != core.SearchTierHot {
			return AssertionFailure{CaseID: s.caseID, Assertion: assertion.Type, Expected: deepID + " tier unchanged hot when skipped", Actual: "tier=" + string(deepTier)}
		}
		if deepCount != 0 {
			return AssertionFailure{CaseID: s.caseID, Assertion: assertion.Type, Expected: deepID + " storage_rewarm_candidate count=0 when skipped", Actual: fmt.Sprintf("count=%d", deepCount)}
		}
		return nil
	}
	if deepTier != wantDeep {
		return AssertionFailure{CaseID: s.caseID, Assertion: assertion.Type, Expected: fmt.Sprintf("%s tier=%s", deepID, wantDeep), Actual: fmt.Sprintf("tier=%s", deepTier)}
	}
	if deepCount != 1 {
		return AssertionFailure{CaseID: s.caseID, Assertion: assertion.Type, Expected: deepID + " storage_rewarm_candidate count=1", Actual: fmt.Sprintf("count=%d", deepCount)}
	}
	return nil
}

type naturalEvalFactOptions struct {
	Predicate          string
	Visibility         core.VisibilityStatus
	Lifecycle          core.LifecycleStatus
	SearchTier         core.SearchTier
	Pinned             bool
	CreatedAt          time.Time
	AccessCount        int
	ReinforcementCount int
}

type naturalEvalSearchSnapshot struct {
	Text        string
	Tier        string
	Visibility  string
	Sensitivity string
	Lifecycle   string
	Searchable  int
	UpdatedAt   string
}

func (s *runState) ensureNaturalEvalPersona() error {
	now := naturalEvalNow().Format(time.RFC3339Nano)
	_, err := s.db.Exec(`
INSERT OR IGNORE INTO personas(id, display_name, created_at, updated_at)
VALUES (?, 'Default', ?, ?)`, defaultPersonaID, now, now)
	return err
}

func (s *runState) seedNaturalEvalFact(id string, factType core.FactType, opts naturalEvalFactOptions) error {
	created := opts.CreatedAt
	if created.IsZero() {
		created = naturalEvalNow().AddDate(0, 0, -45)
	}
	predicate := strings.TrimSpace(opts.Predicate)
	if predicate == "" {
		predicate = "natural_" + id
	}
	visibility := opts.Visibility
	if visibility == "" {
		visibility = core.VisibilityVisible
	}
	lifecycle := opts.Lifecycle
	if lifecycle == "" {
		lifecycle = core.LifecycleActive
	}
	tier := opts.SearchTier
	if tier == "" {
		tier = core.SearchTierHot
	}
	summary := "Natural eval fact " + id
	createdText := created.Format(time.RFC3339Nano)
	_, err := s.db.Exec(`
INSERT INTO facts (
    id, persona_id, predicate, object_literal, content_summary, fact_type,
    ingested_at, extraction_confidence, extraction_confidence_score, importance,
    sensitivity_level, validity_status, visibility_status, lifecycle_status,
    pinned, access_count, reinforcement_count, searchable, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, 'explicit', 0.8, 0.7,
          'normal', 'valid', ?, ?, ?, ?, ?, 1, ?)`,
		id,
		defaultPersonaID,
		predicate,
		id,
		summary,
		string(factType),
		createdText,
		string(visibility),
		string(lifecycle),
		boolToInt(opts.Pinned),
		opts.AccessCount,
		opts.ReinforcementCount,
		createdText,
	)
	if err != nil {
		return fmt.Errorf("seed natural fact %s: %w", id, err)
	}
	_, err = s.db.Exec(`
INSERT INTO memory_search_documents (
    id, persona_id, node_type, node_id, search_text, search_tier,
    visibility_status, sensitivity_level, lifecycle_status, searchable, updated_at
) VALUES (?, ?, 'fact', ?, ?, ?, ?, 'normal', ?, 1, ?)`,
		"search_"+id,
		defaultPersonaID,
		id,
		summary,
		string(tier),
		string(visibility),
		string(lifecycle),
		createdText,
	)
	if err != nil {
		return fmt.Errorf("seed natural search document %s: %w", id, err)
	}
	return nil
}

func (s *runState) seedNaturalEvalState(nodeID string, lastStrengthened time.Time) error {
	_, err := s.db.Exec(`
INSERT INTO memory_natural_states (
    persona_id, node_type, node_id, algorithm_version, natural_strength,
    retrievability, stability_days, decay_exponent, natural_state,
    last_strengthened_at, updated_at
) VALUES (?, 'fact', ?, ?, 1.0, 1.0, 1.0, 0.6, 'salient', ?, ?)`,
		defaultPersonaID,
		nodeID,
		memorycore.NaturalMemoryAlgorithmPowerSleepV1,
		lastStrengthened.Format(time.RFC3339Nano),
		naturalEvalNow().Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("seed natural state %s: %w", nodeID, err)
	}
	return nil
}

func (s *runState) insertNaturalEvalAccessEvent(nodeID string, accessType string, at time.Time) error {
	_, err := s.db.Exec(`
INSERT INTO memory_access_events(id, persona_id, node_type, node_id, access_type, created_at)
VALUES (?, ?, 'fact', ?, ?, ?)`,
		s.naturalEvalNodeID("access_"+nodeID+"_"+accessType),
		defaultPersonaID,
		nodeID,
		accessType,
		at.Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("insert natural access event %s/%s: %w", nodeID, accessType, err)
	}
	return nil
}

func (s *runState) runNaturalEvalCycle(ctx context.Context, now time.Time, runKind memorycore.NaturalMemoryRunKind, markSleepCycle bool, opts memorycore.NaturalMemoryOptions) (*memorycore.RunNaturalMemoryCycleResult, error) {
	result, err := s.service.Ops().RunNaturalMemoryCycle(ctx, memorycore.RunNaturalMemoryCycleRequest{
		PersonaID:      defaultPersonaID,
		RunKind:        runKind,
		Now:            now,
		MarkSleepCycle: markSleepCycle,
		Options:        opts,
	})
	if err != nil {
		return nil, fmt.Errorf("run natural memory cycle: %w", err)
	}
	return result, nil
}

func (s *runState) naturalEvalRetrievability(nodeID string) (float64, error) {
	var value float64
	err := s.db.QueryRow(`
SELECT retrievability
FROM memory_natural_states
WHERE persona_id = ? AND node_type = 'fact' AND node_id = ?`, defaultPersonaID, nodeID).Scan(&value)
	return value, err
}

func (s *runState) naturalEvalEventScore(nodeID string, eventType string) (float64, error) {
	var value float64
	err := s.db.QueryRow(`
SELECT reactivation_score
FROM memory_natural_events
WHERE persona_id = ? AND node_type = 'fact' AND node_id = ? AND event_type = ?
ORDER BY created_at DESC, id DESC
LIMIT 1`, defaultPersonaID, nodeID, eventType).Scan(&value)
	if err == sql.ErrNoRows {
		return 0, AssertionFailure{CaseID: s.caseID, Assertion: eventType, Expected: "event present", Actual: "missing"}
	}
	return value, err
}

func (s *runState) naturalEvalEventCount(nodeID string, eventType string) (int, error) {
	var count int
	err := s.db.QueryRow(`
SELECT COUNT(*)
FROM memory_natural_events
WHERE persona_id = ? AND node_type = 'fact' AND node_id = ? AND event_type = ?`, defaultPersonaID, nodeID, eventType).Scan(&count)
	return count, err
}

func (s *runState) naturalEvalEventTypeCount(eventType string) (int, error) {
	var count int
	err := s.db.QueryRow(`
SELECT COUNT(*)
FROM memory_natural_events
WHERE persona_id = ? AND event_type = ?`, defaultPersonaID, eventType).Scan(&count)
	return count, err
}

func (s *runState) naturalEvalStateCount(nodeID string) (int, error) {
	var count int
	err := s.db.QueryRow(`
SELECT COUNT(*)
FROM memory_natural_states
WHERE persona_id = ? AND node_type = 'fact' AND node_id = ?`, defaultPersonaID, nodeID).Scan(&count)
	return count, err
}

func (s *runState) naturalEvalSearchTier(nodeID string) (core.SearchTier, error) {
	var tier string
	err := s.db.QueryRow(`
SELECT search_tier
FROM memory_search_documents
WHERE persona_id = ? AND node_type = 'fact' AND node_id = ?`, defaultPersonaID, nodeID).Scan(&tier)
	return core.SearchTier(tier), err
}

func (s *runState) naturalEvalFactLifecycle(nodeID string) (core.LifecycleStatus, error) {
	var lifecycle string
	err := s.db.QueryRow(`
SELECT lifecycle_status
FROM facts
WHERE persona_id = ? AND id = ?`, defaultPersonaID, nodeID).Scan(&lifecycle)
	return core.LifecycleStatus(lifecycle), err
}

func (s *runState) naturalEvalSearchSnapshot(nodeID string) (naturalEvalSearchSnapshot, error) {
	var snap naturalEvalSearchSnapshot
	err := s.db.QueryRow(`
SELECT search_text, search_tier, visibility_status, sensitivity_level, lifecycle_status, searchable, updated_at
FROM memory_search_documents
WHERE persona_id = ? AND node_type = 'fact' AND node_id = ?`, defaultPersonaID, nodeID).Scan(
		&snap.Text,
		&snap.Tier,
		&snap.Visibility,
		&snap.Sensitivity,
		&snap.Lifecycle,
		&snap.Searchable,
		&snap.UpdatedAt,
	)
	return snap, err
}

func (s naturalEvalSearchSnapshot) changedColumns(other naturalEvalSearchSnapshot) []string {
	var changed []string
	if s.Text != other.Text {
		changed = append(changed, "search_text")
	}
	if s.Tier != other.Tier {
		changed = append(changed, "search_tier")
	}
	if s.Visibility != other.Visibility {
		changed = append(changed, "visibility_status")
	}
	if s.Sensitivity != other.Sensitivity {
		changed = append(changed, "sensitivity_level")
	}
	if s.Lifecycle != other.Lifecycle {
		changed = append(changed, "lifecycle_status")
	}
	if s.Searchable != other.Searchable {
		changed = append(changed, "searchable")
	}
	if s.UpdatedAt != other.UpdatedAt {
		changed = append(changed, "updated_at")
	}
	return changed
}

func (s *runState) naturalEvalNodeID(suffix string) string {
	base := strings.ToLower(s.caseID + "_" + suffix)
	base = strings.ReplaceAll(base, "-", "_")
	base = strings.ReplaceAll(base, " ", "_")
	return base
}

func naturalEvalNow() time.Time {
	return time.Date(2026, 6, 5, 3, 31, 0, 0, time.FixedZone("CST", 8*60*60))
}

func naturalEvalScheduledNow(assertion Assertion) time.Time {
	localDate := naturalEvalString(assertion.LocalDate, "2026-06-05")
	localTime := naturalEvalString(assertion.LocalTime, "03:30")
	value, err := time.ParseInLocation("2006-01-02 15:04", localDate+" "+localTime, time.FixedZone("CST", 8*60*60))
	if err != nil {
		return naturalEvalNow()
	}
	return value.Add(time.Minute)
}

func naturalEvalFactType(value string, fallback core.FactType) core.FactType {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return core.FactType(value)
}

func naturalEvalInt(value int, fallback int) int {
	if value == 0 {
		return fallback
	}
	return value
}

func naturalEvalString(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}

func naturalEvalTierRank(tier core.SearchTier) int {
	switch tier {
	case core.SearchTierHot:
		return 0
	case core.SearchTierWarm:
		return 1
	case core.SearchTierCold:
		return 2
	case core.SearchTierDeepCold:
		return 3
	default:
		return 1
	}
}

func containsString(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}
