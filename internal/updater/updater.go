package updater

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"appstract/internal/manifest"
	"appstract/internal/winui"
)

type RuntimeState struct {
	CurrentVersion string `json:"current_version"`
	LastCheckAt    string `json:"last_check_at,omitempty"`
	LastUpdateAt   string `json:"last_update_at,omitempty"`
	LastErrorCode  string `json:"last_error_code,omitempty"`
	LastErrorMsg   string `json:"last_error_message,omitempty"`
	PendingVersion string `json:"pending_version,omitempty"`
}

type Manager struct {
	Root          string
	Client        *http.Client
	Now           func() time.Time
	UseCheckver   bool
	GitHubAPIBase string
	ScriptTimeout time.Duration
	KeepVersions  int
	PromptSwitch  bool
	Relaunch      bool
	StopTimeout   time.Duration

	findPIDs func(prefix string) ([]int, error)
	closePID func(pid int) error
	killPID  func(pid int, force bool) error
	launch   func(path string) error
	confirm  func(appName, version string) (bool, error)
}

type switchLogEvent struct {
	Timestamp string `json:"timestamp"`
	App       string `json:"app"`
	Stage     string `json:"stage"`
	Event     string `json:"event"`
	ErrorCode string `json:"error_code,omitempty"`
	Message   string `json:"message,omitempty"`
}

type githubRelease struct {
	TagName string        `json:"tag_name"`
	Assets  []githubAsset `json:"assets"`
}

type githubAsset struct {
	BrowserDownloadURL string `json:"browser_download_url"`
	Name               string `json:"name"`
}

var junctionCreator = createJunction

func NewManager(root string) *Manager {
	return &Manager{
		Root: root,
		Client: &http.Client{
			Timeout: 2 * time.Minute,
		},
		Now:           time.Now,
		GitHubAPIBase: "https://api.github.com",
		ScriptTimeout: 2 * time.Minute,
		KeepVersions:  2,
		StopTimeout:   10 * time.Second,
		findPIDs:      findRunningPIDsByPrefix,
		closePID:      gracefulCloseByPID,
		killPID:       killProcessByPID,
		launch:        launchDetached,
		confirm:       winui.ConfirmUpdateReady,
	}
}

func (m *Manager) UpdateFromManifest(appName, manifestPath string) error {
	man, err := manifest.ParseFile(manifestPath)
	if err != nil {
		return err
	}
	return m.Update(appName, man)
}

func (m *Manager) Update(appName string, man *manifest.Manifest) error {
	if appName == "" {
		return fmt.Errorf("app name is required")
	}
	if man == nil {
		return fmt.Errorf("manifest is required")
	}

	effective := *man
	if m.UseCheckver {
		if err := m.applyCheckver(&effective); err != nil {
			return err
		}
	}

	artifact, err := effective.ResolveArtifact64()
	if err != nil {
		return err
	}
	_ = m.logEvent(appName, "update", "UPDATE_BEGIN", "", "update transaction started")

	lockPath := filepath.Join(m.Root, "apps", appName, ".lock")
	if err := acquireLock(lockPath); err != nil {
		return err
	}
	defer releaseLock(lockPath)

	statePath := filepath.Join(m.Root, "apps", appName, "runtime.json")
	state, err := loadState(statePath)
	if err != nil {
		return err
	}
	state.LastCheckAt = m.Now().UTC().Format(time.RFC3339)

	if state.CurrentVersion == effective.Version && state.CurrentVersion != "" {
		state.PendingVersion = ""
		if err := saveState(statePath, state); err != nil {
			return err
		}
		return m.cleanupOldVersions(appName, effective.Version)
	}

	staging := filepath.Join(m.Root, "apps", appName, "_staging", "v"+effective.Version)
	versionDir := filepath.Join(m.Root, "apps", appName, "v"+effective.Version)
	if err := os.RemoveAll(staging); err != nil {
		return fmt.Errorf("cleanup old staging: %w", err)
	}
	if err := os.MkdirAll(staging, 0o755); err != nil {
		return fmt.Errorf("create staging: %w", err)
	}

	state.PendingVersion = effective.Version
	if err := saveState(statePath, state); err != nil {
		return err
	}

	archivePath := filepath.Join(staging, "package.zip")
	_ = m.logEvent(appName, "download", "PKG_DOWNLOAD_BEGIN", "", artifact.URL)
	if err := m.download(artifact.URL, archivePath); err != nil {
		state.PendingVersion = ""
		state.LastErrorCode = ErrCodePkgDownload
		state.LastErrorMsg = err.Error()
		_ = m.logEvent(appName, "download", "PKG_DOWNLOAD_FAILED", state.LastErrorCode, err.Error())
		_ = saveState(statePath, state)
		return err
	}
	_ = m.logEvent(appName, "download", "PKG_DOWNLOAD_DONE", "", archivePath)
	if err := verifySHA256(archivePath, artifact.Hash); err != nil {
		state.PendingVersion = ""
		state.LastErrorCode = ErrCodePkgVerify
		state.LastErrorMsg = err.Error()
		_ = m.logEvent(appName, "verify", "PKG_VERIFY_FAILED", state.LastErrorCode, err.Error())
		_ = saveState(statePath, state)
		return err
	}
	_ = m.logEvent(appName, "verify", "PKG_VERIFY_DONE", "", "sha256 verified")

	extractedRoot := filepath.Join(staging, "extracted")
	if err := unzip(archivePath, extractedRoot); err != nil {
		state.PendingVersion = ""
		state.LastErrorCode = ErrCodePkgExtract
		state.LastErrorMsg = err.Error()
		_ = m.logEvent(appName, "extract", "PKG_EXTRACT_FAILED", state.LastErrorCode, err.Error())
		_ = saveState(statePath, state)
		return err
	}
	_ = m.logEvent(appName, "extract", "PKG_EXTRACT_DONE", "", extractedRoot)

	sourceDir := extractedRoot
	if artifact.ExtractDir != "" {
		sourceDir = filepath.Join(extractedRoot, artifact.ExtractDir)
	}
	if _, err := os.Stat(sourceDir); err != nil {
		return fmt.Errorf("source extract directory missing: %w", err)
	}
	_ = m.logEvent(appName, "script", "SCRIPT_PREINSTALL_BEGIN", "", "running pre_install hooks")
	if err := m.runPreInstall(appName, sourceDir, effective.PreInstall); err != nil {
		state.PendingVersion = ""
		state.LastErrorCode = ErrCodeScriptPreInstall
		state.LastErrorMsg = err.Error()
		_ = m.logEvent(appName, "script", "SCRIPT_PREINSTALL_FAILED", state.LastErrorCode, err.Error())
		_ = saveState(statePath, state)
		return err
	}
	_ = m.logEvent(appName, "script", "SCRIPT_PREINSTALL_DONE", "", "pre_install completed")

	if err := os.RemoveAll(versionDir); err != nil {
		return fmt.Errorf("cleanup version dir: %w", err)
	}
	if err := os.Rename(sourceDir, versionDir); err != nil {
		return fmt.Errorf("move extracted version: %w", err)
	}

	currentPath := filepath.Join(m.Root, "apps", appName, "current")
	prevTarget, _ := resolveCurrentTarget(currentPath)
	if m.PromptSwitch {
		confirmFn := m.confirm
		if confirmFn == nil {
			confirmFn = winui.ConfirmUpdateReady
		}
		approved, err := confirmFn(appName, effective.Version)
		if err != nil {
			state.PendingVersion = ""
			state.LastErrorCode = ErrCodeSwitchPrompt
			state.LastErrorMsg = err.Error()
			_ = saveState(statePath, state)
			return err
		}
		if !approved {
			state.PendingVersion = ""
			_ = m.logEvent(appName, "switch", "SWITCH_USER_DECLINED", "", "user declined immediate switch")
			return saveState(statePath, state)
		}
	}
	_ = m.logEvent(appName, "switch", "SWITCH_PROCESS_BEGIN", "", "begin process stop for current path")
	if err := m.terminateProcesses(appName, currentPath); err != nil {
		state.PendingVersion = ""
		state.LastErrorCode = ErrCodeSwitchProcess
		state.LastErrorMsg = err.Error()
		_ = m.logEvent(appName, "switch", "SWITCH_PROCESS_FAILED", state.LastErrorCode, err.Error())
		_ = saveState(statePath, state)
		return err
	}
	_ = m.logEvent(appName, "switch", "SWITCH_PROCESS_DONE", "", "target processes stopped")
	if err := switchCurrent(currentPath, versionDir); err != nil {
		state.PendingVersion = ""
		state.LastErrorCode = ErrCodeSwitchCurrent
		state.LastErrorMsg = err.Error()
		_ = m.logEvent(appName, "switch", "SWITCH_CURRENT_FAILED", state.LastErrorCode, err.Error())
		_ = saveState(statePath, state)
		return err
	}
	_ = m.logEvent(appName, "switch", "SWITCH_CURRENT_DONE", "", "current version switched")
	if err := m.healthcheckAndRelaunch(currentPath, effective.Bin); err != nil {
		rollbackErr := rollbackCurrent(currentPath, prevTarget)
		state.PendingVersion = ""
		state.LastErrorCode = ErrCodeSwitchHealthcheck
		state.LastErrorMsg = err.Error()
		_ = m.logEvent(appName, "healthcheck", "SWITCH_HEALTHCHECK_FAILED", state.LastErrorCode, err.Error())
		if rollbackErr != nil {
			state.LastErrorCode = ErrCodeSwitchRollback
			state.LastErrorMsg = rollbackErr.Error()
			_ = m.logEvent(appName, "rollback", "SWITCH_ROLLBACK_FAILED", state.LastErrorCode, rollbackErr.Error())
		}
		_ = saveState(statePath, state)
		if rollbackErr != nil {
			return fmt.Errorf("healthcheck failed: %v; rollback failed: %v", err, rollbackErr)
		}
		_ = m.logEvent(appName, "rollback", "SWITCH_ROLLBACK_DONE", "", "rollback to previous current completed")
		return err
	}
	_ = m.logEvent(appName, "healthcheck", "SWITCH_HEALTHCHECK_DONE", "", "healthcheck passed")

	state.CurrentVersion = effective.Version
	state.PendingVersion = ""
	state.LastUpdateAt = m.Now().UTC().Format(time.RFC3339)
	state.LastErrorCode = ""
	state.LastErrorMsg = ""

	if err := saveState(statePath, state); err != nil {
		return err
	}
	if err := m.cleanupOldVersions(appName, effective.Version); err != nil {
		return err
	}
	_ = m.logEvent(appName, "switch", "SWITCH_DONE", "", "update switch transaction completed")
	_ = m.logEvent(appName, "update", "UPDATE_DONE", "", "update transaction completed")
	return os.RemoveAll(filepath.Join(m.Root, "apps", appName, "_staging"))
}

func (m *Manager) logEvent(appName, stage, event, errorCode, message string) error {
	if appName == "" {
		return nil
	}
	when := time.Now
	if m.Now != nil {
		when = m.Now
	}
	entry := switchLogEvent{
		Timestamp: when().UTC().Format(time.RFC3339),
		App:       appName,
		Stage:     stage,
		Event:     event,
		ErrorCode: errorCode,
		Message:   message,
	}
	b, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("encode event log: %w", err)
	}
	logDir := filepath.Join(m.Root, "apps", appName, "logs")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return fmt.Errorf("create event log dir: %w", err)
	}
	logPath := filepath.Join(logDir, "events-"+when().UTC().Format("20060102")+".log")
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open event log: %w", err)
	}
	defer f.Close()
	if _, err := f.Write(append(b, '\n')); err != nil {
		return fmt.Errorf("write event log: %w", err)
	}
	return nil
}

func (m *Manager) applyCheckver(man *manifest.Manifest) error {
	version, captures, err := m.DiscoverLatest(man)
	if err != nil {
		return err
	}
	if version == "" || version == man.Version {
		return nil
	}
	artifact := man.Autoupdate.Architecture.X64
	if artifact.URL == "" {
		return fmt.Errorf("checkver found newer version %s but autoupdate.64bit.url is empty", version)
	}
	artifact.URL = renderTemplate(artifact.URL, captures)
	artifact.ExtractDir = renderTemplate(artifact.ExtractDir, captures)
	artifact.Hash = ""
	man.Version = version
	man.Architecture.X64 = artifact
	if _, err := man.ResolveArtifact64(); err != nil {
		return fmt.Errorf("checkver resolved newer version %s but no verifiable hash is available: %w", version, err)
	}
	return nil
}

func (m *Manager) DiscoverLatest(man *manifest.Manifest) (string, map[string]string, error) {
	if man.Checkver.GitHub == "" || man.Checkver.Regex == "" || man.Checkver.Replace == "" {
		return "", nil, nil
	}
	owner, repo, err := parseGitHubRepo(man.Checkver.GitHub)
	if err != nil {
		return "", nil, err
	}
	apiBase := strings.TrimSuffix(m.GitHubAPIBase, "/")
	endpoint := fmt.Sprintf("%s/repos/%s/%s/releases/latest", apiBase, owner, repo)

	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return "", nil, fmt.Errorf("build checkver request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := m.Client.Do(req)
	if err != nil {
		return "", nil, fmt.Errorf("%s: checkver request failed: %w", ErrCodeNetCheckverRequest, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return "", nil, fmt.Errorf("%s: checkver http status: %d", ErrCodeNetCheckverHTTP, resp.StatusCode)
	}

	var rel githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return "", nil, fmt.Errorf("decode checkver response: %w", err)
	}

	re, err := regexp.Compile(man.Checkver.Regex)
	if err != nil {
		return "", nil, fmt.Errorf("compile checkver regex: %w", err)
	}
	for _, asset := range rel.Assets {
		matches := re.FindStringSubmatch(asset.BrowserDownloadURL)
		if matches == nil {
			continue
		}
		captures := map[string]string{}
		names := re.SubexpNames()
		for i := 1; i < len(matches) && i < len(names); i++ {
			name := names[i]
			if name == "" {
				continue
			}
			captures[name] = matches[i]
		}
		version := renderTemplate(man.Checkver.Replace, captures)
		if version == "" {
			return "", nil, fmt.Errorf("checkver replace produced empty version")
		}
		return version, captures, nil
	}
	return "", nil, fmt.Errorf("checkver found no matching release assets")
}

func (m *Manager) download(url, dst string) error {
	parsed, err := neturl.Parse(url)
	if err != nil {
		return fmt.Errorf("%s: invalid download url: %w", ErrCodeNetDownload, err)
	}
	if !strings.EqualFold(parsed.Scheme, "https") {
		return fmt.Errorf("%s: insecure download url scheme %q", ErrCodeNetDownload, parsed.Scheme)
	}
	resp, err := m.Client.Get(url)
	if err != nil {
		return fmt.Errorf("%s: download request: %w", ErrCodeNetDownload, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("%s: download http status: %d", ErrCodeNetDownload, resp.StatusCode)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("create download dir: %w", err)
	}
	f, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("create download file: %w", err)
	}
	defer f.Close()
	if _, err := io.Copy(f, resp.Body); err != nil {
		return fmt.Errorf("write download file: %w", err)
	}
	return nil
}

func verifySHA256(path, expected string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open file for hash: %w", err)
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return fmt.Errorf("hash file: %w", err)
	}
	actual := hex.EncodeToString(h.Sum(nil))
	cleanExpected := strings.ToLower(strings.TrimPrefix(expected, "sha256:"))
	if actual != cleanExpected {
		return fmt.Errorf("sha256 mismatch: expected %s got %s", cleanExpected, actual)
	}
	return nil
}

func unzip(src, dst string) error {
	r, err := zip.OpenReader(src)
	if err != nil {
		return fmt.Errorf("open zip: %w", err)
	}
	defer r.Close()
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return fmt.Errorf("create extract root: %w", err)
	}

	for _, f := range r.File {
		targetPath := filepath.Join(dst, f.Name)
		cleanDst := filepath.Clean(dst) + string(os.PathSeparator)
		cleanTarget := filepath.Clean(targetPath)
		if !strings.HasPrefix(cleanTarget, cleanDst) {
			return fmt.Errorf("invalid zip path: %s", f.Name)
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(cleanTarget, 0o755); err != nil {
				return fmt.Errorf("create dir: %w", err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(cleanTarget), 0o755); err != nil {
			return fmt.Errorf("create parent dir: %w", err)
		}
		rc, err := f.Open()
		if err != nil {
			return fmt.Errorf("open zip entry: %w", err)
		}
		out, err := os.OpenFile(cleanTarget, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, f.Mode())
		if err != nil {
			rc.Close()
			return fmt.Errorf("create extracted file: %w", err)
		}
		if _, err := io.Copy(out, rc); err != nil {
			out.Close()
			rc.Close()
			return fmt.Errorf("extract file: %w", err)
		}
		out.Close()
		rc.Close()
	}
	return nil
}

func switchCurrent(currentPath, versionDir string) error {
	if err := os.RemoveAll(currentPath); err != nil {
		return fmt.Errorf("remove current: %w", err)
	}

	if runtime.GOOS == "windows" {
		if err := junctionCreator(currentPath, versionDir); err == nil {
			return nil
		}
	}

	if err := os.MkdirAll(currentPath, 0o755); err != nil {
		return fmt.Errorf("create current dir: %w", err)
	}
	return os.WriteFile(filepath.Join(currentPath, ".appstract-target"), []byte(versionDir), 0o644)
}

func createJunction(linkPath, targetPath string) error {
	if err := os.MkdirAll(filepath.Dir(linkPath), 0o755); err != nil {
		return fmt.Errorf("create current parent: %w", err)
	}
	cmd := exec.Command("cmd", "/c", "mklink", "/J", linkPath, targetPath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("mklink junction failed: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (m *Manager) runPreInstall(appName, dir string, steps []string) error {
	if len(steps) == 0 {
		return nil
	}
	ps, err := findPowerShell()
	if err != nil {
		return err
	}
	logDir := filepath.Join(m.Root, "apps", appName, "logs")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return fmt.Errorf("create log dir: %w", err)
	}
	logPath := filepath.Join(logDir, "preinstall-"+m.Now().UTC().Format("20060102T150405Z")+".log")

	modulePath := filepath.Join(m.Root, "scripts", "Appstract.psm1")
	script := bytes.NewBuffer(nil)
	script.WriteString(`$ErrorActionPreference = "Stop"` + "\n")
	script.WriteString(`$ProgressPreference = "SilentlyContinue"` + "\n")
	script.WriteString(`$dir = '` + escapeSingleQuotedPS(dir) + `'` + "\n")
	script.WriteString(`if (Test-Path '` + escapeSingleQuotedPS(modulePath) + `') { Import-Module '` + escapeSingleQuotedPS(modulePath) + `' -Force }` + "\n")
	for _, step := range steps {
		script.WriteString(step)
		script.WriteString("\n")
	}

	timeout := m.ScriptTimeout
	if timeout <= 0 {
		timeout = 2 * time.Minute
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, ps, "-NoProfile", "-NonInteractive", "-Command", script.String())
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	runErr := cmd.Run()

	if writeErr := os.WriteFile(logPath, out.Bytes(), 0o644); writeErr != nil {
		return fmt.Errorf("write pre_install log: %w", writeErr)
	}
	if ctx.Err() == context.DeadlineExceeded {
		return fmt.Errorf("pre_install timeout (%s), see log: %s", timeout, logPath)
	}
	if runErr != nil {
		return fmt.Errorf("pre_install failed: %w, see log: %s", runErr, logPath)
	}
	return nil
}

func loadState(path string) (RuntimeState, error) {
	var s RuntimeState
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return s, fmt.Errorf("read runtime state: %w", err)
	}
	if err := json.Unmarshal(b, &s); err != nil {
		return s, fmt.Errorf("decode runtime state: %w", err)
	}
	return s, nil
}

func saveState(path string, s RuntimeState) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create state dir: %w", err)
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("encode runtime state: %w", err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		return fmt.Errorf("write runtime state: %w", err)
	}
	return nil
}

type lockInfo struct {
	PID       int    `json:"pid"`
	CreatedAt string `json:"created_at"`
}

var lockPIDRunning = isPIDRunning

func acquireLock(lockPath string) error {
	if err := tryAcquireLock(lockPath); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrExist) {
		return err
	}
	stale, staleErr := lockFileIsStale(lockPath)
	if staleErr != nil || !stale {
		return fmt.Errorf("update already running")
	}
	_ = os.Remove(lockPath)
	if err := tryAcquireLock(lockPath); err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("update already running")
		}
		return err
	}
	return nil
}

func tryAcquireLock(lockPath string) error {
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		return fmt.Errorf("create lock dir: %w", err)
	}
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("acquire lock: %w", err)
	}
	defer f.Close()
	info := lockInfo{
		PID:       os.Getpid(),
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}
	b, err := json.Marshal(info)
	if err != nil {
		return fmt.Errorf("encode lock info: %w", err)
	}
	if _, err := f.Write(b); err != nil {
		return fmt.Errorf("write lock info: %w", err)
	}
	return nil
}

func lockFileIsStale(lockPath string) (bool, error) {
	b, err := os.ReadFile(lockPath)
	if err != nil {
		if os.IsNotExist(err) {
			return true, nil
		}
		return false, fmt.Errorf("read lock file: %w", err)
	}
	if len(strings.TrimSpace(string(b))) == 0 {
		return false, nil
	}
	var info lockInfo
	if err := json.Unmarshal(b, &info); err != nil {
		return false, nil
	}
	if info.PID <= 0 {
		return false, nil
	}
	running, err := lockPIDRunning(info.PID)
	if err != nil {
		return false, nil
	}
	return !running, nil
}

func isPIDRunning(pid int) (bool, error) {
	if pid <= 0 {
		return false, nil
	}
	if runtime.GOOS != "windows" {
		return true, nil
	}
	out, err := exec.Command("tasklist", "/FI", "PID eq "+strconv.Itoa(pid), "/FO", "CSV", "/NH").CombinedOutput()
	if err != nil {
		return false, fmt.Errorf("query pid %d: %w: %s", pid, err, strings.TrimSpace(string(out)))
	}
	raw := strings.ToLower(string(out))
	if strings.Contains(raw, "no tasks are running") {
		return false, nil
	}
	return strings.Contains(raw, `"`+strconv.Itoa(pid)+`"`), nil
}

func releaseLock(lockPath string) {
	_ = os.Remove(lockPath)
}

func parseGitHubRepo(raw string) (string, string, error) {
	u, err := neturl.Parse(raw)
	if err != nil {
		return "", "", fmt.Errorf("invalid checkver.github url: %w", err)
	}
	parts := strings.Split(strings.Trim(strings.TrimSuffix(u.Path, ".git"), "/"), "/")
	if len(parts) < 2 {
		return "", "", fmt.Errorf("invalid checkver.github path: %s", raw)
	}
	return parts[0], parts[1], nil
}

func findPowerShell() (string, error) {
	if p, err := exec.LookPath("pwsh"); err == nil {
		return p, nil
	}
	if p, err := exec.LookPath("powershell"); err == nil {
		return p, nil
	}
	return "", fmt.Errorf("powershell executable not found")
}

func renderTemplate(template string, captures map[string]string) string {
	if template == "" {
		return ""
	}
	out := template
	for k, v := range captures {
		out = strings.ReplaceAll(out, "${"+k+"}", v)
		out = strings.ReplaceAll(out, "$"+k, v)
		out = strings.ReplaceAll(out, "$match"+upperFirst(k), v)
	}
	return out
}

func upperFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func escapeSingleQuotedPS(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}

func (m *Manager) terminateProcesses(appName, prefix string) error {
	if m.findPIDs == nil || m.killPID == nil {
		return nil
	}
	pids, err := m.findPIDs(prefix)
	if err != nil {
		return err
	}
	if len(pids) == 0 {
		_ = m.logEvent(appName, "process", "SWITCH_PROCESS_NONE", "", "no running process matched current path")
		return nil
	}
	_ = m.logEvent(appName, "process", "SWITCH_PROCESS_FOUND", "", fmt.Sprintf("matched_pids=%d", len(pids)))
	for _, pid := range pids {
		if m.closePID != nil {
			if err := m.closePID(pid); err == nil {
				_ = m.logEvent(appName, "process", "SWITCH_PROCESS_GRACEFUL", "", fmt.Sprintf("pid=%d", pid))
			}
		}
	}
	for _, pid := range pids {
		_ = m.killPID(pid, false)
	}
	_ = m.logEvent(appName, "process", "SWITCH_PROCESS_SOFT_KILL", "", "sent non-force termination signal")
	timeout := m.StopTimeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		remain, err := m.findPIDs(prefix)
		if err != nil {
			return fmt.Errorf("%s: query remaining processes: %w", ErrCodeSwitchProcess, err)
		}
		if len(remain) == 0 {
			return nil
		}
		time.Sleep(300 * time.Millisecond)
	}
	remain, err := m.findPIDs(prefix)
	if err != nil {
		return fmt.Errorf("%s: query remaining processes: %w", ErrCodeSwitchProcess, err)
	}
	for _, pid := range remain {
		if err := m.killPID(pid, true); err != nil {
			_ = m.logEvent(appName, "process", "SWITCH_PROCESS_FORCE_FAILED", ErrCodeSwitchProcess, err.Error())
			return err
		}
		_ = m.logEvent(appName, "process", "SWITCH_PROCESS_FORCE_KILL", "", fmt.Sprintf("pid=%d", pid))
	}
	return nil
}

func (m *Manager) healthcheckAndRelaunch(currentPath, bin string) error {
	if bin == "" {
		return fmt.Errorf("manifest bin is required")
	}
	binPath := filepath.Join(currentPath, bin)
	if _, err := os.Stat(binPath); err != nil {
		return fmt.Errorf("healthcheck missing bin %s: %w", binPath, err)
	}
	if m.Relaunch && m.launch != nil {
		if err := m.launch(binPath); err != nil {
			return fmt.Errorf("relaunch failed: %w", err)
		}
	}
	return nil
}

func rollbackCurrent(currentPath, prevTarget string) error {
	if prevTarget == "" {
		return nil
	}
	return switchCurrent(currentPath, prevTarget)
}

func resolveCurrentTarget(currentPath string) (string, error) {
	if link, err := os.Readlink(currentPath); err == nil {
		return link, nil
	}
	marker := filepath.Join(currentPath, ".appstract-target")
	b, err := os.ReadFile(marker)
	if err == nil {
		return strings.TrimSpace(string(b)), nil
	}
	if os.IsNotExist(err) {
		return "", nil
	}
	return "", err
}

func (m *Manager) cleanupOldVersions(appName, currentVersion string) error {
	appDir := filepath.Join(m.Root, "apps", appName)
	entries, err := os.ReadDir(appDir)
	if err != nil {
		return fmt.Errorf("read app directory for cleanup: %w", err)
	}

	type versionEntry struct {
		name    string
		modTime time.Time
	}
	var old []versionEntry
	currentDirName := "v" + currentVersion

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasPrefix(name, "v") || name == currentDirName {
			continue
		}
		info, infoErr := e.Info()
		if infoErr != nil {
			continue
		}
		old = append(old, versionEntry{name: name, modTime: info.ModTime()})
	}

	if m.KeepVersions < 0 {
		m.KeepVersions = 0
	}
	if len(old) <= m.KeepVersions {
		return nil
	}
	sort.Slice(old, func(i, j int) bool {
		return old[i].modTime.After(old[j].modTime)
	})
	for _, victim := range old[m.KeepVersions:] {
		if err := os.RemoveAll(filepath.Join(appDir, victim.name)); err != nil {
			return fmt.Errorf("remove old version %s: %w", victim.name, err)
		}
	}
	return nil
}

func findRunningPIDsByPrefix(prefix string) ([]int, error) {
	ps, err := findPowerShell()
	if err != nil {
		return nil, err
	}
	script := `$prefix = '` + escapeSingleQuotedPS(prefix) + `'; ` +
		`$p = Get-CimInstance Win32_Process | Where-Object { $_.ExecutablePath -and $_.ExecutablePath.StartsWith($prefix, [System.StringComparison]::OrdinalIgnoreCase) } | Select-Object -ExpandProperty ProcessId; ` +
		`if ($null -eq $p) { '' } else { $p | ConvertTo-Json -Compress }`

	out, err := exec.Command(ps, "-NoProfile", "-NonInteractive", "-Command", script).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("query process list: %w", err)
	}
	raw := strings.TrimSpace(string(out))
	if raw == "" || raw == "null" {
		return nil, nil
	}
	if strings.HasPrefix(raw, "[") {
		var arr []int
		if err := json.Unmarshal([]byte(raw), &arr); err == nil {
			return arr, nil
		}
	}
	var single int
	if err := json.Unmarshal([]byte(raw), &single); err == nil {
		return []int{single}, nil
	}
	if n, convErr := strconv.Atoi(raw); convErr == nil {
		return []int{n}, nil
	}
	return nil, fmt.Errorf("unexpected process query output: %s", raw)
}

func killProcessByPID(pid int, force bool) error {
	args := []string{"/PID", strconv.Itoa(pid), "/T"}
	if force {
		args = append(args, "/F")
	}
	out, err := exec.Command("taskkill", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("taskkill pid=%d force=%t failed: %w: %s", pid, force, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func gracefulCloseByPID(pid int) error {
	ps, err := findPowerShell()
	if err != nil {
		return err
	}
	script := `$p = Get-Process -Id ` + strconv.Itoa(pid) + ` -ErrorAction SilentlyContinue; ` +
		`if ($null -eq $p) { exit 0 }; ` +
		`if ($p.MainWindowHandle -eq 0) { exit 0 }; ` +
		`if ($p.CloseMainWindow()) { exit 0 } else { exit 1 }`
	out, err := exec.Command(ps, "-NoProfile", "-NonInteractive", "-Command", script).CombinedOutput()
	if err != nil {
		return fmt.Errorf("graceful close pid=%d failed: %w: %s", pid, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func launchDetached(path string) error {
	cmd := exec.Command(path)
	return cmd.Start()
}
