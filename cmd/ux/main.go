package main

import (
	"fmt"
	"os"
	"strings"

	ux "github.com/lairoai/ux/internal/ux"
)

func main() {
	args := os.Args[1:]

	if len(args) == 0 {
		printUsage()
		os.Exit(1)
	}

	// Split at first "--": everything after goes to extraArgs
	var extraArgs []string
	for i, arg := range args {
		if arg == "--" {
			extraArgs = args[i+1:]
			args = args[:i]
			break
		}
	}

	// Parse arguments
	var task, filter string
	var affected, verbose bool

	for _, arg := range args {
		switch {
		case arg == "--help" || arg == "-h":
			printUsage()
			os.Exit(0)
		case arg == "--affected":
			affected = true
		case arg == "--verbose" || arg == "-v":
			verbose = true
		case task != "" && ux.IsFilterArg(arg):
			filter = arg
		case strings.HasPrefix(arg, "-"):
			fmt.Fprintf(os.Stderr, "unknown flag: %s\n", arg)
			os.Exit(1)
		default:
			if task == "" {
				task = arg
			} else if ux.IsFilterArg(arg) {
				filter = arg
			} else {
				fmt.Fprintf(os.Stderr, "unexpected argument: %s\n", arg)
				os.Exit(1)
			}
		}
	}

	if task == "" {
		printUsage()
		os.Exit(1)
	}

	// Handle migrate before workspace discovery (ux.toml doesn't exist yet)
	if task == "migrate" {
		dir, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		if err := ux.RunMigrate(dir); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		os.Exit(0)
	}

	// Find workspace root
	root, err := ux.FindWorkspaceRoot()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	// Resolve relative filter to absolute //label
	if filter != "" {
		cwd, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		filter, err = ux.ResolveFilter(root, cwd, filter)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	}

	// Load root config
	rootCfg, err := ux.LoadRootConfig(root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	// Discover all packages
	packages, err := ux.DiscoverPackages(root, rootCfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	// Handle built-in commands
	if task == "list" {
		ux.PrintPackageList(packages)
		os.Exit(0)
	}

	// Apply filters
	if filter != "" {
		packages = ux.FilterByLabel(packages, filter)
	}
	if affected {
		packages, err = ux.FilterAffected(root, packages)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error filtering affected packages: %v\n", err)
			os.Exit(1)
		}
	}

	// Keep only packages that define this task
	var relevant []ux.Package
	for _, pkg := range packages {
		if _, ok := pkg.Tasks[task]; ok {
			relevant = append(relevant, pkg)
		}
	}

	if len(relevant) == 0 {
		fmt.Printf("no packages define task %q\n", task)
		os.Exit(0)
	}

	// Validate extra args: reject multi-step tasks
	if len(extraArgs) > 0 {
		for _, pkg := range relevant {
			if cmds := pkg.Tasks[task]; len(cmds) > 1 {
				fmt.Fprintf(os.Stderr, "error: cannot pass extra args (--) to multi-step task %q in %s (%d steps)\n",
					task, pkg.Label, len(cmds))
				os.Exit(1)
			}
		}
	}

	// Resolve task config (default to serial if not configured)
	taskCfg := rootCfg.Tasks[task]

	// Run
	results := ux.RunTask(task, relevant, taskCfg, extraArgs)

	// Print summary
	ux.PrintSummary(task, results, verbose)

	// Exit 1 if any failures
	for _, r := range results {
		if !r.Success {
			os.Exit(1)
		}
	}
}

func printUsage() {
	fmt.Print(`ux - simple monorepo task runner

Usage:
  ux <task> [target] [--affected] [-- extra args...]

Targets:
  //label             Absolute from workspace root
  //dir/...           All packages under dir/
  .                   Package at current directory
  ...  ./...          All packages under current directory
  foo/bar             Relative to current directory

Commands:
  ux <task>                   Run task on all packages
  ux <task> .                 Run task on the package at cwd
  ux <task> ...               Run task on all packages under cwd
  ux <task> //label           Run task on a specific package (absolute)
  ux <task> //dir/...         Run task on all packages under dir/
  ux <task> --affected        Run task only on packages changed vs origin/main
  ux <task> -v                Show failure output inline (verbose)
  ux <task> -- -n auto        Append flags to the underlying command
  ux list                     List all discovered packages and their tasks
  ux migrate                  Migrate from turborepo (reads package.json + turbo.json)

Examples:
  ux lint                     Lint everything (parallel)
  ux test                     Test everything (serial)
  ux test //services/api      Test one package
  ux lint //packages/...      Lint all packages under packages/
  ux lint --affected          Lint only changed packages
  cd packages/api && ux test .   Test from inside a package
  ux test -- -n auto          Append pytest flags

Configuration:
  Root ux.toml defines workspace members and task settings.
  Each package has its own ux.toml defining available tasks.
`)
}
