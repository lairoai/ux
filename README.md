# ux

Simple monorepo task runner for Go and Python projects. Replaces turborepo with something that feels native to non-JS ecosystems.

## Install

```
go install github.com/lairoai/ux@latest
```

## Quick Start

Create a root `ux.toml` in your monorepo:

```toml
[workspace]
members = ["//packages/...", "//services/...", "//workers/...", "//cli"]

[tasks]
lint = { parallel = true }
test = { parallel = false }
build = { parallel = true }
install = { parallel = true }
```

Add a `ux.toml` in each package:

```toml
[package]
name = "ingest"

[tasks]
lint = ["uv run ty check", "uv run ruff check --fix", "uv run ruff format"]
test = "uv run pytest --cov"
```

```toml
[package]
name = "api"

[tasks]
lint = "golangci-lint run ./..."
test = "go test -race ./..."
build = "go build -o bin/api ./cmd/api"
```

Run tasks:

```
ux lint                        # lint everything (parallel)
ux test                        # test everything (serial)
ux build                       # build everything
ux install                     # run wherever defined, skip the rest
```

## Labels

Packages are addressed with `//` labels, following Bazel/Pants conventions:

```
//packages/ingest              # exact package
//packages/...                 # all packages under packages/ (recursive)
//services/...                 # all services
```

Use labels to filter:

```
ux lint //packages/...         # lint only packages/
ux test //services/api         # test one service
ux lint --affected             # only packages changed vs origin/main
```

## Configuration

### Root `ux.toml`

| Field | Description |
|---|---|
| `workspace.members` | List of `//label` patterns to discover packages |
| `tasks.<name>.parallel` | Run this task across packages in parallel (`true`) or serial (`false`, default) |

### Package `ux.toml`

| Field | Description |
|---|---|
| `package.name` | Package name (used in output) |
| `tasks.<name>` | A command string or list of commands to run sequentially |

When a task is a list of commands, they run in order and stop on first failure.

## Output

Results are clear and scannable:

```
ux lint  (3 packages, parallel)

  ✓  //packages/common                        0.8s
  ✗  //packages/ingest                        2.1s
  ✓  //services/api                           1.3s

────────────────────────────────────────────────
FAIL  //packages/ingest
  → uv run ruff check --fix
    src/main.py:12:1: F401 unused import 'os'

────────────────────────────────────────────────
lint: 2 passed, 1 failed
```

- Successes: checkmark + timing, no noise
- Failures: which step failed + its output
- Exit code 1 if anything failed
