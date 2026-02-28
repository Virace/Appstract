package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"appstract/internal/bootstrap"
	"appstract/internal/config"
	"appstract/internal/manifest"
	"appstract/internal/updater"
)

func Execute(args []string, stdout, stderr io.Writer, envHome string) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: appstract <init|run|manifest|update>")
		return 1
	}
	switch args[0] {
	case "init":
		return executeInit(args[1:], stdout, stderr, envHome)
	case "run":
		return executeRun(args[1:], stdout, stderr, envHome)
	case "manifest":
		return executeManifest(args[1:], stdout, stderr)
	case "update":
		return executeUpdate(args[1:], stdout, stderr, envHome)
	default:
		fmt.Fprintf(stderr, "unknown command: %s\n", args[0])
		return 1
	}
}

func executeInit(args []string, stdout, stderr io.Writer, envHome string) int {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	fs.SetOutput(stderr)
	rootFlag := fs.String("root", "", "Appstract root directory")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	root, err := bootstrap.ResolveRoot(envHome, *rootFlag)
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
		return 1
	}
	if fs.NArg() < 1 {
		fmt.Fprintln(stderr, "usage: appstract run [--root <path>] <app>")
		return 1
	}
	app := fs.Arg(0)

	root, err := bootstrap.ResolveRoot(envHome, *rootFlag)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	currentPath := filepath.Join(root, "apps", app, "current")
	if _, err := os.Stat(currentPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			fmt.Fprintf(stderr, "app %q has no current version at %s\n", app, currentPath)
			return 1
		}
		fmt.Fprintln(stderr, err)
		return 1
	}

	// M1 only validates runnable layout; process launch is implemented in M2/M3.
	fmt.Fprintf(stdout, "run-ready: %s (%s)\n", app, currentPath)
	return 0
}

func executeManifest(args []string, stdout, stderr io.Writer) int {
	if len(args) < 2 || args[0] != "validate" {
		fmt.Fprintln(stderr, "usage: appstract manifest validate <file>")
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

func executeUpdate(args []string, stdout, stderr io.Writer, envHome string) int {
	fs := flag.NewFlagSet("update", flag.ContinueOnError)
	fs.SetOutput(stderr)
	rootFlag := fs.String("root", "", "Appstract root directory")
	manifestPath := fs.String("manifest", "", "Manifest file path")
	checkver := fs.Bool("checkver", false, "Resolve latest version from checkver.github")
	promptSwitch := fs.Bool("prompt-switch", false, "Prompt user before switching current version")
	relaunch := fs.Bool("relaunch", false, "Relaunch app after successful switch")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if fs.NArg() < 1 || *manifestPath == "" {
		fmt.Fprintln(stderr, "usage: appstract update [--root <path>] [--checkver] [--prompt-switch] [--relaunch] --manifest <file> <app>")
		return 1
	}
	app := fs.Arg(0)

	root, err := bootstrap.ResolveRoot(envHome, *rootFlag)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if err := bootstrap.InitLayout(root); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	manager := updater.NewManager(root)
	manager.UseCheckver = *checkver
	manager.PromptSwitch = *promptSwitch
	manager.Relaunch = *relaunch
	cfg, err := config.Load(root)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	manager.KeepVersions = cfg.KeepVersions
	if err := manager.UpdateFromManifest(app, *manifestPath); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintf(stdout, "update completed: %s\n", app)
	return 0
}
