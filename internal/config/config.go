package config

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type Config struct {
	KeepVersions int
	OutputLevel  OutputLevel
}

func Default() Config {
	return Config{
		KeepVersions: 2,
		OutputLevel:  OutputLevelDefault,
	}
}

type OutputLevel string

const (
	OutputLevelSilent  OutputLevel = "silent"
	OutputLevelDefault OutputLevel = "default"
	OutputLevelDebug   OutputLevel = "debug"
)

func ParseOutputLevel(raw string) (OutputLevel, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case string(OutputLevelSilent), "quiet", "none", "off", "error":
		return OutputLevelSilent, true
	case string(OutputLevelDefault), "normal", "info":
		return OutputLevelDefault, true
	case string(OutputLevelDebug), "verbose", "trace":
		return OutputLevelDebug, true
	default:
		return OutputLevelDefault, false
	}
}

func Load(root string) (Config, error) {
	cfg := Default()
	path := filepath.Join(root, "config.yaml")
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return cfg, err
	}
	defer f.Close()

	outputConfigured := false
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.Trim(strings.TrimSpace(parts[1]), `"'`)
		switch key {
		case "keep_versions":
			if n, convErr := strconv.Atoi(val); convErr == nil && n >= 0 {
				cfg.KeepVersions = n
			}
		case "output_level":
			if level, ok := ParseOutputLevel(val); ok {
				cfg.OutputLevel = level
				outputConfigured = true
			}
		case "log_level":
			// Backward compatibility for historical config key.
			if outputConfigured {
				continue
			}
			if level, ok := ParseOutputLevel(val); ok {
				cfg.OutputLevel = level
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return cfg, err
	}
	return cfg, nil
}
