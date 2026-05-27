package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	memsqlite "github.com/longyisang/emoagent-memorycore/internal/store/sqlite"
)

const debugCleanupConfirm = "DELETE_MEMORY_DEBUG_DATA"

const (
	debugCleanupScopeAutoExtraction = "auto-extraction"
	debugCleanupScopePersona        = "persona"
	debugCleanupScopeAllDev         = "all-dev"
)

type debugCleanupResult struct {
	PersonaID string                 `json:"persona_id"`
	Profile   string                 `json:"profile,omitempty"`
	Scope     string                 `json:"scope"`
	Since     string                 `json:"since,omitempty"`
	DryRun    bool                   `json:"dry_run"`
	TotalRows int64                  `json:"total_rows"`
	Tables    []debugCleanupTableRow `json:"tables"`
	Note      string                 `json:"note"`
}

type debugCleanupTableRow struct {
	Table string `json:"table"`
	Rows  int64  `json:"rows"`
}

type debugCleanupTarget struct {
	Table     string
	CountSQL  string
	DeleteSQL string
	Args      []any
}

type debugCleanupOptions struct {
	PersonaID string
	Profile   string
	Scope     string
	Since     *time.Time
	Execute   bool
}

func runDebugCleanup(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := newFlagSet("debug-cleanup", stderr)
	var opts commonOptions
	var execute bool
	var confirm string
	var profile string
	var scope string
	var sinceValue string
	addCommonFlags(fs, &opts, formatText)
	fs.BoolVar(&execute, "execute", false, "perform cleanup; default is dry-run")
	fs.StringVar(&confirm, "confirm", "", "required confirmation token for --execute: "+debugCleanupConfirm)
	fs.StringVar(&profile, "profile", "", "execution profile; destructive cleanup requires dev or test")
	fs.StringVar(&scope, "scope", debugCleanupScopeAutoExtraction, "cleanup scope: auto-extraction, persona, all-dev")
	fs.StringVar(&sinceValue, "since", "", "RFC3339 lower bound for auto-extraction audit rows")
	if !parseFlags(fs, args) {
		return 2
	}
	if !requireDB(stderr, fs, opts.DBPath) {
		return 2
	}
	if err := validateFormat(opts.Format, formatText, formatJSON); err != nil {
		return usageError(stderr, fs, err.Error())
	}
	since, err := parseOptionalTimePtr(sinceValue, "--since")
	if err != nil {
		return usageError(stderr, fs, err.Error())
	}
	if err := validateDebugCleanupScope(scope); err != nil {
		return usageError(stderr, fs, err.Error())
	}
	if execute && !isDebugCleanupDevProfile(profile) {
		return usageError(stderr, fs, "--execute requires --profile dev or test")
	}
	if execute && confirm != debugCleanupConfirm {
		return usageError(stderr, fs, "--execute requires --confirm %s", debugCleanupConfirm)
	}

	ctx := context.Background()
	db, err := memsqlite.Open(ctx, opts.DBPath)
	if err != nil {
		return runtimeError(stderr, "open sqlite: %v", err)
	}
	defer db.Close()
	if opts.AutoMigrate {
		if err := db.MigrateWithOptions(ctx, memsqlite.MigrateOptions{EnableFTS: opts.EnableFTS}); err != nil {
			return runtimeError(stderr, "migrate sqlite: %v", err)
		}
	}

	result, err := runDebugCleanupSQL(ctx, db.SQLDB(), debugCleanupOptions{
		PersonaID: opts.PersonaID,
		Profile:   profile,
		Scope:     scope,
		Since:     since,
		Execute:   execute,
	})
	if err != nil {
		return runtimeError(stderr, "debug cleanup: %v", err)
	}
	if opts.Format == formatJSON {
		return writeJSON(stdout, result, opts.Pretty)
	}
	fmt.Fprintf(stdout, "persona_id=%s\n", result.PersonaID)
	if result.Profile != "" {
		fmt.Fprintf(stdout, "profile=%s\n", result.Profile)
	}
	fmt.Fprintf(stdout, "scope=%s\n", result.Scope)
	if result.Since != "" {
		fmt.Fprintf(stdout, "since=%s\n", result.Since)
	}
	fmt.Fprintf(stdout, "dry_run=%s\n", boolText(result.DryRun))
	fmt.Fprintf(stdout, "note=%s\n", result.Note)
	for _, table := range result.Tables {
		fmt.Fprintf(stdout, "%s=%d\n", table.Table, table.Rows)
	}
	fmt.Fprintf(stdout, "total_rows=%d\n", result.TotalRows)
	return 0
}

func runDebugCleanupSQL(ctx context.Context, db *sql.DB, opts debugCleanupOptions) (debugCleanupResult, error) {
	personaID := strings.TrimSpace(opts.PersonaID)
	scope := debugCleanupDefaultString(strings.TrimSpace(opts.Scope), debugCleanupScopeAutoExtraction)
	if scope != debugCleanupScopeAllDev && personaID == "" {
		return debugCleanupResult{}, errors.New("persona id is required")
	}
	result := debugCleanupResult{
		PersonaID: personaID,
		Profile:   strings.TrimSpace(opts.Profile),
		Scope:     scope,
		DryRun:    !opts.Execute,
		Note:      "dev/debug cleanup only; use forget for user-requested deletion",
	}
	if opts.Since != nil && !opts.Since.IsZero() {
		result.Since = opts.Since.UTC().Format(time.RFC3339Nano)
	}
	targets, err := debugCleanupTargets(opts)
	if err != nil {
		return debugCleanupResult{}, err
	}
	return runDebugCleanupTargets(ctx, db, result, targets, opts.Execute)
}

func runDebugCleanupTargets(ctx context.Context, db *sql.DB, result debugCleanupResult, targets []debugCleanupTarget, execute bool) (debugCleanupResult, error) {
	existingTargets := make([]debugCleanupTarget, 0, len(targets))
	for _, target := range targets {
		exists, err := debugCleanupTableExists(ctx, db, target.Table)
		if err != nil {
			return debugCleanupResult{}, err
		}
		row := debugCleanupTableRow{Table: target.Table}
		if exists {
			existingTargets = append(existingTargets, target)
			if err := db.QueryRowContext(ctx, target.CountSQL, target.Args...).Scan(&row.Rows); err != nil {
				return debugCleanupResult{}, err
			}
		}
		result.TotalRows += row.Rows
		result.Tables = append(result.Tables, row)
	}
	if !execute {
		return result, nil
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return debugCleanupResult{}, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	for _, target := range existingTargets {
		if _, err = tx.ExecContext(ctx, target.DeleteSQL, target.Args...); err != nil {
			return debugCleanupResult{}, err
		}
	}
	if err = tx.Commit(); err != nil {
		return debugCleanupResult{}, err
	}
	return result, nil
}

func validateDebugCleanupScope(scope string) error {
	switch strings.TrimSpace(scope) {
	case "", debugCleanupScopeAutoExtraction, debugCleanupScopePersona, debugCleanupScopeAllDev:
		return nil
	default:
		return fmt.Errorf("--scope must be one of auto-extraction, persona, all-dev")
	}
}

func isDebugCleanupDevProfile(profile string) bool {
	switch strings.ToLower(strings.TrimSpace(profile)) {
	case "dev", "test":
		return true
	default:
		return false
	}
}

func debugCleanupTargets(opts debugCleanupOptions) ([]debugCleanupTarget, error) {
	switch debugCleanupDefaultString(strings.TrimSpace(opts.Scope), debugCleanupScopeAutoExtraction) {
	case debugCleanupScopeAutoExtraction:
		return debugCleanupAutoExtractionTargets(strings.TrimSpace(opts.PersonaID), opts.Since), nil
	case debugCleanupScopePersona:
		return debugCleanupPersonaTargets(strings.TrimSpace(opts.PersonaID)), nil
	case debugCleanupScopeAllDev:
		return debugCleanupAllDevTargets(), nil
	default:
		return nil, fmt.Errorf("--scope must be one of auto-extraction, persona, all-dev")
	}
}

func debugCleanupPersonaTargets(personaID string) []debugCleanupTarget {
	return []debugCleanupTarget{
		debugCleanupPersonaTarget("memory_links", personaID),
		debugCleanupPersonaTarget("memory_search_fts", personaID),
		debugCleanupPersonaTarget("memory_search_documents", personaID),
		debugCleanupPersonaTarget("index_sync_queue", personaID),
		debugCleanupPersonaTarget("memory_index_map", personaID),
		debugCleanupPersonaTarget("extraction_runs", personaID),
		debugCleanupPersonaTarget("consolidation_apply_fingerprints", personaID),
		debugCleanupPersonaTarget("consolidation_session_fact_writes", personaID),
		debugCleanupPersonaTarget("memory_access_events", personaID),
		debugCleanupPersonaTarget("facts", personaID),
	}
}

func debugCleanupPersonaTarget(table string, personaID string) debugCleanupTarget {
	return debugCleanupTarget{
		Table:     table,
		CountSQL:  "SELECT COUNT(*) FROM " + table + " WHERE persona_id = ?",
		DeleteSQL: "DELETE FROM " + table + " WHERE persona_id = ?",
		Args:      []any{personaID},
	}
}

func debugCleanupAllDevTargets() []debugCleanupTarget {
	tables := []string{
		"memory_links",
		"memory_search_fts",
		"memory_search_documents",
		"index_sync_queue",
		"memory_index_map",
		"extraction_runs",
		"consolidation_apply_fingerprints",
		"consolidation_session_fact_writes",
		"memory_access_events",
		"facts",
	}
	targets := make([]debugCleanupTarget, 0, len(tables))
	for _, table := range tables {
		targets = append(targets, debugCleanupTarget{
			Table:     table,
			CountSQL:  "SELECT COUNT(*) FROM " + table,
			DeleteSQL: "DELETE FROM " + table,
		})
	}
	return targets
}

func debugCleanupAutoExtractionTargets(personaID string, since *time.Time) []debugCleanupTarget {
	cte, args := debugCleanupAutoExtractionCTE(personaID, since)
	target := func(table string, countTail string, deleteTail string, extraArgs ...any) debugCleanupTarget {
		allArgs := append([]any(nil), args...)
		allArgs = append(allArgs, extraArgs...)
		return debugCleanupTarget{
			Table:     table,
			CountSQL:  cte + "\n" + countTail,
			DeleteSQL: cte + "\n" + deleteTail,
			Args:      allArgs,
		}
	}
	return []debugCleanupTarget{
		target("index_sync_queue",
			`SELECT COUNT(*) FROM index_sync_queue
WHERE persona_id = ?
  AND (
    (node_type = 'fact' AND node_id IN (SELECT id FROM target_facts))
    OR (node_type = 'memory_link' AND node_id IN (SELECT id FROM target_links))
  )`,
			`DELETE FROM index_sync_queue
WHERE persona_id = ?
  AND (
    (node_type = 'fact' AND node_id IN (SELECT id FROM target_facts))
    OR (node_type = 'memory_link' AND node_id IN (SELECT id FROM target_links))
  )`,
			personaID),
		target("memory_search_fts",
			"SELECT COUNT(*) FROM memory_search_fts WHERE persona_id = ? AND node_type = 'fact' AND node_id IN (SELECT id FROM target_facts)",
			"DELETE FROM memory_search_fts WHERE persona_id = ? AND node_type = 'fact' AND node_id IN (SELECT id FROM target_facts)",
			personaID),
		target("memory_search_documents",
			"SELECT COUNT(*) FROM memory_search_documents WHERE persona_id = ? AND node_type = 'fact' AND node_id IN (SELECT id FROM target_facts)",
			"DELETE FROM memory_search_documents WHERE persona_id = ? AND node_type = 'fact' AND node_id IN (SELECT id FROM target_facts)",
			personaID),
		target("memory_links",
			"SELECT COUNT(*) FROM memory_links WHERE id IN (SELECT id FROM target_links)",
			"DELETE FROM memory_links WHERE id IN (SELECT id FROM target_links)"),
		target("memory_index_map",
			"SELECT COUNT(*) FROM memory_index_map WHERE persona_id = ? AND node_type = 'fact' AND node_id IN (SELECT id FROM target_facts)",
			"DELETE FROM memory_index_map WHERE persona_id = ? AND node_type = 'fact' AND node_id IN (SELECT id FROM target_facts)",
			personaID),
		target("memory_access_events",
			"SELECT COUNT(*) FROM memory_access_events WHERE persona_id = ? AND node_type = 'fact' AND node_id IN (SELECT id FROM target_facts)",
			"DELETE FROM memory_access_events WHERE persona_id = ? AND node_type = 'fact' AND node_id IN (SELECT id FROM target_facts)",
			personaID),
		target("facts",
			"SELECT COUNT(*) FROM facts WHERE persona_id = ? AND id IN (SELECT id FROM target_facts)",
			"DELETE FROM facts WHERE persona_id = ? AND id IN (SELECT id FROM target_facts)",
			personaID),
		target("consolidation_session_fact_writes",
			"SELECT COUNT(*) FROM consolidation_session_fact_writes WHERE persona_id = ? AND fact_id IN (SELECT id FROM target_facts)",
			"DELETE FROM consolidation_session_fact_writes WHERE persona_id = ? AND fact_id IN (SELECT id FROM target_facts)",
			personaID),
		target("consolidation_apply_fingerprints",
			"SELECT COUNT(*) FROM consolidation_apply_fingerprints WHERE persona_id = ? AND (fact_id IN (SELECT id FROM target_facts) OR request_id IN (SELECT request_id FROM target_runs))",
			"DELETE FROM consolidation_apply_fingerprints WHERE persona_id = ? AND (fact_id IN (SELECT id FROM target_facts) OR request_id IN (SELECT request_id FROM target_runs))",
			personaID),
		target("extraction_runs",
			"SELECT COUNT(*) FROM extraction_runs WHERE id IN (SELECT id FROM target_runs)",
			"DELETE FROM extraction_runs WHERE id IN (SELECT id FROM target_runs)"),
	}
}

func debugCleanupAutoExtractionCTE(personaID string, since *time.Time) (string, []any) {
	sinceClause := ""
	args := []any{personaID}
	if since != nil && !since.IsZero() {
		sinceClause = " AND created_at >= ?"
		args = append(args, since.UTC().Format(time.RFC3339Nano))
	}
	args = append(args, personaID)
	if since != nil && !since.IsZero() {
		args = append(args, since.UTC().Format(time.RFC3339Nano))
	}
	args = append(args, personaID, personaID)
	return `WITH
target_sessions(session_id) AS (
  SELECT DISTINCT session_id
  FROM extraction_runs
  WHERE persona_id = ?
    AND session_id IS NOT NULL
    AND mode = 'apply'
    AND status IN ('applied', 'partially_failed', 'nothing_applied')` + sinceClause + `
),
target_runs(id, request_id) AS (
  SELECT id, request_id
  FROM extraction_runs
  WHERE persona_id = ?
    AND request_id IS NOT NULL
    AND mode = 'apply'
    AND status IN ('applied', 'partially_failed', 'nothing_applied')` + sinceClause + `
),
target_facts(id) AS (
  SELECT DISTINCT fact_id
  FROM consolidation_apply_fingerprints
  WHERE persona_id = ?
    AND fact_id IS NOT NULL
    AND request_id IN (SELECT request_id FROM target_runs)
),
target_links(id) AS (
  SELECT id
  FROM memory_links
  WHERE persona_id = ?
    AND (
      (from_node_type = 'fact' AND from_node_id IN (SELECT id FROM target_facts))
      OR (to_node_type = 'fact' AND to_node_id IN (SELECT id FROM target_facts))
    )
)`, args
}

func debugCleanupTableExists(ctx context.Context, db *sql.DB, table string) (bool, error) {
	var count int
	err := db.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM sqlite_master
WHERE name = ?`, table).Scan(&count)
	return count > 0, err
}

func debugCleanupDefaultString(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
