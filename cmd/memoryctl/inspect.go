package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	_ "modernc.org/sqlite"
)

type inspectOptions struct {
	commonOptions
	Predicate             string
	Subject               string
	Object                string
	NodeType              string
	NodeID                string
	Limit                 int
	Offset                int
	SensitivityPermission string
	IncludeHidden         bool
	IncludeForgotten      bool
	IncludeRedacted       bool
	IncludePurged         bool
	IncludeInvalid        bool
	IncludeArchived       bool
	All                   bool
}

type factInspectRow struct {
	ID                   string
	Predicate            string
	ContentSummary       string
	ObjectLiteral        sql.NullString
	ValidityStatus       string
	VisibilityStatus     string
	LifecycleStatus      string
	SensitivityLevel     string
	Searchable           bool
	Pinned               bool
	EvidenceCount        int
	VisibleEvidenceCount int
}

type episodeInspectRow struct {
	ID               string
	SessionID        string
	Role             string
	Content          string
	VisibilityStatus string
	SensitivityLevel string
	Searchable       bool
}

func runListFacts(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := newFlagSet("list-facts", stderr)
	opts := inspectOptions{Limit: 50, SensitivityPermission: "normal"}
	addCommonFlags(fs, &opts.commonOptions, formatText)
	addInspectFlags(fs, &opts)
	fs.StringVar(&opts.Predicate, "predicate", "", "predicate filter")
	fs.StringVar(&opts.Subject, "subject", "", "subject entity id filter")
	fs.StringVar(&opts.Object, "object", "", "object literal/entity substring")
	fs.IntVar(&opts.Limit, "limit", 50, "limit")
	fs.IntVar(&opts.Offset, "offset", 0, "offset")
	if !parseFlags(fs, args) {
		return 2
	}
	if !requireDB(stderr, fs, opts.DBPath) {
		return 2
	}
	if err := validateFormat(opts.Format, formatText, formatJSON, formatTSV); err != nil {
		return usageError(stderr, fs, err.Error())
	}
	if err := validateInspectOptions(opts); err != nil {
		return usageError(stderr, fs, err.Error())
	}

	ctx := context.Background()
	db, err := openInspectDB(ctx, opts.DBPath)
	if err != nil {
		return runtimeError(stderr, "open db: %v", err)
	}
	defer db.Close()

	rows, err := queryFacts(ctx, db, opts, "")
	if err != nil {
		return runtimeError(stderr, "list facts: %v", err)
	}
	return outputFactRows(stdout, rows, opts)
}

func runGetNode(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := newFlagSet("get-node", stderr)
	opts := inspectOptions{SensitivityPermission: "normal"}
	addCommonFlags(fs, &opts.commonOptions, formatText)
	addInspectFlags(fs, &opts)
	fs.StringVar(&opts.NodeType, "node-type", "", "node type")
	fs.StringVar(&opts.NodeID, "id", "", "node id")
	if !parseFlags(fs, args) {
		return 2
	}
	if !requireDB(stderr, fs, opts.DBPath) {
		return 2
	}
	if opts.NodeType == "" {
		return usageError(stderr, fs, "--node-type is required")
	}
	if opts.NodeID == "" {
		return usageError(stderr, fs, "--id is required")
	}
	if err := validateFormat(opts.Format, formatText, formatJSON); err != nil {
		return usageError(stderr, fs, err.Error())
	}
	if err := validateInspectOptions(opts); err != nil {
		return usageError(stderr, fs, err.Error())
	}

	ctx := context.Background()
	db, err := openInspectDB(ctx, opts.DBPath)
	if err != nil {
		return runtimeError(stderr, "open db: %v", err)
	}
	defer db.Close()

	switch opts.NodeType {
	case "fact":
		rows, err := queryFacts(ctx, db, opts, opts.NodeID)
		if err != nil {
			return runtimeError(stderr, "get fact: %v", err)
		}
		if len(rows) == 0 {
			if exists, err := nodeExists(ctx, db, opts.PersonaID, "facts", opts.NodeID); err != nil {
				return runtimeError(stderr, "get fact: %v", err)
			} else if exists {
				return runtimeError(stderr, "node exists but is not visible by default; use the matching include flag for debug inspection")
			}
			return runtimeError(stderr, "fact %s not found", opts.NodeID)
		}
		return outputFactRows(stdout, rows, opts)
	case "episode":
		row, visible, err := queryEpisode(ctx, db, opts)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return runtimeError(stderr, "episode %s not found", opts.NodeID)
			}
			return runtimeError(stderr, "get episode: %v", err)
		}
		if !visible {
			return runtimeError(stderr, "node exists but is not visible by default; use the matching include flag for debug inspection")
		}
		return outputEpisode(stdout, row, opts)
	case "entity":
		return outputSimpleNode(ctx, db, stdout, stderr, opts, "entities", "canonical_name")
	case "session":
		return outputSimpleNode(ctx, db, stdout, stderr, opts, "sessions", "channel")
	default:
		return usageError(stderr, fs, "--node-type must be one of fact|episode|entity|session")
	}
}

func addInspectFlags(fs *flag.FlagSet, opts *inspectOptions) {
	fs.StringVar(&opts.SensitivityPermission, "sensitivity-permission", "normal", "sensitivity permission")
	fs.BoolVar(&opts.IncludeHidden, "include-hidden", false, "include hidden nodes")
	fs.BoolVar(&opts.IncludeForgotten, "include-forgotten", false, "include forgotten nodes")
	fs.BoolVar(&opts.IncludeRedacted, "include-redacted", false, "include redacted nodes")
	fs.BoolVar(&opts.IncludePurged, "include-purged", false, "include purged nodes")
	fs.BoolVar(&opts.IncludeInvalid, "include-invalid", false, "include invalid facts")
	fs.BoolVar(&opts.IncludeArchived, "include-archived", false, "include archived facts")
	fs.BoolVar(&opts.All, "all", false, "include all statuses for debug")
}

func validateInspectOptions(opts inspectOptions) error {
	if err := validateOneOf("--sensitivity-permission", opts.SensitivityPermission, "normal", "sensitive", "highly_sensitive"); err != nil {
		return err
	}
	if opts.Limit < 0 {
		return fmt.Errorf("--limit must be non-negative")
	}
	if opts.Offset < 0 {
		return fmt.Errorf("--offset must be non-negative")
	}
	return nil
}

func openInspectDB(ctx context.Context, dbPath string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func queryFacts(ctx context.Context, db *sql.DB, opts inspectOptions, id string) ([]factInspectRow, error) {
	where := []string{"f.persona_id = ?"}
	args := []any{opts.PersonaID}
	if id != "" {
		where = append(where, "f.id = ?")
		args = append(args, id)
	}
	if opts.Predicate != "" {
		where = append(where, "f.predicate = ?")
		args = append(args, opts.Predicate)
	}
	if opts.Subject != "" {
		where = append(where, "f.subject_entity_id = ?")
		args = append(args, opts.Subject)
	}
	if opts.Object != "" {
		where = append(where, "(COALESCE(f.object_literal, '') LIKE ? OR COALESCE(f.object_entity_id, '') LIKE ?)")
		like := "%" + opts.Object + "%"
		args = append(args, like, like)
	}
	if inspectDefaultMode(opts) {
		where = append(where, defaultFactAuthoritySQL())
		args = append(args, sensitivityRank(opts.SensitivityPermission))
	} else {
		where = append(where, includeVisibilitySQL(opts, "f.visibility_status", []string{"visible", "hidden", "forgotten", "purged"}))
		if !opts.All && !opts.IncludeInvalid {
			where = append(where, "f.validity_status IN ('valid', 'uncertain')")
		}
		if !opts.All && !opts.IncludeArchived {
			where = append(where, "f.lifecycle_status NOT IN ('archived', 'deep_archived')")
		}
	}
	query := `
SELECT f.id, f.predicate, f.content_summary, f.object_literal,
       f.validity_status, f.visibility_status, f.lifecycle_status, f.sensitivity_level,
       f.searchable, f.pinned,
       (
         SELECT COUNT(*)
         FROM memory_links l
         WHERE l.persona_id = f.persona_id
           AND l.from_node_type = 'fact'
           AND l.from_node_id = f.id
           AND l.link_type = 'EVIDENCED_BY'
           AND l.to_node_type = 'episode'
       ) AS evidence_count,
       (
         SELECT COUNT(*)
         FROM memory_links l
         JOIN episodes e
           ON e.persona_id = l.persona_id
          AND e.id = l.to_node_id
         WHERE l.persona_id = f.persona_id
           AND l.from_node_type = 'fact'
           AND l.from_node_id = f.id
           AND l.link_type = 'EVIDENCED_BY'
           AND l.to_node_type = 'episode'
           AND e.visibility_status = 'visible'
           AND e.searchable = 1
       ) AS visible_evidence_count
FROM facts f
WHERE ` + strings.Join(where, "\n  AND ") + `
ORDER BY f.created_at ASC
LIMIT ? OFFSET ?`
	args = append(args, opts.Limit, opts.Offset)
	if id != "" {
		query = strings.Replace(query, "LIMIT ? OFFSET ?", "LIMIT 1 OFFSET 0", 1)
		args = args[:len(args)-2]
	}

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []factInspectRow
	for rows.Next() {
		var row factInspectRow
		var searchable, pinned int
		if err := rows.Scan(&row.ID, &row.Predicate, &row.ContentSummary, &row.ObjectLiteral, &row.ValidityStatus, &row.VisibilityStatus, &row.LifecycleStatus, &row.SensitivityLevel, &searchable, &pinned, &row.EvidenceCount, &row.VisibleEvidenceCount); err != nil {
			return nil, err
		}
		row.Searchable = searchable != 0
		row.Pinned = pinned != 0
		result = append(result, row)
	}
	return result, rows.Err()
}

func defaultFactAuthoritySQL() string {
	return `f.visibility_status = 'visible'
  AND f.searchable = 1
  AND f.validity_status IN ('valid', 'uncertain')
  AND f.lifecycle_status NOT IN ('archived', 'deep_archived')
  AND CASE f.sensitivity_level
        WHEN 'highly_sensitive' THEN 2
        WHEN 'sensitive' THEN 1
        ELSE 0
      END <= ?
  AND (
    EXISTS (
      SELECT 1
      FROM memory_links l
      JOIN episodes e
        ON e.persona_id = l.persona_id
       AND e.id = l.to_node_id
      WHERE l.persona_id = f.persona_id
        AND l.from_node_type = 'fact'
        AND l.from_node_id = f.id
        AND l.link_type = 'EVIDENCED_BY'
        AND l.to_node_type = 'episode'
        AND e.visibility_status = 'visible'
        AND e.searchable = 1
    )
    OR (
      f.pinned = 1
      AND NOT EXISTS (
        SELECT 1
        FROM memory_links l
        WHERE l.persona_id = f.persona_id
          AND l.from_node_type = 'fact'
          AND l.from_node_id = f.id
          AND l.link_type = 'EVIDENCED_BY'
          AND l.to_node_type = 'episode'
      )
    )
  )`
}

func includeVisibilitySQL(opts inspectOptions, column string, universe []string) string {
	if opts.All {
		return "1 = 1"
	}
	allowed := map[string]bool{"visible": true}
	if opts.IncludeHidden {
		allowed["hidden"] = true
	}
	if opts.IncludeForgotten {
		allowed["forgotten"] = true
	}
	if opts.IncludeRedacted {
		allowed["redacted"] = true
	}
	if opts.IncludePurged {
		allowed["purged"] = true
	}
	var values []string
	for _, status := range universe {
		if allowed[status] {
			values = append(values, "'"+status+"'")
		}
	}
	return column + " IN (" + strings.Join(values, ", ") + ")"
}

func inspectDefaultMode(opts inspectOptions) bool {
	return !opts.IncludeHidden && !opts.IncludeForgotten && !opts.IncludeRedacted && !opts.IncludePurged && !opts.IncludeInvalid && !opts.IncludeArchived && !opts.All
}

func outputFactRows(stdout io.Writer, rows []factInspectRow, opts inspectOptions) int {
	if opts.Format == formatJSON {
		out := make([]map[string]any, 0, len(rows))
		for _, row := range rows {
			out = append(out, factJSON(row))
		}
		if opts.NodeID != "" && len(out) == 1 {
			return writeJSON(stdout, out[0], opts.Pretty)
		}
		return writeJSON(stdout, out, opts.Pretty)
	}
	if opts.Format == formatTSV {
		fmt.Fprintln(stdout, "id\tpredicate\tcontent_summary\tvalidity_status\tvisibility_status\tlifecycle_status\tsearchable\tpinned")
		for _, row := range rows {
			fmt.Fprintf(stdout, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n", row.ID, row.Predicate, safeFactSummary(row), row.ValidityStatus, row.VisibilityStatus, row.LifecycleStatus, boolText(row.Searchable), boolText(row.Pinned))
		}
		return 0
	}
	for _, row := range rows {
		fmt.Fprintf(stdout, "id=%s\n", row.ID)
		fmt.Fprintf(stdout, "predicate=%s\n", row.Predicate)
		fmt.Fprintf(stdout, "content_summary=%s\n", safeFactSummary(row))
		if objectLiteral, ok := safeFactObjectLiteral(row); ok {
			fmt.Fprintf(stdout, "object_literal=%s\n", objectLiteral)
		}
		fmt.Fprintf(stdout, "validity_status=%s\n", row.ValidityStatus)
		fmt.Fprintf(stdout, "visibility_status=%s\n", row.VisibilityStatus)
		fmt.Fprintf(stdout, "lifecycle_status=%s\n", row.LifecycleStatus)
		fmt.Fprintf(stdout, "sensitivity_level=%s\n", row.SensitivityLevel)
		fmt.Fprintf(stdout, "searchable=%s\n", boolText(row.Searchable))
		fmt.Fprintf(stdout, "pinned=%s\n", boolText(row.Pinned))
	}
	return 0
}

func factJSON(row factInspectRow) map[string]any {
	return map[string]any{
		"id":                row.ID,
		"predicate":         row.Predicate,
		"content_summary":   safeFactSummary(row),
		"object_literal":    safeFactObjectLiteralValue(row),
		"validity_status":   row.ValidityStatus,
		"visibility_status": row.VisibilityStatus,
		"lifecycle_status":  row.LifecycleStatus,
		"sensitivity_level": row.SensitivityLevel,
		"searchable":        row.Searchable,
		"pinned":            row.Pinned,
	}
}

func safeFactSummary(row factInspectRow) string {
	switch row.VisibilityStatus {
	case "forgotten":
		return "[forgotten]"
	case "purged":
		return "[purged]"
	}
	if row.EvidenceCount > 0 && row.VisibleEvidenceCount == 0 {
		return "[redacted]"
	}
	return row.ContentSummary
}

func safeFactObjectLiteral(row factInspectRow) (string, bool) {
	if !row.ObjectLiteral.Valid {
		return "", false
	}
	switch row.VisibilityStatus {
	case "forgotten":
		return "[forgotten]", true
	case "purged":
		return "[purged]", true
	}
	if row.EvidenceCount > 0 && row.VisibleEvidenceCount == 0 {
		return "[redacted]", true
	}
	return row.ObjectLiteral.String, true
}

func safeFactObjectLiteralValue(row factInspectRow) any {
	value, ok := safeFactObjectLiteral(row)
	if !ok {
		return nil
	}
	return value
}

func queryEpisode(ctx context.Context, db *sql.DB, opts inspectOptions) (episodeInspectRow, bool, error) {
	var row episodeInspectRow
	var searchable int
	err := db.QueryRowContext(ctx, `
SELECT id, session_id, role, content, visibility_status, sensitivity_level, searchable
FROM episodes
WHERE persona_id = ? AND id = ?`, opts.PersonaID, opts.NodeID).Scan(&row.ID, &row.SessionID, &row.Role, &row.Content, &row.VisibilityStatus, &row.SensitivityLevel, &searchable)
	if err != nil {
		return row, false, err
	}
	row.Searchable = searchable != 0
	visible := row.VisibilityStatus == "visible" && row.Searchable
	if !visible && !inspectDefaultMode(opts) {
		visible = includeStatusAllowed(opts, row.VisibilityStatus)
	}
	return row, visible, nil
}

func includeStatusAllowed(opts inspectOptions, status string) bool {
	if opts.All {
		return true
	}
	switch status {
	case "hidden":
		return opts.IncludeHidden
	case "forgotten":
		return opts.IncludeForgotten
	case "redacted":
		return opts.IncludeRedacted
	case "purged":
		return opts.IncludePurged
	default:
		return status == "visible"
	}
}

func outputEpisode(stdout io.Writer, row episodeInspectRow, opts inspectOptions) int {
	if opts.Format == formatJSON {
		return writeJSON(stdout, map[string]any{
			"id":                row.ID,
			"session_id":        row.SessionID,
			"role":              row.Role,
			"content":           safeEpisodeContent(row),
			"visibility_status": row.VisibilityStatus,
			"sensitivity_level": row.SensitivityLevel,
			"searchable":        row.Searchable,
		}, opts.Pretty)
	}
	fmt.Fprintf(stdout, "id=%s\n", row.ID)
	fmt.Fprintf(stdout, "session_id=%s\n", row.SessionID)
	fmt.Fprintf(stdout, "role=%s\n", row.Role)
	fmt.Fprintf(stdout, "content=%s\n", safeEpisodeContent(row))
	fmt.Fprintf(stdout, "visibility_status=%s\n", row.VisibilityStatus)
	fmt.Fprintf(stdout, "sensitivity_level=%s\n", row.SensitivityLevel)
	fmt.Fprintf(stdout, "searchable=%s\n", boolText(row.Searchable))
	return 0
}

func safeEpisodeContent(row episodeInspectRow) string {
	switch row.VisibilityStatus {
	case "redacted":
		return "[redacted]"
	case "purged":
		return "[purged]"
	default:
		return row.Content
	}
}

func outputSimpleNode(ctx context.Context, db *sql.DB, stdout io.Writer, stderr io.Writer, opts inspectOptions, table string, valueColumn string) int {
	var value string
	err := db.QueryRowContext(ctx, fmt.Sprintf(`SELECT %s FROM %s WHERE persona_id = ? AND id = ?`, valueColumn, table), opts.PersonaID, opts.NodeID).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return runtimeError(stderr, "%s %s not found", strings.TrimSuffix(table, "s"), opts.NodeID)
	}
	if err != nil {
		return runtimeError(stderr, "get %s: %v", table, err)
	}
	if opts.Format == formatJSON {
		return writeJSON(stdout, map[string]string{"id": opts.NodeID, valueColumn: value}, opts.Pretty)
	}
	fmt.Fprintf(stdout, "id=%s\n", opts.NodeID)
	fmt.Fprintf(stdout, "%s=%s\n", valueColumn, value)
	return 0
}

func nodeExists(ctx context.Context, db *sql.DB, personaID string, table string, nodeID string) (bool, error) {
	var count int
	err := db.QueryRowContext(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM %s WHERE persona_id = ? AND id = ?`, table), personaID, nodeID).Scan(&count)
	return count > 0, err
}
