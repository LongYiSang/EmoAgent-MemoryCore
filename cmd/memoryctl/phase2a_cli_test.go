package main

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunPhase2AOperationalFlow(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "memory.db")

	requireRunOK(t, "init-db", "--db", dbPath, "--enable-fts=false")
	sessionID := requireRunID(t, "start-session", "--db", dbPath, "--channel", "cli", "--format", "id")
	episodeID := requireRunID(t, "append-episode", "--db", dbPath, "--session", sessionID, "--role", "user", "--content", "我不喜欢早上八点开会。", "--format", "id")
	entityID := requireRunID(t, "ensure-entity", "--db", dbPath, "--id", "ent_user", "--name", "User", "--type", "user", "--format", "id")
	if entityID != "ent_user" {
		t.Fatalf("entity id = %q, want ent_user", entityID)
	}
	factID := requireRunID(t,
		"consolidate-fact",
		"--db", dbPath,
		"--subject", entityID,
		"--predicate", "dislikes",
		"--object-literal", "早上八点开会",
		"--summary", "用户不喜欢早上八点开会。",
		"--fact-type", "stable_preference",
		"--source-episode", episodeID,
		"--confidence", "explicit",
		"--importance", "0.7",
		"--valence", "-0.55",
		"--arousal", "0.35",
		"--format", "id",
	)

	retrieved := requireRunText(t, "retrieve", "--db", dbPath, "--query", "早上八点", "--format", "text")
	requireContains(t, retrieved, factID)
	requireContains(t, retrieved, "用户不喜欢早上八点开会。")

	jsonOut := requireRunText(t, "retrieve", "--db", dbPath, "--query", "早上八点", "--format", "json")
	var decoded map[string]any
	if err := json.Unmarshal([]byte(jsonOut), &decoded); err != nil {
		t.Fatalf("retrieve json did not decode: %v\n%s", err, jsonOut)
	}
	analysis, ok := decoded["QueryAnalysis"].(map[string]any)
	if !ok {
		t.Fatalf("retrieve json QueryAnalysis missing or wrong shape: %#v", decoded["QueryAnalysis"])
	}
	if analysis["Raw"] != "早上八点" || analysis["TimeMode"] != "current" {
		t.Fatalf("retrieve json QueryAnalysis = %#v, want raw query with current time mode", analysis)
	}

	endedID := requireRunID(t, "end-session", "--db", dbPath, "--session", sessionID, "--summary", "manual smoke done", "--format", "id")
	if endedID != sessionID {
		t.Fatalf("ended session id = %q, want %q", endedID, sessionID)
	}
}

func TestRunConsolidateRejectedAndNeedsReviewExitOneWithoutEmptyID(t *testing.T) {
	dbPath := seedCLIConsolidationDB(t)

	stdout, stderr, code := runCLI(
		"consolidate-fact",
		"--db", dbPath,
		"--subject", "ent_user",
		"--predicate", "missing_predicate",
		"--object-literal", "咖啡",
		"--summary", "用户喜欢咖啡。",
		"--source-episode", "ep_seed",
		"--format", "id",
	)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1; stdout=%q stderr=%q", code, stdout, stderr)
	}
	if strings.TrimSpace(stdout) != "" {
		t.Fatalf("stdout = %q, want empty id output", stdout)
	}
	requireContains(t, stderr, "rejected")
	requireContains(t, stderr, "predicate schema")

	stdout, stderr, code = runCLI(
		"consolidate-fact",
		"--db", dbPath,
		"--subject", "ent_user",
		"--predicate", "feels_about_agent",
		"--object-literal", "信任",
		"--summary", "用户信任 Agent。",
		"--source-episode", "ep_seed",
		"--format", "json",
	)
	if code != 1 {
		t.Fatalf("needs_review exit code = %d, want 1; stdout=%q stderr=%q", code, stdout, stderr)
	}
	requireContains(t, stdout, `"Status"`)
	requireContains(t, stdout, `needs_review`)
}

func TestRunValidationExitCodeTwoForCLIShapeErrors(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	requireRunOK(t, "init-db", "--db", dbPath)

	_, stderr, code := runCLI(
		"consolidate-fact",
		"--db", dbPath,
		"--subject", "ent_user",
		"--predicate", "likes",
		"--object-literal", "咖啡",
		"--object-entity", "ent_coffee",
		"--summary", "用户喜欢咖啡。",
		"--source-episode", "ep_seed",
	)
	if code != 2 {
		t.Fatalf("object validation code = %d, want 2; stderr=%q", code, stderr)
	}
	requireContains(t, stderr, "exactly one")

	_, stderr, code = runCLI("forget", "--db", dbPath, "--level", "hard_forget", "--node-type", "episode", "--node-id", "ep_seed")
	if code != 2 {
		t.Fatalf("forget validation code = %d, want 2; stderr=%q", code, stderr)
	}
	requireContains(t, stderr, "hard_forget only supports fact")

	_, stderr, code = runCLI("retrieve", "--db", dbPath, "--query", "咖啡", "--format", "id")
	if code != 2 {
		t.Fatalf("unsupported id format code = %d, want 2; stderr=%q", code, stderr)
	}
	requireContains(t, stderr, "--format id")
}

func TestRunInspectAuthorityAndHardForgetSafety(t *testing.T) {
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

	listBefore := requireRunText(t, "list-facts", "--db", dbPath, "--format", "text")
	requireContains(t, listBefore, factID)
	requireContains(t, listBefore, "用户不喜欢早上八点开会。")

	requireRunOK(t, "forget", "--db", dbPath, "--level", "hard_forget", "--node-type", "fact", "--node-id", factID)
	requireRunOK(t, "rebuild-search", "--db", dbPath)

	listAfter := requireRunText(t, "list-facts", "--db", dbPath, "--format", "text")
	requireNotContains(t, listAfter, factID)
	requireNotContains(t, listAfter, "用户不喜欢早上八点开会。")

	forgotten := requireRunText(t, "get-node", "--db", dbPath, "--node-type", "fact", "--id", factID, "--include-forgotten", "--format", "text")
	requireContains(t, forgotten, "visibility_status=forgotten")
	requireContains(t, forgotten, "content_summary=[forgotten]")
	requireNotContains(t, forgotten, "早上八点开会")
}

func TestRunSourceRedactSuppressesRetrievalAndDefaultInspectWithoutChangingFactAssertions(t *testing.T) {
	dbPath := seedCLIConsolidationDB(t)
	factID := requireRunID(t,
		"consolidate-fact",
		"--db", dbPath,
		"--subject", "ent_user",
		"--predicate", "likes",
		"--object-literal", "乌龙茶",
		"--summary", "用户喜欢乌龙茶。",
		"--source-episode", "ep_seed",
		"--format", "id",
	)

	requireRunOK(t, "forget", "--db", dbPath, "--level", "source_redact", "--node-type", "episode", "--node-id", "ep_seed")

	episode := requireRunText(t, "get-node", "--db", dbPath, "--node-type", "episode", "--id", "ep_seed", "--include-redacted", "--format", "text")
	requireContains(t, episode, "visibility_status=redacted")
	requireContains(t, episode, "content=[redacted]")
	requireNotContains(t, episode, "我不喜欢早上八点开会。")

	retrieved := requireRunText(t, "retrieve", "--db", dbPath, "--query", "乌龙茶", "--format", "text")
	requireNotContains(t, retrieved, factID)
	requireNotContains(t, retrieved, "用户喜欢乌龙茶。")

	listed := requireRunText(t, "list-facts", "--db", dbPath, "--format", "text")
	requireNotContains(t, listed, factID)
	requireNotContains(t, listed, "用户喜欢乌龙茶。")

	_, stderr, code := runCLI("get-node", "--db", dbPath, "--node-type", "fact", "--id", factID, "--format", "text")
	if code != 1 {
		t.Fatalf("get-node fact code = %d, want 1; stderr=%q", code, stderr)
	}
	requireContains(t, stderr, "not visible by default")
}

func TestRunListFactsAlignsSensitivityAndPinnedNoEvidenceAuthority(t *testing.T) {
	dbPath := seedCLIConsolidationDB(t)
	sensitiveFactID := requireRunID(t,
		"consolidate-fact",
		"--db", dbPath,
		"--subject", "ent_user",
		"--predicate", "has_boundary",
		"--object-literal", "晚上十点后不要提醒工作",
		"--summary", "用户不希望晚上十点后被提醒工作。",
		"--source-episode", "ep_seed",
		"--sensitivity", "sensitive",
		"--format", "id",
	)
	pinnedFactID := requireRunID(t,
		"consolidate-fact",
		"--db", dbPath,
		"--subject", "ent_user",
		"--predicate", "likes",
		"--object-literal", "手冲咖啡",
		"--summary", "用户喜欢手冲咖啡。",
		"--pinned=true",
		"--user-requested=true",
		"--allow-manual-pin-without-source=true",
		"--format", "id",
	)

	normalList := requireRunText(t, "list-facts", "--db", dbPath, "--format", "text")
	requireNotContains(t, normalList, sensitiveFactID)
	requireNotContains(t, normalList, "用户不希望晚上十点后被提醒工作。")
	requireContains(t, normalList, pinnedFactID)
	requireContains(t, normalList, "用户喜欢手冲咖啡。")

	sensitiveList := requireRunText(t, "list-facts", "--db", dbPath, "--sensitivity-permission", "sensitive", "--format", "text")
	requireContains(t, sensitiveList, sensitiveFactID)
	requireContains(t, sensitiveList, "用户不希望晚上十点后被提醒工作。")
}

func seedCLIConsolidationDB(t *testing.T) string {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "memory.db")
	requireRunOK(t, "init-db", "--db", dbPath)
	sessionID := requireRunID(t, "start-session", "--db", dbPath, "--id", "session_seed", "--format", "id")
	if sessionID != "session_seed" {
		t.Fatalf("session id = %q, want session_seed", sessionID)
	}
	episodeID := requireRunID(t, "append-episode", "--db", dbPath, "--id", "ep_seed", "--session", sessionID, "--content", "我不喜欢早上八点开会。", "--format", "id")
	if episodeID != "ep_seed" {
		t.Fatalf("episode id = %q, want ep_seed", episodeID)
	}
	entityID := requireRunID(t, "ensure-entity", "--db", dbPath, "--id", "ent_user", "--name", "User", "--type", "user", "--format", "id")
	if entityID != "ent_user" {
		t.Fatalf("entity id = %q, want ent_user", entityID)
	}
	return dbPath
}

func requireRunOK(t *testing.T, args ...string) string {
	t.Helper()

	stdout, stderr, code := runCLI(args...)
	if code != 0 {
		t.Fatalf("run %v exit code = %d, want 0; stdout=%q stderr=%q", args, code, stdout, stderr)
	}
	return stdout
}

func requireRunID(t *testing.T, args ...string) string {
	t.Helper()

	out := strings.TrimSpace(requireRunOK(t, args...))
	if out == "" {
		t.Fatalf("run %v printed empty id", args)
	}
	if strings.Contains(out, "\n") {
		t.Fatalf("run %v printed multi-line id output: %q", args, out)
	}
	return out
}

func requireRunText(t *testing.T, args ...string) string {
	t.Helper()
	return requireRunOK(t, args...)
}

func runCLI(args ...string) (string, string, int) {
	var stdout, stderr bytes.Buffer
	code := run(args, &stdout, &stderr)
	return stdout.String(), stderr.String(), code
}

func requireContains(t *testing.T, value string, want string) {
	t.Helper()
	if !strings.Contains(value, want) {
		t.Fatalf("output does not contain %q:\n%s", want, value)
	}
}

func requireNotContains(t *testing.T, value string, unwanted string) {
	t.Helper()
	if strings.Contains(value, unwanted) {
		t.Fatalf("output contains %q:\n%s", unwanted, value)
	}
}
