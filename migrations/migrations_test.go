package migrations

import (
	"crypto/sha256"
	"fmt"
	"testing"
	"testing/fstest"
)

func TestAllFromFSParsesMetadataAndChecksum(t *testing.T) {
	body := "CREATE TABLE sample(id TEXT);\n"
	got, err := allFromFS(fstest.MapFS{
		"0001_initial.sql": {Data: []byte(body)},
	})
	if err != nil {
		t.Fatalf("load migrations: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("migration count = %d, want 1", len(got))
	}
	if got[0].Version != "0001" {
		t.Fatalf("version = %q, want 0001", got[0].Version)
	}
	if got[0].Name != "initial" {
		t.Fatalf("name = %q, want initial", got[0].Name)
	}
	wantChecksum := fmt.Sprintf("%x", sha256.Sum256([]byte(body)))
	if got[0].Checksum != wantChecksum {
		t.Fatalf("checksum = %q, want %q", got[0].Checksum, wantChecksum)
	}
	if got[0].SQL != body {
		t.Fatalf("sql body was not preserved")
	}
}

func TestAllFromFSNormalizesLineEndingsForChecksum(t *testing.T) {
	body := "CREATE TABLE sample(id TEXT);\r\nINSERT INTO sample(id) VALUES ('x');\r\n"
	got, err := allFromFS(fstest.MapFS{
		"0001_initial.sql": {Data: []byte(body)},
	})
	if err != nil {
		t.Fatalf("load migrations: %v", err)
	}

	normalized := "CREATE TABLE sample(id TEXT);\nINSERT INTO sample(id) VALUES ('x');\n"
	wantChecksum := fmt.Sprintf("%x", sha256.Sum256([]byte(normalized)))
	rawChecksum := fmt.Sprintf("%x", sha256.Sum256([]byte(body)))
	if got[0].Checksum != wantChecksum {
		t.Fatalf("checksum = %q, want normalized %q", got[0].Checksum, wantChecksum)
	}
	if !got[0].MatchesChecksum(rawChecksum) {
		t.Fatalf("raw CRLF checksum %q was not accepted as equivalent", rawChecksum)
	}
}

func TestAllFromFSRejectsDuplicateVersion(t *testing.T) {
	_, err := allFromFS(fstest.MapFS{
		"0001_initial.sql": {Data: []byte("SELECT 1;")},
		"0001_second.sql":  {Data: []byte("SELECT 2;")},
	})
	if err == nil {
		t.Fatal("duplicate version was accepted")
	}
}

func TestAllFromFSRejectsDuplicateName(t *testing.T) {
	_, err := allFromFS(fstest.MapFS{
		"0001_initial.sql": {Data: []byte("SELECT 1;")},
		"0002_initial.sql": {Data: []byte("SELECT 2;")},
	})
	if err == nil {
		t.Fatal("duplicate name was accepted")
	}
}

func TestAllFromFSRejectsInvalidFilename(t *testing.T) {
	_, err := allFromFS(fstest.MapFS{
		"initial.sql": {Data: []byte("SELECT 1;")},
	})
	if err == nil {
		t.Fatal("invalid filename was accepted")
	}
}
