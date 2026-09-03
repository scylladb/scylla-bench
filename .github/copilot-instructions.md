# scylla-bench

Always reference these instructions first and fall back to search or bash commands only when you encounter information that does not match what's documented here.

scylla-bench is a benchmarking tool for ScyllaDB written in Go. It minimizes client overhead and provides comprehensive test scenarios for database performance evaluation.

## Working Effectively

### Bootstrap and Build
- Go 1.25 is required (defined in `go.mod`).
- `make build` -- takes 20-25 seconds on first run with downloads. NEVER CANCEL. Set timeout to 60+ minutes for safety.
- Subsequent builds are ~3-5 seconds.
- Build artifacts are placed in `./build/scylla-bench`.

### Testing Requirements

**CRITICAL**: All new features and code changes MUST include comprehensive tests.

#### Unit Tests
- Required for all new functions and methods.
- Use `t.Parallel()` in all test functions to enable parallel execution.
- Run with `-race` to detect race conditions: `go test -race ./...`.
- Mock external dependencies when testing business logic.
- Achieve high code coverage for new functionality.

#### Integration Tests
- Required for all features that interact with ScyllaDB.
- Must use TestContainers with real ScyllaDB instances: `scylladb/scylla:2025.2`.
- Use `t.Parallel()` for concurrent test execution.
- Set `RUN_CONTAINER_TESTS=true` to enable.
- Test against actual ScyllaDB behavior, not mocks.
- Include data validation scenarios when applicable.

#### Test Execution Commands
```bash
# All tests with race detection (required before committing)
make test  # includes -race, coverage, and JSON output piped through gotestfmt

# Container integration tests (Docker required)
RUN_CONTAINER_TESTS=true go test -race -v -timeout 25m -run '^TestIntegration' ./...
RUN_CONTAINER_TESTS=true go test -race -v -run TestIntegrationQuickSmoke ./...

# Memory leak tests
RUN_MEMORY_LEAK_TEST=true go test -race -v -run TestMemoryLeak ./...

# Manual race detection check
go test -race ./...
```

#### Test Structure Requirements
- All tests MUST use `t.Parallel()` unless they modify package-level globals (`main`, `modes.go`).
- Integration tests MUST clean up containers and resources.
- Use table-driven tests for multiple cases.
- Include error-path testing and edge cases.

### Testing
- `make test` takes 30-40 seconds without container tests. NEVER CANCEL — set timeout to 30+ minutes.
- All tests run with `-race` enabled.
- The suite includes:
  - Unit tests for every package (with `t.Parallel()` support).
  - Memory leak tests (Docker + `RUN_MEMORY_LEAK_TEST=true`, 25-30 seconds).
  - TestContainers integration tests (Docker + `RUN_CONTAINER_TESTS=true`, 25-35 seconds per container startup).
- NEVER CANCEL TestContainers tests — they manage Docker containers and generate memory profiles.

### Validation Scenarios
After making changes, ALWAYS validate:
1. **Build**: `make build` succeeds and `./build/scylla-bench --help` shows usage.
2. **Version**: `./build/scylla-bench -version` and `./build/scylla-bench -version-json` work.
3. **Help text**: shows all expected flags and modes.
4. **Tests**: `make test` passes completely.
5. **Container integration** (if modifying test utilities): `RUN_CONTAINER_TESTS=true go test -v ./pkg/testutil`.

### Linting and Formatting
- Format code: `make fmt` -- runs `golangci-lint run --fix` (auto-installs golangci-lint v2.6.0 into `./bin`); takes <1 second once installed.
- ALWAYS run `make fmt` before committing.
- Lint check: `make check` runs `golangci-lint run` without `--fix`. CI also validates linting via GitHub Actions.
- Configured formatters: `gofumpt`, `goimports`, `gci`, `golines` (180-col limit).
- Notable enabled linters: `errcheck`, `errorlint`, `gocritic`, `gocyclo` (limit 50), `govet` with `enable-all` and `shadow=strict`, `lll` (180), `revive`, `staticcheck`.

## Project Structure

### Key Directories
```
.
├── build/              # Build outputs (scylla-bench binary)
├── .github/workflows/  # CI/CD pipelines
├── internal/
│   ├── clock/          # Clock interface (UTC + Manual) — DO NOT call time.Now/Sleep directly
│   └── version/        # Version information handling (-ldflags injection)
├── pkg/
│   ├── config/         # Global benchmark configuration (latency type, hdr file, histograms)
│   ├── ratelimiter/    # Distributed rate limiter
│   ├── results/        # PartialResult/TotalResult with HDR histograms
│   ├── stack/          # Bounded ring buffer in front of histograms
│   ├── testrun/        # Concurrency orchestrator + reporting loop
│   ├── testutil/       # TestContainers helpers for ScyllaDB
│   ├── tools/          # Small helpers
│   ├── worker/         # Per-goroutine counter + latency-recording API
│   └── workloads/      # Generators: sequential, uniform, timeseries, scan
├── random/             # Random distributions (Fixed, Uniform)
└── scripts/            # Helper scripts (extract-driver-version.sh)
```

### Important Files
- `main.go` — CLI argument parsing, gocql cluster setup, dispatch into `pkg/testrun`.
- `modes.go` — Core benchmarking modes (`DoWrites`, `DoBatchedWrites`, `DoReads`, `DoCounterUpdates`, `DoCounterReads`, `DoScanTable`, `DoMixed`).
- `Makefile` — Build, test, lint, and maintenance commands.
- `go.mod` — **CRITICAL**: uses ScyllaDB's fork of gocql via `replace` directive.

### Go Module Replace Directive
The project uses ScyllaDB's fork of the gocql driver:
```
replace github.com/gocql/gocql => github.com/scylladb/gocql v1.18.0
```
**NEVER** install via `go get` or `go install` from outside this module — Go does not honor `replace` directives in non-main modules, and the resulting binary loses shard-awareness.

To swap in a custom gocql commit:
```bash
GOCQL_VERSION=<sha-or-tag> make build-with-custom-gocql-version
```

### TestContainers
- Requires Docker to be running.
- Uses ScyllaDB container image: `scylladb/scylla:2025.2`.
- Container startup takes 25-35 seconds.
- Set `RUN_CONTAINER_TESTS=true` to enable container tests.
- Memory leak tests generate `.pprof` files for analysis.

## Benchmarking Modes

scylla-bench supports multiple benchmarking modes, each designed for specific testing scenarios.

### Available Modes
- **`write`** — Insert new data via INSERT (or batched UNLOGGED INSERTs when `-rows-per-request > 1`).
- **`read`** — Read existing data via SELECT. Sub-modes selected by `-no-lower-bound`, `-provide-upper-bound`, `-in-restriction` (mutually exclusive).
- **`mixed`** — Alternates 50% reads and 50% writes via a global atomic counter (odd = read, even = write). Compatible with sequential, uniform, and timeseries workloads. Allocates extra read/write HDR histograms.
- **`counter_update`** — UPDATE counter columns on a separate `test_counters` table.
- **`counter_read`** — Read counters from `test_counters`.
- **`scan`** — Full table scan via token-range queries. Has its own implicit `scan` workload; `-range-count` controls subdivision.

### Mode Usage Patterns
```bash
# Populate the database first with writes
./build/scylla-bench -workload sequential -mode write -nodes 127.0.0.1

# Read on populated data
./build/scylla-bench -workload uniform -mode read -concurrency 128 -duration 15m

# Mixed read/write workload (50/50)
./build/scylla-bench -workload uniform -mode mixed -concurrency 128 -duration 30m -nodes 127.0.0.1

# Mixed with timeseries
./build/scylla-bench -workload timeseries -mode mixed -duration 15m -concurrency 64

# Counter operations
./build/scylla-bench -workload uniform -mode counter_update -duration 30m
./build/scylla-bench -workload uniform -mode counter_read -duration 5m

# Full table scan
./build/scylla-bench -mode scan -timeout 5m -concurrency 1
```

See `README.md` for the full flag reference and per-workload semantics.

## Building and Running

### Build Commands
- `make build` — Static binary with version info.
- `make build-debug` — Debug build with symbols (`-N -l`).
- `make build-docker-image` — Build production Docker image.
- `make build-sct-docker-image` — Build SCT-specific Docker image.
- `make clean` — Clean build artifacts (`build/`, `coverage.txt`, `dist/`).
- `make fieldalign` — Run `fieldalignment -fix` over the listed packages; rerun after touching struct layouts.

### Docker Development
```bash
# Build local Docker image
make build-docker-image

# Custom tag
DOCKER_IMAGE_TAG=my-scylla-bench make build-docker-image

# Debug image with delve and dev tools
docker build --target debug -t scylla-bench:debug .

# Run from Docker (mixed mode)
docker run --rm --network=host scylla-bench:latest \
  -workload uniform -mode mixed -nodes 127.0.0.1 -duration 30s

# Debug with delve
docker run --rm -p 2345:2345 --network=host scylla-bench:debug \
  -workload uniform -mode mixed -nodes 127.0.0.1
```

### Running scylla-bench
```bash
# Sequential write to populate the database
./build/scylla-bench -workload sequential -mode write -nodes 127.0.0.1

# Read test with high concurrency
./build/scylla-bench -workload uniform -mode read -concurrency 128 -duration 15m -nodes some_node

# Mixed read/write
./build/scylla-bench -workload uniform -mode mixed -concurrency 128 -duration 30m -nodes 127.0.0.1

# Counter update
./build/scylla-bench -workload uniform -mode counter_update -duration 30m -concurrency 128

# Full table scan
./build/scylla-bench -mode scan -timeout 5m -concurrency 1

# Data validation
./build/scylla-bench -workload sequential -mode write -nodes 127.0.0.1 -clustering-row-size 16 -validate-data
```

## Development Workflow

### Before Making Changes
1. Ensure Docker is running (for container tests).
2. `make build` to verify current state.
3. `make test` to establish a baseline.

### During Development
1. Make targeted changes.
2. `make build` after each change.
3. Test specific functionality with the binary.
4. `make fmt` to format code.

### Before Committing
1. `make fmt` — formats all Go code.
2. `make test` — ensure all tests pass.
3. Validate the scenarios above.
4. `make check` — local lint check (CI runs the same).
5. **Single commit per PR** — squash multiple commits into one descriptive commit.

## Common Commands Reference

### Build and Test Timing
- `make build`: 20-25s first time, 3-5s subsequent.
- `make test`: 30-40s (without container/leak gates).
- `make fmt`: <1s once golangci-lint is installed.
- Container tests: 25-35s startup per ScyllaDB container.

### Makefile Targets
```bash
make build               # Build release binary
make build-debug         # Build debug binary
make test                # Run test suite (race + coverage)
make fmt                 # Format with golangci-lint --fix
make check               # Lint check (no autofix)
make clean               # Clean build artifacts
make fieldalign          # Fix struct field alignment
make build-docker-image  # Build Docker container image
```

### Git and Project Info
- Default branch: `master`.
- Binary name: `scylla-bench`.
- Key feature: shard-aware ScyllaDB benchmarking.
- Uses ScyllaDB's gocql fork (see Replace Directive above).

## Troubleshooting

### Known Issues
1. **Container tests require Docker**: ensure the daemon is running before TestContainers tests.
2. **Shard-awareness requires ScyllaDB gocql fork**: never bypass the `go.mod` replace directive.

### Build Failures
- Check Go version (requires 1.25).
- Ensure network access for dependency downloads.
- Clean and rebuild: `make clean && make build`.

### Test Failures
- Container tests: check Docker daemon status.
- Unit tests: check for conflicting processes or permissions.
- Memory leak tests: ensure sufficient available memory.

### Performance Notes
- Binary size is ~8.6MB (normal for static binary).
- First build includes dependency downloads (20-25s).
- TestContainers startup includes ScyllaDB initialization time.
- Memory leak tests generate `.pprof` files for profiling analysis.
