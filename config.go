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
	Workspace WorkspaceConfig       `toml:"workspace"`
	Tasks     map[string]TaskConfig `toml:"tasks"`
}

type WorkspaceConfig struct {
	Members []string `toml:"members"`
}

type TaskConfig struct {
	Parallel bool `toml:"parallel"`
}

// Package is a resolved workspace member with its tasks.
type Package struct {
	Name  string
	Dir   string
	Label string // e.g. //packages/ingest
	Tasks map[string][]string
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
// Members use //label/... syntax:
//
//	//packages/...  → recursively find all ux.toml under packages/
//	//cli           → exact directory cli/
func discoverPackages(root string, cfg *RootConfig) ([]Package, error) {
	var packages []Package
	seen := make(map[string]bool)

	for _, member := range cfg.Workspace.Members {
		label := strings.TrimPrefix(member, "//")

		if strings.HasSuffix(label, "/...") {
			// Recursive discovery
			baseDir := strings.TrimSuffix(label, "/...")
			absBase := filepath.Join(root, baseDir)

			err := filepath.Walk(absBase, func(path string, info os.FileInfo, err error) error {
				if err != nil {
					return nil // skip inaccessible dirs
				}
				if info.Name() == "ux.toml" && path != filepath.Join(root, "ux.toml") {
					dir := filepath.Dir(path)
					if !seen[dir] {
						seen[dir] = true
						pkg, err := loadPackage(root, dir)
						if err != nil {
							return fmt.Errorf("loading %s: %w", path, err)
						}
						if pkg != nil {
							packages = append(packages, *pkg)
						}
					}
				}
				return nil
			})
			if err != nil {
				return nil, err
			}
		} else {
			// Exact directory
			dir := filepath.Join(root, label)
			uxToml := filepath.Join(dir, "ux.toml")
			if _, err := os.Stat(uxToml); err != nil {
				continue // directory doesn't exist yet, skip
			}
			if !seen[dir] {
				seen[dir] = true
				pkg, err := loadPackage(root, dir)
				if err != nil {
					return nil, fmt.Errorf("loading %s: %w", uxToml, err)
				}
				if pkg != nil {
					packages = append(packages, *pkg)
				}
			}
		}
	}

	sort.Slice(packages, func(i, j int) bool {
		return packages[i].Label < packages[j].Label
	})
	return packages, nil
}

// loadPackage reads a per-package ux.toml and returns a Package.
// Returns nil if the file doesn't define a [package] section.
func loadPackage(root, dir string) (*Package, error) {
	path := filepath.Join(dir, "ux.toml")

	var raw struct {
		Package struct {
			Name string `toml:"name"`
		} `toml:"package"`
		Tasks map[string]interface{} `toml:"tasks"`
	}

	_, err := toml.DecodeFile(path, &raw)
	if err != nil {
		return nil, err
	}

	if raw.Package.Name == "" {
		return nil, nil
	}

	rel, _ := filepath.Rel(root, dir)
	label := "//" + filepath.ToSlash(rel)

	tasks := make(map[string][]string)
	for name, v := range raw.Tasks {
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

	return &Package{
		Name:  raw.Package.Name,
		Dir:   dir,
		Label: label,
		Tasks: tasks,
	}, nil
}

// filterByLabel filters packages by a //label or //label/... pattern.
func filterByLabel(packages []Package, filter string) []Package {
	label := strings.TrimPrefix(filter, "//")

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

	// Exact match
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
		return nil, nil // nothing changed
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
