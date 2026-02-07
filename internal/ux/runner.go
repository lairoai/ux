package ux

import (
	"bytes"
	"io"
	"os"
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
func RunTask(task string, packages []Package, cfg TaskConfig) []Result {
	results := make([]Result, len(packages))
	out := newOutput(task, len(packages), cfg.Parallel)

	if cfg.Parallel {
		var wg sync.WaitGroup
		for i, pkg := range packages {
			wg.Add(1)
			go func(i int, pkg Package) {
				defer wg.Done()
				results[i] = executeBuffered(task, pkg)
				out.printResult(results[i])
			}(i, pkg)
		}
		wg.Wait()
	} else {
		for i, pkg := range packages {
			out.printRunning(pkg.Label)
			results[i] = executeStreaming(task, pkg, out)
			out.printResult(results[i])
			out.printBlank()
		}
	}

	return results
}

// executeBuffered runs a task and captures all output into a buffer (for parallel mode).
func executeBuffered(task string, pkg Package) Result {
	cmds := pkg.Tasks[task]
	start := time.Now()

	var allOutput strings.Builder

	for _, cmdStr := range cmds {
		var stdout, stderr bytes.Buffer

		cmd := exec.Command("sh", "-c", cmdStr)
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
				FailedStep: cmdStr,
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

// executeStreaming runs a task and streams output to the terminal in real time
// (for serial mode). Output is also captured into a buffer for log files.
func executeStreaming(task string, pkg Package, out *output) Result {
	cmds := pkg.Tasks[task]
	start := time.Now()

	var allOutput strings.Builder
	pw := &prefixWriter{prefix: "    ", writer: os.Stdout, atStart: true}

	for _, cmdStr := range cmds {
		out.printStep(cmdStr)

		var buf bytes.Buffer
		tee := io.MultiWriter(pw, &buf)

		cmd := exec.Command("sh", "-c", cmdStr)
		cmd.Dir = pkg.Dir
		cmd.Stdout = tee
		cmd.Stderr = tee

		err := cmd.Run()

		allOutput.WriteString(buf.String())

		if err != nil {
			return Result{
				Package:    pkg,
				Success:    false,
				Duration:   time.Since(start),
				FailedStep: cmdStr,
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

// prefixWriter wraps an io.Writer and prepends a prefix at the start of each line.
type prefixWriter struct {
	prefix  string
	writer  io.Writer
	atStart bool
}

func (pw *prefixWriter) Write(p []byte) (int, error) {
	total := len(p)
	for len(p) > 0 {
		if pw.atStart {
			if _, err := io.WriteString(pw.writer, pw.prefix); err != nil {
				return total, err
			}
			pw.atStart = false
		}
		idx := bytes.IndexByte(p, '\n')
		if idx < 0 {
			// No newline — write remainder
			_, err := pw.writer.Write(p)
			return total, err
		}
		// Write through the newline, then flag next write for prefix
		if _, err := pw.writer.Write(p[:idx+1]); err != nil {
			return total, err
		}
		p = p[idx+1:]
		pw.atStart = true
	}
	return total, nil
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
