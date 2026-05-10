package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/longyisang/emoagent-memorycore/internal/memory/extraction"
	memsqlite "github.com/longyisang/emoagent-memorycore/internal/store/sqlite"
	"github.com/longyisang/emoagent-memorycore/pkg/memorycore"
)

func runExtractRequest(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := newFlagSet("extract-request", stderr)
	var opts commonOptions
	var sessionID, trigger, sinceValue, untilValue, timezone string
	var episodeIDs stringList
	var limit, maxFacts, maxLinks int
	var allowSensitive, allowInference, manualPin, manualForget bool
	addCommonFlags(fs, &opts, formatJSON)
	fs.StringVar(&sessionID, "session", "", "session id")
	fs.Var(&episodeIDs, "episode", "episode id; repeatable")
	fs.StringVar(&trigger, "trigger", memorycore.ExtractionTriggerSessionEnd, "extraction trigger")
	fs.IntVar(&limit, "limit", 50, "maximum episodes")
	fs.StringVar(&sinceValue, "since", "", "RFC3339 lower occurrence bound")
	fs.StringVar(&untilValue, "until", "", "RFC3339 upper occurrence bound")
	fs.StringVar(&timezone, "timezone", "Asia/Singapore", "request timezone")
	fs.BoolVar(&allowSensitive, "allow-sensitive-extraction", false, "allow highly sensitive extraction without review")
	fs.BoolVar(&allowInference, "allow-inference", true, "allow inferred candidates")
	fs.BoolVar(&manualPin, "manual-pin", false, "mark request policy as manual pin")
	fs.BoolVar(&manualForget, "manual-forget", false, "mark request policy as manual forget")
	fs.IntVar(&maxFacts, "max-facts", 12, "maximum fact candidates")
	fs.IntVar(&maxLinks, "max-links", 20, "maximum link candidates")
	if !parseFlags(fs, args) {
		return 2
	}
	if !requireDB(stderr, fs, opts.DBPath) {
		return 2
	}
	if err := validateFormat(opts.Format, formatJSON); err != nil {
		return usageError(stderr, fs, err.Error())
	}
	if err := validateExtractionTriggerFlag(trigger); err != nil {
		return usageError(stderr, fs, err.Error())
	}
	since, err := parseOptionalTimePtr(sinceValue, "--since")
	if err != nil {
		return usageError(stderr, fs, err.Error())
	}
	until, err := parseOptionalTimePtr(untilValue, "--until")
	if err != nil {
		return usageError(stderr, fs, err.Error())
	}

	ctx := context.Background()
	db, err := memsqlite.Open(ctx, opts.DBPath)
	if err != nil {
		return runtimeError(stderr, "open db: %v", err)
	}
	defer db.Close()
	if opts.AutoMigrate {
		if err := db.MigrateWithOptions(ctx, memsqlite.MigrateOptions{EnableFTS: opts.EnableFTS}); err != nil {
			return runtimeError(stderr, "migrate db: %v", err)
		}
	}
	req, err := extraction.BuildRequest(ctx, db.SQLDB(), extraction.BuildRequestOptions{
		PersonaID:                opts.PersonaID,
		SessionID:                stringPtr(sessionID),
		EpisodeIDs:               []string(episodeIDs),
		Trigger:                  trigger,
		Limit:                    limit,
		Since:                    since,
		Until:                    until,
		Timezone:                 timezone,
		AllowSensitiveExtraction: allowSensitive,
		AllowInference:           allowInference,
		ManualPin:                manualPin,
		ManualForget:             manualForget,
		MaxFacts:                 maxFacts,
		MaxLinks:                 maxLinks,
	})
	if err != nil {
		return runtimeError(stderr, "build extraction request: %v", err)
	}
	return writeJSON(stdout, req, opts.Pretty)
}

func runExtractValidate(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := newFlagSet("extract-validate", stderr)
	opts, requestPath, responsePath, failOnReview, failOnReject, ok := parseExtractionIOFlags(fs, args, stderr, formatJSON, false)
	if !ok {
		return 2
	}
	req, resp, code := readExtractionInputs(stderr, fs, requestPath, responsePath)
	if code != 0 {
		return code
	}
	gate := extraction.ValidateExtraction(req, resp)
	if opts.Format == formatJSON {
		writeJSON(stdout, gate, opts.Pretty)
	} else {
		writeGateText(stdout, gate)
	}
	return extractionGateExitCode(gate, failOnReview, failOnReject)
}

func runExtractDryRun(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := newFlagSet("extract-dry-run", stderr)
	opts, requestPath, responsePath, failOnReview, failOnReject, ok := parseExtractionIOFlags(fs, args, stderr, formatText, false)
	if !ok {
		return 2
	}
	req, resp, code := readExtractionInputs(stderr, fs, requestPath, responsePath)
	if code != 0 {
		return code
	}
	gate := extraction.ValidateExtraction(req, resp)
	dryRun := extraction.DryRun(req, resp, gate)
	if opts.Format == formatJSON {
		writeJSON(stdout, dryRun, opts.Pretty)
	} else {
		writeDryRunText(stdout, dryRun)
	}
	return extractionGateExitCode(gate, failOnReview, failOnReject)
}

func runExtractApply(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := newFlagSet("extract-apply", stderr)
	opts, requestPath, responsePath, _, _, ok := parseExtractionIOFlags(fs, args, stderr, formatJSON, true)
	if !ok {
		return 2
	}
	req, resp, code := readExtractionInputs(stderr, fs, requestPath, responsePath)
	if code != 0 {
		return code
	}
	gate := extraction.ValidateExtraction(req, resp)
	if gate.Status == "blocked" {
		if opts.Format == formatJSON {
			writeJSON(stdout, gate, opts.Pretty)
		} else {
			writeGateText(stdout, gate)
		}
		return 1
	}

	ctx := context.Background()
	db, err := memsqlite.Open(ctx, opts.DBPath)
	if err != nil {
		return runtimeError(stderr, "open db: %v", err)
	}
	defer db.Close()
	svc, err := openService(ctx, opts)
	if err != nil {
		return runtimeError(stderr, "open memorycore: %v", err)
	}
	defer svc.Close()

	result := extraction.ApplyAcceptedFacts(ctx, svc, db.SQLDB(), req, resp, gate)
	if opts.Format == formatJSON {
		writeJSON(stdout, result, opts.Pretty)
	} else {
		writeApplyText(stdout, result)
	}
	if result.Status != "applied" {
		return 1
	}
	return 0
}

func parseExtractionIOFlags(fs *flag.FlagSet, args []string, stderr io.Writer, defaultFormat string, dbRequired bool) (commonOptions, string, string, bool, bool, bool) {
	var opts commonOptions
	var requestPath, responsePath string
	var failOnReview, failOnReject bool
	addCommonFlags(fs, &opts, defaultFormat)
	fs.StringVar(&requestPath, "request", "", "ExtractionRequest JSON path, or - for stdin")
	fs.StringVar(&responsePath, "response", "", "ExtractionResponse JSON path, or - for stdin")
	fs.BoolVar(&failOnReview, "fail-on-review", false, "return non-zero when gate has needs_review candidates")
	fs.BoolVar(&failOnReject, "fail-on-reject", false, "return non-zero when gate has rejected candidates")
	if !parseFlags(fs, args) {
		return opts, "", "", false, false, false
	}
	if dbRequired && !requireDB(stderr, fs, opts.DBPath) {
		return opts, "", "", false, false, false
	}
	if err := validateFormat(opts.Format, formatText, formatJSON); err != nil {
		usageError(stderr, fs, err.Error())
		return opts, "", "", false, false, false
	}
	if strings.TrimSpace(requestPath) == "" || strings.TrimSpace(responsePath) == "" {
		usageError(stderr, fs, "--request and --response are required")
		return opts, "", "", false, false, false
	}
	if requestPath == "-" && responsePath == "-" {
		usageError(stderr, fs, "--request and --response cannot both read from stdin")
		return opts, "", "", false, false, false
	}
	return opts, requestPath, responsePath, failOnReview, failOnReject, true
}

func readExtractionInputs(stderr io.Writer, fs *flag.FlagSet, requestPath string, responsePath string) (memorycore.ExtractionRequest, memorycore.ExtractionResponse, int) {
	requestData, err := readInputFile(requestPath)
	if err != nil {
		usageError(stderr, fs, "request json: %v", err)
		return memorycore.ExtractionRequest{}, memorycore.ExtractionResponse{}, 2
	}
	req, err := extraction.ParseRequest(strings.NewReader(string(requestData)))
	if err != nil {
		usageError(stderr, fs, "request json: %v", err)
		return memorycore.ExtractionRequest{}, memorycore.ExtractionResponse{}, 2
	}
	responseData, err := readInputFile(responsePath)
	if err != nil {
		usageError(stderr, fs, "response json: %v", err)
		return memorycore.ExtractionRequest{}, memorycore.ExtractionResponse{}, 2
	}
	resp, err := extraction.ParseResponse(strings.NewReader(string(responseData)))
	if err != nil {
		usageError(stderr, fs, "response json: %v", err)
		return memorycore.ExtractionRequest{}, memorycore.ExtractionResponse{}, 2
	}
	return req, resp, 0
}

func extractionGateExitCode(gate memorycore.ExtractionGateResult, failOnReview bool, failOnReject bool) int {
	if gate.Status == "blocked" {
		return 1
	}
	if failOnReview && gate.Summary.NeedsReviewCount > 0 {
		return 1
	}
	if failOnReject && gate.Summary.RejectedCount > 0 {
		return 1
	}
	return 0
}

func writeGateText(stdout io.Writer, gate memorycore.ExtractionGateResult) {
	fmt.Fprintf(stdout, "request_id=%s\n", gate.RequestID)
	fmt.Fprintf(stdout, "status=%s\n", gate.Status)
	fmt.Fprintf(stdout, "accepted_fact_count=%d\n", gate.Summary.AcceptedFactCount)
	fmt.Fprintf(stdout, "needs_review_count=%d\n", gate.Summary.NeedsReviewCount)
	fmt.Fprintf(stdout, "rejected_count=%d\n", gate.Summary.RejectedCount)
	fmt.Fprintf(stdout, "routed_count=%d\n", gate.Summary.RoutedCount)
	fmt.Fprintf(stdout, "not_applied_count=%d\n", gate.Summary.NotAppliedCount)
}

func writeDryRunText(stdout io.Writer, dryRun memorycore.ExtractionDryRunResult) {
	fmt.Fprintf(stdout, "request_id=%s\n", dryRun.RequestID)
	fmt.Fprintln(stdout, "accepted facts")
	for _, fact := range dryRun.FactPreview {
		if fact.Decision == "accept" {
			fmt.Fprintf(stdout, "- %s predicate=%s pinned=%s user_requested=%s\n", fact.CandidateID, fact.Predicate, boolText(fact.Pinned), boolText(fact.UserRequested))
		}
	}
	fmt.Fprintln(stdout, "needs_review")
	for _, fact := range dryRun.FactPreview {
		if fact.Decision == "needs_review" {
			fmt.Fprintf(stdout, "- %s predicate=%s reasons=%s\n", fact.CandidateID, fact.Predicate, strings.Join(fact.ReasonCodes, ","))
		}
	}
	fmt.Fprintln(stdout, "rejected")
	for _, fact := range dryRun.FactPreview {
		if fact.Decision == "reject" {
			fmt.Fprintf(stdout, "- %s predicate=%s reasons=%s\n", fact.CandidateID, fact.Predicate, strings.Join(fact.ReasonCodes, ","))
		}
	}
	fmt.Fprintln(stdout, "entities to ensure")
	for _, entity := range dryRun.EntityPreview {
		fmt.Fprintf(stdout, "- %s decision=%s action=%s\n", entity.CandidateID, entity.Decision, entity.Action)
	}
	fmt.Fprintln(stdout, "pin/forget routing")
	for _, route := range dryRun.RoutedPinIntents {
		fmt.Fprintf(stdout, "- pin %s decision=%s\n", route.CandidateID, route.Decision)
	}
	for _, route := range dryRun.RoutedDeletionIntents {
		fmt.Fprintf(stdout, "- forget %s route_to=%s decision=%s\n", route.CandidateID, route.RouteTo, route.Decision)
	}
	fmt.Fprintln(stdout, "unsupported apply items")
	for _, link := range dryRun.NotAppliedLinks {
		fmt.Fprintf(stdout, "- link %s type=%s decision=%s\n", link.CandidateID, link.LinkType, link.Decision)
	}
	for _, event := range dryRun.NotAppliedAffectEvents {
		fmt.Fprintf(stdout, "- affect_event %s scope=%s decision=%s\n", event.CandidateID, event.Scope, event.Decision)
	}
}

func writeApplyText(stdout io.Writer, result memorycore.ExtractionApplyResult) {
	fmt.Fprintf(stdout, "request_id=%s\n", result.RequestID)
	fmt.Fprintf(stdout, "status=%s\n", result.Status)
	fmt.Fprintf(stdout, "applied_count=%d\n", result.AppliedCount)
	for _, failure := range result.Failures {
		fmt.Fprintf(stdout, "failure candidate_id=%s reason=%s\n", failure.CandidateID, failure.Reason)
	}
}

func validateExtractionTriggerFlag(trigger string) error {
	switch trigger {
	case memorycore.ExtractionTriggerIdleDetect,
		memorycore.ExtractionTriggerSessionEnd,
		memorycore.ExtractionTriggerManualPin,
		memorycore.ExtractionTriggerManualForget,
		memorycore.ExtractionTriggerWorkCandidate,
		memorycore.ExtractionTriggerReprocess:
		return nil
	default:
		return fmt.Errorf("--trigger must be one of idle_detect|session_end|manual_pin|manual_forget|work_candidate|reprocess")
	}
}
