# Data Validation Throughput Test

## Purpose

This test validates the performance impact of the `-validate-data` parameter as reported in [QATOOLS-48](https://scylladb.atlassian.net/browse/QATOOLS-48).

**Note**: The QATOOLS-48 link may require ScyllaDB internal access. The issue reported a ~90% throughput decrease (533 ops/sec → 50 ops/sec) when using `-validate-data` with sequential workload writes.

## Issue Background

When running writes with `-validate-data` parameter, the throughput decreases significantly:
- **Without validation**: ~533 ops/sec
- **With validation**: ~50 ops/sec (~90% decrease)

## Running the Tests

### Benchmark Tests (No Docker Required)

These tests measure the overhead of data generation and validation functions:

```bash
# Run all benchmarks
go test -bench=. -run=^$ -benchtime=1s

# Run specific benchmarks
go test -bench=BenchmarkGenerateDataWithValidation -run=^$
go test -bench=BenchmarkValidateData -run=^$
```

### Integration Test (Requires Docker)

This test measures actual throughput impact with a real ScyllaDB instance:

```bash
# Run the integration test
RUN_CONTAINER_TESTS=true go test -v -run TestValidateDataThroughputImpact

# With timeout for slower systems
RUN_CONTAINER_TESTS=true go test -v -timeout 15m -run TestValidateDataThroughputImpact
```

## Test Results

### Benchmark Results

- **GenerateData without validation**: ~9-11μs per operation, 57KB, 1 allocation
- **GenerateData with validation**: ~100-110μs per operation, 172KB, 8 allocations (**~10-12x slower, 3x more memory**)
- **ValidateData on reads**: ~60μs overhead per operation

### Performance Impact Analysis

The throughput decrease is caused by:

1. **Write Operations**:
   - SHA256 checksum calculation (cryptographically expensive)
   - Random payload generation  
   - Additional memory allocations (3x more memory, 8 allocations vs 1)
   - More CPU time (10-12x slower)

2. **Read Operations**:
   - SHA256 checksum verification
   - Binary data parsing and validation

## Conclusion

The performance impact is **expected and by design**. The `-validate-data` flag provides data integrity guarantees at the cost of throughput.

### Recommendations

- **Enable `-validate-data`** only when data integrity verification is required
- **Disable `-validate-data`** for maximum performance benchmarking
- Use `-validate-data` for correctness testing, not performance testing

## Example Usage

```bash
# High-performance write benchmark (no validation)
scylla-bench -workload=sequential -mode=write -concurrency=400 -nodes=127.0.0.1

# Data integrity verification test (with validation)
scylla-bench -workload=sequential -mode=write -validate-data -concurrency=100 -nodes=127.0.0.1
```

## Test Configuration

The integration test uses the following parameters (scaled down from the original issue):

- Partition Count: 100 (vs 3000 in issue)
- Clustering Row Count: 100 (vs 1000 in issue)
- Clustering Row Size: 51200 bytes (same as issue)
- Concurrency: 4 (vs 400 in issue)
- Test Duration: 10 seconds

These smaller values enable faster test execution while still demonstrating the performance impact.
