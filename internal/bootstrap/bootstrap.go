package bootstrap

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var requiredDirs = []string{
	"manifests",
	"shims",
	"scripts",
	"apps",
}

// ErrInitRequired indicates command workspace must be initialized by `appstract init`.
var ErrInitRequired = errors.New("workspace not initialized")

type LayoutState struct {
	Complete      bool
	MissingDirs   []string
	BinaryOnly    bool
	RootNotExists bool
}

const defaultConfigYAML = `github_token: ""
proxy: ""
check_ttl_seconds: 3600
keep_versions: 2
output_level: "default"
download_timeout_seconds: 120
max_retry: 3
log_level: "info"
`

func ResolveRoot(envHome, flagRoot, executablePath string) (string, error) {
	if flagRoot != "" {
		return flagRoot, nil
	}
	if envHome != "" {
		return envHome, nil
	}
	if executablePath != "" {
		return filepath.Dir(executablePath), nil
	}
	wd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("resolve current working directory: %w", err)
	}
	return wd, nil
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

func InspectLayout(root, executablePath string) (LayoutState, error) {
	var state LayoutState
	if root == "" {
		return state, errors.New("root cannot be empty")
	}
	info, err := os.Stat(root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			state.RootNotExists = true
			state.MissingDirs = append(state.MissingDirs, requiredDirs...)
			return state, nil
		}
		return state, fmt.Errorf("stat root: %w", err)
	}
	if !info.IsDir() {
		return state, fmt.Errorf("root is not a directory: %s", root)
	}

	for _, dir := range requiredDirs {
		dirPath := filepath.Join(root, dir)
		dirInfo, dirErr := os.Stat(dirPath)
		if dirErr != nil {
			if errors.Is(dirErr, os.ErrNotExist) {
				state.MissingDirs = append(state.MissingDirs, dir)
				continue
			}
			return state, fmt.Errorf("stat required directory %s: %w", dir, dirErr)
		}
		if !dirInfo.IsDir() {
			state.MissingDirs = append(state.MissingDirs, dir)
		}
	}
	if len(state.MissingDirs) == 0 {
		state.Complete = true
		return state, nil
	}

	entries, err := os.ReadDir(root)
	if err != nil {
		return state, fmt.Errorf("read root directory: %w", err)
	}
	state.BinaryOnly = isEmptyOrBinaryOnly(entries, executablePath)
	return state, nil
}

func RepairLayout(root string, missingDirs []string) error {
	if root == "" {
		return errors.New("root cannot be empty")
	}
	if len(missingDirs) == 0 {
		return nil
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return fmt.Errorf("create root: %w", err)
	}
	for _, dir := range missingDirs {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			return fmt.Errorf("repair directory %s: %w", dir, err)
		}
	}
	return nil
}

func EnsureReadyForCommand(root, executablePath string) error {
	state, err := InspectLayout(root, executablePath)
	if err != nil {
		return err
	}
	if state.Complete {
		return nil
	}
	if state.BinaryOnly {
		return fmt.Errorf("%w at %s, please run: appstract init --root %s", ErrInitRequired, root, root)
	}
	return RepairLayout(root, state.MissingDirs)
}

func isEmptyOrBinaryOnly(entries []os.DirEntry, executablePath string) bool {
	if len(entries) == 0 {
		return true
	}
	exeName := filepath.Base(executablePath)
	exeNameLower := strings.ToLower(exeName)
	for _, entry := range entries {
		if entry.IsDir() {
			return false
		}
		nameLower := strings.ToLower(entry.Name())
		if exeNameLower == "" {
			return false
		}
		if nameLower != exeNameLower {
			return false
		}
	}
	return true
}
