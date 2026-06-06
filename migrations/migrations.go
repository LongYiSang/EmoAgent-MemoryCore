package migrations

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strings"
)

// FS contains the authoritative SQLite schema migrations for MemoryCore.
//
//go:embed *.sql
var FS embed.FS

type Migration struct {
	Version             string
	Name                string
	Checksum            string
	EquivalentChecksums []string
	SQL                 string
}

func (m Migration) MatchesChecksum(checksum string) bool {
	if checksum == m.Checksum {
		return true
	}
	for _, equivalent := range m.EquivalentChecksums {
		if checksum == equivalent {
			return true
		}
	}
	return false
}

func All() ([]Migration, error) {
	return allFromFS(FS)
}

func allFromFS(fsys fs.FS) ([]Migration, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, err
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})

	migrations := make([]Migration, 0, len(entries))
	versions := make(map[string]string, len(entries))
	names := make(map[string]string, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}

		version, name, err := parseMigrationFileName(entry.Name())
		if err != nil {
			return nil, err
		}
		if existing, ok := versions[version]; ok {
			return nil, fmt.Errorf("duplicate migration version %s in %s and %s", version, existing, entry.Name())
		}
		if existing, ok := names[name]; ok {
			return nil, fmt.Errorf("duplicate migration name %s in %s and %s", name, existing, entry.Name())
		}

		body, err := fs.ReadFile(fsys, entry.Name())
		if err != nil {
			return nil, err
		}
		checksum, equivalents := migrationChecksums(body)
		versions[version] = entry.Name()
		names[name] = entry.Name()
		migrations = append(migrations, Migration{
			Version:             version,
			Name:                name,
			Checksum:            checksum,
			EquivalentChecksums: equivalents,
			SQL:                 string(body),
		})
	}

	return migrations, nil
}

func migrationChecksums(body []byte) (string, []string) {
	normalized := normalizeMigrationChecksumBody(body)
	checksum := sha256Hex(normalized)

	equivalents := make([]string, 0, 2)
	for _, equivalentBody := range [][]byte{
		body,
		bytes.ReplaceAll(normalized, []byte("\n"), []byte("\r\n")),
	} {
		equivalent := sha256Hex(equivalentBody)
		if equivalent != checksum && !containsString(equivalents, equivalent) {
			equivalents = append(equivalents, equivalent)
		}
	}
	return checksum, equivalents
}

func normalizeMigrationChecksumBody(body []byte) []byte {
	normalized := bytes.ReplaceAll(body, []byte("\r\n"), []byte("\n"))
	return bytes.ReplaceAll(normalized, []byte("\r"), []byte("\n"))
}

func sha256Hex(body []byte) string {
	sum := sha256.Sum256(body)
	return fmt.Sprintf("%x", sum)
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func parseMigrationFileName(fileName string) (string, string, error) {
	if !strings.HasSuffix(fileName, ".sql") {
		return "", "", fmt.Errorf("migration %s must use .sql suffix", fileName)
	}
	base := strings.TrimSuffix(fileName, ".sql")
	idx := strings.IndexByte(base, '_')
	if idx != 4 || len(base) <= 5 {
		return "", "", fmt.Errorf("migration %s must use NNNN_name.sql", fileName)
	}
	version := base[:idx]
	name := base[idx+1:]
	for _, ch := range version {
		if ch < '0' || ch > '9' {
			return "", "", fmt.Errorf("migration %s must use numeric version", fileName)
		}
	}
	for _, ch := range name {
		if (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || ch == '_' {
			continue
		}
		return "", "", fmt.Errorf("migration %s has invalid name %q", fileName, name)
	}
	return version, name, nil
}
