package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/longyisang/emoagent-memorycore/pkg/memorycore"
	"gopkg.in/yaml.v3"
)

const SchemaVersion = "memorycore.config.v0.1"

type Config struct {
	SchemaVersion string          `yaml:"schema_version" json:"schema_version"`
	Enabled       bool            `yaml:"enabled" json:"enabled"`
	Core          CoreConfig      `yaml:"core" json:"core"`
	Retrieval     RetrievalConfig `yaml:"retrieval" json:"retrieval"`
	Sidecar       SidecarConfig   `yaml:"sidecar" json:"sidecar"`
	Retention     RetentionConfig `yaml:"retention" json:"retention"`
	Mirror        MirrorConfig    `yaml:"mirror" json:"mirror"`
}

type CoreConfig struct {
	DBPath      string `yaml:"db_path" json:"db_path"`
	PersonaID   string `yaml:"persona_id" json:"persona_id"`
	AutoMigrate bool   `yaml:"auto_migrate" json:"auto_migrate"`
	EnableFTS   bool   `yaml:"enable_fts" json:"enable_fts"`
}

type RetrievalConfig struct {
	UseFTS                bool   `yaml:"use_fts" json:"use_fts"`
	UseMirror             bool   `yaml:"use_mirror" json:"use_mirror"`
	FinalMemoryCount      int    `yaml:"final_memory_count" json:"final_memory_count"`
	ContextBudgetTokens   int    `yaml:"context_budget_tokens" json:"context_budget_tokens"`
	AllowHistorical       bool   `yaml:"allow_historical" json:"allow_historical"`
	AllowDeepArchive      bool   `yaml:"allow_deep_archive" json:"allow_deep_archive"`
	SensitivityPermission string `yaml:"sensitivity_permission" json:"sensitivity_permission"`
}

type SidecarConfig struct {
	Enabled bool   `yaml:"enabled" json:"enabled"`
	URL     string `yaml:"url" json:"url"`
	Adapter string `yaml:"adapter" json:"adapter"`
}

type RetentionConfig struct {
	Jobs                 []string `yaml:"jobs" json:"jobs"`
	DeepArchiveAfterDays int      `yaml:"deep_archive_after_days" json:"deep_archive_after_days"`
}

type MirrorConfig struct {
	SyncLimit int `yaml:"sync_limit" json:"sync_limit"`
}

type RuntimeValidationOptions struct {
	CheckEnv bool
	Env      func(string) string
}

func Default() Config {
	return Config{
		SchemaVersion: SchemaVersion,
		Enabled:       false,
		Core: CoreConfig{
			DBPath:      "./data/memory.db",
			PersonaID:   "default",
			AutoMigrate: true,
			EnableFTS:   true,
		},
		Retrieval: RetrievalConfig{
			UseFTS:                true,
			UseMirror:             false,
			FinalMemoryCount:      8,
			ContextBudgetTokens:   1200,
			AllowHistorical:       false,
			AllowDeepArchive:      false,
			SensitivityPermission: memorycore.SensitivityNormal,
		},
		Sidecar: SidecarConfig{
			Enabled: false,
			URL:     "",
			Adapter: "trivium",
		},
		Retention: RetentionConfig{
			Jobs:                 []string{string(memorycore.RetentionJobDailyTTLExpiry)},
			DeepArchiveAfterDays: 0,
		},
		Mirror: MirrorConfig{
			SyncLimit: 100,
		},
	}
}

func (c *Config) ApplyDefaults() {
	defaults := Default()
	if c.SchemaVersion == "" {
		c.SchemaVersion = defaults.SchemaVersion
	}
	if strings.TrimSpace(c.Core.DBPath) == "" {
		c.Core.DBPath = defaults.Core.DBPath
	}
	if strings.TrimSpace(c.Core.PersonaID) == "" {
		c.Core.PersonaID = defaults.Core.PersonaID
	}
	if c.Retrieval.FinalMemoryCount == 0 {
		c.Retrieval.FinalMemoryCount = defaults.Retrieval.FinalMemoryCount
	}
	if c.Retrieval.ContextBudgetTokens == 0 {
		c.Retrieval.ContextBudgetTokens = defaults.Retrieval.ContextBudgetTokens
	}
	if strings.TrimSpace(c.Retrieval.SensitivityPermission) == "" {
		c.Retrieval.SensitivityPermission = defaults.Retrieval.SensitivityPermission
	}
	if strings.TrimSpace(c.Sidecar.Adapter) == "" {
		c.Sidecar.Adapter = defaults.Sidecar.Adapter
	}
	if c.Retention.Jobs == nil {
		c.Retention.Jobs = append([]string(nil), defaults.Retention.Jobs...)
	}
	if c.Mirror.SyncLimit == 0 {
		c.Mirror.SyncLimit = defaults.Mirror.SyncLimit
	}
}

func (c Config) Validate() error {
	if c.SchemaVersion != "" && c.SchemaVersion != SchemaVersion {
		return fmt.Errorf("schema_version must be %q", SchemaVersion)
	}
	if c.Enabled && strings.TrimSpace(c.Core.DBPath) == "" {
		return fmt.Errorf("core.db_path is required when enabled=true")
	}
	if c.Retrieval.FinalMemoryCount <= 0 {
		return fmt.Errorf("retrieval.final_memory_count must be > 0")
	}
	if c.Retrieval.ContextBudgetTokens <= 0 {
		return fmt.Errorf("retrieval.context_budget_tokens must be > 0")
	}
	switch c.Retrieval.SensitivityPermission {
	case memorycore.SensitivityNormal, memorycore.SensitivitySensitive, memorycore.SensitivityHighlySensitive:
	default:
		return fmt.Errorf("retrieval.sensitivity_permission must be one of normal|sensitive|highly_sensitive")
	}
	if c.Retrieval.UseMirror {
		if !c.Sidecar.Enabled {
			return fmt.Errorf("sidecar.enabled must be true when retrieval.use_mirror=true")
		}
		if c.Sidecar.Adapter != "fake" && strings.TrimSpace(c.Sidecar.URL) == "" {
			return fmt.Errorf("sidecar.url is required when retrieval.use_mirror=true and sidecar.adapter=%q", c.Sidecar.Adapter)
		}
	}
	switch c.Sidecar.Adapter {
	case "fake", "trivium":
	default:
		return fmt.Errorf("sidecar.adapter must be one of fake|trivium")
	}
	if c.Sidecar.Enabled && c.Sidecar.Adapter == "trivium" && strings.TrimSpace(c.Sidecar.URL) == "" {
		return fmt.Errorf("sidecar.url is required when sidecar.enabled=true and sidecar.adapter=trivium")
	}
	if strings.TrimSpace(c.Sidecar.URL) != "" {
		if err := memorycore.ValidateSidecarLoopbackURL(c.Sidecar.URL); err != nil {
			return fmt.Errorf("sidecar.url must be a loopback HTTP URL: %w", err)
		}
	}
	for _, job := range c.Retention.Jobs {
		switch memorycore.RetentionJobName(job) {
		case memorycore.RetentionJobDailyTTLExpiry:
		case memorycore.RetentionJobMonthlyDeepArchive:
			if c.Retention.DeepArchiveAfterDays <= 0 {
				return fmt.Errorf("retention.deep_archive_after_days must be > 0 when monthly_deep_archive is enabled")
			}
		default:
			return fmt.Errorf("retention.jobs contains unknown job %q", job)
		}
	}
	if c.Mirror.SyncLimit <= 0 {
		return fmt.Errorf("mirror.sync_limit must be > 0")
	}
	return nil
}

func (c Config) ValidateRuntime(opts RuntimeValidationOptions) error {
	if err := c.Validate(); err != nil {
		return err
	}
	if opts.CheckEnv && opts.Env == nil {
		_ = opts.Env
	}
	return nil
}

func (c Config) ToOptions() (memorycore.Options, error) {
	adapter, err := c.NewMirrorAdapter()
	if err != nil {
		return memorycore.Options{}, err
	}
	return memorycore.Options{
		DBPath:        c.Core.DBPath,
		PersonaID:     c.Core.PersonaID,
		AutoMigrate:   c.Core.AutoMigrate,
		EnableFTS:     c.Core.EnableFTS,
		MirrorAdapter: adapter,
	}, nil
}

func (c Config) RetrievalPolicy() memorycore.RetrievalPolicy {
	return memorycore.RetrievalPolicy{
		SensitivityPermission: c.Retrieval.SensitivityPermission,
		AllowHistorical:       c.Retrieval.AllowHistorical,
		AllowDeepArchive:      c.Retrieval.AllowDeepArchive,
		FinalMemoryCount:      c.Retrieval.FinalMemoryCount,
		ContextBudgetTokens:   c.Retrieval.ContextBudgetTokens,
		UseFTS:                c.Retrieval.UseFTS,
		UseMirror:             c.Retrieval.UseMirror,
	}
}

func (c Config) RetentionJobs() []memorycore.RetentionJobName {
	jobs := make([]memorycore.RetentionJobName, 0, len(c.Retention.Jobs))
	for _, job := range c.Retention.Jobs {
		jobs = append(jobs, memorycore.RetentionJobName(job))
	}
	return jobs
}

func (c Config) NewMirrorAdapter() (memorycore.MirrorAdapter, error) {
	if !c.Sidecar.Enabled {
		return nil, nil
	}
	switch c.Sidecar.Adapter {
	case "fake":
		return memorycore.NewFakeMirrorAdapter(), nil
	case "trivium":
		if err := memorycore.ValidateSidecarLoopbackURL(c.Sidecar.URL); err != nil {
			return nil, fmt.Errorf("sidecar.url must be a loopback HTTP URL: %w", err)
		}
		return memorycore.NewSidecarMirrorAdapter(c.Sidecar.URL), nil
	default:
		return nil, fmt.Errorf("sidecar.adapter must be one of fake|trivium")
	}
}

func (c *Config) UnmarshalYAML(value *yaml.Node) error {
	if err := rejectUnknownYAMLFields(value, configYAMLFields, ""); err != nil {
		return err
	}
	var patch configPatch
	if err := value.Decode(&patch); err != nil {
		return err
	}
	cfg := Default()
	applyConfigPatch(&cfg, patch)
	*c = cfg
	return nil
}

func (c *Config) UnmarshalJSON(data []byte) error {
	var patch configPatch
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&patch); err != nil {
		return err
	}
	cfg := Default()
	applyConfigPatch(&cfg, patch)
	*c = cfg
	return nil
}

type configPatch struct {
	SchemaVersion *string         `yaml:"schema_version" json:"schema_version"`
	Enabled       *bool           `yaml:"enabled" json:"enabled"`
	Core          *corePatch      `yaml:"core" json:"core"`
	Retrieval     *retrievalPatch `yaml:"retrieval" json:"retrieval"`
	Sidecar       *sidecarPatch   `yaml:"sidecar" json:"sidecar"`
	Retention     *retentionPatch `yaml:"retention" json:"retention"`
	Mirror        *mirrorPatch    `yaml:"mirror" json:"mirror"`
}

type corePatch struct {
	DBPath      *string `yaml:"db_path" json:"db_path"`
	PersonaID   *string `yaml:"persona_id" json:"persona_id"`
	AutoMigrate *bool   `yaml:"auto_migrate" json:"auto_migrate"`
	EnableFTS   *bool   `yaml:"enable_fts" json:"enable_fts"`
}

type retrievalPatch struct {
	UseFTS                *bool   `yaml:"use_fts" json:"use_fts"`
	UseMirror             *bool   `yaml:"use_mirror" json:"use_mirror"`
	FinalMemoryCount      *int    `yaml:"final_memory_count" json:"final_memory_count"`
	ContextBudgetTokens   *int    `yaml:"context_budget_tokens" json:"context_budget_tokens"`
	AllowHistorical       *bool   `yaml:"allow_historical" json:"allow_historical"`
	AllowDeepArchive      *bool   `yaml:"allow_deep_archive" json:"allow_deep_archive"`
	SensitivityPermission *string `yaml:"sensitivity_permission" json:"sensitivity_permission"`
}

type sidecarPatch struct {
	Enabled *bool   `yaml:"enabled" json:"enabled"`
	URL     *string `yaml:"url" json:"url"`
	Adapter *string `yaml:"adapter" json:"adapter"`
}

type retentionPatch struct {
	Jobs                 *[]string `yaml:"jobs" json:"jobs"`
	DeepArchiveAfterDays *int      `yaml:"deep_archive_after_days" json:"deep_archive_after_days"`
}

type mirrorPatch struct {
	SyncLimit *int `yaml:"sync_limit" json:"sync_limit"`
}

func applyConfigPatch(cfg *Config, patch configPatch) {
	if patch.SchemaVersion != nil {
		cfg.SchemaVersion = *patch.SchemaVersion
	}
	if patch.Enabled != nil {
		cfg.Enabled = *patch.Enabled
	}
	if patch.Core != nil {
		applyCorePatch(&cfg.Core, *patch.Core)
	}
	if patch.Retrieval != nil {
		applyRetrievalPatch(&cfg.Retrieval, *patch.Retrieval)
	}
	if patch.Sidecar != nil {
		applySidecarPatch(&cfg.Sidecar, *patch.Sidecar)
	}
	if patch.Retention != nil {
		applyRetentionPatch(&cfg.Retention, *patch.Retention)
	}
	if patch.Mirror != nil {
		applyMirrorPatch(&cfg.Mirror, *patch.Mirror)
	}
}

func applyCorePatch(cfg *CoreConfig, patch corePatch) {
	if patch.DBPath != nil {
		cfg.DBPath = *patch.DBPath
	}
	if patch.PersonaID != nil {
		cfg.PersonaID = *patch.PersonaID
	}
	if patch.AutoMigrate != nil {
		cfg.AutoMigrate = *patch.AutoMigrate
	}
	if patch.EnableFTS != nil {
		cfg.EnableFTS = *patch.EnableFTS
	}
}

func applyRetrievalPatch(cfg *RetrievalConfig, patch retrievalPatch) {
	if patch.UseFTS != nil {
		cfg.UseFTS = *patch.UseFTS
	}
	if patch.UseMirror != nil {
		cfg.UseMirror = *patch.UseMirror
	}
	if patch.FinalMemoryCount != nil {
		cfg.FinalMemoryCount = *patch.FinalMemoryCount
	}
	if patch.ContextBudgetTokens != nil {
		cfg.ContextBudgetTokens = *patch.ContextBudgetTokens
	}
	if patch.AllowHistorical != nil {
		cfg.AllowHistorical = *patch.AllowHistorical
	}
	if patch.AllowDeepArchive != nil {
		cfg.AllowDeepArchive = *patch.AllowDeepArchive
	}
	if patch.SensitivityPermission != nil {
		cfg.SensitivityPermission = *patch.SensitivityPermission
	}
}

func applySidecarPatch(cfg *SidecarConfig, patch sidecarPatch) {
	if patch.Enabled != nil {
		cfg.Enabled = *patch.Enabled
	}
	if patch.URL != nil {
		cfg.URL = *patch.URL
	}
	if patch.Adapter != nil {
		cfg.Adapter = *patch.Adapter
	}
}

func applyRetentionPatch(cfg *RetentionConfig, patch retentionPatch) {
	if patch.Jobs != nil {
		cfg.Jobs = append([]string(nil), (*patch.Jobs)...)
	}
	if patch.DeepArchiveAfterDays != nil {
		cfg.DeepArchiveAfterDays = *patch.DeepArchiveAfterDays
	}
}

func applyMirrorPatch(cfg *MirrorConfig, patch mirrorPatch) {
	if patch.SyncLimit != nil {
		cfg.SyncLimit = *patch.SyncLimit
	}
}

type yamlFieldSet map[string]yamlFieldSet

var configYAMLFields = yamlFieldSet{
	"schema_version": nil,
	"enabled":        nil,
	"core": {
		"db_path":      nil,
		"persona_id":   nil,
		"auto_migrate": nil,
		"enable_fts":   nil,
	},
	"retrieval": {
		"use_fts":                nil,
		"use_mirror":             nil,
		"final_memory_count":     nil,
		"context_budget_tokens":  nil,
		"allow_historical":       nil,
		"allow_deep_archive":     nil,
		"sensitivity_permission": nil,
	},
	"sidecar": {
		"enabled": nil,
		"url":     nil,
		"adapter": nil,
	},
	"retention": {
		"jobs":                    nil,
		"deep_archive_after_days": nil,
	},
	"mirror": {
		"sync_limit": nil,
	},
}

func rejectUnknownYAMLFields(node *yaml.Node, allowed yamlFieldSet, prefix string) error {
	if node == nil {
		return nil
	}
	if node.Kind == yaml.DocumentNode {
		if len(node.Content) == 0 {
			return nil
		}
		return rejectUnknownYAMLFields(node.Content[0], allowed, prefix)
	}
	if node.Kind != yaml.MappingNode {
		return nil
	}
	for idx := 0; idx+1 < len(node.Content); idx += 2 {
		key := node.Content[idx].Value
		childFields, ok := allowed[key]
		fieldPath := joinFieldPath(prefix, key)
		if !ok {
			return fmt.Errorf("unknown config field %s", fieldPath)
		}
		if childFields != nil {
			if err := rejectUnknownYAMLFields(node.Content[idx+1], childFields, fieldPath); err != nil {
				return err
			}
		}
	}
	return nil
}

func joinFieldPath(prefix string, key string) string {
	if prefix == "" {
		return key
	}
	return prefix + "." + key
}
