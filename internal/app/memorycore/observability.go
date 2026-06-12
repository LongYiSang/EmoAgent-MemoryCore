package memorycore

import (
	"context"

	memsqlite "github.com/longyisang/emoagent-memorycore/internal/store/sqlite"
)

func (s *service) GetObservabilitySnapshot(ctx context.Context, req ObservabilitySnapshotRequest) (*ObservabilitySnapshot, error) {
	personaID := defaultString(req.PersonaID, s.persona)
	return s.observability.Snapshot(ctx, memsqlite.ObservabilitySnapshotRequest{
		PersonaID:        personaID,
		Since:            req.Since,
		IncludeDebug:     req.IncludeDebug,
		MirrorConfigured: s.mirrorAdapter != nil,
	})
}
