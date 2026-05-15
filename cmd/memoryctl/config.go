package main

import (
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	memconfig "github.com/longyisang/emoagent-memorycore/config"
)

const formatMarkdown = "markdown"

func runValidateConfig(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := newFlagSet("validate-config", stderr)
	var configPath string
	var format string
	var checkEnv bool
	fs.StringVar(&configPath, "config", "", "MemoryCore config path")
	fs.StringVar(&format, "format", formatText, "output format")
	fs.BoolVar(&checkEnv, "check-env", false, "check environment-backed runtime requirements")
	if !parseFlags(fs, args) {
		return 2
	}
	if strings.TrimSpace(configPath) == "" {
		return usageError(stderr, fs, "--config is required")
	}
	if err := validateFormat(format, formatText, formatJSON); err != nil {
		return usageError(stderr, fs, err.Error())
	}
	cfg, err := loadConfigFile(configPath)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	if err := cfg.ValidateRuntime(memconfig.RuntimeValidationOptions{CheckEnv: checkEnv}); err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	if format == formatJSON {
		return writeJSON(stdout, map[string]any{
			"status":         "ok",
			"schema_version": cfg.SchemaVersion,
			"enabled":        cfg.Enabled,
		}, false)
	}
	fmt.Fprintln(stdout, "config ok")
	return 0
}

func runConfigDocs(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := newFlagSet("config-docs", stderr)
	var format string
	fs.StringVar(&format, "format", formatMarkdown, "output format")
	if !parseFlags(fs, args) {
		return 2
	}
	if err := validateFormat(format, formatMarkdown, formatJSON); err != nil {
		return usageError(stderr, fs, err.Error())
	}
	if format == formatJSON {
		return writeJSON(stdout, memconfig.FieldDescriptors(), false)
	}
	fmt.Fprint(stdout, memconfig.MarkdownReference())
	return 0
}

func loadConfigFile(path string) (memconfig.Config, error) {
	if strings.EqualFold(filepath.Ext(path), ".json") {
		return memconfig.LoadJSON(path)
	}
	return memconfig.LoadYAML(path)
}

func addConfigFlag(fs *flag.FlagSet, opts *commonOptions) {
	fs.StringVar(&opts.ConfigPath, "config", "", "MemoryCore config path")
}

func explicitFlagNames(fs *flag.FlagSet) map[string]bool {
	names := map[string]bool{}
	fs.Visit(func(f *flag.Flag) {
		names[f.Name] = true
	})
	return names
}

func loadCommandConfig(opts commonOptions) (memconfig.Config, bool, error) {
	if strings.TrimSpace(opts.ConfigPath) == "" {
		return memconfig.Config{}, false, nil
	}
	cfg, err := loadConfigFileForOverlay(opts.ConfigPath)
	return cfg, true, err
}

func applyCommonConfig(opts *commonOptions, cfg *memconfig.Config, explicit map[string]bool, stderr io.Writer) {
	if explicit["db"] {
		warnConfigOverride(stderr, "db", "core.db_path")
		cfg.Core.DBPath = opts.DBPath
	} else {
		opts.DBPath = cfg.Core.DBPath
	}
	if explicit["persona"] {
		warnConfigOverride(stderr, "persona", "core.persona_id")
		cfg.Core.PersonaID = opts.PersonaID
	} else {
		opts.PersonaID = cfg.Core.PersonaID
	}
	if explicit["auto-migrate"] {
		warnConfigOverride(stderr, "auto-migrate", "core.auto_migrate")
		cfg.Core.AutoMigrate = opts.AutoMigrate
	} else {
		opts.AutoMigrate = cfg.Core.AutoMigrate
	}
	if explicit["enable-fts"] {
		warnConfigOverride(stderr, "enable-fts", "core.enable_fts")
		cfg.Core.EnableFTS = opts.EnableFTS
	} else {
		opts.EnableFTS = cfg.Core.EnableFTS
	}
}

func warnConfigOverride(stderr io.Writer, flagName string, fieldPath string) {
	fmt.Fprintf(stderr, "warning: --%s overrides memory.%s from config\n", flagName, fieldPath)
}

func joinRetentionJobs(jobs []string) string {
	return strings.Join(jobs, ",")
}

func loadConfigFileForOverlay(path string) (memconfig.Config, error) {
	opts := memconfig.LoadOptions{SkipValidate: true}
	if strings.EqualFold(filepath.Ext(path), ".json") {
		return memconfig.LoadJSONWithOptions(path, opts)
	}
	return memconfig.LoadYAMLWithOptions(path, opts)
}
