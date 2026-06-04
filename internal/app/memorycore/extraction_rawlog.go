package memorycore

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const rawLogSchemaVersion = "memory_extraction_raw_log.v0.1"

type rawLogTrace struct {
	StartedAt time.Time
	Request   ExtractionRequest
	LLM       rawLogLLMTraceSet
}

type rawLogLLMTraceSet struct {
	PreFilter       *rawLogLLMCall `json:"prefilter,omitempty"`
	PreFilterRepair *rawLogLLMCall `json:"prefilter_repair,omitempty"`
	Extraction      *rawLogLLMCall `json:"extraction,omitempty"`
	Repair          *rawLogLLMCall `json:"repair,omitempty"`
}

type rawLogLLMCall struct {
	Request    *ExtractionLLMRequest  `json:"request,omitempty"`
	Response   *ExtractionLLMResponse `json:"response,omitempty"`
	ParseError string                 `json:"parse_error,omitempty"`
}

type rawLogAudit struct {
	Fingerprint          string `json:"fingerprint,omitempty"`
	PromptHash           string `json:"prompt_hash,omitempty"`
	ResponseHash         string `json:"response_hash,omitempty"`
	RepairedResponseHash string `json:"repaired_response_hash,omitempty"`
	PreFilterHash        string `json:"prefilter_hash,omitempty"`
}

type rawLogTimings struct {
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at"`
	DurationMS int64     `json:"duration_ms"`
}

func newRawLogTrace(start time.Time, runReq ExtractionRunRequest) *rawLogTrace {
	if !runReq.RawLog.Enabled {
		return nil
	}
	return &rawLogTrace{StartedAt: start, Request: runReq.Request}
}

func (t *rawLogTrace) recordPreFilterRequest(req ExtractionLLMRequest) {
	if t == nil {
		return
	}
	t.LLM.PreFilter = ensureRawLogCall(t.LLM.PreFilter)
	t.LLM.PreFilter.Request = &req
}

func (t *rawLogTrace) recordPreFilterResponse(resp ExtractionLLMResponse) {
	if t == nil {
		return
	}
	t.LLM.PreFilter = ensureRawLogCall(t.LLM.PreFilter)
	t.LLM.PreFilter.Response = &resp
}

func (t *rawLogTrace) recordPreFilterParseError(err error) {
	if t == nil || err == nil {
		return
	}
	t.LLM.PreFilter = ensureRawLogCall(t.LLM.PreFilter)
	t.LLM.PreFilter.ParseError = err.Error()
}

func (t *rawLogTrace) recordPreFilterRepairRequest(req ExtractionLLMRequest) {
	if t == nil {
		return
	}
	t.LLM.PreFilterRepair = ensureRawLogCall(t.LLM.PreFilterRepair)
	t.LLM.PreFilterRepair.Request = &req
}

func (t *rawLogTrace) recordPreFilterRepairResponse(resp ExtractionLLMResponse) {
	if t == nil {
		return
	}
	t.LLM.PreFilterRepair = ensureRawLogCall(t.LLM.PreFilterRepair)
	t.LLM.PreFilterRepair.Response = &resp
}

func (t *rawLogTrace) recordPreFilterRepairParseError(err error) {
	if t == nil || err == nil {
		return
	}
	t.LLM.PreFilterRepair = ensureRawLogCall(t.LLM.PreFilterRepair)
	t.LLM.PreFilterRepair.ParseError = err.Error()
}

func (t *rawLogTrace) recordExtractionRequest(req ExtractionLLMRequest) {
	if t == nil {
		return
	}
	t.LLM.Extraction = ensureRawLogCall(t.LLM.Extraction)
	t.LLM.Extraction.Request = &req
}

func (t *rawLogTrace) recordExtractionResponse(resp ExtractionLLMResponse) {
	if t == nil {
		return
	}
	t.LLM.Extraction = ensureRawLogCall(t.LLM.Extraction)
	t.LLM.Extraction.Response = &resp
}

func (t *rawLogTrace) recordExtractionParseError(err error) {
	if t == nil || err == nil {
		return
	}
	t.LLM.Extraction = ensureRawLogCall(t.LLM.Extraction)
	t.LLM.Extraction.ParseError = err.Error()
}

func (t *rawLogTrace) recordRepairRequest(req ExtractionLLMRequest) {
	if t == nil {
		return
	}
	t.LLM.Repair = ensureRawLogCall(t.LLM.Repair)
	t.LLM.Repair.Request = &req
}

func (t *rawLogTrace) recordRepairResponse(resp ExtractionLLMResponse) {
	if t == nil {
		return
	}
	t.LLM.Repair = ensureRawLogCall(t.LLM.Repair)
	t.LLM.Repair.Response = &resp
}

func (t *rawLogTrace) recordRepairParseError(err error) {
	if t == nil || err == nil {
		return
	}
	t.LLM.Repair = ensureRawLogCall(t.LLM.Repair)
	t.LLM.Repair.ParseError = err.Error()
}

func ensureRawLogCall(call *rawLogLLMCall) *rawLogLLMCall {
	if call != nil {
		return call
	}
	return &rawLogLLMCall{}
}

func writeRawLog(dir string, result ExtractionRunResult, trace *rawLogTrace, audit rawLogAudit) error {
	if trace == nil {
		return nil
	}
	if strings.TrimSpace(dir) == "" {
		return fmt.Errorf("raw log directory is required")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	finishedAt := time.Now().In(trace.StartedAt.Location())
	artifact := struct {
		SchemaVersion string                  `json:"schema_version"`
		Request       ExtractionRequest       `json:"request"`
		LLM           rawLogLLMTraceSet       `json:"llm"`
		GateResult    *ExtractionGateResult   `json:"gate_result,omitempty"`
		DryRunResult  *ExtractionDryRunResult `json:"dry_run_result,omitempty"`
		ApplyResult   *ExtractionApplyResult  `json:"apply_result,omitempty"`
		Result        ExtractionRunResult     `json:"result"`
		Audit         rawLogAudit             `json:"audit"`
		Timings       rawLogTimings           `json:"timings"`
	}{
		SchemaVersion: rawLogSchemaVersion,
		Request:       trace.Request,
		LLM:           trace.LLM,
		GateResult:    result.GateResult,
		DryRunResult:  result.DryRunResult,
		ApplyResult:   result.ApplyResult,
		Result:        result,
		Audit:         audit,
		Timings: rawLogTimings{
			StartedAt:  trace.StartedAt,
			FinishedAt: finishedAt,
			DurationMS: result.DurationMS,
		},
	}
	data, err := json.MarshalIndent(artifact, "", "  ")
	if err != nil {
		return err
	}
	base := rawLogFilename(trace.StartedAt, result)
	tmp, err := os.CreateTemp(dir, "."+base+".*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, filepath.Join(dir, base))
}

func rawLogFilename(start time.Time, result ExtractionRunResult) string {
	fingerprint := result.Fingerprint
	if len(fingerprint) > 8 {
		fingerprint = fingerprint[:8]
	}
	if fingerprint == "" {
		fingerprint = "nofp"
	}
	return fmt.Sprintf("%s_%s_%s_%s.json",
		start.Format("20060102T150405.000000000-0700"),
		sanitizeRawLogFilenamePart(result.RequestID),
		sanitizeRawLogFilenamePart(string(result.Status)),
		sanitizeRawLogFilenamePart(fingerprint),
	)
}

func sanitizeRawLogFilenamePart(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unknown"
	}
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}
