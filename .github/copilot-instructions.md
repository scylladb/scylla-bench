# scylla-bench

Always reference these instructions first and fallback to search or bash commands only when you encounter unexpected information that does not match the info here.

scylla-bench is a benchmarking tool for ScyllaDB written in Go. It minimizes client overhead and provides comprehensive test scenarios for database performance evaluation.

## Working Effectively

### Bootstrap and Build
- Set up the environment:
  - Go 1.24 is required (defined in go.mod)
  - `make build` -- takes 20-25 seconds on first run with downloads. NEVER CANCEL. Set timeout to 60+ minutes for safety.
  - Subsequent builds are faster (~3-5 seconds)
- Build artifacts are placed in `./build/scylla-bench`

### Testing Requirements

**CRITICAL**: All new features and code changes MUST include comprehensive tests:

#### Unit Tests
- **Required for all new functions and methods**
- Use `t.Parallel()` in all test functions to enable parallel execution
- Run with `-race` flag to detect race conditions: `go test -race ./...`
- Mock external dependencies when testing business logic
- Achieve high code coverage for new functionality

#### Integration Tests
- **Required for all features that interact with ScyllaDB**
- Must use TestContainers with real ScyllaDB instances: `scylladb/scylla:2025.2`
- Use `t.Parallel()` for concurrent test execution
- Set `RUN_CONTAINER_TESTS=true` to enable: `RUN_CONTAINER_TESTS=true go test -race -v ./pkg/testutil`
- Test against actual ScyllaDB behavior, not mocks
- Include data validation scenarios when applicable

#### Test Execution Commands
```bash
# All tests with race detection (required before committing)
make test  # Includes -race flag

# Container integration tests specifically
RUN_CONTAINER_TESTS=true go test -race -v ./pkg/testutil

# Memory leak tests (when applicable)
RUN_MEMORY_LEAK_TEST=true go test -race -v -run TestMemoryLeak

# Manual race detection check
go test -race ./...
```

#### Test Structure Requirements
- All tests MUST use `t.Parallel()` unless they modify global state
- Integration tests MUST clean up containers and resources
- Use table-driven tests for multiple test cases
- Include error path testing and edge cases
- Document test scenarios and expected outcomes

### Testing Requirements

**CRITICAL**: All new features and code changes MUST include comprehensive tests:

#### Unit Tests
- **Required for all new functions and methods**
- Use `t.Parallel()` in all test functions to enable parallel execution
- Run with `-race` flag to detect race conditions: `go test -race ./...`
- Mock external dependencies when testing business logic
- Achieve high code coverage for new functionality

#### Integration Tests
- **Required for all features that interact with ScyllaDB**
- Must use TestContainers with real ScyllaDB instances: `scylladb/scylla:2025.2`
- Use `t.Parallel()` for concurrent test execution
- Set `RUN_CONTAINER_TESTS=true` to enable: `RUN_CONTAINER_TESTS=true go test -race -v ./pkg/testutil`
- Test against actual ScyllaDB behavior, not mocks
- Include data validation scenarios when applicable

#### Test Execution Commands
```bash
# All tests with race detection (required before committing)
make test  # Includes -race flag

# Container integration tests specifically
RUN_CONTAINER_TESTS=true go test -race -v ./pkg/testutil

# Memory leak tests (when applicable)
RUN_MEMORY_LEAK_TEST=true go test -race -v -run TestMemoryLeak

# Manual race detection check
go test -race ./...
```

#### Test Structure Requirements
- All tests MUST use `t.Parallel()` unless they modify global state
- Integration tests MUST clean up containers and resources
- Use table-driven tests for multiple test cases
- Include error path testing and edge cases
- Document test scenarios and expected outcomes

||||||| parent of 2e02a7f (Resolve merge conflicts in copilot-instructions.md)
||||||| parent of 3af6613 (Add comprehensive mode descriptions and testing requirements with race flag and t.Parallel())
=======
### Testing Requirements

**CRITICAL**: All new features and code changes MUST include comprehensive tests:

#### Unit Tests
- **Required for all new functions and methods**
- Use `t.Parallel()` in all test functions to enable parallel execution
- Run with `-race` flag to detect race conditions: `go test -race ./...`
- Mock external dependencies when testing business logic
- Achieve high code coverage for new functionality

#### Integration Tests
- **Required for all features that interact with ScyllaDB**
- Must use TestContainers with real ScyllaDB instances: `scylladb/scylla:2025.2`
- Use `t.Parallel()` for concurrent test execution
- Set `RUN_CONTAINER_TESTS=true` to enable: `RUN_CONTAINER_TESTS=true go test -race -v ./pkg/testutil`
- Test against actual ScyllaDB behavior, not mocks
- Include data validation scenarios when applicable

#### Test Execution Commands
```bash
# All tests with race detection (required before committing)
make test  # Includes -race flag

# Container integration tests specifically
RUN_CONTAINER_TESTS=true go test -race -v ./pkg/testutil

# Memory leak tests (when applicable)
RUN_MEMORY_LEAK_TEST=true go test -race -v -run TestMemoryLeak

# Manual race detection check
go test -race ./...
```

#### Test Structure Requirements
- All tests MUST use `t.Parallel()` unless they modify global state
- Integration tests MUST clean up containers and resources
- Use table-driven tests for multiple test cases
- Include error path testing and edge cases
- Document test scenarios and expected outcomes

||||||| 27f23be
=======
### Testing Requirements

**CRITICAL**: All new features and code changes MUST include comprehensive tests:

#### Unit Tests
- **Required for all new functions and methods**
- Use `t.Parallel()` in all test functions to enable parallel execution
- Run with `-race` flag to detect race conditions: `go test -race ./...`
- Mock external dependencies when testing business logic
- Achieve high code coverage for new functionality

#### Integration Tests
- **Required for all features that interact with ScyllaDB**
- Must use TestContainers with real ScyllaDB instances: `scylladb/scylla:2025.2`
- Use `t.Parallel()` for concurrent test execution
- Set `RUN_CONTAINER_TESTS=true` to enable: `RUN_CONTAINER_TESTS=true go test -race -v ./pkg/testutil`
- Test against actual ScyllaDB behavior, not mocks
- Include data validation scenarios when applicable

#### Test Execution Commands
```bash
# All tests with race detection (required before committing)
make test  # Includes -race flag

# Container integration tests specifically
RUN_CONTAINER_TESTS=true go test -race -v ./pkg/testutil

# Memory leak tests (when applicable)
RUN_MEMORY_LEAK_TEST=true go test -race -v -run TestMemoryLeak

# Manual race detection check
go test -race ./...
```

#### Test Structure Requirements
- All tests MUST use `t.Parallel()` unless they modify global state
- Integration tests MUST clean up containers and resources
- Use table-driven tests for multiple test cases
- Include error path testing and edge cases
- Document test scenarios and expected outcomes

### Testing
- Run all tests: `make test` -- takes 30-40 seconds. NEVER CANCEL. Set timeout to 30+ minutes.
- **All tests run with `-race` flag enabled to detect race conditions**
- The test suite includes:
  - Unit tests for all packages (with `t.Parallel()` support)
  - Memory leak tests (require Docker and `RUN_MEMORY_LEAK_TEST=true`)
  - TestContainers integration tests (with real ScyllaDB instances)
- To run specific test types:
  - Container tests: `RUN_CONTAINER_TESTS=true go test -race -v ./pkg/testutil` (25-35 seconds)
  - Memory leak tests: `RUN_MEMORY_LEAK_TEST=true go test -race -v -run TestMemoryLeak` (25-30 seconds)
  - NEVER CANCEL TestContainers tests - they manage Docker containers and generate memory profiles

### Validation Scenarios
After making changes, ALWAYS validate these scenarios:
1. **Build validation**: `make build` succeeds and `./build/scylla-bench --help` shows usage
2. **Version check**: `./build/scylla-bench -version` and `./build/scylla-bench -version-json` work
3. **Basic functionality**: Test help command shows all expected flags and modes
4. **Test suite**: `make test` passes completely
5. **Container integration** (if modifying test utilities): `RUN_CONTAINER_TESTS=true go test -v ./pkg/testutil`

### Linting and Formatting
- Format code: `make fmt` -- runs gofumpt, takes <1 second
- ALWAYS run `make fmt` before committing
- **KNOWN ISSUE**: `make check` (golangci-lint) currently fails due to dependency conflicts
  - Do NOT try to fix the linting setup unless specifically asked
  - Use `make fmt` for code formatting instead
- CI will validate linting via GitHub Actions

## Project Structure

### Key Directories
```
.
├── build/              # Build outputs (scylla-bench binary)
├── .github/workflows/  # CI/CD pipelines
├── internal/version/   # Version information handling
├── pkg/
│   ├── results/       # Test result data structures
│   ├── testutil/      # TestContainers utilities for ScyllaDB
│   └── workloads/     # Workload generators (sequential, uniform, timeseries)
├── random/            # Random distribution utilities
└── scripts/           # Helper scripts (extract-driver-version.sh)
```

### Important Files
- `main.go` -- Main application entry point with CLI argument parsing
- `modes.go` -- Core benchmarking modes (write, read, counter_update, etc.)
- `Makefile` -- Build, test, and maintenance commands
- `go.mod` -- **CRITICAL**: Uses ScyllaDB's fork of gocql driver via replace directive

### Dependencies and Special Requirements

### Go Module Replace Directive
The project uses ScyllaDB's fork of the gocql driver:
```
replace github.com/gocql/gocql => github.com/scylladb/gocql v1.15.0
```
**NEVER** install via `go get` or `go install` directly - this bypasses the replace directive and breaks shard-awareness.
**CRITICAL**: Direct installation will fail with error about replace directives in non-main modules.

### TestContainers
- Requires Docker to be running
- Uses ScyllaDB container image: `scylladb/scylla:2025.2`
- Container startup takes 25-35 seconds
- Set `RUN_CONTAINER_TESTS=true` to enable container tests
- Memory leak tests generate `.pprof` files for analysis

## Benchmarking Modes

scylla-bench supports multiple benchmarking modes, each designed for specific testing scenarios:

### Available Modes
- **`write`** -- Insert new data into the database using INSERT statements. Creates partitions and clustering rows according to the workload pattern. Essential for populating databases and testing write performance.
- **`read`** -- Read existing data from the main table using SELECT statements. Requires data to be written first. Tests read performance and caching behavior.
- **`mixed`** -- Performs alternating 50% reads and 50% writes using a global atomic counter to ensure true distribution across all threads. Combines write and read operations in a single benchmark run. Compatible with all workloads (sequential, uniform, timeseries).
- **`counter_update`** -- Update counter columns using UPDATE statements with counter increments. Tests counter performance and consistency.
- **`counter_read`** -- Read counter values from the counter table. Used to verify counter updates and test counter read performance.
- **`scan`** -- Perform full table scans using token range queries. Tests large-scale data retrieval and scanning performance without specific partition targeting.

### Mode Usage Patterns
```bash
# Populate database first with writes
./build/scylla-bench -workload sequential -mode write -nodes 127.0.0.1

# Then test reads on populated data
./build/scylla-bench -workload uniform -mode read -concurrency 128 -duration 15m

# Mixed read/write workload (50% reads, 50% writes)
./build/scylla-bench -workload uniform -mode mixed -concurrency 128 -duration 30m -nodes 127.0.0.1

## Benchmarking Modes

scylla-bench supports multiple benchmarking modes, each designed for specific testing scenarios:

### Available Modes
- **`write`** -- Insert new data into the database using INSERT statements. Creates partitions and clustering rows according to the workload pattern. Essential for populating databases and testing write performance.
- **`read`** -- Read existing data from the main table using SELECT statements. Requires data to be written first. Tests read performance and caching behavior.
- **`counter_update`** -- Update counter columns using UPDATE statements with counter increments. Tests counter performance and consistency.
- **`counter_read`** -- Read counter values from the counter table. Used to verify counter updates and test counter read performance.
- **`scan`** -- Perform full table scans using token range queries. Tests large-scale data retrieval and scanning performance without specific partition targeting.

### Mode Usage Patterns
```bash
# Populate database first with writes
./build/scylla-bench -workload sequential -mode write -nodes 127.0.0.1

# Then test reads on populated data
./build/scylla-bench -workload uniform -mode read -concurrency 128 -duration 15m
# Full table scan
./build/scylla-bench -mode scan -timeout 5m -concurrency 1
```

See the "Running scylla-bench" section below for more comprehensive examples.

## Benchmarking Modes

scylla-bench supports multiple benchmarking modes, each designed for specific testing scenarios:

### Available Modes
- **`write`** -- Insert new data into the database using INSERT statements. Creates partitions and clustering rows according to the workload pattern. Essential for populating databases and testing write performance.
- **`read`** -- Read existing data from the main table using SELECT statements. Requires data to be written first. Tests read performance and caching behavior.
- **`counter_update`** -- Update counter columns using UPDATE statements with counter increments. Tests counter performance and consistency.
- **`counter_read`** -- Read counter values from the counter table. Used to verify counter updates and test counter read performance.
- **`scan`** -- Perform full table scans using token range queries. Tests large-scale data retrieval and scanning performance without specific partition targeting.

### Mode Usage Patterns
```bash
# Populate database first with writes
./build/scylla-bench -workload sequential -mode write -nodes 127.0.0.1

# Then test reads on populated data
./build/scylla-bench -workload uniform -mode read -concurrency 128 -duration 15m
=======
# Mixed mode with timeseries workload
./build/scylla-bench -workload timeseries -mode mixed -duration 15m -concurrency 64

# Test counter operations
./build/scylla-bench -workload uniform -mode counter_update -duration 30m
./build/scylla-bench -workload uniform -mode counter_read -duration 5m

# Full table scan
./build/scylla-bench -mode scan -timeout 5m -concurrency 1
```

See the "Running scylla-bench" section below for more comprehensive examples.

## Building and Running

### Build Commands
- `make build` -- Static binary with version info
- `make build-debug` -- Debug version with symbols
- `make build-docker-image` -- Build Docker image
- `make build-sct-docker-image` -- Build SCT-specific Docker image
- `make clean` -- Clean build artifacts

### Docker Development
For local development with Docker:
```bash
# Build local Docker image
make build-docker-image

# Build with custom tag
DOCKER_IMAGE_TAG=my-scylla-bench make build-docker-image

# Build debug image with debugging tools
docker build --target debug -t scylla-bench:debug .

# Run from Docker (example mixed mode)
docker run --rm --network=host scylla-bench:latest \
  -workload uniform -mode mixed -nodes 127.0.0.1 -duration 30s

# Debug with delve debugger
docker run --rm -p 2345:2345 --network=host scylla-bench:debug \
  -workload uniform -mode mixed -nodes 127.0.0.1
```

### Running scylla-bench
Basic usage patterns:
```bash
# Sequential write to populate database
./build/scylla-bench -workload sequential -mode write -nodes 127.0.0.1

# Read test with high concurrency
./build/scylla-bench -workload uniform -mode read -concurrency 128 -duration 15m -nodes some_node

# Mixed read/write test (50% reads, 50% writes)
./build/scylla-bench -workload uniform -mode mixed -concurrency 128 -duration 30m -nodes 127.0.0.1

# Mixed mode with different workloads
./build/scylla-bench -workload sequential -mode mixed -duration 10m -concurrency 64
./build/scylla-bench -workload timeseries -mode mixed -duration 15m -concurrency 32

# Counter update test
./build/scylla-bench -workload uniform -mode counter_update -duration 30m -concurrency 128

# Full table scan
./build/scylla-bench -mode scan -timeout 5m -concurrency 1

# Data validation test
./build/scylla-bench -workload sequential -mode write -nodes 127.0.0.1 -clustering-row-size 16 -validate-data
```

## Development Workflow

### Before Making Changes
1. Ensure Docker is running (for container tests)
2. Run `make build` to verify current state
3. Run `make test` to establish baseline

### During Development
1. Make targeted changes
2. Run `make build` after each change
3. Test specific functionality with the binary
4. Run `make fmt` to format code

### Before Committing
1. Run `make fmt` -- formats all Go code
2. Run `make test` -- ensures all tests pass
3. Validate the main scenarios listed above
4. **Do NOT run** `make check` due to known linting issues
5. **Use single commit per PR** -- Squash multiple commits into one meaningful commit with a descriptive message

## Common Commands Reference

### Build and Test Timing
- `make build`: 20-25 seconds (first time), 3-5 seconds (subsequent)
- `make test`: 30-40 seconds
- `make fmt`: <1 second
- Container tests: 25-35 seconds each

### Makefile Targets
```bash
make build           # Build release binary
make build-debug     # Build debug binary
make test           # Run test suite
make fmt            # Format code with gofumpt
make clean          # Clean build artifacts
make fieldalign     # Fix struct field alignment
make build-docker-image  # Build Docker container image
```

### Git and Project Info
- Default branch: `master`
- Binary name: `scylla-bench`
- Key feature: Shard-aware ScyllaDB benchmarking
- Important: Uses custom gocql driver from ScyllaDB

## Troubleshooting

### Known Issues
1. **golangci-lint dependency conflicts**: `make check` fails - use GitHub Actions for linting validation
2. **Container tests require Docker**: Ensure Docker daemon is running before TestContainers tests
3. **Shard-awareness requires ScyllaDB gocql fork**: Never bypass the go.mod replace directive

### Build Failures
- Check Go version (requires 1.24)
- Ensure network access for dependency downloads
- Clean and rebuild: `make clean && make build`

### Test Failures
- For container tests: Check Docker daemon status
- For unit tests: Check for conflicting processes or permissions
- Memory leak tests require sufficient available memory

### Performance Issues
- Binary size is ~8.6MB (normal for static binary)
- First build includes dependency downloads (20-25 seconds)
- Subsequent builds are much faster (3-5 seconds)
- TestContainers startup includes ScyllaDB initialization time
- Memory leak tests generate pprof files for profiling analysis
