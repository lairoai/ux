package main

import (
	"fmt"
	"os"
	"strings"
)

func main() {
	args := os.Args[1:]

	if len(args) == 0 {
		printUsage()
		os.Exit(1)
	}

	// Parse arguments
	var task, filter string
	var affected bool

	for _, arg := range args {
		switch {
		case arg == "--help" || arg == "-h":
			printUsage()
			os.Exit(0)
		case arg == "--affected":
			affected = true
		case strings.HasPrefix(arg, "//"):
			filter = arg
		case strings.HasPrefix(arg, "-"):
			fmt.Fprintf(os.Stderr, "unknown flag: %s\n", arg)
			os.Exit(1)
		default:
			if task == "" {
				task = arg
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
		if err := runMigrate(dir); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		os.Exit(0)
	}

	// Find workspace root
	root, err := findWorkspaceRoot()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	// Load root config
	rootCfg, err := loadRootConfig(root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	// Discover all packages
	packages, err := discoverPackages(root, rootCfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	// Handle built-in commands
	if task == "list" {
		printPackageList(packages)
		os.Exit(0)
	}

	// Apply filters
	if filter != "" {
		packages = filterByLabel(packages, filter)
	}
	if affected {
		packages, err = filterAffected(root, packages)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error filtering affected packages: %v\n", err)
			os.Exit(1)
		}
	}

	// Keep only packages that define this task
	var relevant []Package
	for _, pkg := range packages {
		if _, ok := pkg.Tasks[task]; ok {
			relevant = append(relevant, pkg)
		}
	}

	if len(relevant) == 0 {
		fmt.Printf("no packages define task %q\n", task)
		os.Exit(0)
	}

	// Resolve task config (default to serial if not configured)
	taskCfg := rootCfg.Tasks[task]

	// Run
	results := runTask(task, relevant, taskCfg)

	// Print summary
	printSummary(task, results)

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
  ux <task> [//label] [--affected]

Commands:
  ux <task>                   Run task on all packages
  ux <task> //label           Run task on a specific package
  ux <task> //dir/...         Run task on all packages under dir/
  ux <task> --affected        Run task only on packages changed vs origin/main
  ux list                     List all discovered packages and their tasks
  ux migrate                  Migrate from turborepo (reads package.json + turbo.json)

Examples:
  ux lint                     Lint everything (parallel)
  ux test                     Test everything (serial)
  ux test //services/api      Test one package
  ux lint //packages/...      Lint all packages under packages/
  ux lint --affected          Lint only changed packages

Configuration:
  Root ux.toml defines workspace members and task settings.
  Each package has its own ux.toml defining available tasks.
`)
}
