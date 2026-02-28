package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"appstract/internal/bootstrap"
	"appstract/internal/config"
	"appstract/internal/manifest"
	"appstract/internal/updater"
)

type updateOptions struct {
	Checkver     bool
	PromptSwitch bool
	Relaunch     bool
}

var runLaunch = func(path string) error {
	cmd := exec.Command(path)
	return cmd.Start()
}

var resolveExecutablePath = os.Executable

var executeUpdateFromManifest = func(root, app, manifestPath string, opts updateOptions) error {
	manager := updater.NewManager(root)
	manager.UseCheckver = opts.Checkver
	manager.PromptSwitch = opts.PromptSwitch
	manager.Relaunch = opts.Relaunch
	cfg, err := config.Load(root)
	if err != nil {
		return err
	}
	manager.KeepVersions = cfg.KeepVersions
	return manager.UpdateFromManifest(app, manifestPath)
}

var runAsyncUpdate = func(root, app, manifestPath string) error {
	return executeUpdateFromManifest(root, app, manifestPath, updateOptions{})
}

func Execute(args []string, stdout, stderr io.Writer, envHome string) int {
	if len(args) == 0 {
		printGlobalUsage(stderr)
		return 1
	}
	switch args[0] {
	case "-h", "--help":
		printGlobalUsage(stdout)
		return 0
	case "help":
		return executeHelp(args[1:], stdout, stderr)
	case "init":
		return executeInit(args[1:], stdout, stderr, envHome)
	case "run":
		return executeRun(args[1:], stdout, stderr, envHome)
	case "manifest":
		return executeManifest(args[1:], stdout, stderr)
	case "add":
		return executeAdd(args[1:], stdout, stderr, envHome)
	case "update":
		return executeUpdate(args[1:], stdout, stderr, envHome)
	default:
		fmt.Fprintf(stderr, "unknown command: %s\n", args[0])
		printGlobalUsage(stderr)
		return 1
	}
}

func printGlobalUsage(w io.Writer) {
	fmt.Fprintln(w, "usage: appstract <command> [options]")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "commands:")
	fmt.Fprintln(w, "  help [command]")
	fmt.Fprintln(w, "      Show command usage details.")
	fmt.Fprintln(w, "  init [--root <path>]")
	fmt.Fprintln(w, "      Initialize Appstract directory layout.")
	fmt.Fprintln(w, "  add [--root <path>] <manifest-file>")
	fmt.Fprintln(w, "      Copy manifest into manifests/ and install the app.")
	fmt.Fprintln(w, "  run [--root <path>] <app>")
	fmt.Fprintln(w, "      Launch app current version and trigger background update.")
	fmt.Fprintln(w, "  update [--root <path>] [--checkver] [--prompt-switch] [--relaunch] [--fail-fast]")
	fmt.Fprintln(w, "      Update apps discovered from manifests/*.json.")
	fmt.Fprintln(w, "  manifest validate <file>")
	fmt.Fprintln(w, "      Validate manifest schema and required fields.")
}

func printCommandUsage(cmd string, w io.Writer) bool {
	switch cmd {
	case "help":
		fmt.Fprintln(w, "usage: appstract help [command]")
		fmt.Fprintln(w, "show global or command-specific usage")
		return true
	case "init":
		fmt.Fprintln(w, "usage: appstract init [--root <path>]")
		fmt.Fprintln(w, "initialize manifests/shims/scripts/apps and config.yaml")
		return true
	case "add":
		fmt.Fprintln(w, "usage: appstract add [--root <path>] <manifest-file>")
		fmt.Fprintln(w, "derive app name from manifest filename, copy to manifests/<app>.json, then install")
		return true
	case "run":
		fmt.Fprintln(w, "usage: appstract run [--root <path>] <app>")
		fmt.Fprintln(w, "if apps/<app>/current is missing but manifests/<app>.json exists, install first")
		return true
	case "update":
		fmt.Fprintln(w, "usage: appstract update [--root <path>] [--checkver] [--prompt-switch] [--relaunch] [--fail-fast]")
		fmt.Fprintln(w, "scan manifests/*.json and update each app")
		return true
	case "manifest":
		fmt.Fprintln(w, "usage: appstract manifest validate <file>")
		fmt.Fprintln(w, "parse and validate manifest file")
		return true
	default:
		return false
	}
}

func executeHelp(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		printGlobalUsage(stdout)
		return 0
	}
	if !printCommandUsage(args[0], stdout) {
		fmt.Fprintf(stderr, "unknown command for help: %s\n", args[0])
		return 1
	}
	return 0
}

func resolveRoot(envHome, flagRoot string) (string, string, error) {
	executablePath, err := resolveExecutablePath()
	if err != nil {
		return "", "", fmt.Errorf("resolve executable path: %w", err)
	}
	root, err := bootstrap.ResolveRoot(envHome, flagRoot, executablePath)
	if err != nil {
		return "", "", err
	}
	return root, executablePath, nil
}

func ensureWorkspaceReady(root, executablePath string) error {
	return bootstrap.EnsureReadyForCommand(root, executablePath)
}

func executeInit(args []string, stdout, stderr io.Writer, envHome string) int {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	fs.SetOutput(stderr)
	rootFlag := fs.String("root", "", "Appstract root directory")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			printCommandUsage("init", stdout)
			return 0
		}
		return 1
	}
	root, _, err := resolveRoot(envHome, *rootFlag)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if err := bootstrap.InitLayout(root); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintf(stdout, "initialized: %s\n", root)
	return 0
}

func executeRun(args []string, stdout, stderr io.Writer, envHome string) int {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(stderr)
	rootFlag := fs.String("root", "", "Appstract root directory")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			printCommandUsage("run", stdout)
			return 0
		}
		return 1
	}
	if fs.NArg() < 1 {
		printCommandUsage("run", stderr)
		return 1
	}
	app := fs.Arg(0)

	root, executablePath, err := resolveRoot(envHome, *rootFlag)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if err := ensureWorkspaceReady(root, executablePath); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}

	manifestPath := filepath.Join(root, "manifests", app+".json")
	currentPath := filepath.Join(root, "apps", app, "current")
	if _, err := os.Stat(currentPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			if _, statErr := os.Stat(manifestPath); statErr != nil {
				if errors.Is(statErr, os.ErrNotExist) {
					fmt.Fprintf(stderr, "app %q has no current version at %s\n", app, currentPath)
					return 1
				}
				fmt.Fprintln(stderr, statErr)
				return 1
			}
			fmt.Fprintf(stdout, "app %q is not installed, auto-installing from manifest: %s\n", app, manifestPath)
			if err := executeUpdateFromManifest(root, app, manifestPath, updateOptions{}); err != nil {
				fmt.Fprintf(stderr, "install app %q for run failed: %v\n", app, err)
				return 1
			}
			fmt.Fprintf(stdout, "auto-install completed: %s\n", app)
			if _, err := os.Stat(currentPath); err != nil {
				fmt.Fprintf(stderr, "app %q has no current version at %s\n", app, currentPath)
				return 1
			}
		} else {
			fmt.Fprintln(stderr, err)
			return 1
		}
	}

	man, err := manifest.ParseFile(manifestPath)
	if err != nil {
		fmt.Fprintf(stderr, "load manifest for run: %v\n", err)
		return 1
	}
	binPath := filepath.Join(currentPath, man.Bin)
	if _, err := os.Stat(binPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			fmt.Fprintf(stderr, "app %q bin missing at %s\n", app, binPath)
			return 1
		}
		fmt.Fprintln(stderr, err)
		return 1
	}
	if err := runLaunch(binPath); err != nil {
		fmt.Fprintf(stderr, "launch app %q failed: %v\n", app, err)
		return 1
	}
	go func() {
		if err := runAsyncUpdate(root, app, manifestPath); err != nil {
			fmt.Fprintf(stderr, "background update failed for %q: %v\n", app, err)
		}
	}()
	fmt.Fprintf(stdout, "run-started: %s (%s)\n", app, binPath)
	return 0
}

func executeManifest(args []string, stdout, stderr io.Writer) int {
	if len(args) == 1 && (args[0] == "-h" || args[0] == "--help") {
		printCommandUsage("manifest", stdout)
		return 0
	}
	if len(args) < 2 || args[0] != "validate" {
		printCommandUsage("manifest", stderr)
		return 1
	}
	m, err := manifest.ParseFile(args[1])
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintf(stdout, "manifest valid: version=%s\n", m.Version)
	return 0
}

func executeAdd(args []string, stdout, stderr io.Writer, envHome string) int {
	fs := flag.NewFlagSet("add", flag.ContinueOnError)
	fs.SetOutput(stderr)
	rootFlag := fs.String("root", "", "Appstract root directory")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			printCommandUsage("add", stdout)
			return 0
		}
		return 1
	}
	if fs.NArg() != 1 {
		printCommandUsage("add", stderr)
		return 1
	}
	sourceManifestPath := fs.Arg(0)
	app, err := deriveAppNameFromManifestPath(sourceManifestPath)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}

	root, executablePath, err := resolveRoot(envHome, *rootFlag)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if err := ensureWorkspaceReady(root, executablePath); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}

	if _, err := manifest.ParseFile(sourceManifestPath); err != nil {
		fmt.Fprintf(stderr, "validate add manifest: %v\n", err)
		return 1
	}

	targetManifestPath := filepath.Join(root, "manifests", app+".json")
	if err := copyFile(sourceManifestPath, targetManifestPath); err != nil {
		fmt.Fprintf(stderr, "copy manifest: %v\n", err)
		return 1
	}

	if err := executeUpdateFromManifest(root, app, targetManifestPath, updateOptions{}); err != nil {
		fmt.Fprintf(stderr, "install app %q from manifest failed: %v\n", app, err)
		return 1
	}
	fmt.Fprintf(stdout, "add completed: %s\n", app)
	return 0
}

func executeUpdate(args []string, stdout, stderr io.Writer, envHome string) int {
	fs := flag.NewFlagSet("update", flag.ContinueOnError)
	fs.SetOutput(stderr)
	rootFlag := fs.String("root", "", "Appstract root directory")
	checkver := fs.Bool("checkver", false, "Resolve latest version from checkver.github")
	promptSwitch := fs.Bool("prompt-switch", false, "Prompt user before switching current version")
	relaunch := fs.Bool("relaunch", false, "Relaunch app after successful switch")
	failFast := fs.Bool("fail-fast", false, "Stop after first failed app update")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			printCommandUsage("update", stdout)
			return 0
		}
		return 1
	}
	if fs.NArg() != 0 {
		printCommandUsage("update", stderr)
		return 1
	}

	root, executablePath, err := resolveRoot(envHome, *rootFlag)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if err := ensureWorkspaceReady(root, executablePath); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}

	manifestsDir := filepath.Join(root, "manifests")
	entries, err := os.ReadDir(manifestsDir)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}

	type job struct {
		app          string
		manifestPath string
	}
	jobs := make([]job, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.EqualFold(filepath.Ext(name), ".json") {
			continue
		}
		app := strings.TrimSuffix(name, filepath.Ext(name))
		if app == "" {
			fmt.Fprintf(stderr, "skip invalid manifest filename: %s\n", name)
			continue
		}
		jobs = append(jobs, job{
			app:          app,
			manifestPath: filepath.Join(manifestsDir, name),
		})
	}

	if len(jobs) == 0 {
		fmt.Fprintf(stdout, "no manifests found in %s\n", manifestsDir)
		return 0
	}

	opts := updateOptions{
		Checkver:     *checkver,
		PromptSwitch: *promptSwitch,
		Relaunch:     *relaunch,
	}

	successCount := 0
	failCount := 0
	for _, item := range jobs {
		if err := executeUpdateFromManifest(root, item.app, item.manifestPath, opts); err != nil {
			failCount++
			fmt.Fprintf(stderr, "update failed: %s (%v)\n", item.app, err)
			if *failFast {
				fmt.Fprintf(stdout, "update summary: total=%d success=%d failed=%d\n", len(jobs), successCount, failCount)
				return 1
			}
			continue
		}
		successCount++
		fmt.Fprintf(stdout, "update completed: %s\n", item.app)
	}
	fmt.Fprintf(stdout, "update summary: total=%d success=%d failed=%d\n", len(jobs), successCount, failCount)
	if failCount > 0 {
		return 1
	}
	return 0
}

func deriveAppNameFromManifestPath(path string) (string, error) {
	base := filepath.Base(path)
	ext := filepath.Ext(base)
	if !strings.EqualFold(ext, ".json") {
		return "", fmt.Errorf("manifest file must end with .json: %s", path)
	}
	app := strings.TrimSuffix(base, ext)
	if strings.TrimSpace(app) == "" {
		return "", fmt.Errorf("cannot derive app name from manifest file: %s", path)
	}
	return app, nil
}

func copyFile(sourcePath, targetPath string) error {
	data, err := os.ReadFile(sourcePath)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(targetPath, data, 0o644)
}
