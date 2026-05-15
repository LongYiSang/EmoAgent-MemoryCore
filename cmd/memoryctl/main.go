package main

import (
	"fmt"
	"io"
	"os"
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
	case "validate-config":
		return runValidateConfig(args[1:], stdout, stderr)
	case "config-docs":
		return runConfigDocs(args[1:], stdout, stderr)
	case "start-session":
		return runStartSession(args[1:], stdout, stderr)
	case "end-session":
		return runEndSession(args[1:], stdout, stderr)
	case "append-episode":
		return runAppendEpisode(args[1:], stdout, stderr)
	case "ensure-entity":
		return runEnsureEntity(args[1:], stdout, stderr)
	case "add-alias":
		return runAddAlias(args[1:], stdout, stderr)
	case "consolidate-fact":
		return runConsolidateFact(args[1:], stdout, stderr)
	case "retrieve":
		return runRetrieve(args[1:], stdout, stderr)
	case "forget":
		return runForget(args[1:], stdout, stderr)
	case "rebuild-search":
		return runRebuildSearch(args[1:], stdout, stderr)
	case "retention-run":
		return runRetention(args[1:], stdout, stderr)
	case "retention-jobs-run":
		return runRetentionJobs(args[1:], stdout, stderr)
	case "compression-apply":
		return runCompressionApply(args[1:], stdout, stderr)
	case "mirror-sync-run":
		return runMirrorSync(args[1:], stdout, stderr)
	case "mirror-rebuild":
		return runMirrorRebuild(args[1:], stdout, stderr)
	case "list-facts":
		return runListFacts(args[1:], stdout, stderr)
	case "get-node":
		return runGetNode(args[1:], stdout, stderr)
	case "extract-request":
		return runExtractRequest(args[1:], stdout, stderr)
	case "extract-validate":
		return runExtractValidate(args[1:], stdout, stderr)
	case "extract-dry-run":
		return runExtractDryRun(args[1:], stdout, stderr)
	case "extract-apply":
		return runExtractApply(args[1:], stdout, stderr)
	case "extract-run":
		return runExtractRun(args[1:], stdout, stderr)
	case "extract-batch":
		return runExtractBatch(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown command %q\n", args[0])
		return 2
	}
}
