package main

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

const (
	reset  = "\033[0m"
	bold   = "\033[1m"
	dim    = "\033[2m"
	red    = "\033[31m"
	green  = "\033[32m"
	cyan   = "\033[36m"
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

func (o *output) printResult(r Result) {
	o.mu.Lock()
	defer o.mu.Unlock()

	icon := green + "✓" + reset
	if !r.Success {
		icon = red + "✗" + reset
	}
	fmt.Printf("  %s  %-40s %s%s%s\n", icon, r.Package.Label, dim, fmtDuration(r.Duration), reset)
}

// printSummary prints failure details and the final summary line.
func printSummary(task string, results []Result) {
	var passed, failed int
	var failures []Result

	for _, r := range results {
		if r.Success {
			passed++
		} else {
			failed++
			failures = append(failures, r)
		}
	}

	fmt.Println()

	// Show failure details
	for _, r := range failures {
		fmt.Printf("%s%s%s\n", dim, separator, reset)
		fmt.Printf("%s%sFAIL%s  %s\n", bold, red, reset, r.Package.Label)
		if r.FailedStep != "" {
			fmt.Printf("  %s→ %s%s\n", dim, r.FailedStep, reset)
		}
		if r.Output != "" {
			fmt.Println()
			lines := strings.Split(strings.TrimRight(r.Output, "\n"), "\n")
			for _, line := range lines {
				fmt.Printf("    %s\n", line)
			}
		}
		fmt.Println()
	}

	fmt.Printf("%s%s%s\n", dim, separator, reset)
	if failed > 0 {
		fmt.Printf("%s: %s%d passed%s, %s%d failed%s\n\n",
			task, green, passed, reset, red, failed, reset)
	} else {
		fmt.Printf("%s: %s%d passed%s\n\n", task, green, passed, reset)
	}
}

// printPackageList prints discovered packages (for `ux list`).
func printPackageList(packages []Package) {
	fmt.Printf("\n%s%sWorkspace packages%s\n\n", bold, cyan, reset)
	for _, pkg := range packages {
		fmt.Printf("  %-40s %s(%s)%s\n", pkg.Label, dim, pkg.Name, reset)
		for task, cmds := range pkg.Tasks {
			if len(cmds) == 1 {
				fmt.Printf("    %s%-12s%s %s\n", green, task, reset, cmds[0])
			} else {
				fmt.Printf("    %s%-12s%s [%d steps]\n", green, task, reset, len(cmds))
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
