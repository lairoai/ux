package ux

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

// migratedPackage holds a workspace member's info during migration.
type migratedPackage struct {
	dir     string
	name    string
	pkgType string
	scripts map[string]string
}

// RunMigrate reads a turborepo workspace and generates ux.toml files.
// It detects package types from marker files, groups common scripts into
// [defaults.<type>.tasks], and emits minimal per-package configs.
func RunMigrate(dir string) error {
	fmt.Printf("\n%s\n\n", styleHeader.Render("ux migrate"))

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

	// 6. Discover all workspace members, detect their types
	var allPkgs []migratedPackage
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
			name := memberPkg.Name
			if idx := strings.LastIndex(name, "/"); idx >= 0 {
				name = name[idx+1:]
			}
			allPkgs = append(allPkgs, migratedPackage{
				dir:     memberDir,
				name:    name,
				pkgType: detectType(memberDir),
				scripts: memberPkg.Scripts,
			})
		}
	}

	// 7. Find common scripts per type → these become [defaults.<type>.tasks]
	typeDefaults := findTypeDefaults(allPkgs)

	// 8. Generate and write root ux.toml (now with defaults)
	rootToml := generateRootTomlWithDefaults(members, taskNames, serialTasks, typeDefaults)
	rootPath := filepath.Join(dir, "ux.toml")
	if written, err := writeFileIfNew(rootPath, rootToml); err != nil {
		return err
	} else if written {
		fmt.Printf("  %s  ux.toml\n", iconSuccess)
	} else {
		fmt.Printf("  %s  ux.toml %s\n", styleDim.Render("~"), styleDim.Render("(already exists, skipped)"))
	}

	// 9. Generate per-package ux.toml (minimal: type + overrides only)
	var migrated int
	for _, pkg := range allPkgs {
		rel, _ := filepath.Rel(dir, pkg.dir)
		pkgToml := generateMinimalPackageToml(pkg, typeDefaults)
		pkgPath := filepath.Join(pkg.dir, "ux.toml")

		if written, err := writeFileIfNew(pkgPath, pkgToml); err != nil {
			return err
		} else if written {
			fmt.Printf("  %s  %s/ux.toml\n", iconSuccess, filepath.ToSlash(rel))
			migrated++
		} else {
			fmt.Printf("  %s  %s/ux.toml %s\n",
				styleDim.Render("~"), filepath.ToSlash(rel), styleDim.Render("(already exists, skipped)"))
		}
	}

	fmt.Printf("\n%s\n", styleBold.Render(fmt.Sprintf("Migrated %d packages.", migrated)))
	fmt.Printf("%s\n\n", styleDim.Render("You can now run: ux list"))
	return nil
}

// findTypeDefaults groups packages by type, then finds scripts that are
// identical across ALL packages of that type. Those become defaults.
func findTypeDefaults(pkgs []migratedPackage) map[string]map[string]string {
	// Group by type
	byType := make(map[string][]migratedPackage)
	for _, pkg := range pkgs {
		if pkg.pkgType != "" {
			byType[pkg.pkgType] = append(byType[pkg.pkgType], pkg)
		}
	}

	result := make(map[string]map[string]string)
	for typeName, typePkgs := range byType {
		if len(typePkgs) < 2 {
			continue // no point in defaults for a single package
		}
		common := findCommonScripts(typePkgs)
		if len(common) > 0 {
			result[typeName] = common
		}
	}
	return result
}

// findCommonScripts returns scripts that are identical across all packages.
func findCommonScripts(pkgs []migratedPackage) map[string]string {
	if len(pkgs) == 0 {
		return nil
	}
	// Start with all scripts from the first package
	common := make(map[string]string)
	for k, v := range pkgs[0].scripts {
		common[k] = v
	}
	// Intersect: keep only scripts present in ALL packages with the same value
	for _, pkg := range pkgs[1:] {
		for k, v := range common {
			if pkg.scripts[k] != v {
				delete(common, k)
			}
		}
	}
	return common
}

func generateRootTomlWithDefaults(members, taskNames []string, serialTasks map[string]bool, typeDefaults map[string]map[string]string) string {
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

	// Write [defaults.<type>.tasks] sections
	var typeNames []string
	for t := range typeDefaults {
		typeNames = append(typeNames, t)
	}
	sort.Strings(typeNames)

	for _, typeName := range typeNames {
		scripts := typeDefaults[typeName]
		b.WriteString(fmt.Sprintf("\n[defaults.%s.tasks]\n", typeName))

		var scriptNames []string
		for k := range scripts {
			scriptNames = append(scriptNames, k)
		}
		sort.Strings(scriptNames)

		for _, k := range scriptNames {
			b.WriteString(fmt.Sprintf("%s = %q\n", k, scripts[k]))
		}
	}

	return b.String()
}

// generateMinimalPackageToml emits a ux.toml with only type + overrides.
// If all scripts match the type defaults, just emit [package] with type.
// If some differ or are extra, emit only the differences in [tasks].
func generateMinimalPackageToml(pkg migratedPackage, typeDefaults map[string]map[string]string) string {
	var b strings.Builder

	b.WriteString("[package]\n")
	b.WriteString(fmt.Sprintf("name = %q\n", pkg.name))
	if pkg.pkgType != "" {
		b.WriteString(fmt.Sprintf("type = %q\n", pkg.pkgType))
	}

	// Figure out which scripts need to be in [tasks] (overrides + extras)
	defaults := typeDefaults[pkg.pkgType]
	var overrides []string

	var scriptNames []string
	for k := range pkg.scripts {
		scriptNames = append(scriptNames, k)
	}
	sort.Strings(scriptNames)

	for _, k := range scriptNames {
		v := pkg.scripts[k]
		if defaults != nil {
			if defaultVal, ok := defaults[k]; ok && defaultVal == v {
				continue // matches default, skip
			}
		}
		overrides = append(overrides, k)
	}

	if len(overrides) > 0 {
		b.WriteString("\n[tasks]\n")
		for _, k := range overrides {
			b.WriteString(fmt.Sprintf("%s = %q\n", k, pkg.scripts[k]))
		}
	}

	return b.String()
}

// --- helpers ---

func parseWorkspaces(raw json.RawMessage) ([]string, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var arr []string
	if err := json.Unmarshal(raw, &arr); err == nil {
		return arr, nil
	}
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

func writeFileIfNew(path, content string) (bool, error) {
	if _, err := os.Stat(path); err == nil {
		return false, nil
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return false, err
	}
	return true, nil
}
