package updater

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"appstract/internal/manifest"
)

func TestUpdateFromManifest_Success(t *testing.T) {
	root := t.TempDir()
	appName := "aria2"

	zipData := buildZip(t, map[string]string{
		"aria2-1.37.0-win-64bit-build1/aria2c.exe": "binary",
	})
	hash := sha256Hex(zipData)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(zipData)
	}))
	defer server.Close()

	manifestPath := filepath.Join(root, "aria2.json")
	manifestJSON := `{
		"version": "1.37.0-1",
		"architecture": {
			"64bit": {
				"url": "` + server.URL + `/aria2.zip",
				"hash": "` + hash + `",
				"extract_dir": "aria2-1.37.0-win-64bit-build1"
			}
		},
		"bin": "aria2c.exe"
	}`
	if err := os.WriteFile(manifestPath, []byte(manifestJSON), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	mgr := NewManager(root)
	mgr.Now = func() time.Time { return time.Date(2026, 2, 27, 12, 0, 0, 0, time.UTC) }
	if err := mgr.UpdateFromManifest(appName, manifestPath); err != nil {
		t.Fatalf("UpdateFromManifest failed: %v", err)
	}

	versionPath := filepath.Join(root, "apps", appName, "v1.37.0-1", "aria2c.exe")
	if _, err := os.Stat(versionPath); err != nil {
		t.Fatalf("expected extracted binary at %s: %v", versionPath, err)
	}

	currentPath := filepath.Join(root, "apps", appName, "current")
	if _, err := os.Stat(currentPath); err != nil {
		t.Fatalf("expected current path to exist: %v", err)
	}

	statePath := filepath.Join(root, "apps", appName, "runtime.json")
	stateBytes, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read runtime state: %v", err)
	}
	var state RuntimeState
	if err := json.Unmarshal(stateBytes, &state); err != nil {
		t.Fatalf("decode runtime state: %v", err)
	}
	if state.CurrentVersion != "1.37.0-1" {
		t.Fatalf("unexpected current version: %s", state.CurrentVersion)
	}
}

func TestUpdateFromManifest_HashMismatch(t *testing.T) {
	root := t.TempDir()
	appName := "aria2"

	zipData := buildZip(t, map[string]string{
		"aria2-1.37.0-win-64bit-build1/aria2c.exe": "binary",
	})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(zipData)
	}))
	defer server.Close()

	man := &manifest.Manifest{
		Version: "1.37.0-1",
		Architecture: manifest.Architecture{
			X64: manifest.Artifact{
				URL:        server.URL + "/aria2.zip",
				Hash:       "0000",
				ExtractDir: "aria2-1.37.0-win-64bit-build1",
			},
		},
		Bin: "aria2c.exe",
	}

	mgr := NewManager(root)
	err := mgr.Update(appName, man)
	if err == nil {
		t.Fatal("expected hash mismatch error")
	}
	statePath := filepath.Join(root, "apps", appName, "runtime.json")
	stateBytes, errRead := os.ReadFile(statePath)
	if errRead != nil {
		t.Fatalf("read runtime state: %v", errRead)
	}
	var state RuntimeState
	if err := json.Unmarshal(stateBytes, &state); err != nil {
		t.Fatalf("decode runtime state: %v", err)
	}
	if state.LastErrorCode != ErrCodePkgVerify {
		t.Fatalf("expected %s, got %s", ErrCodePkgVerify, state.LastErrorCode)
	}
}

func TestDiscoverLatest(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/aria2/aria2/releases/latest" {
			http.NotFound(w, r)
			return
		}
		_, _ = fmt.Fprint(w, `{
			"tag_name":"release-1.37.0",
			"assets":[
				{"browser_download_url":"https://github.com/aria2/aria2/releases/download/release-1.37.0/aria2-1.37.0-win-64bit-build1.zip","name":"aria2-1.37.0-win-64bit-build1.zip"}
			]
		}`)
	}))
	defer api.Close()

	mgr := NewManager(t.TempDir())
	mgr.GitHubAPIBase = api.URL

	man := &manifest.Manifest{
		Version: "1.37.0-1",
		Checkver: manifest.Checkver{
			GitHub:  "https://github.com/aria2/aria2",
			Regex:   "/release-(?:[\\d.]+)/aria2-(?<version>[\\d.]+)-win-64bit-build(?<build>[\\d]+)\\.zip",
			Replace: "${version}-${build}",
		},
		Autoupdate: manifest.Autoupdate{
			Architecture: manifest.Architecture{
				X64: manifest.Artifact{
					URL:        "https://github.com/aria2/aria2/releases/download/release-$matchVersion/aria2-$matchVersion-win-64bit-build$matchBuild.zip",
					ExtractDir: "aria2-$matchVersion-win-64bit-build$matchBuild",
				},
			},
		},
	}

	version, captures, err := mgr.DiscoverLatest(man)
	if err != nil {
		t.Fatalf("DiscoverLatest failed: %v", err)
	}
	if version != "1.37.0-1" {
		t.Fatalf("unexpected version: %s", version)
	}
	if captures["version"] != "1.37.0" || captures["build"] != "1" {
		t.Fatalf("unexpected captures: %#v", captures)
	}
}

func TestUpdateWithCheckverRejectsUnverifiableNewVersion(t *testing.T) {
	root := t.TempDir()
	appName := "aria2"

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/aria2/aria2/releases/latest" {
			http.NotFound(w, r)
			return
		}
		_, _ = fmt.Fprint(w, `{
			"tag_name":"release-1.38.0",
			"assets":[
				{"browser_download_url":"https://github.com/aria2/aria2/releases/download/release-1.38.0/aria2-1.38.0-win-64bit-build1.zip","name":"aria2-1.38.0-win-64bit-build1.zip"}
			]
		}`)
	}))
	defer api.Close()

	man := &manifest.Manifest{
		Version: "1.37.0-1",
		Checkver: manifest.Checkver{
			GitHub:  "https://github.com/aria2/aria2",
			Regex:   "/release-(?:[\\d.]+)/aria2-(?<version>[\\d.]+)-win-64bit-build(?<build>[\\d]+)\\.zip",
			Replace: "${version}-${build}",
		},
		Architecture: manifest.Architecture{
			X64: manifest.Artifact{
				URL:        "https://example.invalid/aria2-1.37.0-win-64bit-build1.zip",
				Hash:       "67d015301eef0b612191212d564c5bb0a14b5b9c4796b76454276a4d28d9b288",
				ExtractDir: "aria2-1.37.0-win-64bit-build1",
			},
		},
		Autoupdate: manifest.Autoupdate{
			Architecture: manifest.Architecture{
				X64: manifest.Artifact{
					URL:        "https://github.com/aria2/aria2/releases/download/release-$matchVersion/aria2-$matchVersion-win-64bit-build$matchBuild.zip",
					ExtractDir: "aria2-$matchVersion-win-64bit-build$matchBuild",
				},
			},
		},
		Bin: "aria2c.exe",
	}

	mgr := NewManager(root)
	mgr.UseCheckver = true
	mgr.GitHubAPIBase = api.URL
	err := mgr.Update(appName, man)
	if err == nil || !strings.Contains(err.Error(), "no verifiable hash") {
		t.Fatalf("expected no verifiable hash error, got: %v", err)
	}
}

func TestUpdate_DownloadHTTPFailureSetsPkgDownload(t *testing.T) {
	root := t.TempDir()
	appName := "aria2"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer server.Close()

	man := &manifest.Manifest{
		Version: "1.37.0-1",
		Architecture: manifest.Architecture{
			X64: manifest.Artifact{
				URL:        server.URL + "/aria2.zip",
				Hash:       "67d015301eef0b612191212d564c5bb0a14b5b9c4796b76454276a4d28d9b288",
				ExtractDir: "aria2-1.37.0-win-64bit-build1",
			},
		},
		Bin: "aria2c.exe",
	}

	mgr := NewManager(root)
	err := mgr.Update(appName, man)
	if err == nil || !strings.Contains(err.Error(), ErrCodeNetDownload) {
		t.Fatalf("expected NET_DOWNLOAD failure, got: %v", err)
	}
	statePath := filepath.Join(root, "apps", appName, "runtime.json")
	stateBytes, readErr := os.ReadFile(statePath)
	if readErr != nil {
		t.Fatalf("read runtime state: %v", readErr)
	}
	var state RuntimeState
	if err := json.Unmarshal(stateBytes, &state); err != nil {
		t.Fatalf("decode runtime state: %v", err)
	}
	if state.LastErrorCode != ErrCodePkgDownload {
		t.Fatalf("expected %s, got %s", ErrCodePkgDownload, state.LastErrorCode)
	}
}

func TestUpdate_CorruptArchiveSetsPkgExtract(t *testing.T) {
	root := t.TempDir()
	appName := "aria2"
	corrupt := []byte("not a zip")
	sum := sha256.Sum256(corrupt)
	hash := hex.EncodeToString(sum[:])

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(w, bytes.NewReader(corrupt))
	}))
	defer server.Close()

	man := &manifest.Manifest{
		Version: "1.37.0-1",
		Architecture: manifest.Architecture{
			X64: manifest.Artifact{
				URL:        server.URL + "/aria2.zip",
				Hash:       hash,
				ExtractDir: "aria2-1.37.0-win-64bit-build1",
			},
		},
		Bin: "aria2c.exe",
	}
	mgr := NewManager(root)
	err := mgr.Update(appName, man)
	if err == nil || !strings.Contains(err.Error(), "open zip") {
		t.Fatalf("expected zip open error, got: %v", err)
	}

	statePath := filepath.Join(root, "apps", appName, "runtime.json")
	stateBytes, readErr := os.ReadFile(statePath)
	if readErr != nil {
		t.Fatalf("read runtime state: %v", readErr)
	}
	var state RuntimeState
	if err := json.Unmarshal(stateBytes, &state); err != nil {
		t.Fatalf("decode runtime state: %v", err)
	}
	if state.LastErrorCode != ErrCodePkgExtract {
		t.Fatalf("expected %s, got %s", ErrCodePkgExtract, state.LastErrorCode)
	}
}

func TestSwitchCurrentFallsBackToMarker(t *testing.T) {
	root := t.TempDir()
	current := filepath.Join(root, "current")
	versionDir := filepath.Join(root, "v1")
	if err := os.MkdirAll(versionDir, 0o755); err != nil {
		t.Fatalf("mkdir version dir: %v", err)
	}

	old := junctionCreator
	junctionCreator = func(linkPath, targetPath string) error {
		return fmt.Errorf("force fallback")
	}
	t.Cleanup(func() { junctionCreator = old })

	if err := switchCurrent(current, versionDir); err != nil {
		t.Fatalf("switchCurrent failed: %v", err)
	}
	marker := filepath.Join(current, ".appstract-target")
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("expected marker fallback file: %v", err)
	}
}

func TestUpdate_PreInstallSuccess(t *testing.T) {
	root := t.TempDir()
	appName := "aria2"

	zipData := buildZip(t, map[string]string{
		"aria2-1.37.0-win-64bit-build1/aria2c.exe": "binary",
	})
	hash := sha256Hex(zipData)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(zipData)
	}))
	defer server.Close()

	man := &manifest.Manifest{
		Version: "1.37.0-1",
		Architecture: manifest.Architecture{
			X64: manifest.Artifact{
				URL:        server.URL + "/aria2.zip",
				Hash:       hash,
				ExtractDir: "aria2-1.37.0-win-64bit-build1",
			},
		},
		Bin: "aria2c.exe",
		PreInstall: []string{
			`Set-Content -Path "$dir\preinstall.txt" -Value "ok"`,
		},
	}

	mgr := NewManager(root)
	if err := mgr.Update(appName, man); err != nil {
		t.Fatalf("Update failed: %v", err)
	}
	markerPath := filepath.Join(root, "apps", appName, "v1.37.0-1", "preinstall.txt")
	if _, err := os.Stat(markerPath); err != nil {
		t.Fatalf("expected pre_install marker file: %v", err)
	}
}

func TestUpdate_PreInstallFailure(t *testing.T) {
	root := t.TempDir()
	appName := "aria2"

	zipData := buildZip(t, map[string]string{
		"aria2-1.37.0-win-64bit-build1/aria2c.exe": "binary",
	})
	hash := sha256Hex(zipData)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(zipData)
	}))
	defer server.Close()

	man := &manifest.Manifest{
		Version: "1.37.0-1",
		Architecture: manifest.Architecture{
			X64: manifest.Artifact{
				URL:        server.URL + "/aria2.zip",
				Hash:       hash,
				ExtractDir: "aria2-1.37.0-win-64bit-build1",
			},
		},
		Bin: "aria2c.exe",
		PreInstall: []string{
			`throw "boom"`,
		},
	}

	mgr := NewManager(root)
	err := mgr.Update(appName, man)
	if err == nil || !strings.Contains(err.Error(), "pre_install failed") {
		t.Fatalf("expected pre_install failed, got: %v", err)
	}
	statePath := filepath.Join(root, "apps", appName, "runtime.json")
	stateBytes, errRead := os.ReadFile(statePath)
	if errRead != nil {
		t.Fatalf("read runtime state: %v", errRead)
	}
	var state RuntimeState
	if err := json.Unmarshal(stateBytes, &state); err != nil {
		t.Fatalf("decode runtime state: %v", err)
	}
	if state.LastErrorCode != ErrCodeScriptPreInstall {
		t.Fatalf("expected %s, got %s", ErrCodeScriptPreInstall, state.LastErrorCode)
	}
	logs, err := filepath.Glob(filepath.Join(root, "apps", appName, "logs", "preinstall-*.log"))
	if err != nil {
		t.Fatalf("glob logs failed: %v", err)
	}
	if len(logs) == 0 {
		t.Fatal("expected preinstall log file")
	}
}

func TestUpdate_CleanupOldVersions(t *testing.T) {
	root := t.TempDir()
	appName := "aria2"
	appDir := filepath.Join(root, "apps", appName)
	for _, v := range []string{"v1.0.0", "v1.1.0", "v1.2.0"} {
		path := filepath.Join(appDir, v)
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatalf("mkdir %s failed: %v", v, err)
		}
		time.Sleep(10 * time.Millisecond)
	}

	zipData := buildZip(t, map[string]string{
		"aria2-1.37.0-win-64bit-build1/aria2c.exe": "binary",
	})
	hash := sha256Hex(zipData)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(zipData)
	}))
	defer server.Close()

	man := &manifest.Manifest{
		Version: "1.37.0-1",
		Architecture: manifest.Architecture{
			X64: manifest.Artifact{
				URL:        server.URL + "/aria2.zip",
				Hash:       hash,
				ExtractDir: "aria2-1.37.0-win-64bit-build1",
			},
		},
		Bin: "aria2c.exe",
	}

	mgr := NewManager(root)
	mgr.KeepVersions = 1
	if err := mgr.Update(appName, man); err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(appDir, "v1.37.0-1")); err != nil {
		t.Fatalf("expected current version dir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(appDir, "v1.2.0")); err != nil {
		t.Fatalf("expected newest old version retained: %v", err)
	}
	if _, err := os.Stat(filepath.Join(appDir, "v1.1.0")); !os.IsNotExist(err) {
		t.Fatalf("expected v1.1.0 removed, err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(appDir, "v1.0.0")); !os.IsNotExist(err) {
		t.Fatalf("expected v1.0.0 removed, err=%v", err)
	}
}

func TestUpdate_NoNewVersionStillCleanupOldVersions(t *testing.T) {
	root := t.TempDir()
	appName := "aria2"
	appDir := filepath.Join(root, "apps", appName)
	if err := os.MkdirAll(filepath.Join(appDir, "v1.37.0-1"), 0o755); err != nil {
		t.Fatalf("mkdir current version failed: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(appDir, "v0.9.0"), 0o755); err != nil {
		t.Fatalf("mkdir v0.9.0 failed: %v", err)
	}
	time.Sleep(10 * time.Millisecond)
	if err := os.MkdirAll(filepath.Join(appDir, "v1.0.0"), 0o755); err != nil {
		t.Fatalf("mkdir v1.0.0 failed: %v", err)
	}
	state := RuntimeState{CurrentVersion: "1.37.0-1"}
	stateBytes, _ := json.Marshal(state)
	if err := os.WriteFile(filepath.Join(appDir, "runtime.json"), stateBytes, 0o644); err != nil {
		t.Fatalf("write runtime state failed: %v", err)
	}

	man := &manifest.Manifest{
		Version: "1.37.0-1",
		Architecture: manifest.Architecture{
			X64: manifest.Artifact{
				URL:  "https://example.invalid/aria2.zip",
				Hash: "67d015301eef0b612191212d564c5bb0a14b5b9c4796b76454276a4d28d9b288",
			},
		},
		Bin: "aria2c.exe",
	}
	mgr := NewManager(root)
	mgr.KeepVersions = 1
	if err := mgr.Update(appName, man); err != nil {
		t.Fatalf("Update failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(appDir, "v1.0.0")); err != nil {
		t.Fatalf("expected newest old version retained: %v", err)
	}
	if _, err := os.Stat(filepath.Join(appDir, "v0.9.0")); !os.IsNotExist(err) {
		t.Fatalf("expected v0.9.0 removed, err=%v", err)
	}
}

func TestUpdate_RollbackOnRelaunchFailure(t *testing.T) {
	root := t.TempDir()
	appName := "aria2"
	appDir := filepath.Join(root, "apps", appName)

	oldVersionDir := filepath.Join(appDir, "v1.0.0")
	if err := os.MkdirAll(oldVersionDir, 0o755); err != nil {
		t.Fatalf("mkdir old version failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(oldVersionDir, "aria2c.exe"), []byte("old"), 0o644); err != nil {
		t.Fatalf("write old bin failed: %v", err)
	}

	currentPath := filepath.Join(appDir, "current")
	if err := switchCurrent(currentPath, oldVersionDir); err != nil {
		t.Fatalf("switch current to old failed: %v", err)
	}

	zipData := buildZip(t, map[string]string{
		"aria2-1.37.0-win-64bit-build1/aria2c.exe": "new",
	})
	hash := sha256Hex(zipData)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(zipData)
	}))
	defer server.Close()

	man := &manifest.Manifest{
		Version: "1.37.0-1",
		Architecture: manifest.Architecture{
			X64: manifest.Artifact{
				URL:        server.URL + "/aria2.zip",
				Hash:       hash,
				ExtractDir: "aria2-1.37.0-win-64bit-build1",
			},
		},
		Bin: "aria2c.exe",
	}

	mgr := NewManager(root)
	mgr.Relaunch = true
	mgr.findPIDs = func(prefix string) ([]int, error) { return nil, nil }
	mgr.killPID = func(pid int, force bool) error { return nil }
	mgr.launch = func(path string) error { return fmt.Errorf("launch failed") }

	err := mgr.Update(appName, man)
	if err == nil || !strings.Contains(err.Error(), "relaunch failed") {
		t.Fatalf("expected relaunch error, got: %v", err)
	}

	target, err := resolveCurrentTarget(currentPath)
	if err != nil {
		t.Fatalf("resolveCurrentTarget failed: %v", err)
	}
	if target != oldVersionDir {
		t.Fatalf("expected rollback to %s, got %s", oldVersionDir, target)
	}

	stateBytes, err := os.ReadFile(filepath.Join(appDir, "runtime.json"))
	if err != nil {
		t.Fatalf("read runtime state failed: %v", err)
	}
	var state RuntimeState
	if err := json.Unmarshal(stateBytes, &state); err != nil {
		t.Fatalf("decode runtime state failed: %v", err)
	}
	if state.LastErrorCode != ErrCodeSwitchHealthcheck {
		t.Fatalf("expected %s, got %s", ErrCodeSwitchHealthcheck, state.LastErrorCode)
	}
}

func TestTerminateProcesses_GracefulThenForce(t *testing.T) {
	mgr := NewManager(t.TempDir())
	mgr.StopTimeout = 1 * time.Millisecond

	findCalls := 0
	mgr.findPIDs = func(prefix string) ([]int, error) {
		findCalls++
		if findCalls == 1 {
			return []int{101, 202}, nil
		}
		return []int{202}, nil
	}

	var graceful []int
	mgr.closePID = func(pid int) error {
		graceful = append(graceful, pid)
		return nil
	}

	var soft []int
	var forced []int
	mgr.killPID = func(pid int, force bool) error {
		if force {
			forced = append(forced, pid)
			return nil
		}
		soft = append(soft, pid)
		return nil
	}

	if err := mgr.terminateProcesses("aria2", `C:\apps\aria2\current`); err != nil {
		t.Fatalf("terminateProcesses failed: %v", err)
	}
	if len(graceful) != 2 {
		t.Fatalf("expected 2 graceful close attempts, got %d", len(graceful))
	}
	if len(soft) != 2 {
		t.Fatalf("expected 2 soft kill attempts, got %d", len(soft))
	}
	if len(forced) != 1 || forced[0] != 202 {
		t.Fatalf("expected force kill on remaining pid 202, got %#v", forced)
	}
}

func TestUpdate_WritesSwitchLog(t *testing.T) {
	root := t.TempDir()
	appName := "aria2"

	zipData := buildZip(t, map[string]string{
		"aria2-1.37.0-win-64bit-build1/aria2c.exe": "binary",
	})
	hash := sha256Hex(zipData)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(zipData)
	}))
	defer server.Close()

	man := &manifest.Manifest{
		Version: "1.37.0-1",
		Architecture: manifest.Architecture{
			X64: manifest.Artifact{
				URL:        server.URL + "/aria2.zip",
				Hash:       hash,
				ExtractDir: "aria2-1.37.0-win-64bit-build1",
			},
		},
		Bin: "aria2c.exe",
	}

	mgr := NewManager(root)
	mgr.Now = func() time.Time { return time.Date(2026, 2, 27, 12, 0, 0, 0, time.UTC) }
	mgr.findPIDs = func(prefix string) ([]int, error) { return nil, nil }
	mgr.closePID = func(pid int) error { return nil }
	mgr.killPID = func(pid int, force bool) error { return nil }

	if err := mgr.Update(appName, man); err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	logPath := filepath.Join(root, "apps", appName, "logs", "events-20260227.log")
	b, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read event log failed: %v", err)
	}
	logText := string(b)
	if !strings.Contains(logText, `"event":"SWITCH_DONE"`) {
		t.Fatalf("expected SWITCH_DONE event in log, got: %s", logText)
	}
	if !strings.Contains(logText, `"event":"SWITCH_PROCESS_NONE"`) {
		t.Fatalf("expected SWITCH_PROCESS_NONE event in log, got: %s", logText)
	}
	if !strings.Contains(logText, `"stage":"update"`) {
		t.Fatalf("expected stage=update event in log, got: %s", logText)
	}
}

func TestAcquireLock_StaleLockRecovered(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), ".lock")
	if err := os.WriteFile(lockPath, []byte(`{"pid":42424242,"created_at":"2026-02-27T12:00:00Z"}`), 0o644); err != nil {
		t.Fatalf("write stale lock failed: %v", err)
	}
	oldChecker := lockPIDRunning
	lockPIDRunning = func(pid int) (bool, error) { return false, nil }
	t.Cleanup(func() { lockPIDRunning = oldChecker })

	if err := acquireLock(lockPath); err != nil {
		t.Fatalf("acquireLock should recover stale lock: %v", err)
	}
	t.Cleanup(func() { releaseLock(lockPath) })

	b, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("read recovered lock failed: %v", err)
	}
	if !strings.Contains(string(b), `"pid":`) {
		t.Fatalf("expected lock metadata with pid, got: %s", string(b))
	}
}

func TestAcquireLock_ActiveLockRejected(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), ".lock")
	if err := os.WriteFile(lockPath, []byte(`{"pid":100,"created_at":"2026-02-27T12:00:00Z"}`), 0o644); err != nil {
		t.Fatalf("write active lock failed: %v", err)
	}
	oldChecker := lockPIDRunning
	lockPIDRunning = func(pid int) (bool, error) { return true, nil }
	t.Cleanup(func() { lockPIDRunning = oldChecker })

	err := acquireLock(lockPath)
	if err == nil || !strings.Contains(err.Error(), "update already running") {
		t.Fatalf("expected update already running, got: %v", err)
	}
}

func TestAcquireLock_ConcurrentContention(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), ".lock")
	oldChecker := lockPIDRunning
	lockPIDRunning = func(pid int) (bool, error) { return true, nil }
	t.Cleanup(func() { lockPIDRunning = oldChecker })

	const workers = 8
	var wg sync.WaitGroup
	results := make(chan error, workers)

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := acquireLock(lockPath)
			if err == nil {
				// Keep lock briefly to maximize contention window.
				time.Sleep(10 * time.Millisecond)
				releaseLock(lockPath)
			}
			results <- err
		}()
	}
	wg.Wait()
	close(results)

	success := 0
	busy := 0
	for err := range results {
		if err == nil {
			success++
			continue
		}
		if strings.Contains(err.Error(), "update already running") {
			busy++
			continue
		}
		t.Fatalf("unexpected lock error: %v", err)
	}
	if success == 0 {
		t.Fatalf("expected at least one successful lock acquire, got %d", success)
	}
	if busy == 0 {
		t.Fatalf("expected lock contention failures, got %d", busy)
	}
}

func TestLockFileIsStale_EmptyAndInvalidContent(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, ".lock")

	if err := os.WriteFile(lockPath, []byte(""), 0o644); err != nil {
		t.Fatalf("write empty lock failed: %v", err)
	}
	stale, err := lockFileIsStale(lockPath)
	if err != nil {
		t.Fatalf("lockFileIsStale empty failed: %v", err)
	}
	if stale {
		t.Fatal("expected empty lock content to be treated as active/non-stale")
	}

	if err := os.WriteFile(lockPath, []byte("{not-json"), 0o644); err != nil {
		t.Fatalf("write invalid lock failed: %v", err)
	}
	stale, err = lockFileIsStale(lockPath)
	if err != nil {
		t.Fatalf("lockFileIsStale invalid json failed: %v", err)
	}
	if stale {
		t.Fatal("expected invalid lock metadata to be treated as active/non-stale")
	}
}

func TestLockFileIsStale_PIDCheckError(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), ".lock")
	if err := os.WriteFile(lockPath, []byte(`{"pid":321,"created_at":"2026-02-27T12:00:00Z"}`), 0o644); err != nil {
		t.Fatalf("write lock failed: %v", err)
	}
	oldChecker := lockPIDRunning
	lockPIDRunning = func(pid int) (bool, error) { return false, fmt.Errorf("probe error") }
	t.Cleanup(func() { lockPIDRunning = oldChecker })

	stale, err := lockFileIsStale(lockPath)
	if err != nil {
		t.Fatalf("lockFileIsStale should swallow pid checker error, got: %v", err)
	}
	if stale {
		t.Fatal("expected PID checker error branch to keep lock as non-stale")
	}
}

func buildZip(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("create zip entry: %v", err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatalf("write zip entry: %v", err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	return buf.Bytes()
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
