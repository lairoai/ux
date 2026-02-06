package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	reset = "\033[0m"
	bold  = "\033[1m"
	dim   = "\033[2m"
	red   = "\033[31m"
	green = "\033[32m"
	cyan  = "\033[36m"
)

const separator = "────────────────────────────────────────────────"

// output handles synchronized printing of task results.
type output struct {
	mu sync.Mutex
}

func newOutput(task string, count int, parallel bool) *output {
	mode := "serial"
	if parallel {
		mode = "parallel"
	}
	fmt.Printf("\n%s%sux %s%s  %s(%d packages, %s)%s\n\n",
		bold, cyan, task, reset, dim, count, mode, reset)
	return &output{}
}

// printRunning shows the "● label" indicator at the start of serial execution.
func (o *output) printRunning(label string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	fmt.Printf("  %s●%s  %s\n", cyan, reset, label)
}

// printStep shows the "→ command" line for each step in serial mode.
func (o *output) printStep(cmdStr string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	fmt.Printf("    %s→ %s%s\n", dim, cmdStr, reset)
}

// printResult shows the ✓/✗ result line after a package completes.
func (o *output) printResult(r Result) {
	o.mu.Lock()
	defer o.mu.Unlock()

	icon := green + "✓" + reset
	if !r.Success {
		icon = red + "✗" + reset
	}
	fmt.Printf("  %s  %-40s %s%s%s\n", icon, r.Package.Label, dim, fmtDuration(r.Duration), reset)
}

// printBlank prints a blank line (used between serial packages).
func (o *output) printBlank() {
	o.mu.Lock()
	defer o.mu.Unlock()
	fmt.Println()
}

// printSummary prints the sorted summary table, writes failure logs, and shows the final count.
// When verbose is true, failure output is printed inline.
func printSummary(task string, results []Result, verbose bool) {
	// Sort by label for a stable, scannable summary
	sorted := make([]Result, len(results))
	copy(sorted, results)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Package.Label < sorted[j].Package.Label
	})

	var passed, failed int
	var failures []Result

	for _, r := range sorted {
		if r.Success {
			passed++
		} else {
			failed++
			failures = append(failures, r)
		}
	}

	// Summary table
	fmt.Printf("%s%s%s\n\n", dim, separator, reset)

	for _, r := range sorted {
		icon := green + "✓" + reset
		if !r.Success {
			icon = red + "✗" + reset
		}
		fmt.Printf("  %s  %-40s %s%s%s\n", icon, r.Package.Label, dim, fmtDuration(r.Duration), reset)
	}

	// Write log files and show details for failures
	if len(failures) > 0 {
		fmt.Println()
		for _, r := range failures {
			logFile := writeFailureLog(task, r)
			fmt.Printf("  %s%sFAIL%s %s\n", bold, red, reset, r.Package.Label)
			if r.FailedStep != "" {
				fmt.Printf("    %s→ %s%s\n", dim, r.FailedStep, reset)
			}
			if verbose && r.Output != "" {
				fmt.Println()
				lines := strings.Split(strings.TrimRight(r.Output, "\n"), "\n")
				for _, line := range lines {
					fmt.Printf("    %s\n", line)
				}
				fmt.Println()
			}
			fmt.Printf("    %slog: %s%s\n", dim, logFile, reset)
		}
	}

	// Final count
	fmt.Printf("\n%s%s%s\n", dim, separator, reset)
	if failed > 0 {
		fmt.Printf("%s: %s%d passed%s, %s%d failed%s\n\n",
			task, green, passed, reset, red, failed, reset)
	} else {
		fmt.Printf("%s: %s%d passed%s\n\n", task, green, passed, reset)
	}
}

// writeFailureLog writes the full output of a failed task to /tmp/ux/<task>/<label>.log.
func writeFailureLog(task string, r Result) string {
	// //packages/ingest → packages-ingest
	name := strings.TrimPrefix(r.Package.Label, "//")
	name = strings.ReplaceAll(name, "/", "-")

	dir := filepath.Join(os.TempDir(), "ux", task)
	os.MkdirAll(dir, 0755)

	path := filepath.Join(dir, name+".log")

	var content strings.Builder
	fmt.Fprintf(&content, "ux %s %s\n", task, r.Package.Label)
	fmt.Fprintf(&content, "dir: %s\n", r.Package.Dir)
	if r.FailedStep != "" {
		fmt.Fprintf(&content, "failed step: %s\n", r.FailedStep)
	}
	fmt.Fprintf(&content, "duration: %s\n", fmtDuration(r.Duration))
	content.WriteString("\n--- output ---\n\n")
	content.WriteString(r.Output)

	os.WriteFile(path, []byte(content.String()), 0644)
	return path
}

// printPackageList prints discovered packages (for `ux list`).
func printPackageList(packages []Package) {
	fmt.Printf("\n%s%sWorkspace packages%s\n\n", bold, cyan, reset)
	for _, pkg := range packages {
		typeStr := ""
		if pkg.Type != "" {
			typeStr = " " + pkg.Type
		}
		fmt.Printf("  %-40s %s(%s)%s%s%s\n", pkg.Label, dim, pkg.Name, reset, cyan, typeStr+reset)

		// Sort task names for stable output
		var taskNames []string
		for t := range pkg.Tasks {
			taskNames = append(taskNames, t)
		}
		sort.Strings(taskNames)

		for _, task := range taskNames {
			cmds := pkg.Tasks[task]
			source := ""
			if s, ok := pkg.TaskSources[task]; ok && s == "default" {
				source = dim + " (default)" + reset
			}
			if len(cmds) == 1 {
				fmt.Printf("    %s%-12s%s %s%s\n", green, task, reset, cmds[0], source)
			} else {
				fmt.Printf("    %s%-12s%s [%d steps]%s\n", green, task, reset, len(cmds), source)
			}
		}
	}
	fmt.Println()
}

func fmtDuration(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	return fmt.Sprintf("%.1fs", d.Seconds())
}
