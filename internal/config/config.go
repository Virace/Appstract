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
}

func Default() Config {
	return Config{
		KeepVersions: 2,
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
		}
	}
	if err := scanner.Err(); err != nil {
		return cfg, err
	}
	return cfg, nil
}
