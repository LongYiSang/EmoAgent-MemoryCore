package memorycore_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/longyisang/emoagent-memorycore/pkg/memorycore"
)

func TestServiceRunCurationApplyMergesEquivalentDrinkPreferences(t *testing.T) {
	ctx := context.Background()
	svc, dbPath := openCurationService(t, ctx)
	defer svc.Close()

	sessionID, userID := seedConsolidationSubject(t, ctx, svc)
	oldEpisode := appendConsolidationEpisode(t, ctx, svc, sessionID, "我喜欢喝无糖饮料。", time.Date(2026, 5, 31, 10, 0, 0, 0, time.UTC))
	newEpisode := appendConsolidationEpisode(t, ctx, svc, sessionID, "我喜欢喝不甜的没有糖的饮料。", time.Date(2026, 5, 31, 11, 0, 0, 0, time.UTC))
	first := consolidateLiteral(t, ctx, svc, userID, "likes", "无糖饮料", "用户喜欢喝无糖饮料。", oldEpisode.ID).Fact
	second := consolidateLiteral(t, ctx, svc, userID, "likes", "不甜的没有糖的饮料", "用户喜欢喝不甜的没有糖的饮料。", newEpisode.ID).Fact

	result, err := svc.RunCuration(ctx, memorycore.RunCurationRequest{
		Mode:                   "apply",
		Trigger:                "test",
		ProviderKind:           memorycore.ExtractionProviderMock,
		ProviderID:             "mock",
		Model:                  "memory-curator",
		MinAutoApplyConfidence: 0.88,
		UpdateCheckpoint:       true,
		Force:                  true,
	})
	if err != nil {
		t.Fatalf("run curation: %v", err)
	}
	if result.Status != "succeeded" || result.AppliedGroupCount != 1 || result.GroupCount != 1 {
		t.Fatalf("curation result = %#v", result)
	}
	if len(result.Groups) != 1 || result.Groups[0].CanonicalFactID == "" {
		t.Fatalf("curation groups = %#v", result.Groups)
	}
	canonicalID := result.Groups[0].CanonicalFactID
	sourceID := first.ID
	if canonicalID == first.ID {
		sourceID = second.ID
	}

	db := openSQLDB(t, dbPath)
	defer db.Close()
	requireCurationAPIFactState(t, db, canonicalID, "active", 1)
	requireCurationAPIFactState(t, db, sourceID, "consolidated", 0)
	requireCurationAPIFactSummary(t, db, canonicalID, "用户在饮料上偏好无糖、口味不甜。")
	requireCurationAPISearchDocument(t, db, canonicalID, true)
	requireCurationAPISearchDocument(t, db, sourceID, false)
	requireCurationAPILink(t, db, canonicalID, "EVIDENCED_BY", oldEpisode.ID)
	requireCurationAPILink(t, db, canonicalID, "EVIDENCED_BY", newEpisode.ID)
	requireCurationAPILink(t, db, canonicalID, "DERIVED_FROM", sourceID)
	requireCurationAPIEvidenceOrder(t, db, canonicalID, []string{newEpisode.ID, oldEpisode.ID})
	requireCurationAPIQueue(t, db, "fact", canonicalID, "upsert_node")
	requireCurationAPIQueue(t, db, "fact", sourceID, "delete_node")
	requireCurationAPICheckpoint(t, db, "default", result.RunID)

	contextResult, err := svc.Retrieve(ctx, memorycore.RetrievalRequest{
		SessionID: &sessionID,
		QueryText: "无糖饮料",
		Policy: memorycore.RetrievalPolicy{
			UseFTS:           true,
			UseMirror:        false,
			FinalMemoryCount: 5,
		},
	})
	if err != nil {
		t.Fatalf("retrieve after curation: %v", err)
	}
	requireMemoryItem(t, contextResult, canonicalID, "用户在饮料上偏好无糖、口味不甜。", "")
	requireMemoryItemAbsent(t, contextResult, sourceID)
}

func TestServiceRunCurationDoesNotMergeUnrelatedSamePredicateSources(t *testing.T) {
	ctx := context.Background()
	svc, dbPath := openCurationService(t, ctx)
	defer svc.Close()

	sessionID, userID := seedConsolidationSubject(t, ctx, svc)
	noSugarEpisode := appendConsolidationEpisode(t, ctx, svc, sessionID, "我喜欢喝无糖饮料。", time.Date(2026, 5, 31, 10, 0, 0, 0, time.UTC))
	lowSweetEpisode := appendConsolidationEpisode(t, ctx, svc, sessionID, "我喜欢喝不甜的没有糖的饮料。", time.Date(2026, 5, 31, 11, 0, 0, 0, time.UTC))
	coconutEpisode := appendConsolidationEpisode(t, ctx, svc, sessionID, "我喜欢喝椰子水。", time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC))
	grandmaEpisode := appendConsolidationEpisode(t, ctx, svc, sessionID, "我喜欢外婆做的家常菜。", time.Date(2026, 5, 31, 13, 0, 0, 0, time.UTC))
	first := consolidateLiteral(t, ctx, svc, userID, "likes", "无糖饮料", "用户喜欢喝无糖饮料。", noSugarEpisode.ID).Fact
	second := consolidateLiteral(t, ctx, svc, userID, "likes", "不甜的没有糖的饮料", "用户喜欢喝不甜的没有糖的饮料。", lowSweetEpisode.ID).Fact
	coconut := consolidateLiteral(t, ctx, svc, userID, "likes", "椰子水", "用户喜欢喝椰子水。", coconutEpisode.ID).Fact
	grandma := consolidateLiteral(t, ctx, svc, userID, "likes", "外婆做的家常菜", "用户喜欢外婆做的家常菜。", grandmaEpisode.ID).Fact

	result, err := svc.RunCuration(ctx, memorycore.RunCurationRequest{
		Mode:                   "apply",
		Trigger:                "test",
		ProviderKind:           memorycore.ExtractionProviderMock,
		ProviderID:             "mock",
		MinAutoApplyConfidence: 0.88,
		Force:                  true,
	})
	if err != nil {
		t.Fatalf("run curation unrelated same predicate: %v", err)
	}
	if result.AppliedGroupCount != 1 {
		t.Fatalf("applied group count = %d, want 1; result=%#v", result.AppliedGroupCount, result)
	}

	canonicalID := result.Groups[0].CanonicalFactID
	sugarSourceID := first.ID
	if canonicalID == first.ID {
		sugarSourceID = second.ID
	}

	db := openSQLDB(t, dbPath)
	defer db.Close()
	requireCurationAPIFactState(t, db, canonicalID, "active", 1)
	requireCurationAPIFactState(t, db, sugarSourceID, "consolidated", 0)
	requireCurationAPIFactState(t, db, coconut.ID, "active", 1)
	requireCurationAPIFactState(t, db, grandma.ID, "active", 1)
}

func TestServiceRunCurationAddsCanonicalSourceFactIDBeforeApply(t *testing.T) {
	ctx := context.Background()
	t.Setenv("TEST_CURATION_API_KEY", "test-key")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var providerReq struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&providerReq); err != nil {
			t.Fatalf("decode provider request: %v", err)
		}
		var payload struct {
			Facts []struct {
				FactID        string `json:"fact_id"`
				ObjectLiteral string `json:"object_literal"`
			} `json:"facts"`
		}
		if len(providerReq.Messages) == 0 {
			t.Fatal("provider request had no messages")
		}
		userContent := providerReq.Messages[len(providerReq.Messages)-1].Content
		jsonStart := strings.Index(userContent, `{"schema_version":`)
		if jsonStart < 0 {
			t.Fatalf("curation prompt missing request JSON: %s", userContent)
		}
		if err := json.Unmarshal([]byte(userContent[jsonStart:]), &payload); err != nil {
			t.Fatalf("decode curation payload: %v", err)
		}
		var canonicalID, sourceID string
		for _, fact := range payload.Facts {
			switch fact.ObjectLiteral {
			case "椰子水":
				canonicalID = fact.FactID
			case "骑行时喝椰子水":
				sourceID = fact.FactID
			}
		}
		if canonicalID == "" || sourceID == "" {
			t.Fatalf("payload facts = %#v, want coconut canonical and riding source", payload.Facts)
		}
		content, err := json.Marshal(map[string]any{
			"schema_version":              "memory_delta_curation.v0.1.response",
			"decision":                    "merge_into_existing",
			"semantic_relation":           "refinement",
			"answer_gain":                 "small",
			"confidence":                  0.9,
			"canonical_fact_id":           canonicalID,
			"source_fact_ids":             []string{sourceID},
			"merged_content_summary":      "用户喜欢喝椰子水，尤其是在骑行时和骑行后喝冰椰子水。",
			"canonical_subject_entity_id": "ent_user",
			"canonical_predicate":         "likes",
			"canonical_fact_type":         "stable_preference",
			"canonical_object_literal":    "椰子水",
			"canonical_object_entity_id":  nil,
			"reason_codes":                []string{"refines_context", "adds_condition"},
			"requires_review":             false,
		})
		if err != nil {
			t.Fatalf("marshal curation response: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model": "curation-test",
			"choices": []any{map[string]any{
				"finish_reason": "stop",
				"message": map[string]any{
					"content": string(content),
				},
			}},
		})
	}))
	defer server.Close()

	dbPath := filepath.Join(t.TempDir(), "memory.db")
	svc, err := memorycore.Open(ctx, memorycore.Options{
		DBPath:      dbPath,
		AutoMigrate: true,
		EnableFTS:   true,
		SemanticOps: memorycore.SemanticOpsOptions{
			Curation: memorycore.SemanticCurationOptions{
				Enabled: true,
				LLM: memorycore.CurationLLMOptions{
					Provider: memorycore.ExtractionProviderOptions{
						Kind:      memorycore.ExtractionProviderOpenAICompatible,
						ID:        "curation_test",
						BaseURL:   server.URL,
						APIKeyEnv: "TEST_CURATION_API_KEY",
						Model:     "curation-test",
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("open curation service: %v", err)
	}
	defer svc.Close()

	sessionID, userID := seedConsolidationSubject(t, ctx, svc)
	oldEpisode := appendConsolidationEpisode(t, ctx, svc, sessionID, "我喜欢喝椰子水。", time.Date(2026, 5, 31, 10, 0, 0, 0, time.UTC))
	newEpisode := appendConsolidationEpisode(t, ctx, svc, sessionID, "我喜欢在骑行时喝椰子水，骑完后也喝冰椰子水。", time.Date(2026, 5, 31, 11, 0, 0, 0, time.UTC))
	canonical := consolidateLiteral(t, ctx, svc, userID, "likes", "椰子水", "用户喜欢喝椰子水。", oldEpisode.ID).Fact
	source := consolidateLiteral(t, ctx, svc, userID, "likes", "骑行时喝椰子水", "用户喜欢在骑行时喝椰子水，骑完后也喝冰椰子水。", newEpisode.ID).Fact

	result, err := svc.RunCuration(ctx, memorycore.RunCurationRequest{
		Mode:                   "apply",
		Trigger:                "test",
		MinAutoApplyConfidence: 0.88,
		Force:                  true,
	})
	if err != nil {
		t.Fatalf("run curation source normalization: %v", err)
	}
	if result.AppliedGroupCount != 1 || result.ReviewGroupCount != 0 {
		t.Fatalf("curation result = %#v, want applied with no review", result)
	}
	if len(result.Groups) != 1 ||
		!containsCurationAPIString(result.Groups[0].SourceFactIDs, canonical.ID) ||
		!containsCurationAPIString(result.Groups[0].SourceFactIDs, source.ID) ||
		!containsCurationAPIString(result.Groups[0].ReasonCodes, "canonical_source_fact_id_added") {
		t.Fatalf("curation groups = %#v, want canonical source id normalized", result.Groups)
	}

	db := openSQLDB(t, dbPath)
	defer db.Close()
	requireCurationAPIFactState(t, db, canonical.ID, "active", 1)
	requireCurationAPIFactState(t, db, source.ID, "consolidated", 0)
}

func TestServiceRunCurationDoesNotAutoMergeComplementFacts(t *testing.T) {
	ctx := context.Background()
	svc, dbPath := openCurationService(t, ctx)
	defer svc.Close()

	sessionID, userID := seedConsolidationSubject(t, ctx, svc)
	likeEpisode := appendConsolidationEpisode(t, ctx, svc, sessionID, "我喜欢无糖饮料。", time.Date(2026, 5, 31, 10, 0, 0, 0, time.UTC))
	dislikeEpisode := appendConsolidationEpisode(t, ctx, svc, sessionID, "我讨厌代糖味。", time.Date(2026, 5, 31, 11, 0, 0, 0, time.UTC))
	likeFact := consolidateLiteral(t, ctx, svc, userID, "likes", "无糖饮料", "用户喜欢无糖饮料。", likeEpisode.ID).Fact
	dislikeFact := consolidateLiteral(t, ctx, svc, userID, "dislikes", "代糖味", "用户讨厌代糖味。", dislikeEpisode.ID).Fact

	result, err := svc.RunCuration(ctx, memorycore.RunCurationRequest{
		Mode:                   "apply",
		Trigger:                "test",
		ProviderKind:           memorycore.ExtractionProviderMock,
		ProviderID:             "mock",
		MinAutoApplyConfidence: 0.88,
		Force:                  true,
	})
	if err != nil {
		t.Fatalf("run curation complement: %v", err)
	}
	if result.AppliedGroupCount != 0 {
		t.Fatalf("applied complement groups = %d, want 0", result.AppliedGroupCount)
	}

	db := openSQLDB(t, dbPath)
	defer db.Close()
	requireCurationAPIFactState(t, db, likeFact.ID, "active", 1)
	requireCurationAPIFactState(t, db, dislikeFact.ID, "active", 1)
}

func TestServiceRunCurationPinnedSourceRequiresReview(t *testing.T) {
	ctx := context.Background()
	svc, dbPath := openCurationService(t, ctx)
	defer svc.Close()

	sessionID, userID := seedConsolidationSubject(t, ctx, svc)
	oldEpisode := appendConsolidationEpisode(t, ctx, svc, sessionID, "我喜欢喝无糖饮料。", time.Date(2026, 5, 31, 10, 0, 0, 0, time.UTC))
	newEpisode := appendConsolidationEpisode(t, ctx, svc, sessionID, "我喜欢喝不甜的没有糖的饮料。", time.Date(2026, 5, 31, 11, 0, 0, 0, time.UTC))
	first := consolidateLiteral(t, ctx, svc, userID, "likes", "无糖饮料", "用户喜欢喝无糖饮料。", oldEpisode.ID).Fact
	second := consolidateLiteral(t, ctx, svc, userID, "likes", "不甜的没有糖的饮料", "用户喜欢喝不甜的没有糖的饮料。", newEpisode.ID).Fact

	db := openSQLDB(t, dbPath)
	defer db.Close()
	if _, err := db.Exec(`UPDATE facts SET created_at = ? WHERE id = ?`, "2026-05-31T10:00:00Z", first.ID); err != nil {
		t.Fatalf("set canonical candidate created_at: %v", err)
	}
	if _, err := db.Exec(`UPDATE facts SET pinned = 1, pin_actor = 'user', pin_reason = 'manual', created_at = ? WHERE id = ?`, "2026-05-31T11:00:00Z", second.ID); err != nil {
		t.Fatalf("pin source fact: %v", err)
	}

	result, err := svc.RunCuration(ctx, memorycore.RunCurationRequest{
		Mode:                   "apply",
		Trigger:                "test",
		ProviderKind:           memorycore.ExtractionProviderMock,
		ProviderID:             "mock",
		MinAutoApplyConfidence: 0.88,
		UpdateCheckpoint:       true,
		Force:                  true,
	})
	if err != nil {
		t.Fatalf("run curation pinned source: %v", err)
	}
	if result.AppliedGroupCount != 0 || result.ReviewGroupCount != 1 {
		t.Fatalf("pinned curation result = %#v", result)
	}
	if len(result.Groups) != 1 || result.Groups[0].GroupStatus != "needs_review" {
		t.Fatalf("pinned group result = %#v", result.Groups)
	}
	requireCurationAPIFactState(t, db, first.ID, "active", 1)
	requireCurationAPIFactState(t, db, second.ID, "active", 1)
	requireCurationAPINoCheckpoint(t, db, "default")
}

func TestServiceRunCurationRawLogRecordsSchemaDriftProviderResponse(t *testing.T) {
	ctx := context.Background()
	rawDir := t.TempDir()
	t.Setenv("TEST_CURATION_API_KEY", "test-key")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"curation-test","choices":[{"finish_reason":"stop","message":{"content":"{\"actions\":[{\"type\":\"create_canonical_fact\"}]}","reasoning_content":"{\"reasoning\":true}"}}]}`))
	}))
	defer server.Close()

	dbPath := filepath.Join(t.TempDir(), "memory.db")
	svc, err := memorycore.Open(ctx, memorycore.Options{
		DBPath:      dbPath,
		AutoMigrate: true,
		EnableFTS:   true,
		SemanticOps: memorycore.SemanticOpsOptions{
			Curation: memorycore.SemanticCurationOptions{
				Enabled: true,
				RawLog:  memorycore.CurationRawLogOptions{Enabled: true, Directory: rawDir},
				LLM: memorycore.CurationLLMOptions{
					Provider: memorycore.ExtractionProviderOptions{
						Kind:      memorycore.ExtractionProviderOpenAICompatible,
						ID:        "curation_test",
						BaseURL:   server.URL,
						APIKeyEnv: "TEST_CURATION_API_KEY",
						Model:     "curation-test",
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("open curation service: %v", err)
	}
	defer svc.Close()

	sessionID, userID := seedConsolidationSubject(t, ctx, svc)
	oldEpisode := appendConsolidationEpisode(t, ctx, svc, sessionID, "我喜欢喝无糖饮料。", time.Date(2026, 5, 31, 10, 0, 0, 0, time.UTC))
	newEpisode := appendConsolidationEpisode(t, ctx, svc, sessionID, "我喜欢喝不甜的没有糖的饮料。", time.Date(2026, 5, 31, 11, 0, 0, 0, time.UTC))
	consolidateLiteral(t, ctx, svc, userID, "likes", "无糖饮料", "用户喜欢喝无糖饮料。", oldEpisode.ID)
	consolidateLiteral(t, ctx, svc, userID, "likes", "不甜的没有糖的饮料", "用户喜欢喝不甜的没有糖的饮料。", newEpisode.ID)

	result, err := svc.RunCuration(ctx, memorycore.RunCurationRequest{
		Mode:    "dry_run",
		Trigger: "test",
		Force:   true,
	})
	if err != nil {
		t.Fatalf("run curation with raw log: %v", err)
	}
	if len(result.Groups) != 1 || !containsCurationAPIString(result.Groups[0].ReasonCodes, "unsupported_response_shape=actions") {
		t.Fatalf("curation groups = %#v, want actions shape drift reason", result.Groups)
	}

	artifact := readSingleCurationRawLog(t, rawDir)
	if artifact["schema_version"] != "memory_curation_raw_log.v0.1" {
		t.Fatalf("schema_version = %#v", artifact["schema_version"])
	}
	groups, ok := artifact["groups"].([]any)
	if !ok || len(groups) != 1 {
		t.Fatalf("raw log groups = %#v", artifact["groups"])
	}
	group, ok := groups[0].(map[string]any)
	if !ok {
		t.Fatalf("raw log group = %#v", groups[0])
	}
	llm, ok := group["llm"].(map[string]any)
	if !ok {
		t.Fatalf("raw log llm = %#v", group["llm"])
	}
	response, ok := llm["response"].(map[string]any)
	if !ok {
		t.Fatalf("raw log response = %#v", llm["response"])
	}
	if response["provider_raw_response"] == "" || response["content_text"] == "" || response["reasoning_text"] == "" || response["text_source"] != "content" {
		t.Fatalf("raw log response = %#v, want raw/content/reasoning/source", response)
	}
	decision, ok := group["decision"].(map[string]any)
	if !ok {
		t.Fatalf("raw log decision = %#v", group["decision"])
	}
	reasons, ok := decision["reason_codes"].([]any)
	if !ok || !containsAnyString(reasons, "unsupported_response_shape=actions") {
		t.Fatalf("raw log decision = %#v, want shape reason", decision)
	}
}

func TestServiceRunCurationRawLogRecordsProviderErrorResponse(t *testing.T) {
	ctx := context.Background()
	rawDir := t.TempDir()
	t.Setenv("TEST_CURATION_API_KEY", "test-key")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"curation-test","choices":[{"finish_reason":"stop","message":{"content":"","reasoning_content":"{\"decision\":\"needs_review\"}"}}]}`))
	}))
	defer server.Close()

	dbPath := filepath.Join(t.TempDir(), "memory.db")
	svc, err := memorycore.Open(ctx, memorycore.Options{
		DBPath:      dbPath,
		AutoMigrate: true,
		EnableFTS:   true,
		SemanticOps: memorycore.SemanticOpsOptions{
			Curation: memorycore.SemanticCurationOptions{
				Enabled: true,
				RawLog:  memorycore.CurationRawLogOptions{Enabled: true, Directory: rawDir},
				LLM: memorycore.CurationLLMOptions{
					Provider: memorycore.ExtractionProviderOptions{
						Kind:      memorycore.ExtractionProviderOpenAICompatible,
						ID:        "curation_test",
						BaseURL:   server.URL,
						APIKeyEnv: "TEST_CURATION_API_KEY",
						Model:     "curation-test",
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("open curation service: %v", err)
	}
	defer svc.Close()

	sessionID, userID := seedConsolidationSubject(t, ctx, svc)
	oldEpisode := appendConsolidationEpisode(t, ctx, svc, sessionID, "我喜欢喝无糖饮料。", time.Date(2026, 5, 31, 10, 0, 0, 0, time.UTC))
	newEpisode := appendConsolidationEpisode(t, ctx, svc, sessionID, "我喜欢喝不甜的没有糖的饮料。", time.Date(2026, 5, 31, 11, 0, 0, 0, time.UTC))
	consolidateLiteral(t, ctx, svc, userID, "likes", "无糖饮料", "用户喜欢喝无糖饮料。", oldEpisode.ID)
	consolidateLiteral(t, ctx, svc, userID, "likes", "不甜的没有糖的饮料", "用户喜欢喝不甜的没有糖的饮料。", newEpisode.ID)

	result, err := svc.RunCuration(ctx, memorycore.RunCurationRequest{
		Mode:    "dry_run",
		Trigger: "test",
		Force:   true,
	})
	if err == nil || !strings.Contains(err.Error(), "provider response content was empty") {
		t.Fatalf("run curation error = %v, want empty content error", err)
	}
	if result == nil || result.Status != "failed" || result.ErrorCount != 1 {
		t.Fatalf("result = %#v, want failed result", result)
	}

	artifact := readSingleCurationRawLog(t, rawDir)
	groups, ok := artifact["groups"].([]any)
	if !ok || len(groups) != 1 {
		t.Fatalf("raw log groups = %#v", artifact["groups"])
	}
	group, ok := groups[0].(map[string]any)
	if !ok {
		t.Fatalf("raw log group = %#v", groups[0])
	}
	if parseErr, _ := group["parse_error"].(string); !strings.Contains(parseErr, "provider response content was empty") {
		t.Fatalf("parse_error = %#v, want provider error", group["parse_error"])
	}
	llm, ok := group["llm"].(map[string]any)
	if !ok {
		t.Fatalf("raw log llm = %#v", group["llm"])
	}
	response, ok := llm["response"].(map[string]any)
	if !ok {
		t.Fatalf("raw log response = %#v", llm["response"])
	}
	if response["provider_raw_response"] == "" || response["reasoning_text"] == "" || response["text_source"] != "content" {
		t.Fatalf("raw log response = %#v, want raw/reasoning/source despite provider error", response)
	}
}

func openCurationService(t *testing.T, ctx context.Context) (memorycore.Service, string) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	svc, err := memorycore.Open(ctx, memorycore.Options{
		DBPath:      dbPath,
		AutoMigrate: true,
		EnableFTS:   true,
		Now: func() time.Time {
			return time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC)
		},
	})
	if err != nil {
		t.Fatalf("open curation service: %v", err)
	}
	return svc, dbPath
}

func requireMemoryItemAbsent(t *testing.T, contextResult *memorycore.MemoryContext, nodeID string) {
	t.Helper()
	for _, block := range contextResult.Blocks {
		for _, item := range block.Items {
			if item.NodeID == nodeID {
				t.Fatalf("memory item %s unexpectedly present in %#v", nodeID, contextResult.Blocks)
			}
		}
	}
}

func requireCurationAPIFactState(t *testing.T, db *sql.DB, factID string, wantLifecycle string, wantSearchable int) {
	t.Helper()
	var lifecycle string
	var searchable int
	if err := db.QueryRow(`SELECT lifecycle_status, searchable FROM facts WHERE id = ?`, factID).Scan(&lifecycle, &searchable); err != nil {
		t.Fatalf("query fact state: %v", err)
	}
	if lifecycle != wantLifecycle || searchable != wantSearchable {
		t.Fatalf("fact %s state = %s/%d, want %s/%d", factID, lifecycle, searchable, wantLifecycle, wantSearchable)
	}
}

func requireCurationAPIFactSummary(t *testing.T, db *sql.DB, factID string, want string) {
	t.Helper()
	var got string
	if err := db.QueryRow(`SELECT content_summary FROM facts WHERE id = ?`, factID).Scan(&got); err != nil {
		t.Fatalf("query fact summary: %v", err)
	}
	if got != want {
		t.Fatalf("summary = %q, want %q", got, want)
	}
}

func requireCurationAPISearchDocument(t *testing.T, db *sql.DB, factID string, wantPresent bool) {
	t.Helper()
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM memory_search_documents WHERE node_type = 'fact' AND node_id = ?`, factID).Scan(&count); err != nil {
		t.Fatalf("count search document: %v", err)
	}
	if (count > 0) != wantPresent {
		t.Fatalf("search document %s present = %t, want %t", factID, count > 0, wantPresent)
	}
}

func requireCurationAPILink(t *testing.T, db *sql.DB, fromID string, linkType string, toID string) {
	t.Helper()
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM memory_links WHERE from_node_id = ? AND link_type = ? AND to_node_id = ?`, fromID, linkType, toID).Scan(&count); err != nil {
		t.Fatalf("count curation link: %v", err)
	}
	if count != 1 {
		t.Fatalf("link %s -%s-> %s count = %d, want 1", fromID, linkType, toID, count)
	}
}

func requireCurationAPIEvidenceOrder(t *testing.T, db *sql.DB, factID string, want []string) {
	t.Helper()
	rows, err := db.Query(`
SELECT e.id
FROM memory_links l
JOIN episodes e
  ON e.persona_id = l.persona_id
 AND e.id = l.to_node_id
WHERE l.from_node_type = 'fact'
  AND l.from_node_id = ?
  AND l.link_type = 'EVIDENCED_BY'
  AND e.visibility_status = 'visible'
ORDER BY e.occurred_at DESC`, factID)
	if err != nil {
		t.Fatalf("query evidence order: %v", err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan evidence order: %v", err)
		}
		got = append(got, id)
	}
	if len(got) != len(want) {
		t.Fatalf("evidence order = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("evidence order = %#v, want %#v", got, want)
		}
	}
}

func requireCurationAPIQueue(t *testing.T, db *sql.DB, nodeType string, nodeID string, operation string) {
	t.Helper()
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM index_sync_queue WHERE node_type = ? AND node_id = ? AND operation = ?`, nodeType, nodeID, operation).Scan(&count); err != nil {
		t.Fatalf("count queue: %v", err)
	}
	if count == 0 {
		t.Fatalf("queue row %s/%s/%s missing", nodeType, nodeID, operation)
	}
}

func requireCurationAPICheckpoint(t *testing.T, db *sql.DB, personaID string, runID string) {
	t.Helper()
	var got string
	if err := db.QueryRow(`SELECT last_successful_run_id FROM memory_curation_checkpoints WHERE persona_id = ?`, personaID).Scan(&got); err != nil {
		t.Fatalf("query curation checkpoint: %v", err)
	}
	if got != runID {
		t.Fatalf("checkpoint run id = %q, want %q", got, runID)
	}
}

func requireCurationAPINoCheckpoint(t *testing.T, db *sql.DB, personaID string) {
	t.Helper()
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM memory_curation_checkpoints WHERE persona_id = ?`, personaID).Scan(&count); err != nil {
		t.Fatalf("count curation checkpoint: %v", err)
	}
	if count != 0 {
		t.Fatalf("checkpoint count = %d, want 0", count)
	}
}

func readSingleCurationRawLog(t *testing.T, dir string) map[string]any {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read curation raw log dir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("raw log files = %d, want 1", len(entries))
	}
	data, err := os.ReadFile(filepath.Join(dir, entries[0].Name()))
	if err != nil {
		t.Fatalf("read curation raw log: %v", err)
	}
	var artifact map[string]any
	if err := json.Unmarshal(data, &artifact); err != nil {
		t.Fatalf("decode curation raw log: %v\n%s", err, string(data))
	}
	return artifact
}

func containsCurationAPIString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func containsAnyString(values []any, want string) bool {
	for _, value := range values {
		if s, ok := value.(string); ok && s == want {
			return true
		}
	}
	return false
}
