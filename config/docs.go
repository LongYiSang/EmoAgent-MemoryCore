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
		{"sidecar.enabled", "bool", "false", "true|false", "always", "Enable a Go-side mirror adapter."},
		{"sidecar.url", "string", "", "loopback HTTP URL", "sidecar.enabled=true and adapter=trivium", "Python sidecar base URL."},
		{"sidecar.adapter", "string", "trivium", "fake|trivium", "always", "Mirror adapter implementation."},
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
