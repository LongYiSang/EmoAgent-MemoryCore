package main

import (
	"context"
	"fmt"
	"io"

	"github.com/longyisang/emoagent-memorycore/internal/core"
	memsqlite "github.com/longyisang/emoagent-memorycore/internal/store/sqlite"
)

func runInitDB(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := newFlagSet("init-db", stderr)
	var opts commonOptions
	addCommonFlags(fs, &opts, formatText)
	if !parseFlags(fs, args) {
		return 2
	}
	if !requireDB(stderr, fs, opts.DBPath) {
		return 2
	}
	if err := validateFormat(opts.Format, formatText, formatJSON); err != nil {
		return usageError(stderr, fs, err.Error())
	}

	ctx := context.Background()
	db, err := memsqlite.Open(ctx, opts.DBPath)
	if err != nil {
		return runtimeError(stderr, "open db: %v", err)
	}
	defer db.Close()

	if err := db.MigrateWithOptions(ctx, memsqlite.MigrateOptions{EnableFTS: opts.EnableFTS}); err != nil {
		return runtimeError(stderr, "migrate db: %v", err)
	}

	store := memsqlite.NewStore(db.SQLDB())
	if err := store.EnsurePersona(ctx, core.Persona{ID: opts.PersonaID, DisplayName: displayNameForPersona(opts.PersonaID)}); err != nil {
		return runtimeError(stderr, "seed persona: %v", err)
	}

	switch opts.Format {
	case formatJSON:
		return writeJSON(stdout, map[string]any{
			"db":          opts.DBPath,
			"persona_id":  opts.PersonaID,
			"initialized": true,
			"enable_fts":  opts.EnableFTS,
		}, opts.Pretty)
	default:
		fmt.Fprintf(stdout, "initialized %s\n", opts.DBPath)
		return 0
	}
}

func displayNameForPersona(personaID string) string {
	if personaID == "default" {
		return "Default"
	}
	return personaID
}
