package bootstrap

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const defaultRoot = `D:\Appstract`

var requiredDirs = []string{
	"manifests",
	"shims",
	"scripts",
	"apps",
}

const defaultConfigYAML = `github_token: ""
proxy: ""
check_ttl_seconds: 3600
keep_versions: 2
download_timeout_seconds: 120
max_retry: 3
log_level: "info"
`

func ResolveRoot(envHome, flagRoot string) (string, error) {
	if flagRoot != "" {
		return flagRoot, nil
	}
	if envHome != "" {
		return envHome, nil
	}
	return defaultRoot, nil
}

func InitLayout(root string) error {
	if root == "" {
		return errors.New("root cannot be empty")
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return fmt.Errorf("create root: %w", err)
	}
	for _, dir := range requiredDirs {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			return fmt.Errorf("create directory %s: %w", dir, err)
		}
	}
	configPath := filepath.Join(root, "config.yaml")
	if _, err := os.Stat(configPath); errors.Is(err, os.ErrNotExist) {
		if err := os.WriteFile(configPath, []byte(defaultConfigYAML), 0o644); err != nil {
			return fmt.Errorf("write config.yaml: %w", err)
		}
	} else if err != nil {
		return fmt.Errorf("stat config.yaml: %w", err)
	}
	return nil
}
