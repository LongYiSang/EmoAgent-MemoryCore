package memorycore_test

import (
	"testing"

	appcore "github.com/longyisang/emoagent-memorycore/internal/app/memorycore"
	"github.com/longyisang/emoagent-memorycore/pkg/memorycore"
)

func mirrorBackendForTest(t *testing.T, adapter appcore.MirrorAdapter) memorycore.MirrorBackend {
	t.Helper()
	backend, err := memorycore.NewMirrorBackendFromAdapter(adapter)
	if err != nil {
		t.Fatalf("wrap mirror backend: %v", err)
	}
	return backend
}
