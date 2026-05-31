package memorycore

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestValidateCurationRawLogOptionsRejectsMissingDirectory(t *testing.T) {
	missingDir := filepath.Join(t.TempDir(), "missing")
	err := validateCurationRawLogOptions(CurationRawLogOptions{Enabled: true, Directory: missingDir})
	var svcErr *ExtractionServiceError
	if !errors.As(err, &svcErr) {
		t.Fatalf("err = %v, want ExtractionServiceError", err)
	}
	if svcErr.Code != "raw_log_directory_missing" {
		t.Fatalf("error code = %q, want raw_log_directory_missing", svcErr.Code)
	}
	if _, statErr := os.Stat(missingDir); !os.IsNotExist(statErr) {
		t.Fatalf("missing raw log dir stat err = %v, want not exist", statErr)
	}
}
