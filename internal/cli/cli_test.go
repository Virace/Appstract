package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExecuteInitCreatesLayout(t *testing.T) {
	root := t.TempDir()
	var out strings.Builder
	var errOut strings.Builder

	code := Execute([]string{"init", "--root", root}, &out, &errOut, "")
	if code != 0 {
		t.Fatalf("expected code 0, got %d, err=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "initialized:") {
		t.Fatalf("unexpected stdout: %s", out.String())
	}
	if _, err := os.Stat(filepath.Join(root, "apps")); err != nil {
		t.Fatalf("expected apps directory: %v", err)
	}
}

func TestExecuteRunFailsWhenCurrentMissing(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "apps", "chrome"), 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	var out strings.Builder
	var errOut strings.Builder

	code := Execute([]string{"run", "--root", root, "chrome"}, &out, &errOut, "")
	if code != 1 {
		t.Fatalf("expected code 1, got %d", code)
	}
	if !strings.Contains(errOut.String(), "has no current version") {
		t.Fatalf("unexpected stderr: %s", errOut.String())
	}
}

func TestExecuteRunReadyWhenCurrentExists(t *testing.T) {
	root := t.TempDir()
	current := filepath.Join(root, "apps", "chrome", "current")
	if err := os.MkdirAll(current, 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	var out strings.Builder
	var errOut strings.Builder

	code := Execute([]string{"run", "--root", root, "chrome"}, &out, &errOut, "")
	if code != 0 {
		t.Fatalf("expected code 0, got %d, err=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "run-ready") {
		t.Fatalf("unexpected stdout: %s", out.String())
	}
}

func TestExecuteManifestValidate(t *testing.T) {
	root := t.TempDir()
	manifestPath := filepath.Join(root, "app.json")
	content := `{
		"version": "1.2.3",
		"checkver": {"github": "https://github.com/owner/repo"},
		"autoupdate": {"architecture": {"64bit": {"url": "https://example.com/app.zip"}}},
		"bin": "app.exe",
		"hash": "sha256:abc"
	}`
	if err := os.WriteFile(manifestPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write file failed: %v", err)
	}

	var out strings.Builder
	var errOut strings.Builder
	code := Execute([]string{"manifest", "validate", manifestPath}, &out, &errOut, "")
	if code != 0 {
		t.Fatalf("expected code 0, got %d, err=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "manifest valid") {
		t.Fatalf("unexpected stdout: %s", out.String())
	}
}

func TestExecuteUnknownCommand(t *testing.T) {
	var out strings.Builder
	var errOut strings.Builder

	code := Execute([]string{"nope"}, &out, &errOut, "")
	if code != 1 {
		t.Fatalf("expected code 1, got %d", code)
	}
	if !strings.Contains(errOut.String(), "unknown command") {
		t.Fatalf("unexpected stderr: %s", errOut.String())
	}
}

func TestExecuteNoArgs(t *testing.T) {
	var out strings.Builder
	var errOut strings.Builder

	code := Execute([]string{}, &out, &errOut, "")
	if code != 1 {
		t.Fatalf("expected code 1, got %d", code)
	}
	if !strings.Contains(errOut.String(), "usage: appstract") {
		t.Fatalf("unexpected stderr: %s", errOut.String())
	}
}

func TestExecuteManifestUsageError(t *testing.T) {
	var out strings.Builder
	var errOut strings.Builder

	code := Execute([]string{"manifest"}, &out, &errOut, "")
	if code != 1 {
		t.Fatalf("expected code 1, got %d", code)
	}
	if !strings.Contains(errOut.String(), "usage: appstract manifest validate") {
		t.Fatalf("unexpected stderr: %s", errOut.String())
	}
}

func TestExecuteManifestValidateFileNotFound(t *testing.T) {
	var out strings.Builder
	var errOut strings.Builder

	code := Execute([]string{"manifest", "validate", "not-found.json"}, &out, &errOut, "")
	if code != 1 {
		t.Fatalf("expected code 1, got %d", code)
	}
	if !strings.Contains(errOut.String(), "read manifest file") {
		t.Fatalf("unexpected stderr: %s", errOut.String())
	}
}

func TestExecuteUpdateUsageError(t *testing.T) {
	var out strings.Builder
	var errOut strings.Builder

	code := Execute([]string{"update", "aria2"}, &out, &errOut, "")
	if code != 1 {
		t.Fatalf("expected code 1, got %d", code)
	}
	if !strings.Contains(errOut.String(), "usage: appstract update") {
		t.Fatalf("unexpected stderr: %s", errOut.String())
	}
}
