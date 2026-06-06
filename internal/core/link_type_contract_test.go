package core

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

func TestAllowedLinkTypesMatchesSQLiteMigration(t *testing.T) {
	sqliteTypes := readSQLiteAllowedLinkTypes(t)
	coreTypes := linkTypesToStrings(AllowedLinkTypes())

	requireUniqueLinkTypes(t, "SQLite migration", sqliteTypes)
	requireUniqueLinkTypes(t, "Go LinkType constants", coreTypes)

	sort.Strings(sqliteTypes)
	sort.Strings(coreTypes)
	if strings.Join(coreTypes, "\n") != strings.Join(sqliteTypes, "\n") {
		t.Fatalf("Go LinkType constants = %#v, want SQLite migration set %#v", coreTypes, sqliteTypes)
	}
}

func readSQLiteAllowedLinkTypes(t *testing.T) []string {
	t.Helper()

	body, err := os.ReadFile(filepath.Join("..", "..", "migrations", "0001_initial.sql"))
	if err != nil {
		t.Fatalf("read initial migration: %v", err)
	}

	checkRE := regexp.MustCompile(`(?s)link_type\s+TEXT\s+NOT\s+NULL\s+CHECK\s*\(\s*link_type\s+IN\s*\((.*?)\)\s*\)`)
	match := checkRE.FindSubmatch(body)
	if match == nil {
		t.Fatal("memory_links.link_type CHECK constraint was not found in initial migration")
	}

	valueRE := regexp.MustCompile(`'([A-Z_]+)'`)
	matches := valueRE.FindAllSubmatch(match[1], -1)
	if len(matches) == 0 {
		t.Fatal("memory_links.link_type CHECK constraint has no values")
	}

	values := make([]string, 0, len(matches))
	for _, match := range matches {
		values = append(values, string(match[1]))
	}
	return values
}

func linkTypesToStrings(values []LinkType) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, string(value))
	}
	return out
}

func requireUniqueLinkTypes(t *testing.T, label string, values []string) {
	t.Helper()

	seen := make(map[string]bool, len(values))
	for _, value := range values {
		if seen[value] {
			t.Fatalf("%s contains duplicate link type %q", label, value)
		}
		seen[value] = true
	}
}
