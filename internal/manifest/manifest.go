package manifest

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

type Manifest struct {
	Version      string       `json:"version"`
	Description  string       `json:"description,omitempty"`
	Checkver     Checkver     `json:"checkver,omitempty"`
	Architecture Architecture `json:"architecture,omitempty"`
	Autoupdate   Autoupdate   `json:"autoupdate,omitempty"`
	Bin          string       `json:"bin"`
	Shortcuts    [][]string   `json:"shortcuts,omitempty"`
	PreInstall   []string     `json:"pre_install,omitempty"`
	Hash         string       `json:"hash,omitempty"`
}

type Checkver struct {
	GitHub  string `json:"github"`
	Regex   string `json:"regex,omitempty"`
	Replace string `json:"replace,omitempty"`
}

type Autoupdate struct {
	Architecture Architecture `json:"architecture"`
}

type Architecture struct {
	X64 Artifact `json:"64bit"`
}

type Artifact struct {
	URL        string `json:"url"`
	Hash       string `json:"hash,omitempty"`
	ExtractDir string `json:"extract_dir,omitempty"`
}

func ParseFile(path string) (*Manifest, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read manifest file: %w", err)
	}
	return ParseBytes(b)
}

func ParseBytes(b []byte) (*Manifest, error) {
	var m Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("decode manifest json: %w", err)
	}
	if err := m.Validate(); err != nil {
		return nil, err
	}
	return &m, nil
}

func (m Manifest) Validate() error {
	if m.Version == "" {
		return errors.New("manifest version is required")
	}
	if m.Bin == "" {
		return errors.New("manifest bin is required")
	}
	if _, err := m.ResolveArtifact64(); err != nil {
		return err
	}
	return nil
}

func (m Manifest) ResolveArtifact64() (Artifact, error) {
	artifact := m.Architecture.X64
	if artifact.URL == "" {
		artifact = m.Autoupdate.Architecture.X64
	}
	if artifact.URL == "" {
		return Artifact{}, errors.New("manifest 64bit artifact url is required")
	}
	if artifact.Hash == "" && m.Hash != "" {
		artifact.Hash = m.Hash
	}
	if artifact.Hash == "" {
		return Artifact{}, errors.New("manifest 64bit artifact hash is required")
	}
	return artifact, nil
}
