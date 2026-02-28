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

func TestLoadOutputLevel(t *testing.T) {
	root := t.TempDir()
	content := "output_level: debug\n"
	if err := os.WriteFile(filepath.Join(root, "config.yaml"), []byte(content), 0o644); err != nil {
		t.Fatalf("write config.yaml failed: %v", err)
	}
	cfg, err := Load(root)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if cfg.OutputLevel != OutputLevelDebug {
		t.Fatalf("expected output_level=debug, got %s", cfg.OutputLevel)
	}
}

func TestLoadOutputLevelFromLegacyLogLevel(t *testing.T) {
	root := t.TempDir()
	content := "log_level: trace\n"
	if err := os.WriteFile(filepath.Join(root, "config.yaml"), []byte(content), 0o644); err != nil {
		t.Fatalf("write config.yaml failed: %v", err)
	}
	cfg, err := Load(root)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if cfg.OutputLevel != OutputLevelDebug {
		t.Fatalf("expected output_level=debug from log_level, got %s", cfg.OutputLevel)
	}
}

func TestOutputLevelOverridesLegacyLogLevel(t *testing.T) {
	root := t.TempDir()
	content := "log_level: trace\noutput_level: silent\n"
	if err := os.WriteFile(filepath.Join(root, "config.yaml"), []byte(content), 0o644); err != nil {
		t.Fatalf("write config.yaml failed: %v", err)
	}
	cfg, err := Load(root)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if cfg.OutputLevel != OutputLevelSilent {
		t.Fatalf("expected output_level=silent, got %s", cfg.OutputLevel)
	}
}
