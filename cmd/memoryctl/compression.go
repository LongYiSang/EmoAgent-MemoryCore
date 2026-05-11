package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/longyisang/emoagent-memorycore/pkg/memorycore"
)

type compressionApplyFile struct {
	PersonaID     string                     `json:"persona_id"`
	SourceFactIDs []string                   `json:"source_fact_ids"`
	Narrative     *compressionNarrativeInput `json:"narrative"`
	Insights      []compressionInsightInput  `json:"insights"`
	Now           string                     `json:"now"`
	DryRun        bool                       `json:"dry_run"`
}

type compressionNarrativeInput struct {
	ID               string   `json:"id"`
	Scope            string   `json:"scope"`
	ScopeRef         string   `json:"scope_ref"`
	Summary          string   `json:"summary"`
	EmotionalTone    string   `json:"emotional_tone"`
	ValenceAvg       *float64 `json:"valence_avg"`
	ArousalAvg       *float64 `json:"arousal_avg"`
	Importance       float64  `json:"importance"`
	ValidFrom        string   `json:"valid_from"`
	ValidTo          string   `json:"valid_to"`
	SensitivityLevel string   `json:"sensitivity_level"`
}

type compressionInsightInput struct {
	ID               string  `json:"id"`
	InsightType      string  `json:"insight_type"`
	Content          string  `json:"content"`
	Confidence       float64 `json:"confidence"`
	Importance       float64 `json:"importance"`
	Valence          float64 `json:"valence"`
	Arousal          float64 `json:"arousal"`
	SensitivityLevel string  `json:"sensitivity_level"`
}

func runCompressionApply(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := newFlagSet("compression-apply", stderr)
	var opts commonOptions
	var requestPath string
	var now string
	var dryRun bool
	addCommonFlags(fs, &opts, formatText)
	fs.StringVar(&requestPath, "request", "", "compression request JSON file path")
	fs.StringVar(&now, "now", "", "RFC3339 now; overrides request now")
	fs.BoolVar(&dryRun, "dry-run", false, "preview compression changes without mutating")
	if !parseFlags(fs, args) {
		return 2
	}
	if !requireDB(stderr, fs, opts.DBPath) {
		return 2
	}
	if strings.TrimSpace(requestPath) == "" {
		return usageError(stderr, fs, "--request is required")
	}
	if err := validateFormat(opts.Format, formatText, formatJSON); err != nil {
		return usageError(stderr, fs, err.Error())
	}

	data, err := readInputFile(requestPath)
	if err != nil {
		return runtimeError(stderr, "read request: %v", err)
	}
	var input compressionApplyFile
	if err := json.Unmarshal(data, &input); err != nil {
		return usageError(stderr, fs, "decode request: %v", err)
	}
	nowValue := input.Now
	if strings.TrimSpace(now) != "" {
		nowValue = now
	}
	parsedNow, err := parseOptionalTime(nowValue, "--now")
	if err != nil {
		return usageError(stderr, fs, err.Error())
	}
	narrative, err := compressionNarrativeRequest(input.Narrative)
	if err != nil {
		return usageError(stderr, fs, err.Error())
	}

	ctx := context.Background()
	svc, err := openService(ctx, opts)
	if err != nil {
		return runtimeError(stderr, "open memorycore: %v", err)
	}
	defer svc.Close()

	result, err := svc.ApplyCompression(ctx, memorycore.ApplyCompressionRequest{
		PersonaID:     compressionPersonaID(input.PersonaID, opts.PersonaID),
		SourceFactIDs: input.SourceFactIDs,
		Narrative:     narrative,
		Insights:      compressionInsightsRequest(input.Insights),
		Now:           parsedNow,
		DryRun:        input.DryRun || dryRun,
	})
	if err != nil {
		return runtimeError(stderr, "compression apply: %v", err)
	}
	if opts.Format == formatJSON {
		return writeJSON(stdout, result, opts.Pretty)
	}
	fmt.Fprintf(stdout, "narrative_id=%s\n", result.NarrativeID)
	fmt.Fprintf(stdout, "insight_ids=%s\n", strings.Join(result.InsightIDs, ","))
	fmt.Fprintf(stdout, "source_facts_consolidated=%d\n", result.SourceFactsConsolidated)
	fmt.Fprintf(stdout, "derived_links=%d\n", len(result.DerivedLinkIDs))
	fmt.Fprintf(stdout, "search_documents_synced=%d\n", result.SearchDocumentsSynced)
	fmt.Fprintf(stdout, "mirror_updates_enqueued=%d\n", result.MirrorUpdatesEnqueued)
	return 0
}

func compressionPersonaID(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func compressionNarrativeRequest(input *compressionNarrativeInput) (*memorycore.NarrativeDraft, error) {
	if input == nil {
		return nil, nil
	}
	validFrom, err := parseOptionalTimePtr(input.ValidFrom, "narrative.valid_from")
	if err != nil {
		return nil, err
	}
	validTo, err := parseOptionalTimePtr(input.ValidTo, "narrative.valid_to")
	if err != nil {
		return nil, err
	}
	return &memorycore.NarrativeDraft{
		ID:               input.ID,
		Scope:            input.Scope,
		ScopeRef:         input.ScopeRef,
		Summary:          input.Summary,
		EmotionalTone:    input.EmotionalTone,
		ValenceAvg:       input.ValenceAvg,
		ArousalAvg:       input.ArousalAvg,
		Importance:       input.Importance,
		ValidFrom:        validFrom,
		ValidTo:          validTo,
		SensitivityLevel: input.SensitivityLevel,
	}, nil
}

func compressionInsightsRequest(inputs []compressionInsightInput) []memorycore.InsightDraft {
	if len(inputs) == 0 {
		return nil
	}
	result := make([]memorycore.InsightDraft, 0, len(inputs))
	for _, input := range inputs {
		result = append(result, memorycore.InsightDraft{
			ID:               input.ID,
			InsightType:      input.InsightType,
			Content:          input.Content,
			Confidence:       input.Confidence,
			Importance:       input.Importance,
			Valence:          input.Valence,
			Arousal:          input.Arousal,
			SensitivityLevel: input.SensitivityLevel,
		})
	}
	return result
}
