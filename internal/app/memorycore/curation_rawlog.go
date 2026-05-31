package memorycore

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	memsqlite "github.com/longyisang/emoagent-memorycore/internal/store/sqlite"
)

const curationRawLogSchemaVersion = "memory_curation_raw_log.v0.1"

type curationRawLogTrace struct {
	StartedAt          time.Time
	Request            RunCurationRequest
	CandidateRetrieval *curationRawLogCandidateRetrieval
	Groups             []curationRawLogGroup
}

type curationRawLogCandidateRetrieval struct {
	Mode                string                         `json:"mode"`
	CandidateLimit      int                            `json:"candidate_limit_per_fact"`
	MirrorMinSimilarity float64                        `json:"mirror_min_similarity"`
	Deltas              []curationRawLogCandidateDelta `json:"deltas"`
}

type curationRawLogCandidateDelta struct {
	DeltaFactID           string                          `json:"delta_fact_id"`
	Strategy              string                          `json:"strategy"`
	MirrorStatus          string                          `json:"mirror_status,omitempty"`
	MirrorDegraded        bool                            `json:"mirror_degraded,omitempty"`
	MirrorFallbackReason  string                          `json:"mirror_fallback_reason,omitempty"`
	FallbackSQLUsed       bool                            `json:"fallback_sql_used,omitempty"`
	FallbackReason        string                          `json:"fallback_reason,omitempty"`
	MirrorCandidates      []curationRawLogMirrorCandidate `json:"mirror_candidates,omitempty"`
	SQLCandidateFactIDs   []string                        `json:"sql_candidate_fact_ids,omitempty"`
	FinalCandidateFactIDs []string                        `json:"final_candidate_fact_ids,omitempty"`
}

type curationRawLogMirrorCandidate struct {
	NodeType            string  `json:"node_type"`
	NodeID              string  `json:"node_id"`
	Source              string  `json:"source,omitempty"`
	Similarity          float64 `json:"similarity"`
	MatchClass          string  `json:"match_class,omitempty"`
	MatchReason         string  `json:"match_reason,omitempty"`
	MergeHint           string  `json:"merge_hint,omitempty"`
	MappedFactID        string  `json:"mapped_fact_id,omitempty"`
	AuthorityDropReason string  `json:"authority_drop_reason,omitempty"`
}

type curationRawLogGroup struct {
	GroupID    string                    `json:"group_id"`
	Facts      []curationRawLogFact      `json:"facts"`
	Payload    curationLLMRequestPayload `json:"payload"`
	LLM        rawLogLLMCall             `json:"llm"`
	Decision   *curationRawLogDecision   `json:"decision,omitempty"`
	ParseError string                    `json:"parse_error,omitempty"`
}

type curationRawLogFact struct {
	FactID           string     `json:"fact_id"`
	Role             string     `json:"role"`
	LatestEvidenceAt *time.Time `json:"latest_evidence_at,omitempty"`
}

type curationRawLogDecision struct {
	Decision                 string   `json:"decision"`
	SemanticRelation         string   `json:"semantic_relation"`
	AnswerGain               string   `json:"answer_gain"`
	Confidence               float64  `json:"confidence"`
	CanonicalFactID          string   `json:"canonical_fact_id,omitempty"`
	SourceFactIDs            []string `json:"source_fact_ids,omitempty"`
	MergedContentSummary     string   `json:"merged_content_summary,omitempty"`
	CanonicalSubjectEntityID string   `json:"canonical_subject_entity_id,omitempty"`
	CanonicalPredicate       string   `json:"canonical_predicate,omitempty"`
	CanonicalObjectLiteral   string   `json:"canonical_object_literal,omitempty"`
	CanonicalObjectEntityID  string   `json:"canonical_object_entity_id,omitempty"`
	CanonicalFactType        string   `json:"canonical_fact_type,omitempty"`
	ReasonCodes              []string `json:"reason_codes,omitempty"`
	LLMResponseHash          string   `json:"llm_response_hash,omitempty"`
	RequiresReview           bool     `json:"requires_review,omitempty"`
}

func newCurationRawLogTrace(start time.Time, req RunCurationRequest, opts CurationRawLogOptions) *curationRawLogTrace {
	if !opts.Enabled {
		return nil
	}
	return &curationRawLogTrace{StartedAt: start.UTC(), Request: req}
}

func (t *curationRawLogTrace) recordCandidateRetrievalStart(opts CurationCandidateRetrievalOptions, candidateLimit int) {
	if t == nil {
		return
	}
	t.CandidateRetrieval = &curationRawLogCandidateRetrieval{
		Mode:                opts.Mode,
		CandidateLimit:      candidateLimit,
		MirrorMinSimilarity: opts.MirrorMinSimilarity,
	}
}

func (t *curationRawLogTrace) recordCandidateRetrievalDelta(item curationRawLogCandidateDelta) {
	if t == nil || t.CandidateRetrieval == nil {
		return
	}
	t.CandidateRetrieval.Deltas = append(t.CandidateRetrieval.Deltas, item)
}

func validateCurationRawLogOptions(opts CurationRawLogOptions) error {
	if !opts.Enabled {
		return nil
	}
	dir := strings.TrimSpace(opts.Directory)
	if dir == "" {
		return extractionServiceError("raw_log_directory_required", "curation raw log directory is required")
	}
	info, err := os.Stat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return extractionServiceError("raw_log_directory_missing", "curation raw log directory does not exist")
		}
		return extractionServiceError("raw_log_directory_invalid", "could not access curation raw log directory")
	}
	if !info.IsDir() {
		return extractionServiceError("raw_log_directory_invalid", "curation raw log path is not a directory")
	}
	return nil
}

func (t *curationRawLogTrace) recordGroup(group memsqlite.CurationCandidateGroup, payload curationLLMRequestPayload, req ExtractionLLMRequest, resp ExtractionLLMResponse, decision memsqlite.CurationDecision, parseErr error) {
	if t == nil {
		return
	}
	entry := curationRawLogGroup{
		GroupID: group.ID,
		Facts:   curationRawLogFacts(group.Facts),
		Payload: payload,
		LLM: rawLogLLMCall{
			Request:  &req,
			Response: &resp,
		},
	}
	if parseErr != nil {
		entry.ParseError = parseErr.Error()
	} else {
		entry.Decision = curationRawLogDecisionFromStore(decision)
	}
	t.Groups = append(t.Groups, entry)
}

func curationRawLogFacts(facts []memsqlite.CurationGroupFact) []curationRawLogFact {
	out := make([]curationRawLogFact, 0, len(facts))
	for _, fact := range facts {
		out = append(out, curationRawLogFact{
			FactID:           fact.FactID,
			Role:             fact.Role,
			LatestEvidenceAt: fact.LatestEvidenceAt,
		})
	}
	return out
}

func curationRawLogDecisionFromStore(decision memsqlite.CurationDecision) *curationRawLogDecision {
	return &curationRawLogDecision{
		Decision:                 decision.Decision,
		SemanticRelation:         decision.SemanticRelation,
		AnswerGain:               decision.AnswerGain,
		Confidence:               decision.Confidence,
		CanonicalFactID:          decision.CanonicalFactID,
		SourceFactIDs:            append([]string(nil), decision.SourceFactIDs...),
		MergedContentSummary:     decision.MergedContentSummary,
		CanonicalSubjectEntityID: decision.CanonicalSubjectEntityID,
		CanonicalPredicate:       decision.CanonicalPredicate,
		CanonicalObjectLiteral:   decision.CanonicalObjectLiteral,
		CanonicalObjectEntityID:  decision.CanonicalObjectEntityID,
		CanonicalFactType:        decision.CanonicalFactType,
		ReasonCodes:              append([]string(nil), decision.ReasonCodes...),
		LLMResponseHash:          decision.LLMResponseHash,
		RequiresReview:           decision.RequiresReview,
	}
}

func writeCurationRawLog(dir string, result *RunCurationResult, trace *curationRawLogTrace) error {
	if trace == nil {
		return nil
	}
	if strings.TrimSpace(dir) == "" {
		return fmt.Errorf("curation raw log directory is required")
	}
	finishedAt := time.Now().UTC()
	artifact := struct {
		SchemaVersion      string                            `json:"schema_version"`
		Request            RunCurationRequest                `json:"request"`
		CandidateRetrieval *curationRawLogCandidateRetrieval `json:"candidate_retrieval,omitempty"`
		Groups             []curationRawLogGroup             `json:"groups"`
		Result             *RunCurationResult                `json:"result,omitempty"`
		Timings            rawLogTimings                     `json:"timings"`
	}{
		SchemaVersion:      curationRawLogSchemaVersion,
		Request:            trace.Request,
		CandidateRetrieval: trace.CandidateRetrieval,
		Groups:             trace.Groups,
		Result:             result,
		Timings: rawLogTimings{
			StartedAt:  trace.StartedAt,
			FinishedAt: finishedAt,
			DurationMS: finishedAt.Sub(trace.StartedAt).Milliseconds(),
		},
	}
	data, err := json.MarshalIndent(artifact, "", "  ")
	if err != nil {
		return err
	}
	base := curationRawLogFilename(trace.StartedAt, result)
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

func failedRunCurationResult(mode string, newFactCount int, groupCount int, groups []CurationGroupResult, trace *curationRawLogTrace) *RunCurationResult {
	llmGroupCount := 0
	if trace != nil {
		llmGroupCount = len(trace.Groups)
	}
	return &RunCurationResult{
		Status:        "failed",
		Mode:          mode,
		NewFactCount:  newFactCount,
		GroupCount:    groupCount,
		LLMGroupCount: llmGroupCount,
		ErrorCount:    1,
		Groups:        append([]CurationGroupResult(nil), groups...),
	}
}

func curationRawLogFilename(start time.Time, result *RunCurationResult) string {
	runID := "unknown"
	status := "unknown"
	if result != nil {
		runID = result.RunID
		status = result.Status
	}
	return fmt.Sprintf("%s_%s_%s.json",
		start.UTC().Format("20060102T150405.000000000Z"),
		sanitizeRawLogFilenamePart(runID),
		sanitizeRawLogFilenamePart(status),
	)
}
