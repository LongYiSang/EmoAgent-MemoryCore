package migrations

import (
	"embed"
	"io/fs"
	"sort"
	"strings"
)

// FS contains the authoritative SQLite schema migrations for MemoryCore.
//
//go:embed *.sql
var FS embed.FS

type Migration struct {
	Version string
	Name    string
	SQL     string
}

func All() ([]Migration, error) {
	entries, err := fs.ReadDir(FS, ".")
	if err != nil {
		return nil, err
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})

	migrations := make([]Migration, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}

		body, err := FS.ReadFile(entry.Name())
		if err != nil {
			return nil, err
		}
		version := entry.Name()
		if idx := strings.IndexByte(version, '_'); idx > 0 {
			version = version[:idx]
		}
		migrations = append(migrations, Migration{
			Version: version,
			Name:    entry.Name(),
			SQL:     string(body),
		})
	}

	return migrations, nil
}
