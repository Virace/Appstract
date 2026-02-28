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
	Output       *commandOutput
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
	if opts.Output != nil {
		manager.OnMessage = opts.Output.onUpdaterMessage
		manager.OnProgress = opts.Output.onUpdaterProgress
	}
	cfg, err := config.Load(root)
	if err != nil {
		return err
	}
	manager.KeepVersions = cfg.KeepVersions
	return manager.UpdateFromManifest(app, manifestPath)
}

var runAsyncUpdate = func(root, app, manifestPath string, opts updateOptions) error {
	return executeUpdateFromManifest(root, app, manifestPath, opts)
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
	fmt.Fprintln(w, "  add [--root <path>] [--output <silent|default|debug>] <manifest-file>")
	fmt.Fprintln(w, "      Copy manifest into manifests/ and install the app.")
	fmt.Fprintln(w, "  run [--root <path>] [--output <silent|default|debug>] <app>")
	fmt.Fprintln(w, "      Launch app current version and trigger background update.")
	fmt.Fprintln(w, "  update [--root <path>] [--output <silent|default|debug>] [--checkver] [--prompt-switch] [--relaunch] [--fail-fast]")
	fmt.Fprintln(w, "      Update apps discovered from manifests/*.json.")
	fmt.Fprintln(w, "  manifest [--output <silent|default|debug>] validate <file>")
	fmt.Fprintln(w, "      Validate manifest schema and required fields.")
}

func printCommandUsage(cmd string, w io.Writer) bool {
	switch cmd {
	case "help":
		fmt.Fprintln(w, "usage: appstract help [command]")
		fmt.Fprintln(w, "show global or command-specific usage")
		return true
	case "init":
		fmt.Fprintln(w, "usage: appstract init [--root <path>] [--output <silent|default|debug>]")
		fmt.Fprintln(w, "initialize manifests/shims/scripts/apps and config.yaml")
		return true
	case "add":
		fmt.Fprintln(w, "usage: appstract add [--root <path>] [--output <silent|default|debug>] <manifest-file>")
		fmt.Fprintln(w, "derive app name from manifest filename, copy to manifests/<app>.json, then install")
		return true
	case "run":
		fmt.Fprintln(w, "usage: appstract run [--root <path>] [--output <silent|default|debug>] <app>")
		fmt.Fprintln(w, "if apps/<app>/current is missing but manifests/<app>.json exists, install first")
		return true
	case "update":
		fmt.Fprintln(w, "usage: appstract update [--root <path>] [--output <silent|default|debug>] [--checkver] [--prompt-switch] [--relaunch] [--fail-fast]")
		fmt.Fprintln(w, "scan manifests/*.json and update each app")
		return true
	case "manifest":
		fmt.Fprintln(w, "usage: appstract manifest [--output <silent|default|debug>] validate <file>")
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
	outputFlag := fs.String("output", "", "Output level: silent|default|debug")
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
	outputLevel, err := resolveOutputLevel(root, *outputFlag)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	output := newCommandOutput(outputLevel, stdout, stderr)
	output.printDefault("initializing workspace: %s", root)
	if err := bootstrap.InitLayout(root); err != nil {
		output.printError("%v", err)
		return 1
	}
	output.printDefault("[ok] initialized: %s", root)
	return 0
}

func executeRun(args []string, stdout, stderr io.Writer, envHome string) int {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(stderr)
	rootFlag := fs.String("root", "", "Appstract root directory")
	outputFlag := fs.String("output", "", "Output level: silent|default|debug")
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
	outputLevel, err := resolveOutputLevel(root, *outputFlag)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	output := newCommandOutput(outputLevel, stdout, stderr)
	updateOpts := updateOptions{
		Output: output,
	}
	output.printDefault("run start: app=%s root=%s", app, root)
	if err := ensureWorkspaceReady(root, executablePath); err != nil {
		output.printError("%v", err)
		return 1
	}

	manifestPath := filepath.Join(root, "manifests", app+".json")
	currentPath := filepath.Join(root, "apps", app, "current")
	if _, err := os.Stat(currentPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			if _, statErr := os.Stat(manifestPath); statErr != nil {
				if errors.Is(statErr, os.ErrNotExist) {
					output.printError("app %q has no current version at %s", app, currentPath)
					return 1
				}
				output.printError("%v", statErr)
				return 1
			}
			output.printDefault("app %q is not installed, auto-installing from manifest: %s", app, manifestPath)
			if err := executeUpdateFromManifest(root, app, manifestPath, updateOpts); err != nil {
				output.printError("install app %q for run failed: %v", app, err)
				return 1
			}
			output.printDefault("[ok] auto-install completed: %s", app)
			if _, err := os.Stat(currentPath); err != nil {
				output.printError("app %q has no current version at %s", app, currentPath)
				return 1
			}
		} else {
			output.printError("%v", err)
			return 1
		}
	}

	man, err := manifest.ParseFile(manifestPath)
	if err != nil {
		output.printError("load manifest for run: %v", err)
		return 1
	}
	binPath := filepath.Join(currentPath, man.Bin)
	if _, err := os.Stat(binPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			output.printError("app %q bin missing at %s", app, binPath)
			return 1
		}
		output.printError("%v", err)
		return 1
	}
	output.printDefault("launching app binary: %s", binPath)
	if err := runLaunch(binPath); err != nil {
		output.printError("launch app %q failed: %v", app, err)
		return 1
	}
	go func() {
		if err := runAsyncUpdate(root, app, manifestPath, updateOpts); err != nil {
			output.printError("background update failed for %q: %v", app, err)
		}
	}()
	output.printDefault("[ok] run-started: %s (%s)", app, binPath)
	return 0
}

func executeManifest(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("manifest", flag.ContinueOnError)
	fs.SetOutput(stderr)
	outputFlag := fs.String("output", "", "Output level: silent|default|debug")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			printCommandUsage("manifest", stdout)
			return 0
		}
		return 1
	}

	remain := fs.Args()
	if len(remain) < 2 || remain[0] != "validate" {
		printCommandUsage("manifest", stderr)
		return 1
	}

	outputLevel, err := parseOutputLevel(*outputFlag)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	output := newCommandOutput(outputLevel, stdout, stderr)

	m, err := manifest.ParseFile(remain[1])
	if err != nil {
		output.printError("%v", err)
		return 1
	}
	output.printDefault("[ok] manifest valid: version=%s", m.Version)
	return 0
}

func executeAdd(args []string, stdout, stderr io.Writer, envHome string) int {
	fs := flag.NewFlagSet("add", flag.ContinueOnError)
	fs.SetOutput(stderr)
	rootFlag := fs.String("root", "", "Appstract root directory")
	outputFlag := fs.String("output", "", "Output level: silent|default|debug")
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
	outputLevel, err := resolveOutputLevel(root, *outputFlag)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	output := newCommandOutput(outputLevel, stdout, stderr)
	updateOpts := updateOptions{
		Output: output,
	}
	output.printDefault("add start: app=%s manifest=%s", app, sourceManifestPath)
	if err := ensureWorkspaceReady(root, executablePath); err != nil {
		output.printError("%v", err)
		return 1
	}

	if _, err := manifest.ParseFile(sourceManifestPath); err != nil {
		output.printError("validate add manifest: %v", err)
		return 1
	}
	output.printDefault("[ok] manifest validated: %s", sourceManifestPath)

	targetManifestPath := filepath.Join(root, "manifests", app+".json")
	if err := copyFile(sourceManifestPath, targetManifestPath); err != nil {
		output.printError("copy manifest: %v", err)
		return 1
	}
	output.printDefault("[ok] manifest saved: %s", targetManifestPath)

	if err := executeUpdateFromManifest(root, app, targetManifestPath, updateOpts); err != nil {
		output.printError("install app %q from manifest failed: %v", app, err)
		return 1
	}
	output.printDefault("[ok] add completed: %s", app)
	return 0
}

func executeUpdate(args []string, stdout, stderr io.Writer, envHome string) int {
	fs := flag.NewFlagSet("update", flag.ContinueOnError)
	fs.SetOutput(stderr)
	rootFlag := fs.String("root", "", "Appstract root directory")
	outputFlag := fs.String("output", "", "Output level: silent|default|debug")
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
	outputLevel, err := resolveOutputLevel(root, *outputFlag)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	output := newCommandOutput(outputLevel, stdout, stderr)
	if err := ensureWorkspaceReady(root, executablePath); err != nil {
		output.printError("%v", err)
		return 1
	}
	output.printDefault("update start: scanning manifests in %s", filepath.Join(root, "manifests"))

	manifestsDir := filepath.Join(root, "manifests")
	entries, err := os.ReadDir(manifestsDir)
	if err != nil {
		output.printError("%v", err)
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
			output.printError("skip invalid manifest filename: %s", name)
			continue
		}
		jobs = append(jobs, job{
			app:          app,
			manifestPath: filepath.Join(manifestsDir, name),
		})
	}

	if len(jobs) == 0 {
		output.printDefault("no manifests found in %s", manifestsDir)
		return 0
	}
	output.printDefault("found %d manifest(s)", len(jobs))

	opts := updateOptions{
		Checkver:     *checkver,
		PromptSwitch: *promptSwitch,
		Relaunch:     *relaunch,
		Output:       output,
	}

	successCount := 0
	failCount := 0
	for _, item := range jobs {
		output.printDefault("updating app: %s", item.app)
		if err := executeUpdateFromManifest(root, item.app, item.manifestPath, opts); err != nil {
			failCount++
			output.printError("update failed: %s (%v)", item.app, err)
			if *failFast {
				output.printDefault("update summary: total=%d success=%d failed=%d", len(jobs), successCount, failCount)
				return 1
			}
			continue
		}
		successCount++
		output.printDefault("[ok] update completed: %s", item.app)
	}
	output.printDefault("update summary: total=%d success=%d failed=%d", len(jobs), successCount, failCount)
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
