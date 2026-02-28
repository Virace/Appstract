package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"appstract/internal/bootstrap"
	"appstract/internal/updater"
)

func TestExecuteNoArgs(t *testing.T) {
	var out strings.Builder
	var errOut strings.Builder

	code := Execute([]string{}, &out, &errOut, "")
	if code != 1 {
		t.Fatalf("expected code 1, got %d", code)
	}
	if !strings.Contains(errOut.String(), "usage: appstract <command>") {
		t.Fatalf("unexpected stderr: %s", errOut.String())
	}
}

func TestExecuteHelp(t *testing.T) {
	var out strings.Builder
	var errOut strings.Builder

	code := Execute([]string{"help"}, &out, &errOut, "")
	if code != 0 {
		t.Fatalf("expected code 0, got %d, err=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "commands:") {
		t.Fatalf("unexpected stdout: %s", out.String())
	}
}

func TestExecuteHelpForCommand(t *testing.T) {
	var out strings.Builder
	var errOut strings.Builder

	code := Execute([]string{"help", "add"}, &out, &errOut, "")
	if code != 0 {
		t.Fatalf("expected code 0, got %d, err=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "usage: appstract add") {
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

func TestExecuteInitUsesExecutableDirectoryByDefault(t *testing.T) {
	root := t.TempDir()
	exePath := filepath.Join(root, "appstract.exe")
	if err := os.WriteFile(exePath, []byte("bin"), 0o755); err != nil {
		t.Fatalf("write exe failed: %v", err)
	}

	oldResolveExecutablePath := resolveExecutablePath
	resolveExecutablePath = func() (string, error) { return exePath, nil }
	t.Cleanup(func() {
		resolveExecutablePath = oldResolveExecutablePath
	})

	var out strings.Builder
	var errOut strings.Builder
	code := Execute([]string{"init"}, &out, &errOut, "")
	if code != 0 {
		t.Fatalf("expected code 0, got %d, err=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "initialized: "+root) {
		t.Fatalf("unexpected stdout: %s", out.String())
	}
	if _, err := os.Stat(filepath.Join(root, "manifests")); err != nil {
		t.Fatalf("expected manifests directory: %v", err)
	}
}

func TestExecuteRunFailsWhenCurrentMissingAndManifestMissing(t *testing.T) {
	root := t.TempDir()
	if err := bootstrap.InitLayout(root); err != nil {
		t.Fatalf("init layout failed: %v", err)
	}
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
	if err := bootstrap.InitLayout(root); err != nil {
		t.Fatalf("init layout failed: %v", err)
	}
	current := filepath.Join(root, "apps", "chrome", "current")
	if err := os.MkdirAll(current, 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
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
	runAsyncUpdate = func(runRoot, app, manifestPath string, opts updateOptions) error {
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

func TestExecuteRunAttemptsInstallWhenCurrentMissing(t *testing.T) {
	root := t.TempDir()
	if err := bootstrap.InitLayout(root); err != nil {
		t.Fatalf("init layout failed: %v", err)
	}
	manifestPath := filepath.Join(root, "manifests", "chrome.json")
	if err := os.WriteFile(manifestPath, []byte(runManifestContent("chrome.exe")), 0o644); err != nil {
		t.Fatalf("write manifest failed: %v", err)
	}

	oldUpdate := executeUpdateFromManifest
	updateCalls := 0
	executeUpdateFromManifest = func(updateRoot, app, path string, opts updateOptions) error {
		updateCalls++
		if updateRoot != root || app != "chrome" || path != manifestPath {
			t.Fatalf("unexpected update call: root=%s app=%s path=%s", updateRoot, app, path)
		}
		current := filepath.Join(root, "apps", "chrome", "current")
		if err := os.MkdirAll(current, 0o755); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(current, "chrome.exe"), []byte("bin"), 0o644)
	}
	t.Cleanup(func() { executeUpdateFromManifest = oldUpdate })

	launchCalled := ""
	oldLaunch := runLaunch
	runLaunch = func(path string) error {
		launchCalled = path
		return nil
	}
	t.Cleanup(func() { runLaunch = oldLaunch })

	done := make(chan struct{})
	oldAsync := runAsyncUpdate
	runAsyncUpdate = func(runRoot, app, path string, opts updateOptions) error {
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
	if updateCalls != 1 {
		t.Fatalf("expected 1 install call, got %d", updateCalls)
	}
	if !strings.Contains(out.String(), "auto-installing from manifest") {
		t.Fatalf("expected auto-installing prompt, got: %s", out.String())
	}
	if !strings.Contains(out.String(), "auto-install completed: chrome") {
		t.Fatalf("expected auto-install completed prompt, got: %s", out.String())
	}
	if launchCalled != filepath.Join(root, "apps", "chrome", "current", "chrome.exe") {
		t.Fatalf("unexpected launch path: %s", launchCalled)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("async update was not triggered")
	}
}

func TestExecuteRunInstallFailureWhenCurrentMissing(t *testing.T) {
	root := t.TempDir()
	if err := bootstrap.InitLayout(root); err != nil {
		t.Fatalf("init layout failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "manifests", "chrome.json"), []byte(runManifestContent("chrome.exe")), 0o644); err != nil {
		t.Fatalf("write manifest failed: %v", err)
	}

	oldUpdate := executeUpdateFromManifest
	executeUpdateFromManifest = func(updateRoot, app, path string, opts updateOptions) error {
		return fmt.Errorf("boom")
	}
	t.Cleanup(func() { executeUpdateFromManifest = oldUpdate })

	var out strings.Builder
	var errOut strings.Builder
	code := Execute([]string{"run", "--root", root, "chrome"}, &out, &errOut, "")
	if code != 1 {
		t.Fatalf("expected code 1, got %d", code)
	}
	if !strings.Contains(errOut.String(), "install app") {
		t.Fatalf("unexpected stderr: %s", errOut.String())
	}
}

func TestExecuteRunFailsWhenManifestMissing(t *testing.T) {
	root := t.TempDir()
	if err := bootstrap.InitLayout(root); err != nil {
		t.Fatalf("init layout failed: %v", err)
	}
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
	if err := bootstrap.InitLayout(root); err != nil {
		t.Fatalf("init layout failed: %v", err)
	}
	current := filepath.Join(root, "apps", "chrome", "current")
	if err := os.MkdirAll(current, 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
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
	if err := bootstrap.InitLayout(root); err != nil {
		t.Fatalf("init layout failed: %v", err)
	}
	current := filepath.Join(root, "apps", "chrome", "current")
	if err := os.MkdirAll(current, 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
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
	runAsyncUpdate = func(runRoot, app, manifestPath string, opts updateOptions) error {
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

func TestExecuteManifestUsageError(t *testing.T) {
	var out strings.Builder
	var errOut strings.Builder

	code := Execute([]string{"manifest"}, &out, &errOut, "")
	if code != 1 {
		t.Fatalf("expected code 1, got %d", code)
	}
	if !strings.Contains(errOut.String(), "usage: appstract manifest") {
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

func TestExecuteAddUsageError(t *testing.T) {
	var out strings.Builder
	var errOut strings.Builder

	code := Execute([]string{"add"}, &out, &errOut, "")
	if code != 1 {
		t.Fatalf("expected code 1, got %d", code)
	}
	if !strings.Contains(errOut.String(), "usage: appstract add") {
		t.Fatalf("unexpected stderr: %s", errOut.String())
	}
}

func TestExecuteAddSuccess(t *testing.T) {
	root := t.TempDir()
	if err := bootstrap.InitLayout(root); err != nil {
		t.Fatalf("init layout failed: %v", err)
	}
	sourceManifest := filepath.Join(t.TempDir(), "chrome.json")
	content := runManifestContent("chrome.exe")
	if err := os.WriteFile(sourceManifest, []byte(content), 0o644); err != nil {
		t.Fatalf("write source manifest failed: %v", err)
	}

	oldUpdate := executeUpdateFromManifest
	called := false
	executeUpdateFromManifest = func(updateRoot, app, path string, opts updateOptions) error {
		called = true
		if updateRoot != root || app != "chrome" {
			t.Fatalf("unexpected update args: root=%s app=%s", updateRoot, app)
		}
		if path != filepath.Join(root, "manifests", "chrome.json") {
			t.Fatalf("unexpected manifest path: %s", path)
		}
		return nil
	}
	t.Cleanup(func() { executeUpdateFromManifest = oldUpdate })

	var out strings.Builder
	var errOut strings.Builder
	code := Execute([]string{"add", "--root", root, sourceManifest}, &out, &errOut, "")
	if code != 0 {
		t.Fatalf("expected code 0, got %d, err=%s", code, errOut.String())
	}
	if !called {
		t.Fatal("expected update call")
	}
	targetManifest := filepath.Join(root, "manifests", "chrome.json")
	saved, err := os.ReadFile(targetManifest)
	if err != nil {
		t.Fatalf("read target manifest failed: %v", err)
	}
	if string(saved) != content {
		t.Fatalf("copied manifest mismatch")
	}
	if !strings.Contains(out.String(), "add completed: chrome") {
		t.Fatalf("unexpected stdout: %s", out.String())
	}
}

func TestExecuteAddRequiresInitOnBinaryOnlyRoot(t *testing.T) {
	root := t.TempDir()
	exePath := filepath.Join(root, "appstract.exe")
	if err := os.WriteFile(exePath, []byte("bin"), 0o755); err != nil {
		t.Fatalf("write exe failed: %v", err)
	}
	sourceManifest := filepath.Join(t.TempDir(), "chrome.json")
	if err := os.WriteFile(sourceManifest, []byte(runManifestContent("chrome.exe")), 0o644); err != nil {
		t.Fatalf("write source manifest failed: %v", err)
	}

	oldResolveExecutablePath := resolveExecutablePath
	resolveExecutablePath = func() (string, error) { return exePath, nil }
	t.Cleanup(func() {
		resolveExecutablePath = oldResolveExecutablePath
	})

	var out strings.Builder
	var errOut strings.Builder
	code := Execute([]string{"add", "--root", root, sourceManifest}, &out, &errOut, "")
	if code != 1 {
		t.Fatalf("expected code 1, got %d", code)
	}
	if !strings.Contains(errOut.String(), "please run: appstract init") {
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

func TestExecuteUpdateNoManifests(t *testing.T) {
	root := t.TempDir()
	if err := bootstrap.InitLayout(root); err != nil {
		t.Fatalf("init layout failed: %v", err)
	}

	var out strings.Builder
	var errOut strings.Builder
	code := Execute([]string{"update", "--root", root}, &out, &errOut, "")
	if code != 0 {
		t.Fatalf("expected code 0, got %d, err=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "no manifests found") {
		t.Fatalf("unexpected stdout: %s", out.String())
	}
}

func TestExecuteUpdateBatchSuccess(t *testing.T) {
	root := t.TempDir()
	if err := bootstrap.InitLayout(root); err != nil {
		t.Fatalf("init layout failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "manifests", "a.json"), []byte(runManifestContent("a.exe")), 0o644); err != nil {
		t.Fatalf("write manifest a failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "manifests", "b.json"), []byte(runManifestContent("b.exe")), 0o644); err != nil {
		t.Fatalf("write manifest b failed: %v", err)
	}

	oldUpdate := executeUpdateFromManifest
	calls := []string{}
	executeUpdateFromManifest = func(updateRoot, app, path string, opts updateOptions) error {
		calls = append(calls, app)
		return nil
	}
	t.Cleanup(func() { executeUpdateFromManifest = oldUpdate })

	var out strings.Builder
	var errOut strings.Builder
	code := Execute([]string{"update", "--root", root}, &out, &errOut, "")
	if code != 0 {
		t.Fatalf("expected code 0, got %d, err=%s", code, errOut.String())
	}
	if len(calls) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(calls))
	}
	if calls[0] != "a" || calls[1] != "b" {
		t.Fatalf("unexpected app order: %v", calls)
	}
	if !strings.Contains(out.String(), "update summary: total=2 success=2 failed=0") {
		t.Fatalf("unexpected stdout: %s", out.String())
	}
}

func TestExecuteUpdateBatchFailureContinueByDefault(t *testing.T) {
	root := t.TempDir()
	if err := bootstrap.InitLayout(root); err != nil {
		t.Fatalf("init layout failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "manifests", "a.json"), []byte(runManifestContent("a.exe")), 0o644); err != nil {
		t.Fatalf("write manifest a failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "manifests", "b.json"), []byte(runManifestContent("b.exe")), 0o644); err != nil {
		t.Fatalf("write manifest b failed: %v", err)
	}

	oldUpdate := executeUpdateFromManifest
	calls := []string{}
	executeUpdateFromManifest = func(updateRoot, app, path string, opts updateOptions) error {
		calls = append(calls, app)
		if app == "a" {
			return fmt.Errorf("boom-a")
		}
		return nil
	}
	t.Cleanup(func() { executeUpdateFromManifest = oldUpdate })

	var out strings.Builder
	var errOut strings.Builder
	code := Execute([]string{"update", "--root", root}, &out, &errOut, "")
	if code != 1 {
		t.Fatalf("expected code 1, got %d", code)
	}
	if len(calls) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(calls))
	}
	if !strings.Contains(errOut.String(), "update failed: a") {
		t.Fatalf("unexpected stderr: %s", errOut.String())
	}
	if !strings.Contains(out.String(), "update summary: total=2 success=1 failed=1") {
		t.Fatalf("unexpected stdout: %s", out.String())
	}
}

func TestExecuteUpdateFailFast(t *testing.T) {
	root := t.TempDir()
	if err := bootstrap.InitLayout(root); err != nil {
		t.Fatalf("init layout failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "manifests", "a.json"), []byte(runManifestContent("a.exe")), 0o644); err != nil {
		t.Fatalf("write manifest a failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "manifests", "b.json"), []byte(runManifestContent("b.exe")), 0o644); err != nil {
		t.Fatalf("write manifest b failed: %v", err)
	}

	oldUpdate := executeUpdateFromManifest
	calls := []string{}
	executeUpdateFromManifest = func(updateRoot, app, path string, opts updateOptions) error {
		calls = append(calls, app)
		if app == "a" {
			return fmt.Errorf("boom-a")
		}
		return nil
	}
	t.Cleanup(func() { executeUpdateFromManifest = oldUpdate })

	var out strings.Builder
	var errOut strings.Builder
	code := Execute([]string{"update", "--root", root, "--fail-fast"}, &out, &errOut, "")
	if code != 1 {
		t.Fatalf("expected code 1, got %d", code)
	}
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d (%v)", len(calls), calls)
	}
	if !strings.Contains(out.String(), "update summary: total=2 success=0 failed=1") {
		t.Fatalf("unexpected stdout: %s", out.String())
	}
}

func TestExecuteUpdateRepairsMissingDirectories(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "manifests"), 0o755); err != nil {
		t.Fatalf("mkdir manifests failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "manifests", "a.json"), []byte(runManifestContent("a.exe")), 0o644); err != nil {
		t.Fatalf("write manifest failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write notes failed: %v", err)
	}

	oldUpdate := executeUpdateFromManifest
	executeUpdateFromManifest = func(updateRoot, app, path string, opts updateOptions) error { return nil }
	t.Cleanup(func() { executeUpdateFromManifest = oldUpdate })

	var out strings.Builder
	var errOut strings.Builder
	code := Execute([]string{"update", "--root", root}, &out, &errOut, "")
	if code != 0 {
		t.Fatalf("expected code 0, got %d, err=%s", code, errOut.String())
	}
	for _, dir := range []string{"apps", "scripts", "shims"} {
		if info, err := os.Stat(filepath.Join(root, dir)); err != nil || !info.IsDir() {
			t.Fatalf("expected repaired directory %s, err=%v", dir, err)
		}
	}
}

func TestExecuteUpdateUsesExecutableDirByDefault(t *testing.T) {
	root := t.TempDir()
	exePath := filepath.Join(root, "appstract.exe")
	if err := os.WriteFile(exePath, []byte("bin"), 0o755); err != nil {
		t.Fatalf("write exe failed: %v", err)
	}
	if err := bootstrap.InitLayout(root); err != nil {
		t.Fatalf("init layout failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "manifests", "a.json"), []byte(runManifestContent("a.exe")), 0o644); err != nil {
		t.Fatalf("write manifest failed: %v", err)
	}

	oldResolveExecutablePath := resolveExecutablePath
	resolveExecutablePath = func() (string, error) { return exePath, nil }
	t.Cleanup(func() {
		resolveExecutablePath = oldResolveExecutablePath
	})

	oldUpdate := executeUpdateFromManifest
	calledRoot := ""
	executeUpdateFromManifest = func(updateRoot, app, path string, opts updateOptions) error {
		calledRoot = updateRoot
		return nil
	}
	t.Cleanup(func() { executeUpdateFromManifest = oldUpdate })

	var out strings.Builder
	var errOut strings.Builder
	code := Execute([]string{"update"}, &out, &errOut, "")
	if code != 0 {
		t.Fatalf("expected code 0, got %d, err=%s", code, errOut.String())
	}
	if calledRoot != root {
		t.Fatalf("expected default root %s, got %s", root, calledRoot)
	}
}

func TestExecuteUpdateSilentOutput(t *testing.T) {
	root := t.TempDir()
	if err := bootstrap.InitLayout(root); err != nil {
		t.Fatalf("init layout failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "manifests", "a.json"), []byte(runManifestContent("a.exe")), 0o644); err != nil {
		t.Fatalf("write manifest failed: %v", err)
	}

	oldUpdate := executeUpdateFromManifest
	executeUpdateFromManifest = func(updateRoot, app, path string, opts updateOptions) error {
		return nil
	}
	t.Cleanup(func() { executeUpdateFromManifest = oldUpdate })

	var out strings.Builder
	var errOut strings.Builder
	code := Execute([]string{"update", "--root", root, "--output", "silent"}, &out, &errOut, "")
	if code != 0 {
		t.Fatalf("expected code 0, got %d, err=%s", code, errOut.String())
	}
	if strings.TrimSpace(out.String()) != "" {
		t.Fatalf("expected silent stdout, got: %q", out.String())
	}
}

func TestExecuteUpdateDebugOutput(t *testing.T) {
	root := t.TempDir()
	if err := bootstrap.InitLayout(root); err != nil {
		t.Fatalf("init layout failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "manifests", "a.json"), []byte(runManifestContent("a.exe")), 0o644); err != nil {
		t.Fatalf("write manifest failed: %v", err)
	}

	oldUpdate := executeUpdateFromManifest
	executeUpdateFromManifest = func(updateRoot, app, path string, opts updateOptions) error {
		if opts.Output == nil {
			t.Fatal("expected output reporter")
		}
		opts.Output.onUpdaterMessage(updater.MessageLevelDebug, "debug trace")
		return nil
	}
	t.Cleanup(func() { executeUpdateFromManifest = oldUpdate })

	var out strings.Builder
	var errOut strings.Builder
	code := Execute([]string{"update", "--root", root, "--output", "debug"}, &out, &errOut, "")
	if code != 0 {
		t.Fatalf("expected code 0, got %d, err=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "[debug] debug trace") {
		t.Fatalf("expected debug trace output, got: %s", out.String())
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
