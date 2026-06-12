package memorycore

const (
	LibraryVersion           = "v0.1.0-alpha.1"
	PublicAPIVersion         = "memorycore.public.v0.1"
	SQLiteSchemaVersion      = "memorycore.sqlite.v0.1"
	RetrievalContractVersion = "memorycore.retrieval.v0.2"
	ConfigSchemaVersion      = "memorycore.config.v0.2"
)

type CapabilityStatus string

const (
	CapabilitySupported    CapabilityStatus = "supported"
	CapabilityExperimental CapabilityStatus = "experimental"
	CapabilityOptional     CapabilityStatus = "optional"
	CapabilityHostOwned    CapabilityStatus = "host_owned"
	CapabilityNotSupported CapabilityStatus = "not_supported"
)

type CapabilityInfo struct {
	Status      CapabilityStatus `json:"status"`
	Stability   string           `json:"stability,omitempty"`
	Description string           `json:"description,omitempty"`
}

type AboutInfo struct {
	Module                   string                    `json:"module"`
	Version                  string                    `json:"version"`
	PublicAPIVersion         string                    `json:"public_api_version"`
	SQLiteSchemaVersion      string                    `json:"sqlite_schema_version"`
	ConfigSchemaVersion      string                    `json:"config_schema_version"`
	RetrievalContractVersion string                    `json:"retrieval_contract_version"`
	SQLiteAuthoritative      bool                      `json:"sqlite_authoritative"`
	SidecarAuthoritative     bool                      `json:"sidecar_authoritative"`
	Capabilities             map[string]CapabilityInfo `json:"capabilities"`
	Notes                    []string                  `json:"notes,omitempty"`
}

func About() AboutInfo {
	return AboutInfo{
		Module:                   "github.com/longyisang/emoagent-memorycore",
		Version:                  LibraryVersion,
		PublicAPIVersion:         PublicAPIVersion,
		SQLiteSchemaVersion:      SQLiteSchemaVersion,
		ConfigSchemaVersion:      ConfigSchemaVersion,
		RetrievalContractVersion: RetrievalContractVersion,
		SQLiteAuthoritative:      true,
		SidecarAuthoritative:     false,
		Capabilities: map[string]CapabilityInfo{
			"sqlite_authority":              supported("stable_alpha", "SQLite is the authoritative memory store."),
			"session_episode":               supported("stable_alpha", "Session and episode append APIs."),
			"entity_alias":                  supported("stable_alpha", "Entity and alias normalization APIs."),
			"fact_consolidation":            supported("stable_alpha", "Deterministic fact consolidation."),
			"extraction_runtime":            supported("alpha", "Mock and OpenAI-compatible extraction runtime."),
			"retrieval_v5":                  supported("alpha", "SQLite authority retrieval with optional sidecar signals."),
			"forget_exact_fact_episode":     supported("alpha", "Exact fact and episode forget MVP."),
			"forget_broad_preview":          experimental("experimental", "Broad forget preview only."),
			"retention_jobs":                supported("alpha", "Manual retention job runner."),
			"natural_memory_cycle":          experimental("experimental", "Natural Memory cycle runner."),
			"compression_storage_contract":  experimental("experimental", "Compression candidate storage contract."),
			"curation":                      experimental("experimental", "Delta curation runner."),
			"mirror_sync":                   optional("alpha", "Optional retrieval mirror sync and rebuild."),
			"sidecar_retrieval_candidates":  optional("alpha", "Optional sidecar candidate retrieval."),
			"sidecar_graph_activation":      optional("alpha", "Optional sidecar graph activation."),
			"sidecar_rerank":                optional("alpha", "Optional safe sidecar rerank signal."),
			"config_loader":                 supported("alpha", "YAML config loader and validation."),
			"agent_affect":                  hostOwned("deprecated", "Owned by the EmoAgent host in v0.1."),
			"user_mood_relationship_affect": hostOwned("deprecated", "Owned by the EmoAgent host in v0.1."),
			"entity_cascade_purge":          notSupported("planned", "Full entity cascade purge is out of scope for v0.1."),
			"review_queue":                  notSupported("planned", "llm_check review queue is out of scope for v0.1."),
			"auto_scheduler":                hostOwned("alpha", "Scheduling is owned by the embedding host."),
			"http_service":                  notSupported("n/a", "MemoryCore v0.1 is an embedded Go module, not an HTTP service."),
		},
		Notes: []string{
			"SQLite remains authoritative; sidecar data is optional and rebuildable.",
			"Agent Affect and mood/relationship affect write loops are host-owned in v0.1.",
		},
	}
}

func supported(stability string, description string) CapabilityInfo {
	return capability(CapabilitySupported, stability, description)
}

func experimental(stability string, description string) CapabilityInfo {
	return capability(CapabilityExperimental, stability, description)
}

func optional(stability string, description string) CapabilityInfo {
	return capability(CapabilityOptional, stability, description)
}

func hostOwned(stability string, description string) CapabilityInfo {
	return capability(CapabilityHostOwned, stability, description)
}

func notSupported(stability string, description string) CapabilityInfo {
	return capability(CapabilityNotSupported, stability, description)
}

func capability(status CapabilityStatus, stability string, description string) CapabilityInfo {
	return CapabilityInfo{Status: status, Stability: stability, Description: description}
}
