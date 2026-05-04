# Data Validation Throughput Test

## Purpose

This test validates the performance impact of the `-validate-data` parameter as reported in [QATOOLS-48](https://scylladb.atlassian.net/browse/QATOOLS-48).

**Note**: The QATOOLS-48 link may require ScyllaDB internal access. The issue reported a ~90% throughput decrease (533 ops/sec → 50 ops/sec) when using `-validate-data` with sequential workload writes.

## Issue Background

When running writes with `-validate-data` parameter, the throughput decreased significantly:
- **Without validation**: ~533 ops/sec
- **With validation**: ~50 ops/sec (~90% decrease)

## Optimizations Applied

The following optimizations dramatically reduced the overhead of `-validate-data`:

1. **Replaced SHA256 with CRC32C**: SHA256 is a cryptographic hash—overkill for data integrity in a benchmarking tool. CRC32C (Castagnoli) is hardware-accelerated on modern x86_64 CPUs via SSE4.2, making it ~100x faster.

2. **Replaced `binary.Write`/`binary.Read` with direct byte manipulation**: The standard library's `binary.Write`/`binary.Read` uses reflection internally, adding significant overhead per call. Direct `binary.LittleEndian.PutUint64`/`Uint64` avoids this entirely.

3. **Single allocation instead of buffer → copy**: The old code allocated a `bytes.Buffer`, grew it, filled it, then allocated a final `[]byte` and copied. The new code allocates the final slice once and writes directly into it.

4. **Deterministic xorshift payload instead of globally-locked random**: The old code used `random.String()` which acquires a global mutex on every call—a severe concurrency bottleneck. The new code uses a deterministic xorshift PRNG seeded from pk/ck, requiring no locks and producing well-distributed non-zero data.

## Running the Tests

### Benchmark Tests (No Docker Required)

```bash
# Run all benchmarks
go test -bench=. -run=^$ -benchtime=1s -benchmem

# Run specific benchmarks
go test -bench=BenchmarkGenerateDataWithValidation -run=^$ -benchmem
go test -bench=BenchmarkValidateData -run=^$ -benchmem
```

### Integration Test (Requires Docker)

```bash
RUN_CONTAINER_TESTS=true go test -v -run TestValidateDataThroughputImpact -timeout 15m
```

## Benchmark Results

### GenerateData (51200 bytes, same as original issue)

| Metric | Before | After | Improvement |
|--------|--------|-------|-------------|
| **with validation** | ~108 μs/op | ~25 μs/op | **4.3x faster** |
| Memory per op | 172 KB | 57 KB | **3x less** |
| Allocations per op | 8 | 1 | **8x fewer** |
| without validation | ~11 μs/op | ~11 μs/op | (baseline) |
| **Validation overhead** | **~10x** | **~2.3x** | |

### ValidateData (51200 bytes)

| Metric | Before | After | Improvement |
|--------|--------|-------|-------------|
| Time per op | ~59 μs/op | ~2.2 μs/op | **27x faster** |
| Memory per op | 115 KB | 0 B | **zero-alloc** |
| Allocations per op | 8 | 0 | **zero-alloc** |

## Test Configuration

The integration test uses these parameters (scaled down from the original issue):

- Partition Count: 100 (vs 3000 in issue)
- Clustering Row Count: 100 (vs 1000 in issue)
- Clustering Row Size: 51200 bytes (same as issue)
- Concurrency: 4 (vs 400 in issue)
- Test Duration: 10 seconds
