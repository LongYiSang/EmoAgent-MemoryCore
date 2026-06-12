package memorycore_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/longyisang/emoagent-memorycore/pkg/memorycore"
)

func TestAboutReportsV01CapabilityBoundary(t *testing.T) {
	info := memorycore.About()

	if info.Module != "github.com/longyisang/emoagent-memorycore" {
		t.Fatalf("module = %q", info.Module)
	}
	if info.Version != memorycore.LibraryVersion || info.Version == "" {
		t.Fatalf("version = %q, library version = %q", info.Version, memorycore.LibraryVersion)
	}
	if info.PublicAPIVersion != memorycore.PublicAPIVersion {
		t.Fatalf("public api version = %q", info.PublicAPIVersion)
	}
	if !info.SQLiteAuthoritative || info.SidecarAuthoritative {
		t.Fatalf("authority flags = sqlite:%t sidecar:%t", info.SQLiteAuthoritative, info.SidecarAuthoritative)
	}

	requireCapabilityStatus(t, info, "sqlite_authority", memorycore.CapabilitySupported)
	requireCapabilityStatus(t, info, "session_episode", memorycore.CapabilitySupported)
	requireCapabilityStatus(t, info, "retrieval_v5", memorycore.CapabilitySupported)
	requireCapabilityStatus(t, info, "forget_broad_preview", memorycore.CapabilityExperimental)
	requireCapabilityStatus(t, info, "mirror_sync", memorycore.CapabilityOptional)
	requireCapabilityStatus(t, info, "agent_affect", memorycore.CapabilityHostOwned)
	requireCapabilityStatus(t, info, "user_mood_relationship_affect", memorycore.CapabilityHostOwned)
	requireCapabilityStatus(t, info, "entity_cascade_purge", memorycore.CapabilityNotSupported)
	requireCapabilityStatus(t, info, "review_queue", memorycore.CapabilityNotSupported)
	requireCapabilityStatus(t, info, "http_service", memorycore.CapabilityNotSupported)
}

func TestAboutJSONUsesSnakeCase(t *testing.T) {
	data, err := json.Marshal(memorycore.About())
	if err != nil {
		t.Fatalf("marshal about: %v", err)
	}
	text := string(data)
	for _, want := range []string{
		`"public_api_version"`,
		`"sqlite_schema_version"`,
		`"config_schema_version"`,
		`"retrieval_contract_version"`,
		`"sqlite_authoritative"`,
		`"sidecar_authoritative"`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("about json missing %s: %s", want, text)
		}
	}
	if strings.Contains(text, "PublicAPIVersion") || strings.Contains(text, "SQLiteAuthoritative") {
		t.Fatalf("about json contains Go field names: %s", text)
	}
}

func requireCapabilityStatus(t *testing.T, info memorycore.AboutInfo, name string, want memorycore.CapabilityStatus) {
	t.Helper()

	got, ok := info.Capabilities[name]
	if !ok {
		t.Fatalf("capability %q missing", name)
	}
	if got.Status != want {
		t.Fatalf("capability %q status = %q, want %q", name, got.Status, want)
	}
}
