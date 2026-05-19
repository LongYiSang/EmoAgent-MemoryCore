package eval

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/longyisang/emoagent-memorycore/internal/app/memorycore"
)

func TestLoadFixtureMetadataAndQualityStubPolicy(t *testing.T) {
	fixture, err := LoadFixtureBytes([]byte(`
schema_version: memory_eval.v0.2
suite: quality_retrieval
quality_mode: true
allow_stub: false
case_id: metadata_quality_case
steps:
  - id: q1
    action: retrieve
    retrieve:
      query_text: hello
assertions: []
`))
	if err != nil {
		t.Fatalf("LoadFixtureBytes() error = %v", err)
	}
	if fixture.SchemaVersion != "memory_eval.v0.2" {
		t.Fatalf("SchemaVersion = %q", fixture.SchemaVersion)
	}
	if fixture.Suite != "quality_retrieval" || !fixture.QualityMode || fixture.AllowStub {
		t.Fatalf("metadata = %#v", fixture)
	}
	if err := fixture.ValidateStubPolicy(FixtureStubPolicyForbid); err != nil {
		t.Fatalf("ValidateStubPolicy(forbid) error = %v", err)
	}
}

func TestMatrixRunnerSQLiteProfileDoesNotRequireSidecar(t *testing.T) {
	fixture := minimalRetrievalFixture()
	report := NewMatrixRunner(MatrixRunnerOptions{
		TempDir:  t.TempDir(),
		Profiles: []Profile{ProfileSQLiteGo},
	}).Run(context.Background(), fixture)

	if report.Failed() {
		t.Fatalf("matrix report failed: %s", report.Error())
	}
	if report.TestPlanVersion != matrixTestPlanVersion {
		t.Fatalf("test plan version = %q, want %q", report.TestPlanVersion, matrixTestPlanVersion)
	}
	if len(report.Profiles) != 1 {
		t.Fatalf("profiles len = %d", len(report.Profiles))
	}
	profile := report.Profiles[0]
	if profile.Profile != ProfileSQLiteGo {
		t.Fatalf("profile = %q", profile.Profile)
	}
	if profile.Capability.RequiresSidecar || profile.Capability.RequiresMirror {
		t.Fatalf("sqlite_go capability = %#v", profile.Capability)
	}
	if profile.Metrics.FallbackCount != 0 {
		t.Fatalf("sqlite_go fallback count = %d", profile.Metrics.FallbackCount)
	}
}

func TestMatrixRunnerMirrorDenseRequiresUsedMirror(t *testing.T) {
	fixture := minimalRetrievalFixture()
	adapter := newQualityMirrorAdapter()
	report := NewMatrixRunner(MatrixRunnerOptions{
		TempDir:       t.TempDir(),
		Profiles:      []Profile{ProfileMirrorRealDense},
		MirrorAdapter: adapter,
		Strict:        true,
	}).Run(context.Background(), fixture)

	if report.Failed() {
		t.Fatalf("matrix report failed: %s", report.Error())
	}
	profile := report.Profiles[0]
	if profile.Capability.Status != CapabilityReady {
		t.Fatalf("capability status = %q", profile.Capability.Status)
	}
	if profile.Metrics.MirrorUsedCount == 0 {
		t.Fatalf("mirror used count = 0")
	}
	if adapter.findCalls == 0 {
		t.Fatalf("FindCandidates was not called")
	}
}

func TestMatrixRunnerQueryAnalysisOnlyAppliesToMirrorProfiles(t *testing.T) {
	fixture := minimalRetrievalFixture()
	semanticCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/retrieval/query-analysis" {
			t.Fatalf("unexpected semantic path %s", r.URL.Path)
		}
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode semantic request: %v", err)
		}
		semanticCalls++
		_ = json.NewEncoder(w).Encode(map[string]any{
			"schema_version": "memory_query_analysis_result.v0.1",
			"request_id":     request["request_id"],
			"status":         "ok",
			"provider":       "eval_real_semantic",
			"analysis": map[string]any{
				"time_mode":      "current",
				"memory_domain":  "user_profile_memory",
				"memory_ability": "direct_fact",
				"evidence_need":  "exact_observation",
				"confidence":     0.9,
				"query_rewrites": []map[string]any{{
					"text":    "semantic coffee",
					"purpose": "semantic_recall",
					"weight":  0.8,
				}},
				"policy_hints": map[string]any{},
			},
		})
	}))
	defer server.Close()

	adapter := newQualityMirrorAdapter()
	report := NewMatrixRunner(MatrixRunnerOptions{
		TempDir:       t.TempDir(),
		Profiles:      []Profile{ProfileSQLiteGo, ProfileMirrorRealDense},
		MirrorAdapter: adapter,
		QueryAnalysis: memorycore.QueryAnalysisOptions{
			Provider:   memorycore.QueryAnalysisProviderSidecar,
			Mode:       memorycore.QueryAnalysisModeSemanticAlways,
			SidecarURL: server.URL,
		},
		Strict: true,
	}).Run(context.Background(), fixture)

	if report.Failed() {
		t.Fatalf("matrix report failed: %s", report.Error())
	}
	if semanticCalls != 1 {
		t.Fatalf("semantic calls = %d, want only mirror profile to call once", semanticCalls)
	}
	sqliteAnalysis := firstRetrievalAnalysis(t, report.Profiles[0].Report)
	if sqliteAnalysis.Source != memorycore.QueryAnalysisSourceRuleOnly {
		t.Fatalf("sqlite_go query source = %q, want rule_only", sqliteAnalysis.Source)
	}
	mirrorAnalysis := firstRetrievalAnalysis(t, report.Profiles[1].Report)
	if mirrorAnalysis.Source != memorycore.QueryAnalysisSourceMerged || len(mirrorAnalysis.QueryRewrites) != 1 {
		t.Fatalf("mirror query analysis = %#v, want merged semantic rewrite", mirrorAnalysis)
	}
	if report.Profiles[1].Metrics.MirrorUsedCount == 0 {
		t.Fatalf("mirror profile did not use mirror")
	}
}

func TestMatrixRunnerReusesQueryAnalysisAcrossMirrorProfiles(t *testing.T) {
	fixture := minimalRetrievalFixture()
	semanticCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/retrieval/query-analysis" {
			t.Fatalf("unexpected semantic path %s", r.URL.Path)
		}
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode semantic request: %v", err)
		}
		semanticCalls++
		_ = json.NewEncoder(w).Encode(map[string]any{
			"schema_version": "memory_query_analysis_result.v0.1",
			"request_id":     request["request_id"],
			"status":         "ok",
			"provider":       "eval_real_semantic",
			"analysis": map[string]any{
				"time_mode":      "current",
				"memory_domain":  "user_profile_memory",
				"memory_ability": "direct_fact",
				"evidence_need":  "exact_observation",
				"confidence":     0.9,
				"query_rewrites": []map[string]any{{
					"text":    "semantic coffee",
					"purpose": "semantic_recall",
					"weight":  0.8,
				}},
				"policy_hints": map[string]any{},
			},
		})
	}))
	defer server.Close()

	adapter := newAdvancedMirrorAdapter()
	report := NewMatrixRunner(MatrixRunnerOptions{
		TempDir:       t.TempDir(),
		Profiles:      []Profile{ProfileMirrorRealDense, ProfileMirrorRealGraph},
		MirrorAdapter: adapter,
		QueryAnalysis: memorycore.QueryAnalysisOptions{
			Provider:   memorycore.QueryAnalysisProviderSidecar,
			Mode:       memorycore.QueryAnalysisModeSemanticAlways,
			SidecarURL: server.URL,
		},
		Strict: true,
	}).Run(context.Background(), fixture)

	if report.Failed() {
		t.Fatalf("matrix report failed: %s", report.Error())
	}
	if semanticCalls != 1 {
		t.Fatalf("semantic calls = %d, want one cached call shared by mirror profiles", semanticCalls)
	}
	for _, profile := range report.Profiles {
		analysis := firstRetrievalAnalysis(t, profile.Report)
		if analysis.Source != memorycore.QueryAnalysisSourceMerged || len(analysis.QueryRewrites) != 1 {
			t.Fatalf("%s query analysis = %#v, want cached merged semantic rewrite", profile.Profile, analysis)
		}
	}
}

func TestMatrixRunnerReusesQueryAnalysisTimeoutAcrossMirrorProfiles(t *testing.T) {
	fixture := minimalRetrievalFixture()
	semanticCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/retrieval/query-analysis" {
			t.Fatalf("unexpected semantic path %s", r.URL.Path)
		}
		semanticCalls++
		time.Sleep(50 * time.Millisecond)
	}))
	defer server.Close()

	adapter := newAdvancedMirrorAdapter()
	report := NewMatrixRunner(MatrixRunnerOptions{
		TempDir:       t.TempDir(),
		Profiles:      []Profile{ProfileMirrorRealDense, ProfileMirrorRealGraph},
		MirrorAdapter: adapter,
		QueryAnalysis: memorycore.QueryAnalysisOptions{
			Provider:   memorycore.QueryAnalysisProviderSidecar,
			Mode:       memorycore.QueryAnalysisModeSemanticAlways,
			SidecarURL: server.URL,
			Timeout:    5 * time.Millisecond,
		},
		Strict: true,
	}).Run(context.Background(), fixture)

	if report.Failed() {
		t.Fatalf("matrix report failed: %s", report.Error())
	}
	if semanticCalls != 1 {
		t.Fatalf("semantic calls = %d, want one cached timeout shared by mirror profiles", semanticCalls)
	}
	for _, profile := range report.Profiles {
		analysis := firstRetrievalAnalysis(t, profile.Report)
		if analysis.Source != memorycore.QueryAnalysisSourceSemanticFallback ||
			analysis.Diagnostics == nil ||
			analysis.Diagnostics.FallbackReason != "semantic_timeout" {
			t.Fatalf("%s query analysis = %#v, want cached semantic timeout fallback", profile.Profile, analysis)
		}
	}
}

func TestMatrixRunnerMirrorDenseAllowsEvalSidecarLatency(t *testing.T) {
	fixture := minimalRetrievalFixture()
	adapter := newQualityMirrorAdapter()
	adapter.findDelay = 120 * time.Millisecond
	report := NewMatrixRunner(MatrixRunnerOptions{
		TempDir:       t.TempDir(),
		Profiles:      []Profile{ProfileMirrorRealDense},
		MirrorAdapter: adapter,
		Strict:        true,
	}).Run(context.Background(), fixture)

	if report.Failed() {
		t.Fatalf("matrix report failed: %s", report.Error())
	}
	if report.Profiles[0].Metrics.MirrorUsedCount == 0 {
		t.Fatalf("mirror used count = 0")
	}
}

func TestMatrixRunnerConfiguresSidecarCacheModeAndArtifactDir(t *testing.T) {
	fixture := minimalRetrievalFixture()
	artifactDir := t.TempDir()
	adapter := newDeterministicMirrorAdapter(fixture.CaseID)
	report := NewMatrixRunner(MatrixRunnerOptions{
		TempDir:            t.TempDir(),
		Profiles:           []Profile{ProfileMirrorRealDense},
		MirrorAdapter:      adapter,
		Strict:             true,
		MirrorArtifactDir:  artifactDir,
		ReuseMirror:        "auto",
		EmbeddingCacheMode: "read_only",
	}).Run(context.Background(), fixture)

	if report.Failed() {
		t.Fatalf("matrix report failed: %s", report.Error())
	}
	if adapter.configureCalls == 0 {
		t.Fatalf("configure calls = 0")
	}
	config := adapter.lastEvalConfig
	if config.EmbeddingCacheMode != "read_only" {
		t.Fatalf("embedding cache mode = %q", config.EmbeddingCacheMode)
	}
	if config.TriviumDir == "" || !strings.Contains(config.TriviumDir, "trivium") {
		t.Fatalf("trivium dir = %q", config.TriviumDir)
	}
	if config.EmbeddingCacheDBPath == "" || !strings.Contains(config.EmbeddingCacheDBPath, "embedding-cache") {
		t.Fatalf("embedding cache db path = %q", config.EmbeddingCacheDBPath)
	}
	if config.SearchableTextVersion != defaultSearchableTextVersion {
		t.Fatalf("searchable text version = %q", config.SearchableTextVersion)
	}
	if config.TextNormalizationVersion != defaultTextNormalizationVersion {
		t.Fatalf("text normalization version = %q", config.TextNormalizationVersion)
	}
	if report.Profiles[0].MirrorArtifact.TriviumDir != config.TriviumDir {
		t.Fatalf("artifact trivium dir = %q, config = %q", report.Profiles[0].MirrorArtifact.TriviumDir, config.TriviumDir)
	}
}

func TestMatrixRunnerMirrorDenseFailsOnRequiredFallback(t *testing.T) {
	fixture := minimalRetrievalFixture()
	adapter := newQualityMirrorAdapter()
	adapter.findErr = errors.New("sidecar unavailable")
	report := NewMatrixRunner(MatrixRunnerOptions{
		TempDir:       t.TempDir(),
		Profiles:      []Profile{ProfileMirrorRealDense},
		MirrorAdapter: adapter,
		Strict:        true,
	}).Run(context.Background(), fixture)

	if !report.Failed() {
		t.Fatal("matrix report passed, want required mirror fallback failure")
	}
	if got := report.Profiles[0].Status; got != ProfileStatusFail {
		t.Fatalf("profile status = %q, want %q", got, ProfileStatusFail)
	}
}

func TestMatrixRunnerMissingProviderStrictFails(t *testing.T) {
	fixture := minimalRetrievalFixture()
	report := NewMatrixRunner(MatrixRunnerOptions{
		TempDir:  t.TempDir(),
		Profiles: []Profile{ProfileMirrorRealDense},
		Strict:   true,
	}).Run(context.Background(), fixture)

	if !report.Failed() {
		t.Fatal("matrix report passed, want missing mirror provider failure")
	}
	profile := report.Profiles[0]
	if profile.Status != ProfileStatusFail {
		t.Fatalf("profile status = %q, want %q", profile.Status, ProfileStatusFail)
	}
	if profile.Capability.CountsAsPass || profile.Capability.IncludedInQualityMetrics {
		t.Fatalf("capability should not count as pass: %#v", profile.Capability)
	}
}

func TestMatrixRunnerMissingProviderLocalSkipDoesNotCountAsPass(t *testing.T) {
	fixture := minimalRetrievalFixture()
	report := NewMatrixRunner(MatrixRunnerOptions{
		TempDir:                  t.TempDir(),
		Profiles:                 []Profile{ProfileMirrorRealDense},
		Strict:                   true,
		AllowSkipMissingProvider: true,
	}).Run(context.Background(), fixture)

	if report.Failed() {
		t.Fatalf("matrix report failed: %s", report.Error())
	}
	profile := report.Profiles[0]
	if profile.Status != ProfileStatusSkip {
		t.Fatalf("profile status = %q, want %q", profile.Status, ProfileStatusSkip)
	}
	if profile.Capability.CountsAsPass || profile.Capability.IncludedInQualityMetrics {
		t.Fatalf("skipped capability should not count as pass: %#v", profile.Capability)
	}
}

func TestMatrixRunnerRerankProfileSkipsWhenEvalConfigReportsNoLiveRerankProvider(t *testing.T) {
	fixture := minimalRetrievalFixture()
	adapter := newAdvancedMirrorAdapter()
	adapter.evalRerankAvailable = false
	adapter.evalRerankMode = "none"
	report := NewMatrixRunner(MatrixRunnerOptions{
		TempDir:                  t.TempDir(),
		Profiles:                 []Profile{ProfileMirrorRealGraphRerank},
		MirrorAdapter:            adapter,
		Strict:                   true,
		AllowSkipMissingProvider: true,
	}).Run(context.Background(), fixture)

	if report.Failed() {
		t.Fatalf("matrix report failed: %s", report.Error())
	}
	profile := report.Profiles[0]
	if profile.Status != ProfileStatusSkip {
		t.Fatalf("profile status = %q, want %q; capability=%#v error=%s", profile.Status, ProfileStatusSkip, profile.Capability, profile.Error)
	}
	if profile.Capability.Status != CapabilityMissing || profile.Capability.RerankProviderAvailable {
		t.Fatalf("capability = %#v, want missing rerank provider", profile.Capability)
	}
	if profile.Capability.RerankProviderMode != "none" {
		t.Fatalf("rerank provider mode = %q, want none", profile.Capability.RerankProviderMode)
	}
	if adapter.rerankCalls != 0 {
		t.Fatalf("rerank calls = %d, want preflight skip before runtime rerank", adapter.rerankCalls)
	}
}

func TestMatrixRunnerUnreachableSidecarLocalSkipDoesNotReportReady(t *testing.T) {
	fixture := minimalRetrievalFixture()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	report := NewMatrixRunner(MatrixRunnerOptions{
		TempDir:                  t.TempDir(),
		Profiles:                 []Profile{ProfileMirrorRealDense},
		SidecarURL:               "http://127.0.0.1:1",
		Strict:                   true,
		AllowSkipMissingProvider: true,
		EmbeddingCacheMode:       "read_only",
	}).Run(ctx, fixture)

	if report.Failed() {
		t.Fatalf("matrix report failed: %s", report.Error())
	}
	profile := report.Profiles[0]
	if profile.Status != ProfileStatusSkip {
		t.Fatalf("profile status = %q, want %q; capability=%#v error=%s", profile.Status, ProfileStatusSkip, profile.Capability, profile.Error)
	}
	if profile.Capability.Status != CapabilityMissing || profile.Capability.SidecarAvailable {
		t.Fatalf("capability = %#v, want missing sidecar preflight", profile.Capability)
	}
}

func TestProfileHardMetricsFailQualityProfiles(t *testing.T) {
	tests := []struct {
		name    string
		profile Profile
		metrics MatrixMetrics
		want    string
	}{
		{
			name:    "mirror fallback",
			profile: ProfileMirrorRealDense,
			metrics: MatrixMetrics{FallbackCount: 1},
			want:    "fallback_count=1",
		},
		{
			name:    "graph not used",
			profile: ProfileMirrorRealGraph,
			metrics: MatrixMetrics{GraphRequiredButNotUsedCount: 1},
			want:    "graph_required_but_not_used_count=1",
		},
		{
			name:    "rerank not used",
			profile: ProfileMirrorRealGraphRerank,
			metrics: MatrixMetrics{RerankRequiredButNotUsedCount: 1},
			want:    "rerank_required_but_not_used_count=1",
		},
		{
			name:    "stub used",
			profile: ProfileSQLiteGo,
			metrics: MatrixMetrics{StubUsedCount: 1},
			want:    "stub_used_count=1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reason := profileHardMetricFailureReason(tt.profile, tt.metrics)
			if !strings.Contains(reason, tt.want) {
				t.Fatalf("reason = %q, want %q", reason, tt.want)
			}
		})
	}
}

func TestMatrixRunnerReusesMirrorArtifactForSameFixture(t *testing.T) {
	fixture := minimalRetrievalFixture()
	artifactDir := t.TempDir()
	firstAdapter := newDeterministicMirrorAdapter(fixture.CaseID)
	first := NewMatrixRunner(MatrixRunnerOptions{
		TempDir:           t.TempDir(),
		Profiles:          []Profile{ProfileMirrorRealDense},
		MirrorAdapter:     firstAdapter,
		Strict:            true,
		MirrorArtifactDir: artifactDir,
		ReuseMirror:       "auto",
	}).Run(context.Background(), fixture)
	if first.Failed() {
		t.Fatalf("first matrix run failed: %s", first.Error())
	}
	if firstAdapter.upserts == 0 {
		t.Fatalf("first run upserts = 0")
	}

	secondAdapter := newDeterministicMirrorAdapter(fixture.CaseID)
	second := NewMatrixRunner(MatrixRunnerOptions{
		TempDir:           t.TempDir(),
		Profiles:          []Profile{ProfileMirrorRealDense},
		MirrorAdapter:     secondAdapter,
		Strict:            true,
		MirrorArtifactDir: artifactDir,
		ReuseMirror:       "auto",
	}).Run(context.Background(), fixture)
	if second.Failed() {
		t.Fatalf("second matrix run failed: %s", second.Error())
	}
	if secondAdapter.upserts != 0 {
		t.Fatalf("second run upserts = %d, want reuse without rebuild", secondAdapter.upserts)
	}
	if second.Profiles[0].Metrics.MirrorManifestHash == "" {
		t.Fatalf("second run mirror manifest hash is empty")
	}
	if second.Profiles[0].MirrorArtifact.ManifestHash == "" || !second.Profiles[0].MirrorArtifact.Reused {
		t.Fatalf("second run mirror artifact = %#v, want reused artifact hash", second.Profiles[0].MirrorArtifact)
	}
}

func TestMatrixRunnerRebuildsMirrorArtifactWhenEmbeddingIdentityChanges(t *testing.T) {
	fixture := minimalRetrievalFixture()
	artifactDir := t.TempDir()
	firstAdapter := newDeterministicMirrorAdapter(fixture.CaseID)
	firstAdapter.evalEmbedding = testEvalEmbedding("model-a", "3")
	first := NewMatrixRunner(MatrixRunnerOptions{
		TempDir:           t.TempDir(),
		Profiles:          []Profile{ProfileMirrorRealDense},
		MirrorAdapter:     firstAdapter,
		Strict:            true,
		MirrorArtifactDir: artifactDir,
		ReuseMirror:       "auto",
	}).Run(context.Background(), fixture)
	if first.Failed() {
		t.Fatalf("first matrix run failed: %s", first.Error())
	}
	if firstAdapter.upserts == 0 {
		t.Fatalf("first run upserts = 0")
	}

	secondAdapter := newDeterministicMirrorAdapter(fixture.CaseID)
	secondAdapter.evalEmbedding = testEvalEmbedding("model-b", "3")
	second := NewMatrixRunner(MatrixRunnerOptions{
		TempDir:           t.TempDir(),
		Profiles:          []Profile{ProfileMirrorRealDense},
		MirrorAdapter:     secondAdapter,
		Strict:            true,
		MirrorArtifactDir: artifactDir,
		ReuseMirror:       "auto",
	}).Run(context.Background(), fixture)
	if second.Failed() {
		t.Fatalf("second matrix run failed: %s", second.Error())
	}
	if secondAdapter.upserts == 0 {
		t.Fatalf("second run upserts = 0, want rebuild after embedding model changes")
	}
	if second.Profiles[0].MirrorArtifact.Reused {
		t.Fatalf("second run reused stale mirror artifact: %#v", second.Profiles[0].MirrorArtifact)
	}
}

func TestMatrixRunnerRebuildsManifestOnlyMirrorArtifact(t *testing.T) {
	fixture := minimalRetrievalFixture()
	artifactDir := t.TempDir()
	profileDir := filepath.Join(artifactDir, fixture.StableHash(), "sidecar", defaultSearchableTextVersion)
	if err := os.MkdirAll(profileDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeJSONFile(filepath.Join(profileDir, "manifest.json"), mirrorManifest{
		SchemaVersion:            "memory_eval.mirror_manifest.v0.1",
		DatasetHash:              fixture.StableHash(),
		SQLiteSeedHash:           fixture.StableHash(),
		FixtureHash:              fixture.StableHash(),
		SearchableTextVersion:    defaultSearchableTextVersion,
		TextNormalizationVersion: defaultTextNormalizationVersion,
		Embedding: map[string]string{
			"fingerprint": "sidecar",
		},
		NodeCount: 1,
	}); err != nil {
		t.Fatal(err)
	}
	if err := writeJSONFile(filepath.Join(profileDir, "build_report.json"), map[string]any{"nodes_upserted": 1}); err != nil {
		t.Fatal(err)
	}

	adapter := newDeterministicMirrorAdapter(fixture.CaseID)
	report := NewMatrixRunner(MatrixRunnerOptions{
		TempDir:           t.TempDir(),
		Profiles:          []Profile{ProfileMirrorRealDense},
		MirrorAdapter:     adapter,
		Strict:            true,
		MirrorArtifactDir: artifactDir,
		ReuseMirror:       "auto",
	}).Run(context.Background(), fixture)
	if report.Failed() {
		t.Fatalf("matrix run failed: %s", report.Error())
	}
	if adapter.upserts == 0 {
		t.Fatalf("upserts = 0, want rebuild when manifest has no trivium files")
	}
	if report.Profiles[0].MirrorArtifact.Reused {
		t.Fatalf("mirror artifact reused without trivium files: %#v", report.Profiles[0].MirrorArtifact)
	}
}

func TestMatrixRunnerRebuildsMirrorArtifactWhenOnlyUnrelatedFileExists(t *testing.T) {
	fixture := minimalRetrievalFixture()
	artifactDir := t.TempDir()
	profileDir := filepath.Join(artifactDir, fixture.StableHash(), "sidecar", defaultSearchableTextVersion)
	triviumDir := filepath.Join(profileDir, "trivium")
	if err := os.MkdirAll(triviumDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeJSONFile(filepath.Join(profileDir, "manifest.json"), mirrorManifest{
		SchemaVersion:            "memory_eval.mirror_manifest.v0.1",
		DatasetHash:              fixture.StableHash(),
		SQLiteSeedHash:           fixture.StableHash(),
		FixtureHash:              fixture.StableHash(),
		MigrationHash:            defaultMirrorMigrationHash,
		EdgeManifestHash:         defaultMirrorEdgeManifestHash,
		RetrievalParamsHash:      defaultMirrorRetrievalParamsHash,
		SearchableTextVersion:    defaultSearchableTextVersion,
		TextNormalizationVersion: defaultTextNormalizationVersion,
		TriviumAdapterVersion:    defaultTriviumAdapterVersion,
		TriviumDBVersion:         defaultTriviumDBVersion,
		Embedding:                defaultMirrorEmbeddingIdentity().Embedding,
		NodeCount:                1,
	}); err != nil {
		t.Fatal(err)
	}
	if err := writeJSONFile(filepath.Join(profileDir, "build_report.json"), map[string]any{"nodes_upserted": 1, "edges_upserted": 0}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(triviumDir, "notes.txt"), []byte("not a trivium artifact"), 0o644); err != nil {
		t.Fatal(err)
	}

	adapter := newDeterministicMirrorAdapter(fixture.CaseID)
	report := NewMatrixRunner(MatrixRunnerOptions{
		TempDir:           t.TempDir(),
		Profiles:          []Profile{ProfileMirrorRealDense},
		MirrorAdapter:     adapter,
		Strict:            true,
		MirrorArtifactDir: artifactDir,
		ReuseMirror:       "auto",
	}).Run(context.Background(), fixture)
	if report.Failed() {
		t.Fatalf("matrix run failed: %s", report.Error())
	}
	if adapter.upserts == 0 {
		t.Fatalf("upserts = 0, want rebuild when trivium dir only has unrelated files")
	}
}

func TestMatrixRunnerRebuildsMirrorArtifactWhenLiveMirrorCountsMismatchManifest(t *testing.T) {
	fixture := minimalRetrievalFixture()
	artifactDir := t.TempDir()
	profileDir := filepath.Join(artifactDir, fixture.StableHash(), "sidecar", defaultSearchableTextVersion)
	triviumDir := filepath.Join(profileDir, "trivium")
	if err := os.MkdirAll(triviumDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeJSONFile(filepath.Join(profileDir, "manifest.json"), mirrorManifest{
		SchemaVersion:            defaultMirrorManifestSchema,
		DatasetHash:              fixture.StableHash(),
		SQLiteSeedHash:           fixture.StableHash(),
		FixtureHash:              fixture.StableHash(),
		MigrationHash:            defaultMirrorMigrationHash,
		EdgeManifestHash:         defaultMirrorEdgeManifestHash,
		RetrievalParamsHash:      defaultMirrorRetrievalParamsHash,
		SearchableTextVersion:    defaultSearchableTextVersion,
		TextNormalizationVersion: defaultTextNormalizationVersion,
		TriviumAdapterVersion:    defaultTriviumAdapterVersion,
		TriviumDBVersion:         defaultTriviumDBVersion,
		Embedding:                defaultMirrorEmbeddingIdentity().Embedding,
		NodeCount:                1,
		EdgeCount:                0,
	}); err != nil {
		t.Fatal(err)
	}
	if err := writeJSONFile(filepath.Join(profileDir, "build_report.json"), map[string]any{"nodes_upserted": 1, "edges_upserted": 0}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(triviumDir, "stale-but-nonempty.tdb"), []byte("stale artifact"), 0o644); err != nil {
		t.Fatal(err)
	}

	adapter := newDeterministicMirrorAdapter(fixture.CaseID)
	adapter.reportZeroStatsUntilUpsert = true
	report := NewMatrixRunner(MatrixRunnerOptions{
		TempDir:           t.TempDir(),
		Profiles:          []Profile{ProfileMirrorRealDense},
		MirrorAdapter:     adapter,
		Strict:            true,
		MirrorArtifactDir: artifactDir,
		ReuseMirror:       "auto",
	}).Run(context.Background(), fixture)
	if report.Failed() {
		t.Fatalf("matrix run failed: %s", report.Error())
	}
	if adapter.upserts == 0 {
		t.Fatalf("upserts = 0, want rebuild when live mirror counts do not match manifest")
	}
	if report.Profiles[0].MirrorArtifact.Reused {
		t.Fatalf("mirror artifact reused despite live count mismatch: %#v", report.Profiles[0].MirrorArtifact)
	}
}

func TestFormatMatrixReportIncludesCacheStats(t *testing.T) {
	out := FormatMatrixReport(MatrixReport{
		CaseID: "cache_stats_case",
		Profiles: []ProfileMatrixReport{{
			Profile: ProfileMirrorRealGraphRerank,
			Status:  ProfileStatusPass,
			Capability: CapabilityReport{
				RerankProviderMode: "live",
				RerankCache:        false,
			},
			Metrics: MatrixMetrics{
				EmbeddingCacheHits:     3,
				EmbeddingCacheMisses:   2,
				EmbeddingLiveCallCount: 2,
				RerankLiveCallCount:    1,
			},
		}},
	})

	for _, want := range []string{
		"test_plan_version: memory_eval_matrix.v0.2",
		"embedding_cache_hits: 3",
		"embedding_cache_misses: 2",
		"embedding_live_call_count: 2",
		"rerank_live_call_count: 1",
		"rerank_provider_mode: live",
		"rerank_cache: false",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("report =\n%s\nwant %q", out, want)
		}
	}
}

func TestFormatMatrixDetailReportComparesProfilesByQuestion(t *testing.T) {
	fixture := &Fixture{
		CaseID: "quality_case",
		Steps: []Step{
			{
				ID:     "f_target",
				Action: "fact",
				Fact: &FactStep{
					ContentSummary: "用户晚上九点后更适合深度工作。",
				},
			},
			{
				ID:     "q001_fact",
				Action: "retrieve",
				Retrieve: &RetrieveStep{
					QueryText: "晚上九点 深度工作",
				},
			},
			{
				ID:     "q002_fact",
				Action: "retrieve",
				Retrieve: &RetrieveStep{
					QueryText: "下午 邮件",
				},
			},
		},
		Assertions: []Assertion{
			{
				Type:            "selected_recall_at_k",
				Name:            "q001 recalls target",
				Step:            "q001_fact",
				RelevantNodeIDs: []string{"$f_target.fact_id"},
				At:              4,
				Min:             1,
			},
			{
				Type:            "selected_recall_at_k",
				Name:            "q002 recalls other",
				Step:            "q002_fact",
				RelevantNodeIDs: []string{"f_other"},
				At:              4,
				Min:             1,
			},
		},
	}
	report := MatrixReport{
		CaseID: fixture.CaseID,
		Profiles: []ProfileMatrixReport{
			{
				Profile: ProfileSQLiteGo,
				Status:  ProfileStatusPass,
				Report: Report{
					CaseID: fixture.CaseID,
					Steps: []StepReport{{
						ID:        "q001_fact",
						Action:    "retrieve",
						QueryText: "晚上九点 深度工作",
						Retrieval: &memorycore.MemoryContext{
							Blocks: []memorycore.MemoryBlock{{
								BlockType: "experience_context",
								Items: []memorycore.MemoryContextItem{{
									NodeType:         "fact",
									NodeID:           "f_target",
									Summary:          "用户晚上九点后更适合深度工作。",
									HistoricalStatus: "current",
								}},
							}},
						},
					}, {
						ID:        "q002_fact",
						Action:    "retrieve",
						QueryText: "下午 邮件",
						Retrieval: &memorycore.MemoryContext{
							Blocks: []memorycore.MemoryBlock{{
								BlockType: "experience_context",
								Items: []memorycore.MemoryContextItem{{
									NodeType:         "fact",
									NodeID:           "f_other",
									Summary:          "用户下午适合处理邮件。",
									HistoricalStatus: "current",
								}},
							}},
						},
					}},
					Results: []AssertionResult{
						{Name: "q001 recalls target", Type: "selected_recall_at_k"},
						{Name: "q002 recalls other", Type: "selected_recall_at_k"},
					},
				},
			},
			{
				Profile: ProfileMirrorRealDense,
				Status:  ProfileStatusFail,
				Error:   "selected_recall_at_8 below threshold",
				Report: Report{
					CaseID: fixture.CaseID,
					Steps: []StepReport{{
						ID:        "q001_fact",
						Action:    "retrieve",
						QueryText: "晚上九点 深度工作",
						Retrieval: &memorycore.MemoryContext{
							Blocks: []memorycore.MemoryBlock{{
								BlockType: "experience_context",
								Items: []memorycore.MemoryContextItem{{
									NodeType:         "fact",
									NodeID:           "f_other",
									Summary:          "用户上午适合处理邮件。",
									HistoricalStatus: "current",
								}},
							}},
						},
					}, {
						ID:        "q002_fact",
						Action:    "retrieve",
						QueryText: "下午 邮件",
						Retrieval: &memorycore.MemoryContext{
							Blocks: []memorycore.MemoryBlock{{
								BlockType: "experience_context",
								Items: []memorycore.MemoryContextItem{{
									NodeType:         "fact",
									NodeID:           "f_other",
									Summary:          "用户下午适合处理邮件。",
									HistoricalStatus: "current",
								}},
							}},
						},
					}},
					Results: []AssertionResult{{
						Name: "q001 recalls target",
						Type: "selected_recall_at_k",
						Err: AssertionFailure{
							CaseID:    fixture.CaseID,
							Assertion: "selected_recall_at_k",
							Expected:  "recall@4 >= 1.000",
							Actual:    "recall=0.000 selected=f_other relevant=f_target",
						},
					}, {
						Name: "q002 recalls other",
						Type: "selected_recall_at_k",
					}},
				},
			},
		},
	}

	out := FormatMatrixDetailReport(fixture, report)
	for _, want := range []string{
		"matrix_detail_report",
		"test_plan_version: memory_eval_matrix.v0.2",
		"case_id: quality_case",
		"profile_summary:",
		"| profile | status | capability | assertion_failures | selected_recall_at_8 | precision_at_8 | fallback_count | graph_activation_used_count | rerank_live_call_count |",
		"| mirror_real_dense | fail |  | 1 | 0.000 | 0.000 | 0 | 0 | 0 |",
		"failure_index:",
		"| question_id | sqlite_go | mirror_real_dense |",
		"| q001_fact | PASS | FAIL selected_recall_at_k |",
		"| q002_fact | PASS | PASS |",
		"question_id: q001_fact",
		"问题: 晚上九点 深度工作",
		"期望:",
		"relevant_node_ids=f_target",
		"结果对比:",
		"| profile | result | failed_assertions |",
		"| sqlite_go | PASS | - |",
		"| mirror_real_dense | FAIL | selected_recall_at_k |",
		"实际结果:",
		"profile: sqlite_go",
		"PASS [selected_recall_at_k] q001 recalls target",
		"experience_context fact:f_target current 用户晚上九点后更适合深度工作。",
		"profile: mirror_real_dense",
		"FAIL [selected_recall_at_k] q001 recalls target: expected=recall@4 >= 1.000 actual=recall=0.000 selected=f_other relevant=f_target",
		"experience_context fact:f_other current 用户上午适合处理邮件。",
		"question_id: q002_fact",
		"| mirror_real_dense | PASS | - |",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("detail report =\n%s\nwant %q", out, want)
		}
	}
	if strings.Contains(out, "error: selected_recall_at_8 below threshold") {
		t.Fatalf("detail report should not repeat profile-level errors inside question blocks:\n%s", out)
	}
	if strings.Contains(out, "\nstatus: ") || strings.Contains(out, "\ncapability: ") {
		t.Fatalf("detail report should keep status and capability only in profile summary:\n%s", out)
	}
}

func TestComputeMatrixMetricsUsesCandidatePoolForCandidateRecall(t *testing.T) {
	fixture := minimalRetrievalFixture()
	report := Report{
		CaseID: fixture.CaseID,
		Steps: []StepReport{{
			ID:     "q1",
			Action: "retrieve",
			Retrieval: &memorycore.MemoryContext{
				Mirror: &memorycore.MirrorRetrievalDiagnostics{
					Status: "used",
					Candidates: []memorycore.MirrorCandidateDiagnostics{{
						SQLiteFactID: "f1",
						Rank:         1,
					}},
				},
			},
		}},
	}

	metrics := ComputeMatrixMetrics(fixture, report)

	if metrics.SelectedRecallAt8 != 0 {
		t.Fatalf("selected recall = %.3f, want 0", metrics.SelectedRecallAt8)
	}
	if metrics.CandidateRecallAt80 != 1 {
		t.Fatalf("candidate recall = %.3f, want 1", metrics.CandidateRecallAt80)
	}
}

func TestComputeMatrixMetricsDerivesPrecisionFromSelectedRecallAssertions(t *testing.T) {
	fixture := minimalRetrievalFixture()
	report := Report{
		CaseID: fixture.CaseID,
		Steps: []StepReport{{
			ID:     "q1",
			Action: "retrieve",
			Retrieval: &memorycore.MemoryContext{
				Blocks: []memorycore.MemoryBlock{{
					BlockType: memorycore.MemoryBlockTypeFacts,
					Items: []memorycore.MemoryContextItem{
						{NodeID: "f1"},
						{NodeID: "unrelated"},
					},
				}},
			},
		}},
	}

	metrics := ComputeMatrixMetrics(fixture, report)

	if metrics.SelectedRecallAt8 != 1 {
		t.Fatalf("selected recall = %.3f, want 1", metrics.SelectedRecallAt8)
	}
	if metrics.PrecisionAt8 != 0.5 {
		t.Fatalf("precision = %.3f, want 0.5", metrics.PrecisionAt8)
	}
}

func TestComputeProfileMatrixMetricsCountsOnlyRequiredStageFallbacks(t *testing.T) {
	report := Report{
		Steps: []StepReport{{
			ID:     "q1",
			Action: "retrieve",
			Retrieval: &memorycore.MemoryContext{
				Mirror: &memorycore.MirrorRetrievalDiagnostics{
					Status: "used",
				},
				GraphActivation: &memorycore.GraphActivationDiagnostics{
					Status: "adapter_missing",
				},
				Rerank: &memorycore.RerankDiagnostics{
					Status: "adapter_missing",
				},
			},
		}},
	}

	dense := ComputeProfileMatrixMetrics(nil, report, ProfileMirrorRealDense)
	if dense.FallbackCount != 0 || dense.GraphRequiredButNotUsedCount != 0 || dense.RerankRequiredButNotUsedCount != 0 {
		t.Fatalf("dense metrics = %#v, want unrequired graph/rerank fallbacks ignored", dense)
	}
	graph := ComputeProfileMatrixMetrics(nil, report, ProfileMirrorRealGraph)
	if graph.FallbackCount != 1 || graph.GraphRequiredButNotUsedCount != 1 || graph.RerankRequiredButNotUsedCount != 0 {
		t.Fatalf("graph metrics = %#v, want only graph fallback counted", graph)
	}
	rerank := ComputeProfileMatrixMetrics(nil, report, ProfileMirrorRealGraphRerank)
	if rerank.FallbackCount != 2 || rerank.GraphRequiredButNotUsedCount != 1 || rerank.RerankRequiredButNotUsedCount != 1 {
		t.Fatalf("rerank metrics = %#v, want graph and rerank fallbacks counted", rerank)
	}
}

func TestMatrixRunnerGraphAndRerankProfilesRequireUsedStages(t *testing.T) {
	fixture := minimalRetrievalFixture()
	adapter := newAdvancedMirrorAdapter()
	report := NewMatrixRunner(MatrixRunnerOptions{
		TempDir:       t.TempDir(),
		Profiles:      []Profile{ProfileMirrorRealGraph, ProfileMirrorRealGraphRerank},
		MirrorAdapter: adapter,
		Strict:        true,
	}).Run(context.Background(), fixture)

	if report.Failed() {
		t.Fatalf("matrix report failed: %s", report.Error())
	}
	if report.Profiles[0].Metrics.GraphActivationUsedCount == 0 {
		t.Fatalf("graph profile did not record graph activation use")
	}
	if report.Profiles[1].Metrics.RerankLiveCallCount == 0 {
		t.Fatalf("rerank profile did not record live rerank call")
	}
	if adapter.activationCalls == 0 || adapter.rerankCalls == 0 {
		t.Fatalf("adapter calls activation=%d rerank=%d", adapter.activationCalls, adapter.rerankCalls)
	}
}

func minimalRetrievalFixture() *Fixture {
	return &Fixture{
		CaseID: "matrix_minimal",
		Seed: Seed{
			Sessions: []SessionSeed{{ID: "s1", Channel: "api"}},
			Entities: []EntitySeed{{
				ID:            "user",
				CanonicalName: "EvalUser",
				EntityType:    "user",
			}},
			Episodes: []EpisodeSeed{{
				ID:         "ep1",
				SessionID:  "s1",
				Role:       "user",
				Content:    "用户喜欢咖啡。",
				OccurredAt: "2026-04-28T10:00:00+08:00",
			}},
		},
		Steps: []Step{
			{
				ID:     "f1",
				Action: "fact",
				Fact: &FactStep{
					SubjectEntityID:  "user",
					Predicate:        "likes",
					ObjectLiteral:    stringPtr("咖啡"),
					ContentSummary:   "用户喜欢咖啡。",
					FactType:         "stable_preference",
					Confidence:       "explicit",
					ConfidenceScore:  0.95,
					Importance:       0.9,
					SourceEpisodeIDs: []string{"ep1"},
				},
			},
			{ID: "rebuild_search", Action: "rebuild_search", RebuildSearch: &RebuildSearchStep{}},
			{
				ID:     "q1",
				Action: "retrieve",
				Retrieve: &RetrieveStep{
					QueryText: "咖啡",
					Policy: RetrievalPolicy{
						FinalMemoryCount: 4,
					},
				},
			},
		},
		Assertions: []Assertion{
			{
				Type:            "selected_recall_at_k",
				Name:            "finds coffee",
				Step:            "q1",
				RelevantNodeIDs: []string{"$f1.fact_id"},
				At:              4,
				Min:             1,
			},
			{
				Type:             "forbidden_recall_zero",
				Name:             "no forbidden",
				Step:             "q1",
				ForbiddenNodeIDs: []string{"missing"},
			},
		},
	}
}

func firstRetrievalAnalysis(t *testing.T, report Report) *memorycore.QueryAnalysis {
	t.Helper()
	for _, step := range report.Steps {
		if step.Retrieval != nil && step.Retrieval.QueryAnalysis != nil {
			return step.Retrieval.QueryAnalysis
		}
	}
	t.Fatalf("no retrieval query analysis in report: %s", report.DebugString())
	return nil
}

func stringPtr(value string) *string {
	return &value
}

type qualityMirrorAdapter struct {
	nextID    int64
	nodeIDs   []int64
	findCalls int
	findErr   error
	findDelay time.Duration
}

type deterministicMirrorAdapter struct {
	qualityMirrorAdapter
	caseID                     string
	upserts                    int
	configureCalls             int
	lastEvalConfig             memorycore.MirrorEvalConfigRequest
	triviumDir                 string
	evalEmbedding              map[string]string
	evalRerankAvailable        bool
	evalRerankMode             string
	reportZeroStatsUntilUpsert bool
}

type advancedMirrorAdapter struct {
	deterministicMirrorAdapter
	activationCalls int
	rerankCalls     int
}

func newAdvancedMirrorAdapter() *advancedMirrorAdapter {
	adapter := &advancedMirrorAdapter{}
	adapter.evalRerankAvailable = true
	adapter.evalRerankMode = "live"
	return adapter
}

func (a *advancedMirrorAdapter) UpsertNode(ctx context.Context, payload memorycore.MirrorNodePayload) (memorycore.MirrorNodeUpsertResult, error) {
	a.upserts++
	if err := writeDeterministicTriviumFile(a.triviumDir, payload); err != nil {
		return memorycore.MirrorNodeUpsertResult{}, err
	}
	return memorycore.MirrorNodeUpsertResult{
		MirrorNodeID: stableTriviumNodeID(payload.PersonaID, payload.NodeType, payload.SQLiteNodeID),
	}, nil
}

func (a *advancedMirrorAdapter) FindCandidates(ctx context.Context, req memorycore.MirrorCandidateRequest) (*memorycore.MirrorCandidateResult, error) {
	a.findCalls++
	return &memorycore.MirrorCandidateResult{
		Candidates: []memorycore.MirrorCandidate{{
			TriviumNodeID: stableTriviumNodeID(defaultPersonaID, "fact", "f1"),
			Score:         0.95,
			Source:        "trivium_vector",
			Rank:          1,
		}},
	}, nil
}

func (a *advancedMirrorAdapter) ActivateGraph(ctx context.Context, req memorycore.MirrorActivationRequest) (*memorycore.MirrorActivationResult, error) {
	a.activationCalls++
	return &memorycore.MirrorActivationResult{
		Candidates: []memorycore.MirrorActivationCandidate{{
			TriviumNodeID: stableTriviumNodeID(defaultPersonaID, "fact", "f1"),
			Score:         0.9,
			Source:        "graph_activation",
			Rank:          1,
			Paths: []memorycore.MirrorActivationPath{{
				TriviumNodeIDs: []int64{stableTriviumNodeID(defaultPersonaID, "fact", "f1")},
				LinkTypes:      []string{"SELF"},
			}},
		}},
	}, nil
}

func (a *advancedMirrorAdapter) Rerank(ctx context.Context, req memorycore.MirrorRerankRequest) (*memorycore.MirrorRerankResult, error) {
	a.rerankCalls++
	return &memorycore.MirrorRerankResult{
		Items: []memorycore.MirrorRerankItem{{
			NodeID:      "f1",
			NodeType:    "fact",
			RerankScore: 0.9,
			DebugReason: "live test rerank",
		}},
	}, nil
}

func newDeterministicMirrorAdapter(caseID string) *deterministicMirrorAdapter {
	return &deterministicMirrorAdapter{caseID: caseID}
}

func (a *deterministicMirrorAdapter) UpsertNode(ctx context.Context, payload memorycore.MirrorNodePayload) (memorycore.MirrorNodeUpsertResult, error) {
	a.upserts++
	if err := writeDeterministicTriviumFile(a.triviumDir, payload); err != nil {
		return memorycore.MirrorNodeUpsertResult{}, err
	}
	return memorycore.MirrorNodeUpsertResult{
		MirrorNodeID: stableTriviumNodeID(payload.PersonaID, payload.NodeType, payload.SQLiteNodeID),
	}, nil
}

func writeDeterministicTriviumFile(triviumDir string, payload memorycore.MirrorNodePayload) error {
	if triviumDir == "" {
		return nil
	}
	if err := os.MkdirAll(triviumDir, 0o755); err != nil {
		return err
	}
	name := sanitizeFileName(payload.PersonaID + "_" + payload.NodeType + "_" + payload.SQLiteNodeID)
	return os.WriteFile(filepath.Join(triviumDir, name+".tdb"), []byte("trivium"), 0o644)
}

func (a *deterministicMirrorAdapter) FindCandidates(ctx context.Context, req memorycore.MirrorCandidateRequest) (*memorycore.MirrorCandidateResult, error) {
	a.findCalls++
	return &memorycore.MirrorCandidateResult{
		Candidates: []memorycore.MirrorCandidate{{
			TriviumNodeID: stableTriviumNodeID(defaultPersonaID, "fact", "f1"),
			Score:         0.95,
			Source:        "trivium_vector",
			Rank:          1,
		}},
	}, nil
}

func (a *deterministicMirrorAdapter) ClearNamespace(ctx context.Context, personaID string) error {
	a.nodeIDs = nil
	if strings.TrimSpace(a.triviumDir) == "" {
		return nil
	}
	entries, err := os.ReadDir(a.triviumDir)
	if err != nil {
		return nil
	}
	for _, entry := range entries {
		if entry.Type().IsRegular() && looksLikeTriviumArtifactFile(entry.Name()) {
			if err := os.Remove(filepath.Join(a.triviumDir, entry.Name())); err != nil {
				return err
			}
		}
	}
	return nil
}

func (a *deterministicMirrorAdapter) ConfigureEval(ctx context.Context, req memorycore.MirrorEvalConfigRequest) (*memorycore.MirrorEvalConfigResult, error) {
	a.configureCalls++
	a.lastEvalConfig = req
	a.triviumDir = req.TriviumDir
	nodeCount := countDeterministicTriviumFiles(req.TriviumDir)
	if a.reportZeroStatsUntilUpsert && a.upserts == 0 {
		nodeCount = 0
	}
	return &memorycore.MirrorEvalConfigResult{
		TriviumDir:              req.TriviumDir,
		EmbeddingCacheMode:      req.EmbeddingCacheMode,
		EmbeddingCacheDBPath:    req.EmbeddingCacheDBPath,
		Embedding:               a.evalEmbedding,
		TriviumAdapterVersion:   defaultTriviumAdapterVersion,
		TriviumDBVersion:        defaultTriviumDBVersion,
		RerankProviderAvailable: a.evalRerankAvailable,
		RerankProviderMode:      a.evalRerankMode,
		RerankCache:             false,
		MirrorStatsAvailable:    true,
		MirrorNodeCount:         nodeCount,
		MirrorEdgeCount:         0,
	}, nil
}

func countDeterministicTriviumFiles(dir string) int {
	if strings.TrimSpace(dir) == "" {
		return 0
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	count := 0
	for _, entry := range entries {
		if entry.Type().IsRegular() && looksLikeTriviumArtifactFile(entry.Name()) {
			count++
		}
	}
	return count
}

func testEvalEmbedding(model string, dimensions string) map[string]string {
	embedding := defaultMirrorEmbeddingIdentity().Embedding
	embedding["model"] = model
	embedding["dimensions"] = dimensions
	embedding["fingerprint"] = hashString(model + "\x00" + dimensions)
	return embedding
}

func newQualityMirrorAdapter() *qualityMirrorAdapter {
	return &qualityMirrorAdapter{nextID: 100}
}

func (a *qualityMirrorAdapter) UpsertNode(ctx context.Context, payload memorycore.MirrorNodePayload) (memorycore.MirrorNodeUpsertResult, error) {
	a.nextID++
	if payload.NodeType == "fact" {
		a.nodeIDs = append(a.nodeIDs, a.nextID)
	}
	return memorycore.MirrorNodeUpsertResult{MirrorNodeID: a.nextID}, nil
}

func (a *qualityMirrorAdapter) DeleteNode(ctx context.Context, ref memorycore.MirrorNodeRef) error {
	return nil
}

func (a *qualityMirrorAdapter) UpsertEdge(ctx context.Context, payload memorycore.MirrorEdgePayload) error {
	return nil
}

func (a *qualityMirrorAdapter) DeleteEdge(ctx context.Context, ref memorycore.MirrorEdgeRef) error {
	return nil
}

func (a *qualityMirrorAdapter) ClearNamespace(ctx context.Context, personaID string) error {
	a.nodeIDs = nil
	return nil
}

func (a *qualityMirrorAdapter) FindCandidates(ctx context.Context, req memorycore.MirrorCandidateRequest) (*memorycore.MirrorCandidateResult, error) {
	a.findCalls++
	if a.findDelay > 0 {
		select {
		case <-time.After(a.findDelay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if a.findErr != nil {
		return nil, a.findErr
	}
	if len(a.nodeIDs) == 0 {
		return &memorycore.MirrorCandidateResult{}, nil
	}
	return &memorycore.MirrorCandidateResult{
		Candidates: []memorycore.MirrorCandidate{{
			TriviumNodeID: a.nodeIDs[0],
			Score:         0.95,
			Source:        "trivium_vector",
			Rank:          1,
		}},
	}, nil
}
