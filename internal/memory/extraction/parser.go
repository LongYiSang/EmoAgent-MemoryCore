package extraction

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/longyisang/emoagent-memorycore/pkg/memorycore"
)

func ParseRequest(r io.Reader) (memorycore.ExtractionRequest, error) {
	var req memorycore.ExtractionRequest
	if err := strictDecode(r, &req); err != nil {
		return memorycore.ExtractionRequest{}, err
	}
	if req.SchemaVersion != memorycore.ExtractionRequestSchemaVersion {
		return memorycore.ExtractionRequest{}, fmt.Errorf("schema_version must be %s", memorycore.ExtractionRequestSchemaVersion)
	}
	if strings.TrimSpace(req.RequestID) == "" {
		return memorycore.ExtractionRequest{}, fmt.Errorf("request_id is required")
	}
	if strings.TrimSpace(req.PersonaID) == "" {
		return memorycore.ExtractionRequest{}, fmt.Errorf("persona_id is required")
	}
	if !validExtractionTrigger(req.Trigger) {
		return memorycore.ExtractionRequest{}, fmt.Errorf("trigger is invalid")
	}
	if strings.TrimSpace(req.Timezone) == "" {
		return memorycore.ExtractionRequest{}, fmt.Errorf("timezone is required")
	}
	return req, nil
}

func ParseResponse(r io.Reader) (memorycore.ExtractionResponse, error) {
	var resp memorycore.ExtractionResponse
	if err := strictDecode(r, &resp); err != nil {
		return memorycore.ExtractionResponse{}, err
	}
	if resp.SchemaVersion != memorycore.ExtractionResponseSchemaVersion {
		return memorycore.ExtractionResponse{}, fmt.Errorf("schema_version must be %s", memorycore.ExtractionResponseSchemaVersion)
	}
	if strings.TrimSpace(resp.RequestID) == "" {
		return memorycore.ExtractionResponse{}, fmt.Errorf("request_id is required")
	}
	if strings.TrimSpace(resp.PersonaID) == "" {
		return memorycore.ExtractionResponse{}, fmt.Errorf("persona_id is required")
	}
	if !validExtractionTrigger(resp.Trigger) {
		return memorycore.ExtractionResponse{}, fmt.Errorf("trigger is invalid")
	}
	return resp, nil
}

func ParsePreFilterResponse(r io.Reader) (memorycore.ExtractionPreFilterResponse, error) {
	var resp memorycore.ExtractionPreFilterResponse
	if err := strictDecode(r, &resp); err != nil {
		return memorycore.ExtractionPreFilterResponse{}, err
	}
	if !validPreFilterSchemaVersion(resp.SchemaVersion) {
		return memorycore.ExtractionPreFilterResponse{}, fmt.Errorf("schema_version must be %s", memorycore.ExtractionPreFilterSchemaVersion)
	}
	if strings.TrimSpace(resp.RequestID) == "" {
		return memorycore.ExtractionPreFilterResponse{}, fmt.Errorf("request_id is required")
	}
	if strings.TrimSpace(resp.PersonaID) == "" {
		return memorycore.ExtractionPreFilterResponse{}, fmt.Errorf("persona_id is required")
	}
	if !validExtractionTrigger(resp.Trigger) {
		return memorycore.ExtractionPreFilterResponse{}, fmt.Errorf("trigger is invalid")
	}
	for _, episode := range resp.Episodes {
		if strings.TrimSpace(episode.EpisodeID) == "" {
			return memorycore.ExtractionPreFilterResponse{}, fmt.Errorf("episode_id is required")
		}
		switch episode.RoutingHint {
		case "extract", "forget_manager", "pin_manager", "skip", "review", "route":
		default:
			return memorycore.ExtractionPreFilterResponse{}, fmt.Errorf("routing_hint is invalid")
		}
	}
	return resp, nil
}

func validPreFilterSchemaVersion(version string) bool {
	switch version {
	case memorycore.ExtractionPreFilterSchemaVersion, "memory_extraction_prefilter.v0.1":
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
