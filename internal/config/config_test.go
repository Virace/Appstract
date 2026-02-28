package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDefaultsWhenMissing(t *testing.T) {
	cfg, err := Load(t.TempDir())
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if cfg.KeepVersions != 2 {
		t.Fatalf("expected default keep_versions=2, got %d", cfg.KeepVersions)
	}
}

func TestLoadKeepVersions(t *testing.T) {
	root := t.TempDir()
	content := "keep_versions: 5\n"
	if err := os.WriteFile(filepath.Join(root, "config.yaml"), []byte(content), 0o644); err != nil {
		t.Fatalf("write config.yaml failed: %v", err)
	}
	cfg, err := Load(root)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if cfg.KeepVersions != 5 {
		t.Fatalf("expected keep_versions=5, got %d", cfg.KeepVersions)
	}
}
