package eval

import (
	"fmt"
	"strings"
)

type Profile string

const (
	ProfileSQLiteGo                Profile = "sqlite_go"
	ProfileMirrorRealDense         Profile = "mirror_real_dense"
	ProfileMirrorRealGraph         Profile = "mirror_real_graph"
	ProfileMirrorRealGraphRerank   Profile = "mirror_real_graph_rerank"
	ProfileMirrorRealRerankNoGraph Profile = "mirror_real_rerank_no_graph"
	ProfileRuleOnlyRaw             Profile = "rule_only_raw"
	ProfileSemanticParseOnly       Profile = "semantic_parse_only"
	ProfileSemanticRewriteOnly     Profile = "semantic_rewrite_only"
	ProfileSemanticFullCurrent     Profile = "semantic_full_current"
	ProfileSemanticFullSoftGated   Profile = "semantic_full_soft_gated"
	ProfileRerankOff               Profile = "rerank_off"
	ProfileRerankSelective         Profile = "rerank_selective"
	ProfileSoftRoutingEnabled      Profile = "soft_routing_enabled"
)

type ProfileStatus string

const (
	ProfileStatusPass ProfileStatus = "pass"
	ProfileStatusFail ProfileStatus = "fail"
	ProfileStatusSkip ProfileStatus = "skip"
)

type CapabilityStatus string

const (
	CapabilityReady   CapabilityStatus = "ready"
	CapabilityMissing CapabilityStatus = "capability_missing"
)

type CapabilityReport struct {
	Profile                    Profile          `json:"profile"`
	QualityMode                bool             `json:"quality_mode"`
	AllowStub                  bool             `json:"allow_stub"`
	RequiresSidecar            bool             `json:"requires_sidecar"`
	RequiresEmbedding          bool             `json:"requires_embedding"`
	RequiresMirror             bool             `json:"requires_mirror"`
	RequiresGraphActivation    bool             `json:"requires_graph_activation"`
	RequiresRerankProvider     bool             `json:"requires_rerank_provider"`
	SidecarAvailable           bool             `json:"sidecar_available"`
	EmbeddingProviderAvailable bool             `json:"embedding_provider_available"`
	MirrorReady                bool             `json:"mirror_ready"`
	GraphActivationAvailable   bool             `json:"graph_activation_available"`
	RerankProviderAvailable    bool             `json:"rerank_provider_available"`
	RerankProviderMode         string           `json:"rerank_provider_mode,omitempty"`
	RerankCache                bool             `json:"rerank_cache"`
	EmbeddingCacheMode         string           `json:"embedding_cache_mode,omitempty"`
	Status                     CapabilityStatus `json:"status"`
	Reason                     string           `json:"reason,omitempty"`
	CountsAsPass               bool             `json:"counts_as_pass"`
	IncludedInQualityMetrics   bool             `json:"included_in_quality_metrics"`
}

type profileRequirements struct {
	RequiresSidecar         bool
	RequiresEmbedding       bool
	RequiresMirror          bool
	RequiresGraphActivation bool
	RequiresRerankProvider  bool
}

func ParseProfiles(value string) ([]Profile, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return []Profile{ProfileSQLiteGo}, nil
	}
	parts := strings.Split(value, ",")
	profiles := make([]Profile, 0, len(parts))
	for _, part := range parts {
		profile, err := ParseProfile(part)
		if err != nil {
			return nil, err
		}
		profiles = append(profiles, profile)
	}
	return profiles, nil
}

func ParseProfile(value string) (Profile, error) {
	profile := Profile(strings.TrimSpace(value))
	switch profile {
	case "semantic_on_low_confidence":
		return ProfileSemanticFullSoftGated, nil
	case "semantic_full":
		return ProfileSemanticFullCurrent, nil
	}
	switch profile {
	case ProfileSQLiteGo,
		ProfileMirrorRealDense,
		ProfileMirrorRealGraph,
		ProfileMirrorRealGraphRerank,
		ProfileMirrorRealRerankNoGraph,
		ProfileRuleOnlyRaw,
		ProfileSemanticParseOnly,
		ProfileSemanticRewriteOnly,
		ProfileSemanticFullCurrent,
		ProfileSemanticFullSoftGated,
		ProfileRerankOff,
		ProfileRerankSelective,
		ProfileSoftRoutingEnabled:
		return profile, nil
	default:
		return "", fmt.Errorf("unknown profile %q", value)
	}
}

func NormalizeEmbeddingCacheMode(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return "off"
	}
	return value
}

func ValidateEmbeddingCacheMode(value string) error {
	switch NormalizeEmbeddingCacheMode(value) {
	case "off", "read_write", "read_only", "refresh":
		return nil
	default:
		return fmt.Errorf("embedding-cache-mode must be one of off, read_write, read_only, refresh")
	}
}

func (p Profile) Requirements() profileRequirements {
	switch p {
	case ProfileMirrorRealDense,
		ProfileRuleOnlyRaw,
		ProfileSemanticParseOnly,
		ProfileSemanticRewriteOnly,
		ProfileSemanticFullCurrent,
		ProfileSemanticFullSoftGated,
		ProfileRerankOff,
		ProfileSoftRoutingEnabled:
		return profileRequirements{
			RequiresSidecar:   true,
			RequiresEmbedding: true,
			RequiresMirror:    true,
		}
	case ProfileRerankSelective:
		return profileRequirements{
			RequiresSidecar:        true,
			RequiresEmbedding:      true,
			RequiresMirror:         true,
			RequiresRerankProvider: true,
		}
	case ProfileMirrorRealGraph:
		return profileRequirements{
			RequiresSidecar:         true,
			RequiresEmbedding:       true,
			RequiresMirror:          true,
			RequiresGraphActivation: true,
		}
	case ProfileMirrorRealGraphRerank:
		return profileRequirements{
			RequiresSidecar:         true,
			RequiresEmbedding:       true,
			RequiresMirror:          true,
			RequiresGraphActivation: true,
			RequiresRerankProvider:  true,
		}
	case ProfileMirrorRealRerankNoGraph:
		return profileRequirements{
			RequiresSidecar:        true,
			RequiresEmbedding:      true,
			RequiresMirror:         true,
			RequiresRerankProvider: true,
		}
	default:
		return profileRequirements{}
	}
}

func (p Profile) UsesMirror() bool {
	return p.Requirements().RequiresMirror
}

func (p Profile) UsesSemanticQueryAnalysis() bool {
	switch p {
	case ProfileSemanticParseOnly,
		ProfileSemanticRewriteOnly,
		ProfileSemanticFullCurrent,
		ProfileSemanticFullSoftGated,
		ProfileSoftRoutingEnabled:
		return true
	default:
		return false
	}
}
