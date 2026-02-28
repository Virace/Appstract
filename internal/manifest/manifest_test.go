package manifest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseBytesValid(t *testing.T) {
	json := `{
		"version": "1.2.3",
		"checkver": {"github": "https://github.com/owner/repo"},
		"autoupdate": {
			"architecture": {
				"64bit": {
					"url": "https://example.com/app.zip",
					"extract_dir": "app-1.2.3"
				}
			}
		},
		"bin": "app.exe",
		"hash": "sha256:abc"
	}`

	m, err := ParseBytes([]byte(json))
	if err != nil {
		t.Fatalf("ParseBytes failed: %v", err)
	}
	if m.Version != "1.2.3" {
		t.Fatalf("unexpected version: %s", m.Version)
	}
}

func TestParseBytesMissingHash(t *testing.T) {
	json := `{
		"version": "1.2.3",
		"checkver": {"github": "https://github.com/owner/repo"},
		"autoupdate": {"architecture": {"64bit": {"url": "https://example.com/app.zip"}}},
		"bin": "app.exe"
	}`

	_, err := ParseBytes([]byte(json))
	if err == nil || !strings.Contains(err.Error(), "hash") {
		t.Fatalf("expected hash validation error, got: %v", err)
	}
}

func TestParseBytesInvalidJSON(t *testing.T) {
	_, err := ParseBytes([]byte(`{`))
	if err == nil {
		t.Fatal("expected json decode error")
	}
}

func TestParseBytesMissingRequiredFields(t *testing.T) {
	json := `{
		"version": "1.2.3",
		"autoupdate": {"architecture": {"64bit": {"extract_dir": "app"}}},
		"bin": "app.exe",
		"hash": "sha256:abc"
	}`

	_, err := ParseBytes([]byte(json))
	if err == nil || !strings.Contains(err.Error(), "artifact url") {
		t.Fatalf("expected artifact url validation error, got: %v", err)
	}
}

func TestParseBytesWithArchitectureHash(t *testing.T) {
	json := `{
		"version": "1.37.0-1",
		"architecture": {
			"64bit": {
				"url": "https://example.com/aria2.zip",
				"hash": "67d015301eef0b612191212d564c5bb0a14b5b9c4796b76454276a4d28d9b288",
				"extract_dir": "aria2-1.37.0-win-64bit-build1"
			}
		},
		"bin": "aria2c.exe"
	}`

	m, err := ParseBytes([]byte(json))
	if err != nil {
		t.Fatalf("ParseBytes failed: %v", err)
	}
	artifact, err := m.ResolveArtifact64()
	if err != nil {
		t.Fatalf("ResolveArtifact64 failed: %v", err)
	}
	if artifact.Hash == "" {
		t.Fatal("expected non-empty hash")
	}
}

func TestParseFile(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "app.json")
	json := `{
		"version": "1.2.3",
		"checkver": {"github": "https://github.com/owner/repo"},
		"autoupdate": {"architecture": {"64bit": {"url": "https://example.com/app.zip"}}},
		"bin": "app.exe",
		"hash": "sha256:abc"
	}`
	if err := os.WriteFile(path, []byte(json), 0o644); err != nil {
		t.Fatalf("write manifest file: %v", err)
	}

	m, err := ParseFile(path)
	if err != nil {
		t.Fatalf("ParseFile failed: %v", err)
	}
	if m.Version != "1.2.3" {
		t.Fatalf("unexpected version: %s", m.Version)
	}
}

func TestParseFileMissing(t *testing.T) {
	_, err := ParseFile(filepath.Join(t.TempDir(), "missing.json"))
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}
