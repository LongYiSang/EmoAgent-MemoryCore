package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/longyisang/emoagent-memorycore/internal/core"
	memsqlite "github.com/longyisang/emoagent-memorycore/internal/store/sqlite"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "command is required")
		return 2
	}

	switch args[0] {
	case "init-db":
		return runInitDB(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown command %q\n", args[0])
		return 2
	}
}

func runInitDB(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := flag.NewFlagSet("init-db", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dbPath := fs.String("db", "", "SQLite database path")
	personaID := fs.String("persona", "default", "persona id to seed")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(*dbPath) == "" {
		fmt.Fprintln(stderr, "--db is required")
		fs.Usage()
		return 2
	}

	ctx := context.Background()
	db, err := memsqlite.Open(ctx, *dbPath)
	if err != nil {
		fmt.Fprintf(stderr, "open db: %v\n", err)
		return 1
	}
	defer db.Close()

	if err := db.Migrate(ctx); err != nil {
		fmt.Fprintf(stderr, "migrate db: %v\n", err)
		return 1
	}

	store := memsqlite.NewStore(db.SQLDB())
	if err := store.EnsurePersona(ctx, core.Persona{ID: *personaID, DisplayName: "Default"}); err != nil {
		fmt.Fprintf(stderr, "seed persona: %v\n", err)
		return 1
	}

	fmt.Fprintf(stdout, "initialized %s\n", *dbPath)
	return 0
}
