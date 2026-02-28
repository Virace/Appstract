package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
	if err := os.MkdirAll(filepath.Join(root, "manifests"), 0o755); err != nil {
		t.Fatalf("mkdir manifests failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(current, "chrome.exe"), []byte(""), 0o644); err != nil {
		t.Fatalf("write bin failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "manifests", "chrome.json"), []byte(runManifestContent("chrome.exe")), 0o644); err != nil {
		t.Fatalf("write manifest failed: %v", err)
	}

	launchCalled := ""
	oldLaunch := runLaunch
	runLaunch = func(path string) error {
		launchCalled = path
		return nil
	}
	t.Cleanup(func() { runLaunch = oldLaunch })

	done := make(chan struct{})
	oldAsync := runAsyncUpdate
	runAsyncUpdate = func(runRoot, app, manifestPath string) error {
		if runRoot != root || app != "chrome" {
			t.Fatalf("unexpected async update args: root=%s app=%s", runRoot, app)
		}
		close(done)
		return nil
	}
	t.Cleanup(func() { runAsyncUpdate = oldAsync })

	var out strings.Builder
	var errOut strings.Builder

	code := Execute([]string{"run", "--root", root, "chrome"}, &out, &errOut, "")
	if code != 0 {
		t.Fatalf("expected code 0, got %d, err=%s", code, errOut.String())
	}
	if launchCalled != filepath.Join(current, "chrome.exe") {
		t.Fatalf("unexpected launch path: %s", launchCalled)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("async update was not triggered")
	}
	if !strings.Contains(out.String(), "run-started") {
		t.Fatalf("unexpected stdout: %s", out.String())
	}
}

func TestExecuteRunFailsWhenManifestMissing(t *testing.T) {
	root := t.TempDir()
	current := filepath.Join(root, "apps", "chrome", "current")
	if err := os.MkdirAll(current, 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(current, "chrome.exe"), []byte(""), 0o644); err != nil {
		t.Fatalf("write bin failed: %v", err)
	}

	var out strings.Builder
	var errOut strings.Builder
	code := Execute([]string{"run", "--root", root, "chrome"}, &out, &errOut, "")
	if code != 1 {
		t.Fatalf("expected code 1, got %d", code)
	}
	if !strings.Contains(errOut.String(), "load manifest for run") {
		t.Fatalf("unexpected stderr: %s", errOut.String())
	}
}

func TestExecuteRunFailsWhenBinMissing(t *testing.T) {
	root := t.TempDir()
	current := filepath.Join(root, "apps", "chrome", "current")
	if err := os.MkdirAll(current, 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "manifests"), 0o755); err != nil {
		t.Fatalf("mkdir manifests failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "manifests", "chrome.json"), []byte(runManifestContent("chrome.exe")), 0o644); err != nil {
		t.Fatalf("write manifest failed: %v", err)
	}

	var out strings.Builder
	var errOut strings.Builder
	code := Execute([]string{"run", "--root", root, "chrome"}, &out, &errOut, "")
	if code != 1 {
		t.Fatalf("expected code 1, got %d", code)
	}
	if !strings.Contains(errOut.String(), "bin missing") {
		t.Fatalf("unexpected stderr: %s", errOut.String())
	}
}

func TestExecuteRunLaunchSuccessEvenIfBackgroundUpdateFails(t *testing.T) {
	root := t.TempDir()
	current := filepath.Join(root, "apps", "chrome", "current")
	if err := os.MkdirAll(current, 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "manifests"), 0o755); err != nil {
		t.Fatalf("mkdir manifests failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(current, "chrome.exe"), []byte(""), 0o644); err != nil {
		t.Fatalf("write bin failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "manifests", "chrome.json"), []byte(runManifestContent("chrome.exe")), 0o644); err != nil {
		t.Fatalf("write manifest failed: %v", err)
	}

	oldLaunch := runLaunch
	runLaunch = func(path string) error { return nil }
	t.Cleanup(func() { runLaunch = oldLaunch })

	done := make(chan struct{})
	oldAsync := runAsyncUpdate
	runAsyncUpdate = func(runRoot, app, manifestPath string) error {
		close(done)
		return fmt.Errorf("boom")
	}
	t.Cleanup(func() { runAsyncUpdate = oldAsync })

	var out strings.Builder
	var errOut strings.Builder
	code := Execute([]string{"run", "--root", root, "chrome"}, &out, &errOut, "")
	if code != 0 {
		t.Fatalf("expected code 0, got %d, err=%s", code, errOut.String())
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("async update was not triggered")
	}
	if !strings.Contains(errOut.String(), "background update failed") {
		t.Fatalf("unexpected stderr: %s", errOut.String())
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

func runManifestContent(bin string) string {
	return `{
		"version": "1.2.3",
		"autoupdate": {"architecture": {"64bit": {"url": "https://example.com/app.zip"}}},
		"bin": "` + bin + `",
		"hash": "sha256:abc"
	}`
}
