package sqlite_test

import (
	"context"
	"testing"
)

func TestNaturalMigrationCreatesTables(t *testing.T) {
	ctx := context.Background()
	db := openMigratedDB(t, ctx)
	defer db.Close()

	requireTable(t, db.SQLDB(), "memory_natural_states")
	requireTable(t, db.SQLDB(), "memory_natural_events")
	requireTable(t, db.SQLDB(), "memory_natural_runs")
	requireTable(t, db.SQLDB(), "memory_natural_compression_candidates")
}
