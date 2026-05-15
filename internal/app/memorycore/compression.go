package memorycore

import (
	"context"
	"errors"
	"fmt"
	memsqlite "github.com/longyisang/emoagent-memorycore/internal/store/sqlite"
)

func (s *service) ApplyCompression(ctx context.Context, req ApplyCompressionRequest) (*ApplyCompressionResult, error) {
	personaID := defaultString(req.PersonaID, s.persona)
	result, err := s.compress.Apply(ctx, memsqlite.CompressionRequest{
		PersonaID:     personaID,
		SourceFactIDs: req.SourceFactIDs,
		Narrative:     narrativeDraftToStore(req.Narrative),
		Insights:      insightDraftsToStore(req.Insights),
		Now:           req.Now,
		DryRun:        req.DryRun,
	})
	if errors.Is(err, memsqlite.ErrInvalidCompressionRequest) {
		return nil, fmt.Errorf("%w: %v", ErrInvalidRequest, err)
	}
	if err != nil {
		return nil, err
	}
	return compressionResultFromStore(result), nil
}
