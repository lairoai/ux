# ux

A simple monorepo task runner for Go, Python, and Rust projects. Replaces turborepo with something that doesn't assume you're writing JavaScript.

## Install

Requires Go 1.24+.

```sh
# Build from source
make build

# Install to /usr/local/bin (or set PREFIX)
make install

# Or install directly with go
go install github.com/lairoai/ux/cmd/ux@latest
```

## Quick start

### 1. Create a root `ux.toml`

```toml
[workspace]
members = [
  "//packages/...",
  "//services/..."
]

[tasks]
lint = { parallel = true }
test = { parallel = false }
build = { parallel = true }

[defaults.python.tasks]
lint = ["uv run ty check", "uv run ruff check"]
test = "uv run pytest"
build = "uv run build"

[defaults.go.tasks]
lint = "golangci-lint run ./..."
test = "go test ./..."
build = "go build ./..."
```

### 2. Optionally add per-package `ux.toml` for overrides

```toml
[package]
name = "api"
type = "python"

[tasks]
# Override a default for this specific package
test = "uv run pytest -x --timeout=30"
```

Most packages don't need their own `ux.toml` at all. If a directory contains a recognized marker file (`pyproject.toml`, `go.mod`, `Cargo.toml`), the type is auto-detected and default tasks apply automatically.

### 3. Run tasks

```sh
ux lint          # Lint all packages
ux test          # Test all packages
ux build         # Build all packages
ux list          # Show discovered packages and their tasks
```

## Usage

```
ux <task> [//label] [flags]
```

### Commands

| Command | Description |
|---------|-------------|
| `ux <task>` | Run a task across all packages that define it |
| `ux list` | List all discovered packages, their types, and tasks |
| `ux migrate` | Generate `ux.toml` files from an existing turborepo setup |

### Labels

Labels use `//` prefix syntax (Bazel/Pants conventions) to target specific packages or directories:

```sh
ux test //services/api          # Run test on one package
ux lint //packages/...          # Run lint on all packages under packages/
ux test //...                   # Run test on everything (same as ux test)
```

### Flags

| Flag | Description |
|------|-------------|
| `--affected` | Only run on packages with changes vs `origin/main` |
| `-v`, `--verbose` | Print failure output inline in the summary |
| `-h`, `--help` | Show help |

### Examples

```sh
ux lint                         # Lint everything in parallel
ux test                         # Test everything serially
ux test //services/api          # Test one package
ux lint //packages/...          # Lint all packages under packages/
ux lint --affected              # Lint only packages changed vs origin/main
ux test -v                      # Test everything, show failure output inline
```

## Configuration

### Root `ux.toml`

The root config defines the workspace, task behavior, and type defaults.

```toml
[workspace]
members = [
  "//packages/...",      # All directories under packages/
  "//services/...",      # All directories under services/
  "//tools/codegen"      # A specific directory
]

[tasks]
lint = { parallel = true }     # Run lint across packages in parallel
test = { parallel = false }    # Run test serially (streaming output)
build = { parallel = true }

# Default tasks for all Python packages
[defaults.python.tasks]
lint = ["uv run ty check", "uv run ruff check"]
test = "uv run pytest"

# Default tasks for all Go packages
[defaults.go.tasks]
lint = "golangci-lint run ./..."
test = "go test ./..."
```

**`[workspace]`** — `members` lists directories to scan. Use `//dir/...` for recursive matching or `//dir/name` for an exact path.

**`[tasks]`** — Controls execution mode. `parallel = true` runs packages concurrently (output buffered). `parallel = false` runs them one at a time (output streamed live).

**`[defaults.<type>.tasks]`** — Default commands for a package type. A task value can be a string (single command) or an array of strings (multi-step, run in order, stop on first failure).

### Package `ux.toml` (optional)

Per-package configs override or extend the defaults.

```toml
[package]
name = "api"
type = "python"    # Can be omitted if auto-detected

[tasks]
# Only list tasks that differ from the type defaults
test = "uv run pytest -x --timeout=30"
```

If a package has no `ux.toml`, its type is auto-detected from marker files and all tasks come from the type defaults.

### Type auto-detection

| Marker file | Detected type |
|-------------|---------------|
| `pyproject.toml` | `python` |
| `go.mod` | `go` |
| `Cargo.toml` | `rust` |

Checked in priority order. The first match wins.

### Task resolution

Tasks resolve in this order (highest priority first):

1. Per-package `[tasks]` in the package's `ux.toml`
2. Type defaults from root `[defaults.<type>.tasks]`

`ux list` shows each task's source with a `(default)` annotation.

## Output

### Parallel tasks

Parallel tasks buffer output per-package and print results as they complete:

```
ux lint  (6 packages, parallel)

  ✓  //packages/auth                           120ms
  ✓  //packages/datamodels                     85ms
  ✗  //packages/ingest                         340ms
  ✓  //services/api                            210ms
```

### Serial tasks

Serial tasks stream output live with indentation:

```
ux test  (3 packages, serial)

  ●  //packages/auth
    → uv run pytest
    ....
  ✓  //packages/auth                           1.2s
```

### Summary

Every run ends with a sorted summary table:

```
────────────────────────────────────────────────

  ✓  //packages/auth                           1.2s
  ✗  //packages/ingest                         3.4s
  ✓  //services/api                            2.1s

  FAIL //packages/ingest
    → uv run pytest
    log: /tmp/ux/test/packages-ingest.log

────────────────────────────────────────────────
test: 2 passed, 1 failed
```

- Failure logs are written to `/tmp/ux/<task>/<label>.log` with full output
- Use `-v` to print failure output inline in the summary
- Exit code is 1 if any package failed, 0 otherwise

## Migrating from turborepo

If you have an existing turborepo workspace:

```sh
cd /path/to/your/monorepo
ux migrate
```

This reads your `package.json` (workspaces) and `turbo.json` (task definitions), then generates:

- A root `ux.toml` with workspace members, task config, and type defaults
- Per-package `ux.toml` files with only the overrides needed

The migration detects which tasks should be serial (from `--concurrency=1` in turbo scripts), finds common scripts across packages of the same type to create `[defaults.<type>.tasks]`, and emits minimal per-package configs with only the differences.

Existing `ux.toml` files are never overwritten. Run `ux list` after migration to verify.

## Project layout

```
ux/
├── cmd/ux/main.go              # CLI entry point
├── internal/ux/
│   ├── config.go               # Config types, workspace discovery, filtering
│   ├── runner.go               # Task execution (parallel + serial)
│   ├── output.go               # Terminal output, summary, failure logs
│   └── migrate.go              # Turborepo migration
├── go.mod
├── go.sum
└── Makefile
```
