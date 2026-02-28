package bootstrap

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveRoot(t *testing.T) {
	got, err := ResolveRoot("H:\\EnvHome", "H:\\FlagRoot", "H:\\Bin\\appstract.exe")
	if err != nil {
		t.Fatalf("ResolveRoot returned error: %v", err)
	}
	if got != "H:\\FlagRoot" {
		t.Fatalf("expected flag root, got %q", got)
	}

	got, err = ResolveRoot("H:\\EnvHome", "", "H:\\Bin\\appstract.exe")
	if err != nil {
		t.Fatalf("ResolveRoot returned error: %v", err)
	}
	if got != "H:\\EnvHome" {
		t.Fatalf("expected env root, got %q", got)
	}

	got, err = ResolveRoot("", "", "/tmp/appstract/appstract")
	if err != nil {
		t.Fatalf("ResolveRoot returned error: %v", err)
	}
	if got != "/tmp/appstract" {
		t.Fatalf("expected executable directory root, got %q", got)
	}

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd failed: %v", err)
	}
	got, err = ResolveRoot("", "", "")
	if err != nil {
		t.Fatalf("ResolveRoot returned error: %v", err)
	}
	if got != wd {
		t.Fatalf("expected cwd root %q, got %q", wd, got)
	}
}

func TestInitLayoutCreatesStructureAndIsIdempotent(t *testing.T) {
	root := t.TempDir()

	if err := InitLayout(root); err != nil {
		t.Fatalf("InitLayout first call failed: %v", err)
	}
	if err := InitLayout(root); err != nil {
		t.Fatalf("InitLayout second call failed: %v", err)
	}

	for _, dir := range []string{"manifests", "shims", "scripts", "apps"} {
		path := filepath.Join(root, dir)
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("expected %s exists: %v", path, err)
		}
		if !info.IsDir() {
			t.Fatalf("expected %s to be directory", path)
		}
	}

	configPath := filepath.Join(root, "config.yaml")
	b, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("expected config.yaml: %v", err)
	}
	if len(b) == 0 {
		t.Fatalf("config.yaml should not be empty")
	}
}

func TestInitLayoutEmptyRoot(t *testing.T) {
	if err := InitLayout(""); err == nil {
		t.Fatal("expected error for empty root")
	}
}

func TestInspectLayoutComplete(t *testing.T) {
	root := t.TempDir()
	if err := InitLayout(root); err != nil {
		t.Fatalf("InitLayout failed: %v", err)
	}

	state, err := InspectLayout(root, filepath.Join(root, "appstract.exe"))
	if err != nil {
		t.Fatalf("InspectLayout failed: %v", err)
	}
	if !state.Complete {
		t.Fatalf("expected complete layout, got %+v", state)
	}
	if len(state.MissingDirs) != 0 {
		t.Fatalf("expected no missing dirs, got %v", state.MissingDirs)
	}
}

func TestInspectLayoutRootNotExists(t *testing.T) {
	root := filepath.Join(t.TempDir(), "missing")
	state, err := InspectLayout(root, filepath.Join(root, "appstract.exe"))
	if err != nil {
		t.Fatalf("InspectLayout failed: %v", err)
	}
	if !state.RootNotExists {
		t.Fatalf("expected RootNotExists=true, got %+v", state)
	}
	if len(state.MissingDirs) != len(requiredDirs) {
		t.Fatalf("expected %d missing dirs, got %d", len(requiredDirs), len(state.MissingDirs))
	}
}

func TestEnsureReadyForCommandRepairsMissingDirs(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "apps"), 0o755); err != nil {
		t.Fatalf("mkdir apps failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "extra.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write extra file failed: %v", err)
	}

	if err := EnsureReadyForCommand(root, filepath.Join(root, "appstract.exe")); err != nil {
		t.Fatalf("EnsureReadyForCommand failed: %v", err)
	}

	for _, dir := range requiredDirs {
		if info, err := os.Stat(filepath.Join(root, dir)); err != nil || !info.IsDir() {
			t.Fatalf("expected repaired directory %s, err=%v", dir, err)
		}
	}
}

func TestEnsureReadyForCommandBinaryOnlyRequiresInit(t *testing.T) {
	root := t.TempDir()
	exePath := filepath.Join(root, "appstract.exe")
	if err := os.WriteFile(exePath, []byte("binary"), 0o755); err != nil {
		t.Fatalf("write exe failed: %v", err)
	}

	err := EnsureReadyForCommand(root, exePath)
	if err == nil {
		t.Fatal("expected init required error")
	}
	if !errors.Is(err, ErrInitRequired) {
		t.Fatalf("expected ErrInitRequired, got %v", err)
	}
	if !strings.Contains(err.Error(), "please run: appstract init") {
		t.Fatalf("unexpected error text: %v", err)
	}
}
