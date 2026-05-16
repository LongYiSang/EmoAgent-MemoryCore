package sqlite

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/longyisang/emoagent-memorycore/internal/core"
)

func BenchmarkRetrievalScoringBatchPrefetch_3000Facts(b *testing.B) {
	ctx := context.Background()
	db, repo, facts := openRetrievalBenchmarkDB(b, ctx, 3000, false)
	defer db.Close()

	candidates := make(map[string]retrievalCandidate, len(facts))
	for i, fact := range facts {
		candidates[fact.ID] = retrievalCandidate{
			FactID:           fact.ID,
			FusedAnchorScore: 1,
			AnchorEnergy:     1 - float64(i%10)*0.01,
			GraphEnergy:      0.5,
		}
	}
	req := RetrievalRequest{PersonaID: "default"}
	query := QueryAnalysis{Raw: "benchmark", Normalized: "benchmark", Terms: []string{"benchmark"}}
	policy := RetrievalPolicy{SensitivityPermission: string(core.SensitivityNormal), FinalMemoryCount: 50, ContextBudgetTokens: 100000}

	scored, _, err := repo.scoreCandidates(ctx, req, query, policy, prefetchNow(), candidates)
	if err != nil {
		b.Fatalf("warm scoreCandidates: %v", err)
	}
	if len(scored) != len(facts) {
		b.Fatalf("warm scored count = %d, want %d", len(scored), len(facts))
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, err := repo.scoreCandidates(ctx, req, query, policy, prefetchNow(), candidates); err != nil {
			b.Fatalf("scoreCandidates: %v", err)
		}
	}
}

func BenchmarkRetrievalReconstructionBatchPrefetch_3000Facts(b *testing.B) {
	ctx := context.Background()
	db, repo, facts := openRetrievalBenchmarkDB(b, ctx, 3000, true)
	defer db.Close()

	selected := make([]scoredFact, 0, 100)
	for i := 0; i < 100; i++ {
		selected = append(selected, scoredFact{Fact: facts[i], Score: 1, TokenCost: 1})
	}
	req := RetrievalRequest{PersonaID: "default"}
	query := QueryAnalysis{MemoryAbility: MemoryAbilityCausalExplain}
	policy := RetrievalPolicy{SensitivityPermission: string(core.SensitivityNormal), FinalMemoryCount: 100, ContextBudgetTokens: 100000}

	blocks, _, _, err := repo.reconstructMemoryBlocks(ctx, req, query, policy, selected)
	if err != nil {
		b.Fatalf("warm reconstructMemoryBlocks: %v", err)
	}
	if len(blocks) != 1 || len(blocks[0].Items) != len(selected) {
		b.Fatalf("warm blocks = %#v, want one block with %d items", blocks, len(selected))
	}
	if len(blocks[0].Items[0].SourceRefs) == 0 || len(blocks[0].Items[0].RelatedFacts) == 0 {
		b.Fatalf("warm first item missing source refs or related facts: %#v", blocks[0].Items[0])
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, _, err := repo.reconstructMemoryBlocks(ctx, req, query, policy, selected); err != nil {
			b.Fatalf("reconstructMemoryBlocks: %v", err)
		}
	}
}

func openRetrievalBenchmarkDB(b *testing.B, ctx context.Context, factCount int, includeFactLinks bool) (*DB, *RetrievalRepository, []core.Fact) {
	b.Helper()

	db, err := Open(ctx, filepath.Join(b.TempDir(), "memory.db"))
	if err != nil {
		b.Fatalf("open db: %v", err)
	}
	if err := db.Migrate(ctx); err != nil {
		db.Close()
		b.Fatalf("migrate db: %v", err)
	}
	store := NewStore(db.SQLDB())
	if err := store.EnsurePersona(ctx, core.Persona{ID: "default", DisplayName: "Default"}); err != nil {
		b.Fatalf("ensure persona: %v", err)
	}
	if err := store.EnsureSession(ctx, core.Session{ID: "s1", PersonaID: "default", Channel: core.ChannelAPI}); err != nil {
		b.Fatalf("ensure session: %v", err)
	}
	if err := NewEntityRepository(db.SQLDB()).Upsert(ctx, core.Entity{ID: "ent_user", PersonaID: "default", CanonicalName: "Long", EntityType: core.EntityTypeUser}); err != nil {
		b.Fatalf("upsert entity: %v", err)
	}
	if err := NewEpisodeRepository(db.SQLDB()).Append(ctx, core.Episode{
		ID:         "ep_visible",
		PersonaID:  "default",
		SessionID:  "s1",
		Content:    "benchmark source",
		OccurredAt: prefetchNow(),
		SourceType: core.SourceTypeChat,
	}); err != nil {
		b.Fatalf("append episode: %v", err)
	}

	tx, err := db.SQLDB().BeginTx(ctx, nil)
	if err != nil {
		b.Fatalf("begin seed tx: %v", err)
	}
	factStmt, err := tx.PrepareContext(ctx, `
INSERT INTO facts (
    id, persona_id, subject_entity_id, predicate, object_literal,
    content_summary, fact_type, extraction_confidence,
    extraction_confidence_score, importance, sensitivity_level,
    validity_status, visibility_status, lifecycle_status, searchable
) VALUES (?, 'default', 'ent_user', 'likes', ?, ?, 'stable_preference', 'explicit', 0.8, ?, 'normal', 'valid', 'visible', 'active', 1)`)
	if err != nil {
		_ = tx.Rollback()
		b.Fatalf("prepare fact insert: %v", err)
	}
	linkStmt, err := tx.PrepareContext(ctx, `
INSERT INTO memory_links (
    id, persona_id, from_node_type, from_node_id, link_type,
    to_node_type, to_node_id, direction, confidence, weight,
    created_by, visibility_status, searchable
) VALUES (?, 'default', 'fact', ?, ?, ?, ?, 'forward', 1.0, ?, 'system', 'visible', 1)`)
	if err != nil {
		_ = factStmt.Close()
		_ = tx.Rollback()
		b.Fatalf("prepare link insert: %v", err)
	}
	facts := make([]core.Fact, 0, factCount)
	for i := 0; i < factCount; i++ {
		factID := fmt.Sprintf("fact_bench_%04d", i)
		summary := fmt.Sprintf("benchmark fact %04d mentions retrieval batch prefetch", i)
		importance := 0.5 + float64(i%50)/100
		if _, err := factStmt.ExecContext(ctx, factID, summary, summary, importance); err != nil {
			_ = linkStmt.Close()
			_ = factStmt.Close()
			_ = tx.Rollback()
			b.Fatalf("insert fact %s: %v", factID, err)
		}
		if _, err := linkStmt.ExecContext(ctx, "link_evidence_"+factID, factID, string(core.LinkTypeEvidencedBy), string(core.NodeTypeEpisode), "ep_visible", 1.0); err != nil {
			_ = linkStmt.Close()
			_ = factStmt.Close()
			_ = tx.Rollback()
			b.Fatalf("insert evidence link %s: %v", factID, err)
		}
		summaryCopy := summary
		facts = append(facts, core.Fact{
			ID:                        factID,
			PersonaID:                 "default",
			SubjectEntityID:           ptrForPrefetch("ent_user"),
			Predicate:                 "likes",
			ObjectLiteral:             &summaryCopy,
			ContentSummary:            summary,
			FactType:                  core.FactTypeStablePreference,
			ExtractionConfidence:      core.ExtractionConfidenceExplicit,
			ExtractionConfidenceScore: 0.8,
			Importance:                importance,
			SensitivityLevel:          core.SensitivityNormal,
			ValidityStatus:            core.ValidityValid,
			VisibilityStatus:          core.VisibilityVisible,
			LifecycleStatus:           core.LifecycleActive,
			Searchable:                true,
			CreatedAt:                 prefetchNow().Add(-time.Duration(i) * time.Minute),
		})
	}
	if includeFactLinks {
		linkID := 0
		for i := 0; i < factCount; i++ {
			fromID := facts[i].ID
			for offset := 1; offset <= 4; offset++ {
				toID := facts[(i+offset)%factCount].ID
				weight := 1 - float64(offset-1)*0.1
				if _, err := linkStmt.ExecContext(ctx, fmt.Sprintf("link_fact_%05d", linkID), fromID, "CAUSED_BY", string(core.NodeTypeFact), toID, weight); err != nil {
					_ = linkStmt.Close()
					_ = factStmt.Close()
					_ = tx.Rollback()
					b.Fatalf("insert fact link %d: %v", linkID, err)
				}
				linkID++
			}
		}
	}
	if err := linkStmt.Close(); err != nil {
		_ = factStmt.Close()
		_ = tx.Rollback()
		b.Fatalf("close link stmt: %v", err)
	}
	if err := factStmt.Close(); err != nil {
		_ = tx.Rollback()
		b.Fatalf("close fact stmt: %v", err)
	}
	if err := tx.Commit(); err != nil {
		b.Fatalf("commit seed tx: %v", err)
	}
	return db, NewRetrievalRepository(db.SQLDB(), nil, prefetchNow), facts
}
