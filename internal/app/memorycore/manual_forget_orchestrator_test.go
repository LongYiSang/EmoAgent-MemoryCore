package memorycore

import "testing"

func TestNormalizeManualForgetLevelMapsProviderAliases(t *testing.T) {
	tests := map[string]string{
		"complete":         ForgetLevelPurge,
		"delete_all":       ForgetLevelPurge,
		"full_delete":      ForgetLevelPurge,
		"delete_memory":    ForgetLevelHard,
		"avoid_mention":    ForgetLevelSoft,
		"source_redaction": ForgetLevelSourceRedact,
	}
	for input, want := range tests {
		if got := normalizeManualForgetLevel(input); got != want {
			t.Fatalf("normalizeManualForgetLevel(%q) = %q, want %q", input, got, want)
		}
	}
}
