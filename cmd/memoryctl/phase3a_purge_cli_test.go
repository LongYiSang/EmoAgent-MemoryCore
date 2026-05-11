package main

import (
	"testing"

	"github.com/longyisang/emoagent-memorycore/pkg/memorycore"
)

func TestRunForgetPurgeFactHidesFromRetrieveAndShowsPurgedStatus(t *testing.T) {
	dbPath := seedCLIConsolidationDB(t)
	factID := requireRunID(t,
		"consolidate-fact",
		"--db", dbPath,
		"--subject", "ent_user",
		"--predicate", "likes",
		"--object-literal", "手冲咖啡",
		"--summary", "用户喜欢手冲咖啡。",
		"--source-episode", "ep_seed",
		"--format", "id",
	)

	requireRunOK(t, "forget", "--db", dbPath, "--level", "purge", "--node-type", "fact", "--node-id", factID)

	retrieved := requireRunText(t, "retrieve", "--db", dbPath, "--session", "session_seed", "--query", "手冲咖啡", "--format", "text")
	requireNotContains(t, retrieved, factID)
	requireNotContains(t, retrieved, "用户喜欢手冲咖啡。")

	inspected := requireRunText(t, "get-node", "--db", dbPath, "--node-type", "fact", "--id", factID, "--include-purged", "--format", "text")
	requireContains(t, inspected, "visibility_status=purged")
	requireContains(t, inspected, "content_summary=[purged]")
	requireNotContains(t, inspected, "用户喜欢手冲咖啡。")
}

func TestRunForgetPurgeEpisodeHidesDependentsAndMarksEpisodePurged(t *testing.T) {
	dbPath := seedCLIConsolidationDB(t)
	factID := requireRunID(t,
		"consolidate-fact",
		"--db", dbPath,
		"--subject", "ent_user",
		"--predicate", "dislikes",
		"--object-literal", "早上八点开会",
		"--summary", "用户不喜欢早上八点开会。",
		"--source-episode", "ep_seed",
		"--format", "id",
	)

	requireRunOK(t, "forget", "--db", dbPath, "--level", "purge", "--node-type", "episode", "--node-id", "ep_seed")

	retrieved := requireRunText(t, "retrieve", "--db", dbPath, "--session", "session_seed", "--query", "早上八点开会", "--format", "text")
	requireNotContains(t, retrieved, factID)
	requireNotContains(t, retrieved, "用户不喜欢早上八点开会。")

	inspected := requireRunText(t, "get-node", "--db", dbPath, "--node-type", "episode", "--id", "ep_seed", "--include-purged", "--format", "text")
	requireContains(t, inspected, "visibility_status=purged")
	requireContains(t, inspected, "content=[purged]")
	requireNotContains(t, inspected, "我不喜欢早上八点开会。")
}

func TestValidateForgetFlagsPurgeAllowsFactAndEpisode(t *testing.T) {
	if err := validateForgetFlags(
		"purge",
		memorycore.ForgetNodeFact,
		"fact-id",
		memorycore.ForgetScopeExactNode,
		memorycore.ForgetActorUser,
		memorycore.ForgetReasonUserRequested,
	); err != nil {
		t.Fatalf("validate purge+fact err = %v, want nil", err)
	}

	if err := validateForgetFlags(
		"purge",
		memorycore.ForgetNodeEpisode,
		"episode-id",
		memorycore.ForgetScopeExactNode,
		memorycore.ForgetActorUser,
		memorycore.ForgetReasonUserRequested,
	); err != nil {
		t.Fatalf("validate purge+episode err = %v, want nil", err)
	}
}

func TestValidateForgetFlagsRejectsUnsupportedPurgeNodeType(t *testing.T) {
	if err := validateForgetFlags(
		"purge",
		"entity",
		"entity-id",
		memorycore.ForgetScopeExactNode,
		memorycore.ForgetActorUser,
		memorycore.ForgetReasonUserRequested,
	); err == nil {
		t.Fatal("validate purge+unsupported node type err = nil, want error")
	}
}
