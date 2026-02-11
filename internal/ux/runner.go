package ux

import (
	"bytes"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// Result captures the outcome of running a task on a single package.
type Result struct {
	Package    Package
	Success    bool
	Duration   time.Duration
	FailedStep string
	Output     string
}

// RunTask executes a task across all packages, respecting parallel/serial config.
// extraArgs are appended to each command (only valid for single-command tasks).
func RunTask(task string, packages []Package, cfg TaskConfig, extraArgs []string) []Result {
	results := make([]Result, len(packages))
	out := newOutput(task, len(packages), cfg.Parallel)

	if cfg.Parallel {
		var wg sync.WaitGroup
		for i, pkg := range packages {
			wg.Add(1)
			out.markStarted(pkg.Label)
			go func(i int, pkg Package) {
				defer wg.Done()
				results[i] = executeBuffered(task, pkg, extraArgs)
				out.markCompleted(results[i])
			}(i, pkg)
		}
		wg.Wait()
	} else {
		for i, pkg := range packages {
			out.markStarted(pkg.Label)
			results[i] = executeBuffered(task, pkg, extraArgs)
			out.markCompleted(results[i])
		}
	}

	out.clearProgress()
	return results
}

// executeBuffered runs a task and captures all output into a buffer.
func executeBuffered(task string, pkg Package, extraArgs []string) Result {
	cmds := pkg.Tasks[task]
	start := time.Now()

	var allOutput strings.Builder
	extra := ""
	if len(extraArgs) > 0 {
		extra = " " + strings.Join(extraArgs, " ")
	}

	for _, cmdStr := range cmds {
		var stdout, stderr bytes.Buffer

		cmd := exec.Command("sh", "-c", cmdStr+extra)
		cmd.Dir = pkg.Dir
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr

		err := cmd.Run()

		if stdout.Len() > 0 {
			allOutput.WriteString(stdout.String())
		}
		if stderr.Len() > 0 {
			allOutput.WriteString(stderr.String())
		}

		if err != nil {
			return Result{
				Package:    pkg,
				Success:    false,
				Duration:   time.Since(start),
				FailedStep: cmdStr + extra,
				Output:     allOutput.String(),
			}
		}
	}

	return Result{
		Package:  pkg,
		Success:  true,
		Duration: time.Since(start),
		Output:   allOutput.String(),
	}
}

// gitDiffFiles returns the list of files changed vs origin/main.
func gitDiffFiles(root string) (string, error) {
	cmd := exec.Command("git", "diff", "--name-only", "origin/main...HEAD")
	cmd.Dir = root
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &bytes.Buffer{} // suppress stderr
	err := cmd.Run()
	if err != nil {
		// Fallback: try without merge-base syntax
		cmd2 := exec.Command("git", "diff", "--name-only", "origin/main")
		cmd2.Dir = root
		out.Reset()
		cmd2.Stdout = &out
		err = cmd2.Run()
	}
	return out.String(), err
}
