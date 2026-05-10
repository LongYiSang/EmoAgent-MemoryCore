package eval

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadFixtureBytesValidatesRequiredCaseID(t *testing.T) {
	_, err := LoadFixtureBytes([]byte(`
steps: []
`))
	if err == nil {
		t.Fatal("LoadFixtureBytes err is nil, want missing case_id error")
	}
	if !strings.Contains(err.Error(), "case_id") {
		t.Fatalf("error = %q, want case_id", err.Error())
	}
}

func TestLoadFixtureBytesRejectsUnknownStepAction(t *testing.T) {
	_, err := LoadFixtureBytes([]byte(`
case_id: BAD_STEP
steps:
  - id: unknown
    action: teleport
`))
	if err == nil {
		t.Fatal("LoadFixtureBytes err is nil, want unknown step action error")
	}
	if !strings.Contains(err.Error(), "BAD_STEP") || !strings.Contains(err.Error(), "teleport") {
		t.Fatalf("error = %q, want case id and action", err.Error())
	}
}

func TestRunnerReportsBadReferenceWithCaseID(t *testing.T) {
	fixture, err := LoadFixtureBytes([]byte(`
case_id: BAD_REF
steps:
  - id: retrieve
    action: retrieve
    retrieve:
      query_text: coffee
assertions:
  - type: memory_not_contains
    step: retrieve
    node_id: $missing.fact_id
`))
	if err != nil {
		t.Fatalf("load fixture: %v", err)
	}

	report := NewRunner(RunnerOptions{TempDir: t.TempDir()}).Run(context.Background(), fixture)
	if !report.Failed() {
		t.Fatal("report passed, want bad reference failure")
	}
	if !strings.Contains(report.Error(), "BAD_REF") || !strings.Contains(report.Error(), "$missing.fact_id") {
		t.Fatalf("report error = %q, want case id and missing ref", report.Error())
	}
}

func TestEvalRunnerMirrorStubMatchesSchema(t *testing.T) {
	tempDir := t.TempDir()
	fixture, err := LoadFixtureBytes([]byte(`
case_id: MIRROR_STUB_SCHEMA
steps:
  - id: retrieve
    action: retrieve
    mirror_stub:
      index_mapped_node_id: fact_mirror_stub
      index_mapped_type: fact
    retrieve:
      query_text: coffee
`))
	if err != nil {
		t.Fatalf("load fixture: %v", err)
	}

	report := NewRunner(RunnerOptions{TempDir: tempDir}).Run(context.Background(), fixture)
	if report.Failed() {
		t.Fatalf("run fixture: %v", report.Err)
	}

	db, err := sql.Open("sqlite", filepath.Join(tempDir, "MIRROR_STUB_SCHEMA.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	var status string
	var triviumType string
	if err := db.QueryRow(`
SELECT index_status, typeof(trivium_node_id)
FROM memory_index_map
WHERE node_id = ?`, "fact_mirror_stub").Scan(&status, &triviumType); err != nil {
		t.Fatalf("query mirror stub: %v", err)
	}
	if status != "indexed" {
		t.Fatalf("index_status = %q, want indexed", status)
	}
	if triviumType != "integer" {
		t.Fatalf("typeof(trivium_node_id) = %q, want integer", triviumType)
	}
}

func TestAssertionFailureIncludesExpectedAndActual(t *testing.T) {
	err := AssertionFailure{
		CaseID:    "ASSERT_FORMAT",
		Assertion: "memory_contains",
		Expected:  "node fact_01 present",
		Actual:    "no memory items",
	}

	message := err.Error()
	for _, want := range []string{"ASSERT_FORMAT", "memory_contains", "expected=node fact_01 present", "actual=no memory items"} {
		if !strings.Contains(message, want) {
			t.Fatalf("error = %q, want %q", message, want)
		}
	}
}
