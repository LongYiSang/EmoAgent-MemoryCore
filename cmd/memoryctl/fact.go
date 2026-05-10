package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/longyisang/emoagent-memorycore/pkg/memorycore"
)

type consolidateJSONInput struct {
	PersonaID string  `json:"persona_id"`
	SessionID *string `json:"session_id"`
	Trigger   string  `json:"trigger"`
	Candidate struct {
		SubjectEntityID  string    `json:"subject_entity_id"`
		Predicate        string    `json:"predicate"`
		ObjectEntityID   *string   `json:"object_entity_id"`
		ObjectLiteral    *string   `json:"object_literal"`
		ContentSummary   string    `json:"content_summary"`
		FactType         string    `json:"fact_type"`
		ValidFrom        *jsonTime `json:"valid_from"`
		ValidTo          *jsonTime `json:"valid_to"`
		Confidence       string    `json:"confidence"`
		ConfidenceScore  float64   `json:"confidence_score"`
		Importance       float64   `json:"importance"`
		Valence          float64   `json:"valence"`
		Arousal          float64   `json:"arousal"`
		Sensitivity      string    `json:"sensitivity"`
		SourceEpisodeIDs []string  `json:"source_episode_ids"`
		Pinned           bool      `json:"pinned"`
		UserRequested    bool      `json:"user_requested"`
	} `json:"candidate"`
	Policy struct {
		Action                      string `json:"action"`
		Approved                    bool   `json:"approved"`
		AllowManualPinWithoutSource bool   `json:"allow_manual_pin_without_source"`
	} `json:"policy"`
}

type jsonTime struct {
	value string
}

func (t *jsonTime) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		return nil
	}
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	t.value = value
	return nil
}

func runConsolidateFact(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := newFlagSet("consolidate-fact", stderr)
	var opts commonOptions
	var inputPath, subject, predicate, summary, factType, objectLiteral, objectEntity, sessionID, trigger string
	var validFrom, validTo, confidence, sensitivity, policyAction string
	var confidenceScore, importance, valence, arousal float64
	var pinned, userRequested, approved, allowManualPinWithoutSource bool
	var sources stringList
	addCommonFlags(fs, &opts, formatText)
	fs.StringVar(&inputPath, "input", "", "JSON input path, or - for stdin")
	fs.StringVar(&subject, "subject", "", "subject entity id")
	fs.StringVar(&predicate, "predicate", "", "predicate")
	fs.StringVar(&summary, "summary", "", "content summary")
	fs.StringVar(&factType, "fact-type", "", "fact type")
	fs.StringVar(&objectLiteral, "object-literal", "", "object literal")
	fs.StringVar(&objectEntity, "object-entity", "", "object entity id")
	fs.StringVar(&sessionID, "session", "", "session id")
	fs.StringVar(&trigger, "trigger", memorycore.ConsolidationTriggerManual, "consolidation trigger")
	fs.StringVar(&validFrom, "valid-from", "", "RFC3339 valid from")
	fs.StringVar(&validTo, "valid-to", "", "RFC3339 valid to")
	fs.StringVar(&confidence, "confidence", memorycore.ConfidenceExplicit, "confidence")
	fs.Float64Var(&confidenceScore, "confidence-score", 0.9, "confidence score")
	fs.Float64Var(&importance, "importance", 0.5, "importance")
	fs.Float64Var(&valence, "valence", 0, "valence")
	fs.Float64Var(&arousal, "arousal", 0, "arousal")
	fs.StringVar(&sensitivity, "sensitivity", memorycore.SensitivityNormal, "sensitivity")
	fs.Var(&sources, "source-episode", "source episode id; repeatable")
	fs.BoolVar(&pinned, "pinned", false, "pinned")
	fs.BoolVar(&userRequested, "user-requested", false, "user requested")
	fs.StringVar(&policyAction, "policy-action", "", "policy action")
	fs.BoolVar(&approved, "approved", true, "approved")
	fs.BoolVar(&allowManualPinWithoutSource, "allow-manual-pin-without-source", false, "allow manual pin without source")
	if !parseFlags(fs, args) {
		return 2
	}
	if !requireDB(stderr, fs, opts.DBPath) {
		return 2
	}
	if err := validateFormat(opts.Format, formatText, formatJSON, formatID); err != nil {
		return usageError(stderr, fs, err.Error())
	}

	var req memorycore.ConsolidateCandidateRequest
	var err error
	if inputPath != "" {
		req, err = readConsolidateJSON(inputPath, opts.PersonaID)
		if err != nil {
			return usageError(stderr, fs, "input json: %v", err)
		}
	} else {
		if err := validateConsolidateFlagShape(subject, predicate, summary, factType, objectLiteral, objectEntity, trigger, confidence, sensitivity, confidenceScore, importance, valence, arousal); err != nil {
			return usageError(stderr, fs, err.Error())
		}
		from, err := parseOptionalTimePtr(validFrom, "--valid-from")
		if err != nil {
			return usageError(stderr, fs, err.Error())
		}
		to, err := parseOptionalTimePtr(validTo, "--valid-to")
		if err != nil {
			return usageError(stderr, fs, err.Error())
		}
		req = memorycore.ConsolidateCandidateRequest{
			PersonaID: opts.PersonaID,
			SessionID: stringPtr(sessionID),
			Trigger:   trigger,
			Candidate: memorycore.ManualFactCandidate{
				SubjectEntityID:  subject,
				Predicate:        predicate,
				ObjectEntityID:   stringPtr(objectEntity),
				ObjectLiteral:    stringPtr(objectLiteral),
				ContentSummary:   summary,
				FactType:         factType,
				ValidFrom:        from,
				ValidTo:          to,
				Confidence:       confidence,
				ConfidenceScore:  confidenceScore,
				Importance:       importance,
				Valence:          valence,
				Arousal:          arousal,
				Sensitivity:      sensitivity,
				SourceEpisodeIDs: []string(sources),
				Pinned:           pinned,
				UserRequested:    userRequested,
			},
			Policy: memorycore.ConsolidationPolicy{
				Action:                      policyAction,
				Approved:                    approved,
				AllowManualPinWithoutSource: allowManualPinWithoutSource,
			},
		}
	}

	ctx := context.Background()
	svc, err := openService(ctx, opts)
	if err != nil {
		return runtimeError(stderr, "open memorycore: %v", err)
	}
	defer svc.Close()

	result, err := svc.ConsolidateCandidate(ctx, req)
	if err != nil {
		return runtimeError(stderr, "consolidate fact: %v", err)
	}
	if result.Status == memorycore.ConsolidationStatusRejected || result.Status == memorycore.ConsolidationStatusNeedsReview || result.Fact == nil {
		return outputRejectedConsolidation(stdout, stderr, result, opts)
	}
	switch opts.Format {
	case formatID:
		return idOutput(stdout, result.Fact.ID)
	case formatJSON:
		return writeJSON(stdout, result, opts.Pretty)
	default:
		fmt.Fprintf(stdout, "status=%s\n", result.Status)
		fmt.Fprintf(stdout, "action=%s\n", result.Action)
		fmt.Fprintf(stdout, "fact_id=%s\n", result.Fact.ID)
		return 0
	}
}

func validateConsolidateFlagShape(subject string, predicate string, summary string, factType string, objectLiteral string, objectEntity string, trigger string, confidence string, sensitivity string, confidenceScore float64, importance float64, valence float64, arousal float64) error {
	if subject == "" {
		return fmt.Errorf("--subject is required")
	}
	if predicate == "" {
		return fmt.Errorf("--predicate is required")
	}
	if summary == "" {
		return fmt.Errorf("--summary is required")
	}
	if (objectLiteral == "") == (objectEntity == "") {
		return fmt.Errorf("exactly one of --object-literal or --object-entity must be set")
	}
	if factType != "" {
		if err := validateOneOf("--fact-type", factType, memorycore.FactTypeCoreIdentity, memorycore.FactTypeSignificantEvent, memorycore.FactTypeStablePreference, memorycore.FactTypeRelationalState, memorycore.FactTypeCommitment, memorycore.FactTypeTransientContext, memorycore.FactTypeTaskRelevantContext); err != nil {
			return err
		}
	}
	if err := validateOneOf("--trigger", trigger, memorycore.ConsolidationTriggerManual, memorycore.ConsolidationTriggerWorkCandidate, memorycore.ConsolidationTriggerAgentAffect); err != nil {
		return err
	}
	if err := validateOneOf("--confidence", confidence, memorycore.ConfidenceExplicit, memorycore.ConfidenceInferred, memorycore.ConfidenceAmbiguous); err != nil {
		return err
	}
	if err := validateOneOf("--sensitivity", sensitivity, memorycore.SensitivityNormal, memorycore.SensitivitySensitive, memorycore.SensitivityHighlySensitive); err != nil {
		return err
	}
	for name, value := range map[string]float64{
		"--confidence-score": confidenceScore,
		"--importance":       importance,
		"--arousal":          arousal,
	} {
		if err := validateFloatRange(name, value, 0, 1); err != nil {
			return err
		}
	}
	return validateFloatRange("--valence", valence, -1, 1)
}

func readConsolidateJSON(inputPath string, defaultPersona string) (memorycore.ConsolidateCandidateRequest, error) {
	data, err := readInputFile(inputPath)
	if err != nil {
		return memorycore.ConsolidateCandidateRequest{}, err
	}
	var input consolidateJSONInput
	if err := json.Unmarshal(data, &input); err != nil {
		return memorycore.ConsolidateCandidateRequest{}, err
	}
	personaID := input.PersonaID
	if personaID == "" {
		personaID = defaultPersona
	}
	validFrom, err := jsonTimePtr(input.Candidate.ValidFrom, "candidate.valid_from")
	if err != nil {
		return memorycore.ConsolidateCandidateRequest{}, err
	}
	validTo, err := jsonTimePtr(input.Candidate.ValidTo, "candidate.valid_to")
	if err != nil {
		return memorycore.ConsolidateCandidateRequest{}, err
	}
	return memorycore.ConsolidateCandidateRequest{
		PersonaID: personaID,
		SessionID: input.SessionID,
		Trigger:   input.Trigger,
		Candidate: memorycore.ManualFactCandidate{
			SubjectEntityID:  input.Candidate.SubjectEntityID,
			Predicate:        input.Candidate.Predicate,
			ObjectEntityID:   input.Candidate.ObjectEntityID,
			ObjectLiteral:    input.Candidate.ObjectLiteral,
			ContentSummary:   input.Candidate.ContentSummary,
			FactType:         input.Candidate.FactType,
			ValidFrom:        validFrom,
			ValidTo:          validTo,
			Confidence:       input.Candidate.Confidence,
			ConfidenceScore:  input.Candidate.ConfidenceScore,
			Importance:       input.Candidate.Importance,
			Valence:          input.Candidate.Valence,
			Arousal:          input.Candidate.Arousal,
			Sensitivity:      input.Candidate.Sensitivity,
			SourceEpisodeIDs: input.Candidate.SourceEpisodeIDs,
			Pinned:           input.Candidate.Pinned,
			UserRequested:    input.Candidate.UserRequested,
		},
		Policy: memorycore.ConsolidationPolicy{
			Action:                      input.Policy.Action,
			Approved:                    input.Policy.Approved,
			AllowManualPinWithoutSource: input.Policy.AllowManualPinWithoutSource,
		},
	}, nil
}

func jsonTimePtr(value *jsonTime, name string) (*time.Time, error) {
	if value == nil || value.value == "" {
		return nil, nil
	}
	return parseOptionalTimePtr(value.value, name)
}

func outputRejectedConsolidation(stdout io.Writer, stderr io.Writer, result *memorycore.ConsolidationResult, opts commonOptions) int {
	reason := result.RejectedReason
	label := "rejected"
	if result.Status == memorycore.ConsolidationStatusNeedsReview {
		reason = result.NeedsReviewReason
		label = "needs_review"
	}
	if reason == "" {
		reason = "no fact was produced"
	}
	switch opts.Format {
	case formatJSON:
		writeJSON(stdout, result, opts.Pretty)
	default:
		fmt.Fprintf(stderr, "%s: %s\n", label, reason)
	}
	return 1
}
