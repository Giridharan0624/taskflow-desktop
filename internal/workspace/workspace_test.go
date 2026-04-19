package workspace

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func tempPath(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "workspace.json")
	SetPathForTest(p)
	t.Cleanup(func() { SetPathForTest("") })
	return p
}

func TestLoad_NoFile_ReturnsErrNoWorkspace(t *testing.T) {
	tempPath(t)
	_, err := Load()
	if !errors.Is(err, ErrNoWorkspace) {
		t.Fatalf("want ErrNoWorkspace, got %v", err)
	}
}

func TestSaveLoad_Roundtrip(t *testing.T) {
	tempPath(t)
	if err := Save("acme"); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got != "acme" {
		t.Fatalf("want acme, got %q", got)
	}
}

func TestSave_RejectsInvalidSlug(t *testing.T) {
	tempPath(t)
	bad := []string{"", "a", "ab", "AB", "with space", "trailing-", "-leading", "with_underscore", "thisslugiswaywaywaywaywaytoolong12345"}
	for _, s := range bad {
		if err := Save(s); err == nil {
			t.Errorf("Save(%q) should have failed", s)
		}
	}
}

func TestSave_AcceptsValidSlug(t *testing.T) {
	tempPath(t)
	good := []string{"acme", "neurostack", "abc", "123", "a-b-c", "abc-123"}
	for _, s := range good {
		if err := Save(s); err != nil {
			t.Errorf("Save(%q): %v", s, err)
		}
	}
}

func TestSave_NormalizesCaseAndWhitespace(t *testing.T) {
	tempPath(t)
	if err := Save("  ACME  "); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got != "acme" {
		t.Fatalf("want acme (normalized), got %q", got)
	}
}

func TestClear_RemovesFile(t *testing.T) {
	p := tempPath(t)
	if err := Save("acme"); err != nil {
		t.Fatalf("save: %v", err)
	}
	if err := Clear(); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatalf("file still exists: %v", err)
	}
	if _, err := Load(); !errors.Is(err, ErrNoWorkspace) {
		t.Fatalf("want ErrNoWorkspace post-clear, got %v", err)
	}
}

func TestClear_NoFile_NoError(t *testing.T) {
	tempPath(t)
	if err := Clear(); err != nil {
		t.Fatalf("Clear on missing file should be nil, got %v", err)
	}
}

func TestLoad_CorruptFile_ReturnsErrNoWorkspace(t *testing.T) {
	p := tempPath(t)
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(p, []byte("{not json"), 0o600); err != nil {
		t.Fatalf("write corrupt file: %v", err)
	}
	_, err := Load()
	if !errors.Is(err, ErrNoWorkspace) {
		t.Fatalf("want ErrNoWorkspace for corrupt file, got %v", err)
	}
}

func TestLoad_WrongVersion_ReturnsErrNoWorkspace(t *testing.T) {
	p := tempPath(t)
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(p, []byte(`{"version":99,"slug":"acme"}`), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, err := Load()
	if !errors.Is(err, ErrNoWorkspace) {
		t.Fatalf("want ErrNoWorkspace for wrong version, got %v", err)
	}
}
