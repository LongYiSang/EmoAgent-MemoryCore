package memorycore

import (
	"time"

	memsqlite "github.com/longyisang/emoagent-memorycore/internal/store/sqlite"
)

type ObservabilitySnapshotRequest struct {
	PersonaID    string    `json:"persona_id,omitempty"`
	Since        time.Time `json:"since,omitempty"`
	IncludeDebug bool      `json:"include_debug,omitempty"`
}

type ObservabilitySnapshot = memsqlite.ObservabilitySnapshot
type StoreObservability = memsqlite.StoreObservability
type RetrievalObservability = memsqlite.RetrievalObservability
type ExtractionObservability = memsqlite.ExtractionObservability
type ForgettingObservability = memsqlite.ForgettingObservability
type RetentionObservability = memsqlite.RetentionObservability
type MirrorObservability = memsqlite.MirrorObservability
type NaturalMemoryObservability = memsqlite.NaturalMemoryObservability
