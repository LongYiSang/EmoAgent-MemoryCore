package main

import "testing"

func TestRunCurationRunDryRunAndApply(t *testing.T) {
	dbPath := seedCLIConsolidationDB(t)
	first := requireRunID(t,
		"consolidate-fact",
		"--db", dbPath,
		"--subject", "ent_user",
		"--predicate", "likes",
		"--object-literal", "无糖饮料",
		"--summary", "用户喜欢喝无糖饮料。",
		"--source-episode", "ep_seed",
		"--format", "id",
	)
	second := requireRunID(t,
		"consolidate-fact",
		"--db", dbPath,
		"--subject", "ent_user",
		"--predicate", "likes",
		"--object-literal", "不甜的没有糖的饮料",
		"--summary", "用户喜欢喝不甜的没有糖的饮料。",
		"--source-episode", "ep_seed",
		"--format", "id",
	)

	dryRun := requireRunOK(t, "curation-run", "--db", dbPath, "--mode", "dry-run", "--provider-id", "mock", "--format", "json")
	requireContains(t, dryRun, `"mode":"dry_run"`)
	requireContains(t, dryRun, `"applied_group_count":0`)
	requireContains(t, dryRun, `"group_count":1`)
	requireContains(t, requireRunOK(t, "get-node", "--db", dbPath, "--node-type", "fact", "--id", second), "searchable=true")

	applied := requireRunOK(t, "curation-run", "--db", dbPath, "--mode", "apply", "--provider-id", "mock", "--model", "memory-curator", "--update-checkpoint", "--format", "json")
	requireContains(t, applied, `"status":"succeeded"`)
	requireContains(t, applied, `"applied_group_count":1`)
	firstNode := requireRunOK(t, "get-node", "--db", dbPath, "--node-type", "fact", "--id", first, "--all")
	secondNode := requireRunOK(t, "get-node", "--db", dbPath, "--node-type", "fact", "--id", second, "--all")
	combinedNodes := firstNode + secondNode
	requireContains(t, combinedNodes, "用户在饮料上偏好无糖、口味不甜。")
	requireContains(t, combinedNodes, "lifecycle_status=consolidated")
}
