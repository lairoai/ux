package ux

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/lipgloss"
	"golang.org/x/term"
)

var (
	styleHeader  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("36"))
	styleDim     = lipgloss.NewStyle().Foreground(lipgloss.Color("242"))
	styleSuccess = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	styleFail    = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	styleBold    = lipgloss.NewStyle().Bold(true)
	styleLabel   = lipgloss.NewStyle().Foreground(lipgloss.Color("86")) // Cyan-ish

	iconSuccess = styleSuccess.Render("✓")
	iconFail    = styleFail.Render("✗")
	iconRunning = styleDim.Render("●")

	styleBox = lipgloss.NewStyle().
			PaddingLeft(2).
			PaddingRight(2).
			Border(lipgloss.NormalBorder(), false, false, false, true).
			BorderForeground(lipgloss.Color("240"))
)

const separator = "────────────────────────────────────────────────"

const clearLine = "\033[2K"

// output handles synchronized progress display during task execution.
type output struct {
	mu        sync.Mutex
	task      string
	total     int
	parallel  bool
	completed int
	failed    int
	running   []string
	isTTY     bool
	progress  progress.Model
}

func newOutput(task string, count int, parallel bool) *output {
	mode := "serial"
	if parallel {
		mode = "parallel"
	}

	header := styleHeader.Render("ux " + task)
	info := styleDim.Render(fmt.Sprintf("(%d packages, %s)", count, mode))
	fmt.Printf("\n%s  %s\n", header, info)

	// Create a progress bar with a nice gradient
	pg := progress.New(
		progress.WithDefaultGradient(),
		progress.WithoutPercentage(),
		progress.WithWidth(40),
	)

	return &output{
		task:     task,
		total:    count,
		parallel: parallel,
		isTTY:    term.IsTerminal(int(os.Stdout.Fd())),
		progress: pg,
	}
}

// markStarted records that a package has begun execution and updates progress.
func (o *output) markStarted(label string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.running = append(o.running, label)
	o.updateProgress()
}

// markCompleted records that a package has finished and updates progress.
func (o *output) markCompleted(r Result) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.completed++
	if !r.Success {
		o.failed++
	}
	// Remove from running
	for i, label := range o.running {
		if label == r.Package.Label {
			o.running = append(o.running[:i], o.running[i+1:]...)
			break
		}
	}
	o.updateProgress()
}

// updateProgress writes a single-line progress indicator. Must be called with mu held.
func (o *output) updateProgress() {
	if !o.isTTY {
		if o.completed > 0 && o.completed == o.total {
			passed := o.completed - o.failed
			status := fmt.Sprintf("  [%d/%d]", o.completed, o.total)
			if passed > 0 {
				status += " " + styleSuccess.Render(fmt.Sprintf("%d passed", passed))
			}
			if o.failed > 0 {
				status += " " + styleFail.Render(fmt.Sprintf("%d failed", o.failed))
			}
			fmt.Printf("%s\n", status)
		}
		return
	}

	// Update bar width based on terminal width
	width, _, err := term.GetSize(int(os.Stdout.Fd()))
	if err == nil {
		// Set bar to roughly 1/4 of terminal width, min 20, max 60
		barWidth := width / 4
		if barWidth < 20 {
			barWidth = 20
		}
		if barWidth > 60 {
			barWidth = 60
		}
		o.progress.Width = barWidth
	}

	ratio := float64(o.completed) / float64(o.total)
	bar := o.progress.ViewAs(ratio)

	passed := o.completed - o.failed
	status := fmt.Sprintf("  %s %d/%d", bar, o.completed, o.total)

	if passed > 0 {
		status += " " + styleSuccess.Render(fmt.Sprintf("%d", passed))
	}
	if o.failed > 0 {
		status += " " + styleFail.Render(fmt.Sprintf("%d", o.failed))
	}

	if len(o.running) > 0 {
		status += "  " + styleDim.Render(o.running[0])
		if len(o.running) > 1 {
			status += styleDim.Render(fmt.Sprintf(" +%d more", len(o.running)-1))
		}
	}

	fmt.Printf("\r%s%s", clearLine, status)
}

// clearProgress clears the progress line before summary output.
func (o *output) clearProgress() {
	if o.isTTY {
		fmt.Printf("\r%s", clearLine)
	}
}

// PrintSummary prints the sorted summary table, writes failure logs, and shows the final count.
// When verbose is true, failure output is printed inline.
func PrintSummary(task string, results []Result, verbose bool) {
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

	fmt.Printf("\n  %s\n\n", styleBold.Render("Results"))

	var rows []string
	for _, r := range sorted {
		icon := iconSuccess
		if !r.Success {
			icon = iconFail
		}
		label := styleLabel.Render(fmt.Sprintf("%-40s", r.Package.Label))
		dur := styleDim.Render(fmtDuration(r.Duration))
		rows = append(rows, fmt.Sprintf("  %s  %s %s", icon, label, dur))
	}

	fmt.Println(styleBox.Render(strings.Join(rows, "\n")))

	// Write log files and show details for failures
	if len(failures) > 0 {
		fmt.Println()
		for _, r := range failures {
			logFile := writeFailureLog(task, r)
			failHeader := styleFail.Bold(true).Render("FAIL")
			fmt.Printf("  %s %s\n", failHeader, r.Package.Label)
			if r.FailedStep != "" {
				fmt.Printf("    %s\n", styleDim.Render("→ "+r.FailedStep))
			}
			if verbose && r.Output != "" {
				fmt.Println()
				lines := strings.Split(strings.TrimRight(r.Output, "\n"), "\n")
				for _, line := range lines {
					fmt.Printf("    %s\n", line)
				}
				fmt.Println()
			}
			fmt.Printf("    %s\n", styleDim.Render("log: "+logFile))
		}
	}

	// Final count
	finalStatus := ""
	if failed > 0 {
		finalStatus = fmt.Sprintf("%s  %s  %s",
			styleBold.Render(task+":"),
			styleSuccess.Render(fmt.Sprintf("%d passed", passed)),
			styleFail.Render(fmt.Sprintf("%d failed", failed)))
	} else {
		finalStatus = fmt.Sprintf("%s  %s", styleBold.Render(task+":"), styleSuccess.Render(fmt.Sprintf("%d passed", passed)))
	}
	fmt.Printf("\n  %s\n\n", finalStatus)
}

// writeFailureLog writes the full output of a failed task to /tmp/ux/<task>/<label>.log.
func writeFailureLog(task string, r Result) string {
	// //packages/ingest → packages-ingest
	name := strings.TrimPrefix(r.Package.Label, "//")
	name = strings.ReplaceAll(name, "/", "-")

	dir := filepath.Join(os.TempDir(), "ux", task)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return ""
	}

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

	if err := os.WriteFile(path, []byte(content.String()), 0644); err != nil {
		return ""
	}
	return path
}

// PrintPackageList prints discovered packages (for `ux list`).
func PrintPackageList(packages []Package) {
	fmt.Printf("\n%s\n\n", styleHeader.Render("Workspace packages"))
	for _, pkg := range packages {
		typeStr := ""
		if pkg.Type != "" {
			typeStr = " " + styleHeader.Foreground(lipgloss.Color("36")).Render(pkg.Type)
		}
		label := pkg.Label
		name := styleDim.Render("(" + pkg.Name + ")")
		fmt.Printf("  %-40s %s%s\n", label, name, typeStr)

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
				source = styleDim.Render(" (default)")
			}
			taskName := styleSuccess.Render(fmt.Sprintf("%-12s", task))
			if len(cmds) == 1 {
				fmt.Printf("    %s %s%s\n", taskName, cmds[0], source)
			} else {
				fmt.Printf("    %s [%d steps]%s\n", taskName, len(cmds), source)
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
