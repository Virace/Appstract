package bootstrap

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveRoot(t *testing.T) {
	got, err := ResolveRoot("H:\\EnvHome", "")
	if err != nil {
		t.Fatalf("ResolveRoot returned error: %v", err)
	}
	if got != "H:\\EnvHome" {
		t.Fatalf("expected env root, got %q", got)
	}

	got, err = ResolveRoot("H:\\EnvHome", "H:\\FlagRoot")
	if err != nil {
		t.Fatalf("ResolveRoot returned error: %v", err)
	}
	if got != "H:\\FlagRoot" {
		t.Fatalf("expected flag root, got %q", got)
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
