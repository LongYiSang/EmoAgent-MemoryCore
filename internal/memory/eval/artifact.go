package eval

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/longyisang/emoagent-memorycore/internal/app/memorycore"
)

const defaultSearchableTextVersion = "memorycore_searchable_text_v1"
const defaultTextNormalizationVersion = "v1"
const defaultMirrorManifestSchema = "memory_eval.mirror_manifest.v0.1"
const defaultMirrorMigrationHash = "repo_migrations"
const defaultMirrorEdgeManifestHash = "memorycore_edge_policy_v1"
const defaultMirrorRetrievalParamsHash = "mirror_candidate_contract_v1"
const defaultTriviumAdapterVersion = "memorycore_sidecar.trivium.v1"
const defaultTriviumDBVersion = "unknown"

type MirrorArtifactIdentity struct {
	Embedding             map[string]string
	MigrationHash         string
	EdgeManifestHash      string
	RetrievalParamsHash   string
	TriviumAdapterVersion string
	TriviumDBVersion      string
}

func defaultMirrorEmbeddingIdentity() MirrorArtifactIdentity {
	embedding := map[string]string{
		"provider_kind":              "sidecar",
		"base_url_hash":              "unknown",
		"model":                      "unknown",
		"dimensions":                 "unknown",
		"encoding_format":            "float",
		"text_normalization_version": defaultTextNormalizationVersion,
		"searchable_text_version":    defaultSearchableTextVersion,
	}
	embedding["fingerprint"] = "sidecar"
	return MirrorArtifactIdentity{
		Embedding:             embedding,
		MigrationHash:         defaultMirrorMigrationHash,
		EdgeManifestHash:      defaultMirrorEdgeManifestHash,
		RetrievalParamsHash:   defaultMirrorRetrievalParamsHash,
		TriviumAdapterVersion: defaultTriviumAdapterVersion,
		TriviumDBVersion:      defaultTriviumDBVersion,
	}
}

func (i MirrorArtifactIdentity) Fingerprint() string {
	if value := strings.TrimSpace(i.Embedding["fingerprint"]); value != "" {
		return value
	}
	return embeddingFingerprint(i.Embedding)
}

func (i MirrorArtifactIdentity) normalized() MirrorArtifactIdentity {
	defaults := defaultMirrorEmbeddingIdentity()
	out := MirrorArtifactIdentity{
		Embedding:             cloneStringMap(defaults.Embedding),
		MigrationHash:         defaultString(i.MigrationHash, defaults.MigrationHash),
		EdgeManifestHash:      defaultString(i.EdgeManifestHash, defaults.EdgeManifestHash),
		RetrievalParamsHash:   defaultString(i.RetrievalParamsHash, defaults.RetrievalParamsHash),
		TriviumAdapterVersion: defaultString(i.TriviumAdapterVersion, defaults.TriviumAdapterVersion),
		TriviumDBVersion:      defaultString(i.TriviumDBVersion, defaults.TriviumDBVersion),
	}
	for key, value := range i.Embedding {
		if strings.TrimSpace(value) != "" {
			out.Embedding[key] = value
		}
	}
	out.Embedding["text_normalization_version"] = defaultString(out.Embedding["text_normalization_version"], defaultTextNormalizationVersion)
	out.Embedding["searchable_text_version"] = defaultString(out.Embedding["searchable_text_version"], defaultSearchableTextVersion)
	out.Embedding["fingerprint"] = defaultString(out.Embedding["fingerprint"], embeddingFingerprint(out.Embedding))
	return out
}

func (i MirrorArtifactIdentity) merge(other MirrorArtifactIdentity) MirrorArtifactIdentity {
	out := i.normalized()
	for key, value := range other.Embedding {
		if strings.TrimSpace(value) != "" {
			out.Embedding[key] = value
		}
	}
	out.MigrationHash = defaultString(other.MigrationHash, out.MigrationHash)
	out.EdgeManifestHash = defaultString(other.EdgeManifestHash, out.EdgeManifestHash)
	out.RetrievalParamsHash = defaultString(other.RetrievalParamsHash, out.RetrievalParamsHash)
	out.TriviumAdapterVersion = defaultString(other.TriviumAdapterVersion, out.TriviumAdapterVersion)
	out.TriviumDBVersion = defaultString(other.TriviumDBVersion, out.TriviumDBVersion)
	out.Embedding["fingerprint"] = defaultString(out.Embedding["fingerprint"], embeddingFingerprint(out.Embedding))
	return out
}

type MirrorArtifactManager struct {
	RootDir                  string
	ReuseMode                string
	EmbeddingFingerprint     string
	SearchableTextVersion    string
	TextNormalizationVersion string
	EmbeddingCacheMode       string
	EmbeddingCacheDBPath     string
	Identity                 MirrorArtifactIdentity
}

type MirrorArtifactReport struct {
	ManifestHash         string
	ManifestPath         string
	TriviumDir           string
	EmbeddingCacheDBPath string
	Reused               bool
	NodeCount            int
	EdgeCount            int
	SidecarConfigured    bool
}

type mirrorArtifactEvalState struct {
	Configured       bool
	TriviumDir       string
	EmbeddingCacheDB string
	Identity         MirrorArtifactIdentity
	StatsAvailable   bool
	StatsError       string
	MirrorNodeCount  int
	MirrorEdgeCount  int
}

type mirrorManifest struct {
	SchemaVersion            string            `json:"schema_version"`
	DatasetHash              string            `json:"dataset_hash"`
	SQLiteSeedHash           string            `json:"sqlite_seed_hash"`
	FixtureHash              string            `json:"fixture_hash"`
	MigrationHash            string            `json:"migration_hash"`
	RetrievalParamsHash      string            `json:"retrieval_params_hash"`
	TriviumAdapterVersion    string            `json:"trivium_adapter_version"`
	TriviumDBVersion         string            `json:"triviumdb_version"`
	SearchableTextVersion    string            `json:"searchable_text_version"`
	TextNormalizationVersion string            `json:"text_normalization_version"`
	EdgeManifestHash         string            `json:"edge_manifest_hash"`
	Embedding                map[string]string `json:"embedding"`
	NodeCount                int               `json:"node_count"`
	EdgeCount                int               `json:"edge_count"`
	HiddenOrPurgedNodeCount  int               `json:"hidden_or_purged_node_count"`
	BuiltAt                  string            `json:"built_at"`
}

func (m *MirrorArtifactManager) Ensure(ctx context.Context, state *runState) (MirrorArtifactReport, error) {
	if m == nil {
		return MirrorArtifactReport{}, fmt.Errorf("mirror artifact manager is nil")
	}
	root := strings.TrimSpace(m.RootDir)
	if root == "" {
		root = filepath.Join(state.tempRoot, "mirrors")
	}
	if absRoot, err := filepath.Abs(root); err == nil {
		root = absRoot
	}
	fixtureHash := state.fixture.StableHash()
	searchableTextVersion := defaultString(m.SearchableTextVersion, defaultSearchableTextVersion)
	textNormalizationVersion := defaultString(m.TextNormalizationVersion, defaultTextNormalizationVersion)
	identity := m.Identity.normalized()
	if strings.TrimSpace(m.EmbeddingFingerprint) != "" {
		identity.Embedding["fingerprint"] = strings.TrimSpace(m.EmbeddingFingerprint)
	}
	profileDir := filepath.Join(root, fixtureHash, identity.Fingerprint(), searchableTextVersion)
	triviumDir := filepath.Join(profileDir, "trivium")
	manifestPath := filepath.Join(profileDir, "manifest.json")
	buildReportPath := filepath.Join(profileDir, "build_report.json")
	cacheDBPath := strings.TrimSpace(m.EmbeddingCacheDBPath)
	if cacheDBPath == "" {
		cacheDBPath = filepath.Join(filepath.Dir(root), "embedding-cache", "embeddings.sqlite3")
	}
	if absCacheDBPath, err := filepath.Abs(cacheDBPath); err == nil {
		cacheDBPath = absCacheDBPath
	}
	configuredEval, err := m.configureEvalSidecar(ctx, state, triviumDir, cacheDBPath)
	if err != nil {
		return MirrorArtifactReport{}, err
	}
	identity = identity.merge(configuredEval.Identity)
	if fingerprint := identity.Fingerprint(); fingerprint != filepath.Base(filepath.Dir(profileDir)) {
		profileDir = filepath.Join(root, fixtureHash, fingerprint, searchableTextVersion)
		triviumDir = filepath.Join(profileDir, "trivium")
		manifestPath = filepath.Join(profileDir, "manifest.json")
		buildReportPath = filepath.Join(profileDir, "build_report.json")
		configuredEval, err = m.configureEvalSidecar(ctx, state, triviumDir, cacheDBPath)
		if err != nil {
			return MirrorArtifactReport{}, err
		}
		identity = identity.merge(configuredEval.Identity)
	}
	if configuredEval.TriviumDir != "" {
		triviumDir = configuredEval.TriviumDir
	}
	if configuredEval.EmbeddingCacheDB != "" {
		cacheDBPath = configuredEval.EmbeddingCacheDB
	}

	if strings.ToLower(strings.TrimSpace(m.ReuseMode)) != "never" {
		if manifest, ok := readMirrorManifest(manifestPath); ok && manifestMatches(manifest, fixtureHash, searchableTextVersion, textNormalizationVersion, identity) {
			if !buildReportMatches(buildReportPath, manifest.NodeCount, manifest.EdgeCount) ||
				!triviumArtifactReady(triviumDir, manifest.NodeCount) ||
				!mirrorArtifactCountsMatch(configuredEval, manifest.NodeCount, manifest.EdgeCount) {
				goto rebuild
			}
			if err := state.rehydrateMirrorIndexMap(ctx); err != nil {
				return MirrorArtifactReport{}, err
			}
			report := MirrorArtifactReport{
				ManifestPath:         manifestPath,
				TriviumDir:           triviumDir,
				EmbeddingCacheDBPath: cacheDBPath,
				Reused:               true,
				NodeCount:            manifest.NodeCount,
				EdgeCount:            manifest.EdgeCount,
				SidecarConfigured:    configuredEval.Configured,
			}
			report.ManifestHash = hashFile(manifestPath)
			return report, nil
		}
	}

rebuild:
	result, err := state.service.RebuildMirror(ctx, memorycore.RebuildMirrorRequest{PersonaID: state.persona})
	if err != nil {
		return MirrorArtifactReport{}, err
	}
	if result.Failed > 0 {
		return MirrorArtifactReport{}, fmt.Errorf("mirror rebuild failed nodes=%d", result.Failed)
	}
	if err := os.MkdirAll(triviumDir, 0o755); err != nil {
		return MirrorArtifactReport{}, err
	}
	if !triviumArtifactReady(triviumDir, result.NodesUpserted) {
		return MirrorArtifactReport{}, fmt.Errorf("mirror rebuild did not produce trivium artifact files in %s", triviumDir)
	}
	configuredEval, err = m.configureEvalSidecar(ctx, state, triviumDir, cacheDBPath)
	if err != nil {
		return MirrorArtifactReport{}, err
	}
	if err := verifyMirrorArtifactCounts(configuredEval, result.NodesUpserted, result.EdgesUpserted); err != nil {
		return MirrorArtifactReport{}, err
	}
	manifest := mirrorManifest{
		SchemaVersion:            defaultMirrorManifestSchema,
		DatasetHash:              fixtureHash,
		SQLiteSeedHash:           fixtureHash,
		FixtureHash:              fixtureHash,
		MigrationHash:            identity.MigrationHash,
		RetrievalParamsHash:      identity.RetrievalParamsHash,
		TriviumAdapterVersion:    identity.TriviumAdapterVersion,
		TriviumDBVersion:         identity.TriviumDBVersion,
		SearchableTextVersion:    searchableTextVersion,
		TextNormalizationVersion: textNormalizationVersion,
		EdgeManifestHash:         identity.EdgeManifestHash,
		Embedding:                cloneStringMap(identity.Embedding),
		NodeCount:                result.NodesUpserted,
		EdgeCount:                result.EdgesUpserted,
		HiddenOrPurgedNodeCount:  0,
		BuiltAt:                  time.Now().UTC().Format(time.RFC3339Nano),
	}
	if err := writeJSONFile(manifestPath, manifest); err != nil {
		return MirrorArtifactReport{}, err
	}
	if err := writeJSONFile(buildReportPath, result); err != nil {
		return MirrorArtifactReport{}, err
	}
	return MirrorArtifactReport{
		ManifestHash:         hashFile(manifestPath),
		ManifestPath:         manifestPath,
		TriviumDir:           triviumDir,
		EmbeddingCacheDBPath: cacheDBPath,
		Reused:               false,
		NodeCount:            result.NodesUpserted,
		EdgeCount:            result.EdgesUpserted,
		SidecarConfigured:    configuredEval.Configured,
	}, nil
}

func (m *MirrorArtifactManager) configureEvalSidecar(ctx context.Context, state *runState, triviumDir string, cacheDBPath string) (mirrorArtifactEvalState, error) {
	configurator, ok := state.mirrorAdapter.(memorycore.MirrorEvalConfigurator)
	if !ok || configurator == nil {
		return mirrorArtifactEvalState{}, nil
	}
	mode := NormalizeEmbeddingCacheMode(m.EmbeddingCacheMode)
	result, err := configurator.ConfigureEval(ctx, memorycore.MirrorEvalConfigRequest{
		TriviumDir:               triviumDir,
		EmbeddingCacheMode:       mode,
		EmbeddingCacheDBPath:     cacheDBPath,
		SearchableTextVersion:    defaultString(m.SearchableTextVersion, defaultSearchableTextVersion),
		TextNormalizationVersion: defaultString(m.TextNormalizationVersion, defaultTextNormalizationVersion),
	})
	if err != nil {
		return mirrorArtifactEvalState{}, fmt.Errorf("configure eval sidecar: %w", err)
	}
	if result == nil {
		return mirrorArtifactEvalState{}, nil
	}
	identity := MirrorArtifactIdentity{
		Embedding:             cloneStringMap(result.Embedding),
		TriviumAdapterVersion: result.TriviumAdapterVersion,
		TriviumDBVersion:      result.TriviumDBVersion,
	}
	return mirrorArtifactEvalState{
		Configured:       true,
		TriviumDir:       strings.TrimSpace(result.TriviumDir),
		EmbeddingCacheDB: strings.TrimSpace(result.EmbeddingCacheDBPath),
		Identity:         identity,
		StatsAvailable:   result.MirrorStatsAvailable,
		StatsError:       strings.TrimSpace(result.MirrorStatsError),
		MirrorNodeCount:  result.MirrorNodeCount,
		MirrorEdgeCount:  result.MirrorEdgeCount,
	}, nil
}

func mirrorArtifactCountsMatch(evalState mirrorArtifactEvalState, nodeCount int, edgeCount int) bool {
	if !evalState.Configured || !evalState.StatsAvailable {
		return false
	}
	return evalState.MirrorNodeCount == nodeCount && evalState.MirrorEdgeCount == edgeCount
}

func verifyMirrorArtifactCounts(evalState mirrorArtifactEvalState, nodeCount int, edgeCount int) error {
	if !evalState.Configured {
		return fmt.Errorf("mirror artifact count verification unavailable: sidecar eval configure is not supported")
	}
	if !evalState.StatsAvailable {
		if evalState.StatsError != "" {
			return fmt.Errorf("mirror artifact count verification unavailable: %s", evalState.StatsError)
		}
		return fmt.Errorf("mirror artifact count verification unavailable")
	}
	if evalState.MirrorNodeCount != nodeCount || evalState.MirrorEdgeCount != edgeCount {
		return fmt.Errorf(
			"mirror artifact count mismatch: manifest nodes=%d edges=%d actual nodes=%d edges=%d",
			nodeCount,
			edgeCount,
			evalState.MirrorNodeCount,
			evalState.MirrorEdgeCount,
		)
	}
	return nil
}

func (s *runState) rehydrateMirrorIndexMap(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `
SELECT 'entity', id FROM entities WHERE persona_id = ? AND visibility_status = 'visible' AND searchable = 1
UNION ALL
SELECT 'fact', id FROM facts WHERE persona_id = ? AND visibility_status = 'visible' AND searchable = 1
UNION ALL
SELECT 'narrative', id FROM narratives WHERE persona_id = ? AND visibility_status = 'visible' AND searchable = 1
UNION ALL
SELECT 'insight', id FROM insights WHERE persona_id = ? AND visibility_status = 'visible' AND searchable = 1`,
		s.persona, s.persona, s.persona, s.persona)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var nodeType, nodeID string
		if err := rows.Scan(&nodeType, &nodeID); err != nil {
			return err
		}
		triviumID := stableTriviumNodeID(s.persona, nodeType, nodeID)
		_, err := s.db.ExecContext(ctx, `
INSERT INTO memory_index_map (
    id, persona_id, node_type, node_id, trivium_node_id,
    index_status, indexed_at, updated_at, error_message
) VALUES (?, ?, ?, ?, ?, 'indexed', ?, ?, NULL)
ON CONFLICT(persona_id, node_type, node_id) DO UPDATE SET
    trivium_node_id = excluded.trivium_node_id,
    index_status = 'indexed',
    indexed_at = excluded.indexed_at,
    updated_at = excluded.updated_at,
    error_message = NULL`,
			"eval_reuse_"+s.persona+"_"+nodeType+"_"+nodeID,
			s.persona,
			nodeType,
			nodeID,
			triviumID,
			time.Now().UTC().Format(time.RFC3339Nano),
			time.Now().UTC().Format(time.RFC3339Nano),
		)
		if err != nil {
			return err
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `
INSERT INTO mirror_persona_state (persona_id, state, reason, updated_at)
VALUES (?, 'ready', NULL, ?)
ON CONFLICT(persona_id) DO UPDATE SET
    state = 'ready',
    reason = NULL,
    updated_at = excluded.updated_at`, s.persona, time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

func stableTriviumNodeID(personaID string, nodeType string, nodeID string) int64 {
	hash := sha256.New()
	for _, part := range []string{personaID, nodeType, nodeID} {
		_, _ = hash.Write([]byte(part))
		_, _ = hash.Write([]byte{0})
	}
	sum := hash.Sum(nil)
	const maxInt64 = uint64(1<<63 - 1)
	value := int64(binary.BigEndian.Uint64(sum[:8]) & maxInt64)
	if value == 0 {
		return 1
	}
	return value
}

func readMirrorManifest(path string) (mirrorManifest, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return mirrorManifest{}, false
	}
	var manifest mirrorManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return mirrorManifest{}, false
	}
	return manifest, true
}

func manifestMatches(manifest mirrorManifest, fixtureHash string, searchableTextVersion string, textNormalizationVersion string, identity MirrorArtifactIdentity) bool {
	identity = identity.normalized()
	return manifest.SchemaVersion == defaultMirrorManifestSchema &&
		manifest.FixtureHash == fixtureHash &&
		manifest.DatasetHash == fixtureHash &&
		manifest.SQLiteSeedHash == fixtureHash &&
		manifest.MigrationHash == identity.MigrationHash &&
		manifest.EdgeManifestHash == identity.EdgeManifestHash &&
		manifest.RetrievalParamsHash == identity.RetrievalParamsHash &&
		manifest.TriviumAdapterVersion == identity.TriviumAdapterVersion &&
		manifest.TriviumDBVersion == identity.TriviumDBVersion &&
		manifest.SearchableTextVersion == searchableTextVersion &&
		manifest.TextNormalizationVersion == textNormalizationVersion &&
		stringMapsEqual(manifest.Embedding, identity.Embedding)
}

func triviumArtifactReady(dir string, nodeCount int) bool {
	if strings.TrimSpace(dir) == "" {
		return false
	}
	if nodeCount <= 0 {
		return false
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if !entry.Type().IsRegular() {
			continue
		}
		if !looksLikeTriviumArtifactFile(entry.Name()) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if info.Size() > 0 {
			return true
		}
	}
	return false
}

func buildReportMatches(path string, nodeCount int, edgeCount int) bool {
	if !fileExists(path) {
		return false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var report struct {
		NodesUpserted int `json:"nodes_upserted"`
		EdgeCount     int `json:"edge_count"`
		EdgesUpserted int `json:"edges_upserted"`
	}
	if err := json.Unmarshal(data, &report); err != nil {
		return false
	}
	if report.NodesUpserted != nodeCount {
		return false
	}
	if report.EdgesUpserted != edgeCount && report.EdgeCount != edgeCount {
		return false
	}
	return true
}

func looksLikeTriviumArtifactFile(name string) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return false
	}
	if strings.Contains(name, ".tdb") || strings.Contains(name, "trivium") {
		return true
	}
	switch filepath.Ext(name) {
	case ".db", ".sqlite", ".sqlite3", ".bin":
		return true
	default:
		return false
	}
}

func fileExists(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func writeJSONFile(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}

func hashFile(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func hashString(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func embeddingFingerprint(embedding map[string]string) string {
	normalized := cloneStringMap(embedding)
	delete(normalized, "fingerprint")
	data, err := json.Marshal(normalized)
	if err != nil {
		return "sidecar"
	}
	return hashString(string(data))
}

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func stringMapsEqual(left map[string]string, right map[string]string) bool {
	if len(left) != len(right) {
		return false
	}
	for key, value := range right {
		if left[key] != value {
			return false
		}
	}
	return true
}
