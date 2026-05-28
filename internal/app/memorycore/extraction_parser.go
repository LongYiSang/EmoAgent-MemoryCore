package memorycore

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

func ParseRequest(r io.Reader) (ExtractionRequest, error) {
	var req ExtractionRequest
	if err := strictDecode(r, &req); err != nil {
		return ExtractionRequest{}, err
	}
	if req.SchemaVersion != ExtractionRequestSchemaVersion {
		return ExtractionRequest{}, fmt.Errorf("schema_version must be %s", ExtractionRequestSchemaVersion)
	}
	if strings.TrimSpace(req.RequestID) == "" {
		return ExtractionRequest{}, fmt.Errorf("request_id is required")
	}
	if strings.TrimSpace(req.PersonaID) == "" {
		return ExtractionRequest{}, fmt.Errorf("persona_id is required")
	}
	if !validExtractionTrigger(req.Trigger) {
		return ExtractionRequest{}, fmt.Errorf("trigger is invalid")
	}
	if strings.TrimSpace(req.Timezone) == "" {
		return ExtractionRequest{}, fmt.Errorf("timezone is required")
	}
	return req, nil
}

func ParseResponse(r io.Reader) (ExtractionResponse, error) {
	resp, _, err := ParseResponseWithRepairReport(r)
	return resp, err
}

func ParseResponseWithRepairReport(r io.Reader) (ExtractionResponse, ContractRepairReport, error) {
	var resp ExtractionResponse
	data, err := io.ReadAll(r)
	if err != nil {
		return ExtractionResponse{}, ContractRepairReport{}, err
	}
	normalized, report, normalizeErr := NormalizeExtractionResponseContract(data)
	if normalizeErr != nil {
		normalized = data
	}
	if err := strictDecodeBytes(normalized, &resp); err != nil {
		return ExtractionResponse{}, ContractRepairReport{}, err
	}
	if resp.SchemaVersion != ExtractionResponseSchemaVersion {
		return ExtractionResponse{}, ContractRepairReport{}, fmt.Errorf("schema_version must be %s", ExtractionResponseSchemaVersion)
	}
	if strings.TrimSpace(resp.RequestID) == "" {
		return ExtractionResponse{}, ContractRepairReport{}, fmt.Errorf("request_id is required")
	}
	if strings.TrimSpace(resp.PersonaID) == "" {
		return ExtractionResponse{}, ContractRepairReport{}, fmt.Errorf("persona_id is required")
	}
	if !validExtractionTrigger(resp.Trigger) {
		return ExtractionResponse{}, ContractRepairReport{}, fmt.Errorf("trigger is invalid")
	}
	return resp, report, nil
}

func ParsePreFilterResponse(r io.Reader) (ExtractionPreFilterResponse, error) {
	var resp ExtractionPreFilterResponse
	if err := strictDecode(r, &resp); err != nil {
		return ExtractionPreFilterResponse{}, err
	}
	if !validPreFilterSchemaVersion(resp.SchemaVersion) {
		return ExtractionPreFilterResponse{}, fmt.Errorf("schema_version must be %s", ExtractionPreFilterSchemaVersion)
	}
	if strings.TrimSpace(resp.RequestID) == "" {
		return ExtractionPreFilterResponse{}, fmt.Errorf("request_id is required")
	}
	if strings.TrimSpace(resp.PersonaID) == "" {
		return ExtractionPreFilterResponse{}, fmt.Errorf("persona_id is required")
	}
	if !validExtractionTrigger(resp.Trigger) {
		return ExtractionPreFilterResponse{}, fmt.Errorf("trigger is invalid")
	}
	for _, episode := range resp.Episodes {
		if strings.TrimSpace(episode.EpisodeID) == "" {
			return ExtractionPreFilterResponse{}, fmt.Errorf("episode_id is required")
		}
		switch episode.RoutingHint {
		case "extract", "forget_manager", "pin_manager", "skip", "review", "route":
		default:
			return ExtractionPreFilterResponse{}, fmt.Errorf("routing_hint is invalid")
		}
	}
	return resp, nil
}

func validPreFilterSchemaVersion(version string) bool {
	switch version {
	case ExtractionPreFilterSchemaVersion, "memory_extraction_prefilter.v0.1":
		return true
	default:
		return false
	}
}

func strictDecode(r io.Reader, out any) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	return strictDecodeBytes(data, out)
}

func strictDecodeBytes(data []byte, out any) error {
	if strings.HasPrefix(strings.TrimSpace(string(data)), "```") {
		return fmt.Errorf("json must not be wrapped in markdown code fences")
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		return err
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("trailing JSON value after top-level object")
		}
		return fmt.Errorf("trailing garbage after top-level object: %w", err)
	}
	return nil
}
