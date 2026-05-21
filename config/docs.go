package config

import (
	"fmt"
	"strings"
)

type FieldDescriptor struct {
	Field        string `json:"field"`
	Type         string `json:"type"`
	Default      string `json:"default"`
	Allowed      string `json:"allowed"`
	RequiredWhen string `json:"required_when"`
	Description  string `json:"description"`
}

func FieldDescriptors() []FieldDescriptor {
	return []FieldDescriptor{
		{"schema_version", "string", SchemaVersion, SchemaVersion, "optional", "Configuration contract version."},
		{"enabled", "bool", "false", "true|false", "always", "Embedding switch for parent applications."},
		{"core.db_path", "string", "./data/memory.db", "non-empty", "enabled=true", "SQLite database path."},
		{"core.persona_id", "string", "default", "non-empty", "always", "Persona ID used for service requests."},
		{"core.auto_migrate", "bool", "true", "true|false", "always", "Apply SQLite migrations when opening the service."},
		{"core.enable_fts", "bool", "true", "true|false", "always", "Enable optional FTS migrations when migrations run."},
		{"retrieval.use_fts", "bool", "true", "true|false", "always", "Use SQLite FTS candidates when available."},
		{"retrieval.use_mirror", "bool", "false", "true|false", "always", "Use sidecar mirror candidates."},
		{"retrieval.final_memory_count", "int", "8", "> 0", "always", "Maximum final memory items."},
		{"retrieval.context_budget_tokens", "int", "1200", "> 0", "always", "Context budget for retrieved memory blocks."},
		{"retrieval.allow_historical", "bool", "false", "true|false", "always", "Allow historical facts in retrieval."},
		{"retrieval.allow_deep_archive", "bool", "false", "true|false", "always", "Allow deep-archived facts in retrieval."},
		{"retrieval.sensitivity_permission", "string", "normal", "normal|sensitive|highly_sensitive", "always", "Maximum sensitivity level allowed for retrieval."},
		{"query_analysis.provider", "string", "none", "none|sidecar", "always", "Semantic query-analysis provider."},
		{"query_analysis.mode", "string", "rule_only", "rule_only|semantic_always|semantic_on_low_confidence|semantic_rewrite_only|legacy_only|shadow_adaptive|adaptive|adaptive_safe|adaptive_full", "always", "Semantic query-analysis trigger mode."},
		{"query_analysis.sidecar_url", "string", "http://127.0.0.1:8765", "loopback HTTP URL", "query_analysis.provider=sidecar", "Query-analysis sidecar base URL."},
		{"query_analysis.timeout_ms", "int", "1500", "> 0", "always", "Independent semantic query-analysis timeout."},
		{"query_analysis.min_confidence_to_override", "float", "0.72", "(0, 1]", "always", "Minimum semantic confidence required to override rule fields."},
		{"query_analysis.min_entity_semantic_confidence", "float", "0.70", "(0, 1]", "always", "Minimum semantic entity confidence."},
		{"query_analysis.min_rule_fit", "float", "0.66", "(0, 1]", "always", "Adaptive route minimum rule-fit score."},
		{"query_analysis.min_anchor_readiness", "float", "0.45", "(0, 1]", "always", "Adaptive route minimum anchor-readiness score."},
		{"query_analysis.semantic_need", "float", "0.58", "(0, 1]", "always", "Adaptive route semantic-need threshold."},
		{"query_analysis.min_complexity_for_semantic", "float", "0.50", "(0, 1]", "always", "Adaptive route minimum complexity for semantic analysis."},
		{"query_analysis.full_semantic_complexity", "float", "0.72", "(0, 1]", "always", "Adaptive-full threshold for full semantic analysis."},
		{"query_analysis.decompose_complexity", "float", "0.80", "(0, 1]", "always", "Adaptive route threshold for semantic decomposition."},
		{"query_analysis.min_semantic_field_confidence", "float", "0.70", "(0, 1]", "always", "Minimum semantic field confidence for field-level merge."},
		{"query_analysis.min_override_margin", "float", "0.08", "(0, 1]", "always", "Minimum semantic-over-rule confidence margin for field override."},
		{"query_analysis.high_safety_risk", "float", "0.80", "(0, 1]", "always", "Safety-risk threshold that disables normal semantic analysis."},
		{"query_analysis.max_query_rewrites", "int", "5", "> 0", "always", "Maximum request-local semantic query rewrites."},
		{"query_analysis.max_semantic_anchors", "int", "8", "> 0", "always", "Maximum request-local semantic anchors."},
		{"query_analysis.semantic_total_energy_cap", "float", "5.0", "> 0", "always", "Total energy cap for semantic rewrite and anchor hints."},
		{"query_analysis.max_generated_dense_weight_sum", "float", "3.0", "> 0", "always", "Maximum generated dense query weight sum."},
		{"query_analysis.include_rationale_summary", "bool", "false", "true|false", "always", "Include semantic rationale summaries when explicitly enabled."},
		{"sidecar.enabled", "bool", "false", "true|false", "always", "Enable a Go-side mirror adapter."},
		{"sidecar.url", "string", "", "loopback HTTP URL", "sidecar.enabled=true and adapter=trivium", "Python sidecar base URL."},
		{"sidecar.adapter", "string", "trivium", "fake|trivium", "always", "Mirror adapter implementation."},
		{"sidecar.total_timeout_ms", "int", "400", "> 0", "sidecar.enabled=true or retrieval.use_mirror=true", "Total budget for all sidecar retrieval stages."},
		{"sidecar.mirror_timeout_ms", "int", "80", "> 0", "sidecar.enabled=true or retrieval.use_mirror=true", "Mirror candidate stage timeout."},
		{"sidecar.activation_timeout_ms", "int", "150", "> 0", "sidecar.enabled=true or retrieval.use_mirror=true", "Graph activation stage timeout."},
		{"sidecar.rerank_timeout_ms", "int", "100", "> 0", "sidecar.enabled=true or retrieval.use_mirror=true", "Safe rerank stage timeout."},
		{"sidecar.breaker_enabled", "bool", "true", "true|false", "always", "Enable persona/stage circuit breakers for sidecar retrieval stages."},
		{"sidecar.breaker_window", "int", "20", "> 0", "sidecar.breaker_enabled=true", "Rolling result window for sidecar circuit breakers."},
		{"sidecar.breaker_failure_threshold", "int", "3", "> 0", "sidecar.breaker_enabled=true", "Failures needed to open a sidecar circuit breaker."},
		{"sidecar.breaker_open_ms", "int", "60000", "> 0", "sidecar.breaker_enabled=true", "Circuit breaker open interval in milliseconds."},
		{"sidecar.activation_max_edges_scanned_per_request", "int", "10000", "> 0", "sidecar.enabled=true or retrieval.use_mirror=true", "Python graph activation edge scan budget."},
		{"sidecar.activation_max_neighbors_per_node", "int", "100", "> 0", "sidecar.enabled=true or retrieval.use_mirror=true", "Python graph activation per-node neighbor budget."},
		{"sidecar.activation_max_wall_ms", "int", "120", "> 0", "sidecar.enabled=true or retrieval.use_mirror=true", "Python graph activation wall budget in milliseconds."},
		{"retention.jobs", "[]string", "daily_ttl_expiry", "daily_ttl_expiry|monthly_deep_archive", "always", "Retention jobs selected for retention-jobs-run."},
		{"retention.deep_archive_after_days", "int", "0", ">= 0", "monthly_deep_archive", "Archive age threshold for monthly deep archive."},
		{"mirror.sync_limit", "int", "100", "> 0", "always", "Maximum mirror queue rows to process."},
	}
}

func MarkdownReference() string {
	var builder strings.Builder
	builder.WriteString("| Field | Type | Default | Allowed | Required When | Description |\n")
	builder.WriteString("| --- | --- | --- | --- | --- | --- |\n")
	for _, field := range FieldDescriptors() {
		fmt.Fprintf(&builder, "| `%s` | %s | `%s` | %s | %s | %s |\n",
			field.Field,
			field.Type,
			field.Default,
			field.Allowed,
			field.RequiredWhen,
			field.Description,
		)
	}
	return builder.String()
}
