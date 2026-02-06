package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type packageJSON struct {
	Name       string            `json:"name"`
	Workspaces json.RawMessage   `json:"workspaces"`
	Scripts    map[string]string `json:"scripts"`
}

type turboJSON struct {
	Tasks map[string]json.RawMessage `json:"tasks"`
}

// runMigrate reads a turborepo workspace and generates ux.toml files.
func runMigrate(dir string) error {
	fmt.Printf("\n%s%sux migrate%s\n\n", bold, cyan, reset)

	// 1. Read root package.json
	rootPkg, err := readPackageJSON(filepath.Join(dir, "package.json"))
	if err != nil {
		return fmt.Errorf("reading root package.json: %w\n  (run this from the root of your turborepo)", err)
	}

	workspacePatterns, err := parseWorkspaces(rootPkg.Workspaces)
	if err != nil {
		return fmt.Errorf("parsing workspaces: %w", err)
	}
	if len(workspacePatterns) == 0 {
		return fmt.Errorf("root package.json has no workspaces defined")
	}

	// 2. Try to read turbo.json for task definitions
	turbo, _ := readTurboJSON(filepath.Join(dir, "turbo.json"))

	// 3. Detect which tasks are serial from root scripts (--concurrency=1)
	serialTasks := detectSerialTasks(rootPkg.Scripts)

	// 4. Collect all task names from turbo.json + root scripts
	taskNames := collectTaskNames(turbo, rootPkg.Scripts)

	// 5. Convert npm workspace patterns to //... labels
	members := convertWorkspacePatterns(workspacePatterns)

	// 6. Generate and write root ux.toml
	rootToml := generateRootToml(members, taskNames, serialTasks)
	rootPath := filepath.Join(dir, "ux.toml")
	if written, err := writeFileIfNew(rootPath, rootToml); err != nil {
		return err
	} else if written {
		fmt.Printf("  %s✓%s  ux.toml\n", green, reset)
	} else {
		fmt.Printf("  %s~%s  ux.toml %s(already exists, skipped)%s\n", dim, reset, dim, reset)
	}

	// 7. Expand workspace patterns and migrate each member
	var migrated int
	for _, pattern := range workspacePatterns {
		dirs, err := expandWorkspaceGlob(dir, pattern)
		if err != nil {
			continue
		}
		for _, memberDir := range dirs {
			memberPkg, err := readPackageJSON(filepath.Join(memberDir, "package.json"))
			if err != nil {
				continue
			}
			if len(memberPkg.Scripts) == 0 {
				continue
			}

			rel, _ := filepath.Rel(dir, memberDir)
			pkgToml := generatePackageToml(memberPkg)
			pkgPath := filepath.Join(memberDir, "ux.toml")

			if written, err := writeFileIfNew(pkgPath, pkgToml); err != nil {
				return err
			} else if written {
				fmt.Printf("  %s✓%s  %s/ux.toml\n", green, reset, filepath.ToSlash(rel))
				migrated++
			} else {
				fmt.Printf("  %s~%s  %s/ux.toml %s(already exists, skipped)%s\n",
					dim, reset, filepath.ToSlash(rel), dim, reset)
			}
		}
	}

	fmt.Printf("\n%sMigrated %d packages.%s\n", bold, migrated, reset)
	fmt.Printf("%sYou can now run: ux list%s\n\n", dim, reset)
	return nil
}

// parseWorkspaces handles both array and object forms of "workspaces" in package.json.
//
//	Array form:  ["packages/*", "services/*"]
//	Object form: {"packages": ["packages/*", "services/*"]}
func parseWorkspaces(raw json.RawMessage) ([]string, error) {
	if len(raw) == 0 {
		return nil, nil
	}

	// Try array first
	var arr []string
	if err := json.Unmarshal(raw, &arr); err == nil {
		return arr, nil
	}

	// Try object form (yarn-style)
	var obj struct {
		Packages []string `json:"packages"`
	}
	if err := json.Unmarshal(raw, &obj); err == nil {
		return obj.Packages, nil
	}

	return nil, fmt.Errorf("unrecognized workspaces format")
}

func readPackageJSON(path string) (*packageJSON, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var pkg packageJSON
	if err := json.Unmarshal(data, &pkg); err != nil {
		return nil, err
	}
	return &pkg, nil
}

func readTurboJSON(path string) (*turboJSON, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var t turboJSON
	if err := json.Unmarshal(data, &t); err != nil {
		return nil, err
	}
	return &t, nil
}

// detectSerialTasks parses root scripts for "turbo run <task> --concurrency=1".
func detectSerialTasks(scripts map[string]string) map[string]bool {
	serial := make(map[string]bool)
	for _, cmd := range scripts {
		if !strings.Contains(cmd, "--concurrency=1") {
			continue
		}
		if task := extractTurboTaskName(cmd); task != "" {
			serial[task] = true
		}
	}
	return serial
}

// collectTaskNames gathers unique task names from turbo.json and root scripts.
func collectTaskNames(turbo *turboJSON, scripts map[string]string) []string {
	seen := make(map[string]bool)

	if turbo != nil {
		for name := range turbo.Tasks {
			seen[name] = true
		}
	}

	for _, cmd := range scripts {
		if task := extractTurboTaskName(cmd); task != "" {
			seen[task] = true
		}
	}

	var names []string
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// extractTurboTaskName pulls the task name from "turbo run <task> [flags...]".
func extractTurboTaskName(cmd string) string {
	parts := strings.Fields(cmd)
	for i, p := range parts {
		if p == "run" && i+1 < len(parts) {
			task := parts[i+1]
			if !strings.HasPrefix(task, "-") {
				return task
			}
		}
	}
	return ""
}

// convertWorkspacePatterns turns npm patterns into //label syntax.
//
//	"packages/*"  → "//packages/..."
//	"cli"         → "//cli"
func convertWorkspacePatterns(patterns []string) []string {
	var members []string
	for _, p := range patterns {
		p = strings.TrimSuffix(p, "/")
		switch {
		case strings.HasSuffix(p, "/*"):
			members = append(members, "//"+strings.TrimSuffix(p, "/*")+"/...")
		case strings.HasSuffix(p, "/**"):
			members = append(members, "//"+strings.TrimSuffix(p, "/**")+"/...")
		default:
			members = append(members, "//"+p)
		}
	}
	return members
}

func generateRootToml(members, taskNames []string, serialTasks map[string]bool) string {
	var b strings.Builder

	b.WriteString("[workspace]\nmembers = [\n")
	for i, m := range members {
		b.WriteString(fmt.Sprintf("  %q", m))
		if i < len(members)-1 {
			b.WriteString(",")
		}
		b.WriteString("\n")
	}
	b.WriteString("]\n\n[tasks]\n")

	for _, name := range taskNames {
		parallel := !serialTasks[name]
		b.WriteString(fmt.Sprintf("%s = { parallel = %v }\n", name, parallel))
	}

	return b.String()
}

func generatePackageToml(pkg *packageJSON) string {
	var b strings.Builder

	// Derive short name: "@scope/foo" → "foo"
	name := pkg.Name
	if idx := strings.LastIndex(name, "/"); idx >= 0 {
		name = name[idx+1:]
	}

	b.WriteString("[package]\n")
	b.WriteString(fmt.Sprintf("name = %q\n\n", name))
	b.WriteString("[tasks]\n")

	var names []string
	for k := range pkg.Scripts {
		names = append(names, k)
	}
	sort.Strings(names)

	for _, k := range names {
		b.WriteString(fmt.Sprintf("%s = %q\n", k, pkg.Scripts[k]))
	}

	return b.String()
}

// expandWorkspaceGlob expands an npm workspace pattern to matching directories.
func expandWorkspaceGlob(root, pattern string) ([]string, error) {
	full := filepath.Join(root, pattern)
	matches, err := filepath.Glob(full)
	if err != nil {
		return nil, err
	}

	var dirs []string
	for _, m := range matches {
		info, err := os.Stat(m)
		if err != nil || !info.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(m, "package.json")); err == nil {
			dirs = append(dirs, m)
		}
	}
	sort.Strings(dirs)
	return dirs, nil
}

// writeFileIfNew writes content to path only if the file doesn't already exist.
// Returns (true, nil) if written, (false, nil) if skipped.
func writeFileIfNew(path, content string) (bool, error) {
	if _, err := os.Stat(path); err == nil {
		return false, nil
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return false, err
	}
	return true, nil
}
