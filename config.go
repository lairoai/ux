package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

// RootConfig is the workspace-level ux.toml.
type RootConfig struct {
	Workspace WorkspaceConfig          `toml:"workspace"`
	Tasks     map[string]TaskConfig    `toml:"tasks"`
	Defaults  map[string]TypeDefaults  `toml:"defaults"`
}

type WorkspaceConfig struct {
	Members []string `toml:"members"`
}

type TaskConfig struct {
	Parallel bool `toml:"parallel"`
}

// TypeDefaults defines default tasks for a package type (e.g., python, go).
type TypeDefaults struct {
	Tasks map[string]interface{} `toml:"tasks"`
}

// Package is a resolved workspace member with its tasks.
type Package struct {
	Name        string
	Type        string // "python", "go", etc. May be empty for legacy packages.
	Dir         string
	Label       string // e.g. //packages/ingest
	Tasks       map[string][]string
	TaskSources map[string]string // "default" or "override" per task name
}

// Marker files mapped to their type, checked in priority order.
var markerPriority = []struct {
	file     string
	typeName string
}{
	{"pyproject.toml", "python"},
	{"go.mod", "go"},
	{"Cargo.toml", "rust"},
}

// Directories to skip during recursive walks.
var skipDirs = map[string]bool{
	"node_modules": true, "vendor": true, "__pycache__": true,
	"venv": true, ".venv": true, "dist": true, "build": true,
}

// findWorkspaceRoot walks up from cwd looking for a ux.toml with [workspace].
func findWorkspaceRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		path := filepath.Join(dir, "ux.toml")
		if _, err := os.Stat(path); err == nil {
			var probe struct {
				Workspace *WorkspaceConfig `toml:"workspace"`
			}
			if _, err := toml.DecodeFile(path, &probe); err == nil && probe.Workspace != nil {
				return dir, nil
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no workspace root found (looking for ux.toml with [workspace])")
		}
		dir = parent
	}
}

// loadRootConfig parses the root ux.toml.
func loadRootConfig(root string) (*RootConfig, error) {
	var cfg RootConfig
	_, err := toml.DecodeFile(filepath.Join(root, "ux.toml"), &cfg)
	if err != nil {
		return nil, fmt.Errorf("parsing root ux.toml: %w", err)
	}
	return &cfg, nil
}

// discoverPackages resolves workspace members into packages.
// It finds directories that have a ux.toml OR a recognized marker file
// (pyproject.toml, go.mod, Cargo.toml) and resolves their tasks using
// type defaults + per-package overrides.
func discoverPackages(root string, cfg *RootConfig) ([]Package, error) {
	var packages []Package
	seen := make(map[string]bool)

	defaults := resolveDefaults(cfg.Defaults)

	for _, member := range cfg.Workspace.Members {
		label := strings.TrimPrefix(member, "//")

		if strings.HasSuffix(label, "/...") {
			baseDir := strings.TrimSuffix(label, "/...")
			absBase := filepath.Join(root, baseDir)

			err := filepath.Walk(absBase, func(path string, info os.FileInfo, err error) error {
				if err != nil {
					return nil
				}
				if !info.IsDir() {
					return nil
				}
				// Skip hidden and junk directories
				name := info.Name()
				if name != "." && strings.HasPrefix(name, ".") {
					return filepath.SkipDir
				}
				if skipDirs[name] {
					return filepath.SkipDir
				}
				// Don't treat the workspace root as a package
				if path == root {
					return nil
				}
				// Don't treat the base dir itself as a package (e.g., packages/)
				if path == absBase {
					return nil
				}
				if seen[path] {
					return nil
				}
				if !isPackageDir(path) {
					return nil
				}
				seen[path] = true
				pkg, err := resolvePackage(root, path, defaults)
				if err != nil {
					return fmt.Errorf("loading %s: %w", path, err)
				}
				if pkg != nil {
					packages = append(packages, *pkg)
				}
				return nil
			})
			if err != nil {
				return nil, err
			}
		} else {
			dir := filepath.Join(root, label)
			if seen[dir] {
				continue
			}
			if !isPackageDir(dir) {
				continue
			}
			seen[dir] = true
			pkg, err := resolvePackage(root, dir, defaults)
			if err != nil {
				return nil, fmt.Errorf("loading %s: %w", dir, err)
			}
			if pkg != nil {
				packages = append(packages, *pkg)
			}
		}
	}

	sort.Slice(packages, func(i, j int) bool {
		return packages[i].Label < packages[j].Label
	})
	return packages, nil
}

// isPackageDir returns true if the directory has a ux.toml or a recognized marker file.
func isPackageDir(dir string) bool {
	if _, err := os.Stat(filepath.Join(dir, "ux.toml")); err == nil {
		return true
	}
	for _, m := range markerPriority {
		if _, err := os.Stat(filepath.Join(dir, m.file)); err == nil {
			return true
		}
	}
	return false
}

// detectType checks for marker files and returns the detected type, or "".
func detectType(dir string) string {
	for _, m := range markerPriority {
		if _, err := os.Stat(filepath.Join(dir, m.file)); err == nil {
			return m.typeName
		}
	}
	return ""
}

// resolveDefaults pre-parses the [defaults.<type>.tasks] sections into resolved commands.
func resolveDefaults(raw map[string]TypeDefaults) map[string]map[string][]string {
	result := make(map[string]map[string][]string)
	for typeName, td := range raw {
		result[typeName] = parseTasks(td.Tasks)
	}
	return result
}

// parseTasks converts raw TOML task values (string or []string) to resolved []string commands.
func parseTasks(raw map[string]interface{}) map[string][]string {
	if raw == nil {
		return nil
	}
	tasks := make(map[string][]string)
	for name, v := range raw {
		switch val := v.(type) {
		case string:
			tasks[name] = []string{val}
		case []interface{}:
			var cmds []string
			for _, item := range val {
				if s, ok := item.(string); ok {
					cmds = append(cmds, s)
				}
			}
			tasks[name] = cmds
		}
	}
	return tasks
}

// resolvePackage loads a package from a directory, merging type defaults with per-package overrides.
//
// Resolution order (highest priority first):
//  1. Per-package [tasks] in ux.toml
//  2. Type defaults from root [defaults.<type>.tasks]
//
// Type is determined by: explicit type in ux.toml > auto-detected from marker files.
func resolvePackage(root, dir string, defaults map[string]map[string][]string) (*Package, error) {
	rel, _ := filepath.Rel(root, dir)
	label := "//" + filepath.ToSlash(rel)

	var name, explicitType string
	var overrideTasks map[string][]string

	// Try loading ux.toml
	uxPath := filepath.Join(dir, "ux.toml")
	if _, err := os.Stat(uxPath); err == nil {
		var raw struct {
			Package struct {
				Name string `toml:"name"`
				Type string `toml:"type"`
			} `toml:"package"`
			Tasks map[string]interface{} `toml:"tasks"`
		}
		if _, err := toml.DecodeFile(uxPath, &raw); err != nil {
			return nil, err
		}
		name = raw.Package.Name
		explicitType = raw.Package.Type
		overrideTasks = parseTasks(raw.Tasks)
	}

	// Default name to directory basename
	if name == "" {
		name = filepath.Base(dir)
	}

	// Determine type: explicit > auto-detect
	pkgType := explicitType
	if pkgType == "" {
		pkgType = detectType(dir)
	}

	// No type and no explicit tasks → not a usable package
	if pkgType == "" && len(overrideTasks) == 0 {
		return nil, nil
	}

	// Merge: start with type defaults, then apply per-package overrides
	tasks := make(map[string][]string)
	taskSources := make(map[string]string)

	if pkgType != "" {
		if dt, ok := defaults[pkgType]; ok {
			for k, v := range dt {
				tasks[k] = v
				taskSources[k] = "default"
			}
		}
	}
	for k, v := range overrideTasks {
		tasks[k] = v
		taskSources[k] = "override"
	}

	// No tasks resolved → skip
	if len(tasks) == 0 {
		return nil, nil
	}

	return &Package{
		Name:        name,
		Type:        pkgType,
		Dir:         dir,
		Label:       label,
		Tasks:       tasks,
		TaskSources: taskSources,
	}, nil
}

// filterByLabel filters packages by a //label or //label/... pattern.
// //... matches all packages.
func filterByLabel(packages []Package, filter string) []Package {
	label := strings.TrimPrefix(filter, "//")

	// //... means everything
	if label == "..." {
		return packages
	}

	if strings.HasSuffix(label, "/...") {
		prefix := strings.TrimSuffix(label, "/...")
		var result []Package
		for _, pkg := range packages {
			pkgPath := strings.TrimPrefix(pkg.Label, "//")
			if strings.HasPrefix(pkgPath, prefix+"/") || pkgPath == prefix {
				result = append(result, pkg)
			}
		}
		return result
	}

	var result []Package
	for _, pkg := range packages {
		pkgPath := strings.TrimPrefix(pkg.Label, "//")
		if pkgPath == label {
			result = append(result, pkg)
		}
	}
	return result
}

// filterAffected keeps only packages that have changed files vs origin/main.
func filterAffected(root string, packages []Package) ([]Package, error) {
	raw, err := gitDiffFiles(root)
	if err != nil {
		return nil, err
	}

	changedFiles := strings.Split(strings.TrimSpace(raw), "\n")
	if len(changedFiles) == 1 && changedFiles[0] == "" {
		return nil, nil
	}

	var result []Package
	for _, pkg := range packages {
		rel, _ := filepath.Rel(root, pkg.Dir)
		prefix := filepath.ToSlash(rel) + "/"
		for _, f := range changedFiles {
			if strings.HasPrefix(f, prefix) {
				result = append(result, pkg)
				break
			}
		}
	}
	return result, nil
}
