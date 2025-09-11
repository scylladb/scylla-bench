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

### Testing
- Run all tests: `make test` -- takes 30-40 seconds. NEVER CANCEL. Set timeout to 30+ minutes.
- The test suite includes:
  - Unit tests for all packages
  - Memory leak tests (require Docker and `RUN_MEMORY_LEAK_TEST=true`)
  - TestContainers integration tests
- To run specific test types:
  - Container tests: `RUN_CONTAINER_TESTS=true go test -v ./pkg/testutil` (25-35 seconds)
  - Memory leak tests: `RUN_MEMORY_LEAK_TEST=true go test -v -run TestMemoryLeak` (25-30 seconds)
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

## Building and Running

### Build Commands
- `make build` -- Static binary with version info
- `make build-debug` -- Debug version with symbols
- `make build-docker-image` -- Build Docker image
- `make clean` -- Clean build artifacts

### Running scylla-bench
Basic usage patterns:
```bash
# Sequential write to populate database
./build/scylla-bench -workload sequential -mode write -nodes 127.0.0.1

# Read test with high concurrency
./build/scylla-bench -workload uniform -mode read -concurrency 128 -duration 15m -nodes some_node

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