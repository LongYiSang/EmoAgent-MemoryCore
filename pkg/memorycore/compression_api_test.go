package memorycore_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/longyisang/emoagent-memorycore/pkg/memorycore"
)

func TestServiceApplyCompressionWritesNarrativeInsightAndConsolidatesSources(t *testing.T) {
	ctx := context.Background()
	svc, dbPath := openConsolidationService(t, ctx)
	defer svc.Close()

	sessionID, userID := seedConsolidationSubject(t, ctx, svc)
	firstEpisode := appendConsolidationEpisode(t, ctx, svc, sessionID, "我压力大的时候希望你先理解我。", time.Date(2026, 5, 10, 9, 0, 0, 0, time.UTC))
	secondEpisode := appendConsolidationEpisode(t, ctx, svc, sessionID, "压力大时不要直接给建议。", time.Date(2026, 5, 10, 10, 0, 0, 0, time.UTC))
	firstFact := consolidateLiteral(t, ctx, svc, userID, "likes", "先理解", "用户压力大时希望先被理解。", firstEpisode.ID).Fact
	secondFact := consolidateLiteral(t, ctx, svc, userID, "likes", "不要直接建议", "用户压力大时不喜欢直接给建议。", secondEpisode.ID).Fact

	result, err := svc.ApplyCompression(ctx, memorycore.ApplyCompressionRequest{
		SourceFactIDs: []string{firstFact.ID, secondFact.ID},
		Narrative: &memorycore.NarrativeDraft{
			ID:               "narrative_api",
			Scope:            "topic",
			ScopeRef:         "stress_support",
			Summary:          "用户在压力场景中更需要先被情绪承接。",
			EmotionalTone:    "stressed",
			Importance:       0.72,
			SensitivityLevel: memorycore.SensitivityNormal,
		},
		Insights: []memorycore.InsightDraft{
			{
				ID:               "insight_api",
				InsightType:      "coping_strategy",
				Content:          "压力场景中先共情，再提供建议。",
				Confidence:       0.82,
				Importance:       0.76,
				Valence:          0.1,
				Arousal:          0.3,
				SensitivityLevel: memorycore.SensitivityNormal,
			},
		},
	})
	if err != nil {
		t.Fatalf("apply compression: %v", err)
	}
	if result.NarrativeID != "narrative_api" || len(result.InsightIDs) != 1 || result.InsightIDs[0] != "insight_api" {
		t.Fatalf("compression ids = %#v", result)
	}
	if result.SourceFactsConsolidated != 2 || len(result.DerivedLinkIDs) != 4 || result.SearchDocumentsSynced != 4 || result.MirrorUpdatesEnqueued != 0 {
		t.Fatalf("compression result = %#v", result)
	}

	db := openSQLDB(t, dbPath)
	defer db.Close()
	requireServiceNarrativeRow(t, db, "narrative_api", "用户在压力场景中更需要先被情绪承接。")
	requireServiceInsightRow(t, db, "insight_api", "压力场景中先共情，再提供建议。")
	requireFactLifecycleVisibility(t, db, firstFact.ID, "consolidated", "visible")
	requireFactLifecycleVisibility(t, db, secondFact.ID, "consolidated", "visible")
	requireLink(t, db, "narrative_api", "DERIVED_FROM", firstFact.ID)
	requireLink(t, db, "insight_api", "DERIVED_FROM", secondFact.ID)
}

func TestServiceApplyCompressionValidationErrors(t *testing.T) {
	tests := []struct {
		name                 string
		mutate               func(t *testing.T, db *sql.DB, factID string)
		req                  func(firstFactID string, secondFactID string) memorycore.ApplyCompressionRequest
		wantSecondValidity   string
		wantSecondLifecycle  string
		wantSecondVisibility string
	}{
		{
			name: "requires at least two unique sources",
			req: func(firstFactID string, _ string) memorycore.ApplyCompressionRequest {
				return validCompressionAPIRequest(firstFactID, firstFactID)
			},
		},
		{
			name: "requires generated draft",
			req: func(firstFactID string, secondFactID string) memorycore.ApplyCompressionRequest {
				return memorycore.ApplyCompressionRequest{SourceFactIDs: []string{firstFactID, secondFactID}}
			},
		},
		{
			name: "rejects invalid narrative scope",
			req: func(firstFactID string, secondFactID string) memorycore.ApplyCompressionRequest {
				req := validCompressionAPIRequest(firstFactID, secondFactID)
				req.Narrative.Scope = "year"
				return req
			},
		},
		{
			name: "rejects invalid insight type",
			req: func(firstFactID string, secondFactID string) memorycore.ApplyCompressionRequest {
				req := validCompressionAPIRequest(firstFactID, secondFactID)
				req.Insights[0].InsightType = "prediction"
				return req
			},
		},
		{
			name: "rejects hidden source before mutation",
			mutate: func(t *testing.T, db *sql.DB, factID string) {
				t.Helper()
				if _, err := db.Exec(`UPDATE facts SET visibility_status = 'hidden' WHERE id = ?`, factID); err != nil {
					t.Fatalf("hide fact: %v", err)
				}
			},
			req:                  validCompressionAPIRequest,
			wantSecondVisibility: memorycore.VisibilityHidden,
		},
		{
			name: "rejects invalidated source before mutation",
			mutate: func(t *testing.T, db *sql.DB, factID string) {
				t.Helper()
				if _, err := db.Exec(`UPDATE facts SET validity_status = 'invalidated' WHERE id = ?`, factID); err != nil {
					t.Fatalf("invalidate fact: %v", err)
				}
			},
			req:                validCompressionAPIRequest,
			wantSecondValidity: memorycore.ValidityInvalidated,
		},
		{
			name: "rejects archived source before mutation",
			mutate: func(t *testing.T, db *sql.DB, factID string) {
				t.Helper()
				if _, err := db.Exec(`UPDATE facts SET lifecycle_status = 'archived' WHERE id = ?`, factID); err != nil {
					t.Fatalf("archive fact: %v", err)
				}
			},
			req:                 validCompressionAPIRequest,
			wantSecondLifecycle: "archived",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			svc, dbPath := openConsolidationService(t, ctx)
			defer svc.Close()

			sessionID, userID := seedConsolidationSubject(t, ctx, svc)
			firstEpisode := appendConsolidationEpisode(t, ctx, svc, sessionID, "我压力大的时候希望你先理解我。", time.Date(2026, 5, 10, 9, 0, 0, 0, time.UTC))
			secondEpisode := appendConsolidationEpisode(t, ctx, svc, sessionID, "压力大时不要直接给建议。", time.Date(2026, 5, 10, 10, 0, 0, 0, time.UTC))
			firstFact := consolidateLiteral(t, ctx, svc, userID, "likes", "先理解", "用户压力大时希望先被理解。", firstEpisode.ID).Fact
			secondFact := consolidateLiteral(t, ctx, svc, userID, "likes", "不要直接建议", "用户压力大时不喜欢直接给建议。", secondEpisode.ID).Fact

			db := openSQLDB(t, dbPath)
			defer db.Close()
			if tt.mutate != nil {
				tt.mutate(t, db, secondFact.ID)
			}

			_, err := svc.ApplyCompression(ctx, tt.req(firstFact.ID, secondFact.ID))
			if !errors.Is(err, memorycore.ErrInvalidRequest) {
				t.Fatalf("apply compression err = %v, want ErrInvalidRequest", err)
			}
			requireServiceTableCount(t, db, "narratives", 0)
			requireServiceTableCount(t, db, "insights", 0)
			requireServiceDerivedLinkCount(t, db, 0)
			wantSecondValidity := tt.wantSecondValidity
			if wantSecondValidity == "" {
				wantSecondValidity = memorycore.ValidityValid
			}
			wantSecondLifecycle := tt.wantSecondLifecycle
			if wantSecondLifecycle == "" {
				wantSecondLifecycle = "active"
			}
			wantSecondVisibility := tt.wantSecondVisibility
			if wantSecondVisibility == "" {
				wantSecondVisibility = memorycore.VisibilityVisible
			}
			requireFactValidityStatus(t, db, firstFact.ID, memorycore.ValidityValid)
			requireFactValidityStatus(t, db, secondFact.ID, wantSecondValidity)
			requireFactLifecycleVisibility(t, db, firstFact.ID, "active", "visible")
			requireFactLifecycleVisibility(t, db, secondFact.ID, wantSecondLifecycle, wantSecondVisibility)
		})
	}
}

func validCompressionAPIRequest(firstFactID string, secondFactID string) memorycore.ApplyCompressionRequest {
	return memorycore.ApplyCompressionRequest{
		SourceFactIDs: []string{firstFactID, secondFactID},
		Narrative: &memorycore.NarrativeDraft{
			Scope:            "topic",
			Summary:          "用户在压力场景中更需要先被情绪承接。",
			Importance:       0.72,
			SensitivityLevel: memorycore.SensitivityNormal,
		},
		Insights: []memorycore.InsightDraft{
			{
				InsightType:      "coping_strategy",
				Content:          "压力场景中先共情，再提供建议。",
				Confidence:       0.82,
				Importance:       0.76,
				SensitivityLevel: memorycore.SensitivityNormal,
			},
		},
	}
}

func requireServiceNarrativeRow(t *testing.T, db *sql.DB, narrativeID string, wantSummary string) {
	t.Helper()

	var summary string
	if err := db.QueryRow(`SELECT summary FROM narratives WHERE id = ?`, narrativeID).Scan(&summary); err != nil {
		t.Fatalf("query narrative: %v", err)
	}
	if summary != wantSummary {
		t.Fatalf("narrative summary = %q, want %q", summary, wantSummary)
	}
}

func requireServiceInsightRow(t *testing.T, db *sql.DB, insightID string, wantContent string) {
	t.Helper()

	var content string
	if err := db.QueryRow(`SELECT content FROM insights WHERE id = ?`, insightID).Scan(&content); err != nil {
		t.Fatalf("query insight: %v", err)
	}
	if content != wantContent {
		t.Fatalf("insight content = %q, want %q", content, wantContent)
	}
}

func requireServiceTableCount(t *testing.T, db *sql.DB, table string, want int) {
	t.Helper()

	var got int
	if err := db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&got); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	if got != want {
		t.Fatalf("%s rows = %d, want %d", table, got, want)
	}
}

func requireServiceDerivedLinkCount(t *testing.T, db *sql.DB, want int) {
	t.Helper()

	var got int
	if err := db.QueryRow(`SELECT COUNT(*) FROM memory_links WHERE link_type = 'DERIVED_FROM'`).Scan(&got); err != nil {
		t.Fatalf("count derived links: %v", err)
	}
	if got != want {
		t.Fatalf("derived links = %d, want %d", got, want)
	}
}

func requireFactValidityStatus(t *testing.T, db *sql.DB, factID string, want string) {
	t.Helper()

	var got string
	if err := db.QueryRow(`SELECT validity_status FROM facts WHERE id = ?`, factID).Scan(&got); err != nil {
		t.Fatalf("query fact validity: %v", err)
	}
	if got != want {
		t.Fatalf("fact %s validity = %q, want %q", factID, got, want)
	}
}
