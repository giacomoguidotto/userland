package csvfile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteAndReadPreserveQuotedFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "declarations.csv")
	header := []string{"repository", "path"}
	rows := [][]string{{"git@example.test:team/repo.git", "services/api"}, {"quoted, repository", "path with spaces"}}

	if err := Write(path, header, rows, 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := Read(path, header)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[1][0] != rows[1][0] || got[1][1] != rows[1][1] {
		t.Fatalf("round trip changed rows: %#v", got)
	}
}

func TestReadRejectsTheWrongHeader(t *testing.T) {
	path := filepath.Join(t.TempDir(), "declarations.csv")
	if err := os.WriteFile(path, []byte("path,repository\nservices/api,repo\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := Read(path, []string{"repository", "path"})
	if err == nil || !strings.Contains(err.Error(), "invalid header") {
		t.Fatalf("wrong header was accepted: %v", err)
	}
}

func TestReadReportsMalformedRecords(t *testing.T) {
	path := filepath.Join(t.TempDir(), "declarations.csv")
	if err := os.WriteFile(path, []byte("repository,path\nrepo\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := Read(path, []string{"repository", "path"})
	if err == nil || !strings.Contains(err.Error(), path) || !strings.Contains(err.Error(), "record on line 2") {
		t.Fatalf("malformed record error lacks context: %v", err)
	}
}

func TestReadAcceptsAnEmptyOverride(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.csv")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	rows, err := Read(path, []string{"repository", "path"})
	if err != nil || len(rows) != 0 {
		t.Fatalf("empty override = %#v, %v", rows, err)
	}
}
