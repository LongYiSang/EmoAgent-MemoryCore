package main

import (
	"context"
	"fmt"
	"io"

	"github.com/longyisang/emoagent-memorycore/pkg/memorycore"
)

func runForget(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := newFlagSet("forget", stderr)
	var opts commonOptions
	var level, nodeType, nodeID, scope, actor, reason string
	addCommonFlags(fs, &opts, formatText)
	fs.StringVar(&level, "level", "", "forget level")
	fs.StringVar(&nodeType, "node-type", "", "target node type")
	fs.StringVar(&nodeID, "node-id", "", "target node id")
	fs.StringVar(&scope, "scope", memorycore.ForgetScopeExactNode, "scope")
	fs.StringVar(&actor, "actor", memorycore.ForgetActorUser, "actor")
	fs.StringVar(&reason, "reason", memorycore.ForgetReasonUserRequested, "reason code")
	if !parseFlags(fs, args) {
		return 2
	}
	if !requireDB(stderr, fs, opts.DBPath) {
		return 2
	}
	if err := validateFormat(opts.Format, formatText, formatJSON, formatID); err != nil {
		return usageError(stderr, fs, err.Error())
	}
	if err := validateForgetFlags(level, nodeType, nodeID, scope, actor, reason); err != nil {
		return usageError(stderr, fs, err.Error())
	}

	ctx := context.Background()
	svc, err := openService(ctx, opts)
	if err != nil {
		return runtimeError(stderr, "open memorycore: %v", err)
	}
	defer svc.Close()

	result, err := svc.Forget(ctx, memorycore.ForgetRequest{
		PersonaID:  opts.PersonaID,
		Actor:      actor,
		ReasonCode: reason,
		Level:      level,
		Target: memorycore.ForgetTarget{
			ScopeMode: scope,
			NodeType:  nodeType,
			NodeID:    nodeID,
		},
	})
	if err != nil {
		return runtimeError(stderr, "forget: %v", err)
	}
	switch opts.Format {
	case formatID:
		return idOutput(stdout, result.DeletionEventID)
	case formatJSON:
		return writeJSON(stdout, result, opts.Pretty)
	default:
		fmt.Fprintf(stdout, "deletion_event_id=%s\n", result.DeletionEventID)
		fmt.Fprintf(stdout, "target_node_type=%s\n", result.TargetNodeType)
		fmt.Fprintf(stdout, "target_node_id=%s\n", result.TargetNodeID)
		fmt.Fprintf(stdout, "search_documents_deleted=%d\n", result.SearchDocumentsDeleted)
		fmt.Fprintf(stdout, "fts_rows_deleted=%d\n", result.FTSRowsDeleted)
		fmt.Fprintf(stdout, "mirror_deletes_enqueued=%d\n", result.MirrorDeletesEnqueued)
		fmt.Fprintf(stdout, "links_scrubbed=%d\n", result.LinksScrubbed)
		return 0
	}
}

func validateForgetFlags(level string, nodeType string, nodeID string, scope string, actor string, reason string) error {
	if err := validateOneOf("--level", level, memorycore.ForgetLevelSoft, memorycore.ForgetLevelHard, memorycore.ForgetLevelSourceRedact, memorycore.ForgetLevelPurge); err != nil {
		return err
	}
	if err := validateOneOf("--node-type", nodeType, memorycore.ForgetNodeFact, memorycore.ForgetNodeEpisode); err != nil {
		return err
	}
	if nodeID == "" {
		return fmt.Errorf("--node-id is required")
	}
	if err := validateOneOf("--scope", scope, memorycore.ForgetScopeExactNode); err != nil {
		return err
	}
	if err := validateOneOf("--actor", actor, memorycore.ForgetActorUser, memorycore.ForgetActorSystem, memorycore.ForgetActorAdmin); err != nil {
		return err
	}
	if err := validateOneOf("--reason", reason, memorycore.ForgetReasonUserRequested, memorycore.ForgetReasonRetentionPolicy, memorycore.ForgetReasonSafety, memorycore.ForgetReasonAdminPolicy); err != nil {
		return err
	}
	switch level {
	case memorycore.ForgetLevelSoft:
		if nodeType != memorycore.ForgetNodeFact {
			return fmt.Errorf("soft_forget only supports fact targets")
		}
	case memorycore.ForgetLevelHard:
		if nodeType != memorycore.ForgetNodeFact {
			return fmt.Errorf("hard_forget only supports fact targets")
		}
	case memorycore.ForgetLevelSourceRedact:
		if nodeType != memorycore.ForgetNodeEpisode {
			return fmt.Errorf("source_redact only supports episode targets")
		}
	case memorycore.ForgetLevelPurge:
		if nodeType != memorycore.ForgetNodeFact && nodeType != memorycore.ForgetNodeEpisode {
			return fmt.Errorf("purge only supports fact or episode targets")
		}
	}
	return nil
}
