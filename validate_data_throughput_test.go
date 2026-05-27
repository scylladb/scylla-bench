package main

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/scylladb/scylla-bench/pkg/testutil"
	"github.com/scylladb/scylla-bench/pkg/workloads"
)

// TestValidateDataThroughputImpact tests if the -validate-data parameter
// significantly impacts throughput as reported in the issue.
//
// Issue Context:
// When running writes (sequential workload) with -validate-data parameter,
// the throughput was very low (~50 ops/sec vs ~533 without, a 90% decrease).
//
// After optimization (CRC32C + direct byte ops + single alloc + lock-free fill):
// - GenerateData with validation: ~2.3x slower (was ~10x)
// - ValidateData during reads: ~2μs per operation (was ~60μs)
//
// This test requires Docker and is skipped by default.
// To run: RUN_CONTAINER_TESTS=true go test -v -run TestValidateDataThroughputImpact
func TestValidateDataThroughputImpact(t *testing.T) {
	// Skip if not explicitly enabled
	if os.Getenv("RUN_CONTAINER_TESTS") != "true" {
		t.Skip("Skipping container test. Set RUN_CONTAINER_TESTS=true to run")
	}

	// Create a context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	// Create a ScyllaDB container
	container, err := testutil.NewScyllaDBContainer(ctx)
	if err != nil {
		t.Fatalf("Failed to create ScyllaDB container: %v", err)
	}
	defer func() {
		if err = container.Close(ctx); err != nil {
			t.Logf("Failed to close container: %v", err)
		}
	}()

	// Create keyspace and table
	keyspace := "test_validate"
	table := "test_table"
	err = container.CreateKeyspace(keyspace, 1)
	if err != nil {
		t.Fatalf("Failed to create keyspace: %v", err)
	}

	err = container.CreateTable(keyspace, table)
	if err != nil {
		t.Fatalf("Failed to create table: %v", err)
	}

	// Test parameters similar to the issue report
	const (
		partitionCount     = 100   // Smaller than issue (3000) for faster test
		clusteringRowCount = 100   // Smaller than issue (1000) for faster test
		clusteringRowSize  = 51200 // Same as issue
		testConcurrency    = 4     // Smaller than issue (400) for faster test
		testDuration       = 10 * time.Second
	)

	// Run test without validation
	throughputWithoutValidation := measureWriteThroughput(
		t,
		container,
		keyspace,
		table,
		partitionCount,
		clusteringRowCount,
		clusteringRowSize,
		testConcurrency,
		testDuration,
		false, // validateData = false
	)

	// Truncate table between tests
	err = container.TruncateTable(keyspace, table)
	if err != nil {
		t.Fatalf("Failed to truncate table: %v", err)
	}

	// Allow some time for truncation to complete
	time.Sleep(2 * time.Second)

	// Run test with validation
	throughputWithValidation := measureWriteThroughput(
		t,
		container,
		keyspace,
		table,
		partitionCount,
		clusteringRowCount,
		clusteringRowSize,
		testConcurrency,
		testDuration,
		true, // validateData = true
	)

	// Calculate the performance impact
	performanceRetention := throughputWithValidation / throughputWithoutValidation
	throughputDecrease := (1.0 - performanceRetention) * 100.0

	// Log results
	t.Logf("\n=== Throughput Test Results ===")
	t.Logf("Test Configuration:")
	t.Logf("  Partition Count:       %d", partitionCount)
	t.Logf("  Clustering Row Count:  %d", clusteringRowCount)
	t.Logf("  Clustering Row Size:   %d bytes", clusteringRowSize)
	t.Logf("  Concurrency:           %d", testConcurrency)
	t.Logf("  Test Duration:         %v", testDuration)
	t.Logf("\nResults:")
	t.Logf("  Without -validate-data: %.2f ops/sec", throughputWithoutValidation)
	t.Logf("  With -validate-data:    %.2f ops/sec", throughputWithValidation)
	t.Logf("  Performance retention:  %.2f%% (%.2fx)", performanceRetention*100, performanceRetention)
	t.Logf("  Throughput decrease:    %.2f%%", throughputDecrease)

	// The issue reported ~90% decrease (533 ops -> 50 ops)
	// We consider the issue still present if throughput decreases by more than 50%
	const maxAcceptableDecrease = 50.0

	if throughputDecrease > maxAcceptableDecrease {
		t.Errorf("REGRESSION: -validate-data causes %.2f%% throughput decrease (threshold: %.2f%%)",
			throughputDecrease, maxAcceptableDecrease)
		t.Logf("After optimization, the overhead should be well under 50%%.")
	} else {
		t.Logf("\n✅ -validate-data causes only %.2f%% throughput decrease (threshold: %.2f%%)",
			throughputDecrease, maxAcceptableDecrease)
	}
}

// measureWriteThroughput performs a write workload and measures the throughput
func measureWriteThroughput(
	t *testing.T,
	container *testutil.ScyllaDBContainer,
	keyspace, table string,
	partitionCount, clusteringRowCount int64,
	clusteringRowSize int64,
	testConcurrency int,
	duration time.Duration,
	validateData bool,
) float64 {
	t.Helper()

	// Create cluster config
	cluster := container.GetClusterConfig()
	cluster.Keyspace = keyspace
	cluster.NumConns = testConcurrency
	cluster.DisableInitialHostLookup = true

	// Create session
	session, err := cluster.CreateSession()
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}
	defer session.Close()

	// Create workload generator (sequential workload like in the issue)
	workload, err := workloads.NewSequentialVisitAll(
		0,                                 // rowOffset
		partitionCount*clusteringRowCount, // rowCount
		clusteringRowCount,                // clusteringRowCount
	)
	if err != nil {
		t.Fatalf("Failed to create workload: %v", err)
	}

	// Create a done channel to signal when to stop
	done := make(chan struct{})

	// Run workload concurrently
	type threadStats struct {
		ops int64
	}
	stats := make([]threadStats, testConcurrency)

	// Use WaitGroup to ensure all goroutines finish properly
	var wg sync.WaitGroup
	wg.Add(testConcurrency)

	for i := 0; i < testConcurrency; i++ {
		go func(threadID int) {
			defer wg.Done()

			request := fmt.Sprintf(
				"INSERT INTO %s.%s (pk, ck, v) VALUES (?, ?, ?)",
				keyspace,
				table,
			)

			ops := int64(0)

			for {
				select {
				case <-done:
					stats[threadID].ops = ops
					return
				default:
					// Generate next operation
					pk := workload.NextPartitionKey()
					if workload.IsPartitionDone() {
						stats[threadID].ops = ops
						return
					}
					ck := workload.NextClusteringKey()

					// Generate data with or without validation
					value, generateErr := GenerateData(pk, ck, clusteringRowSize, validateData)
					if generateErr != nil {
						// Log error but don't fail the test - just stop this goroutine
						t.Logf("Thread %d: Failed to generate data: %v", threadID, generateErr)
						stats[threadID].ops = ops
						return
					}

					// Execute write
					if execErr := session.Query(request, pk, ck, value).Exec(); execErr != nil {
						// Don't fail on errors, just continue
						continue
					}

					// Record successful operation
					ops++
				}
			}
		}(i)
	}

	// Wait for the test duration
	time.Sleep(duration)

	// Signal all goroutines to stop
	close(done)

	// Wait for all goroutines to finish
	wg.Wait()

	// Calculate throughput from collected stats
	var totalOps int64
	for i := 0; i < testConcurrency; i++ {
		totalOps += stats[i].ops
	}

	// Calculate operations per second
	actualDuration := duration.Seconds()
	throughput := float64(totalOps) / actualDuration

	t.Logf("Completed %d operations in %.2fs with validateData=%v (throughput: %.2f ops/sec)",
		totalOps, actualDuration, validateData, throughput)

	return throughput
}

// BenchmarkGenerateDataWithValidation benchmarks the GenerateData function
// to understand the overhead of data validation.
//
// Optimizations applied:
// 1. Replaced SHA256 (crypto) with CRC32C (hardware-accelerated) for checksumming
// 2. Replaced binary.Write reflection-based I/O with direct byte manipulation
// 3. Single allocation instead of buffer → copy
// 4. Deterministic xorshift payload instead of globally-locked random source
//
// Expected Results (after optimization):
// - Without validation: ~10-12μs per operation (simple byte array allocation)
// - With validation: ~25-27μs per operation (~2.3x slower — down from ~10x)
func BenchmarkGenerateDataWithValidation(b *testing.B) {
	const size = 51200 // Same as in the issue

	b.Run("without_validation", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, _ = GenerateData(int64(i), int64(i), size, false)
		}
	})

	b.Run("with_validation", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, _ = GenerateData(int64(i), int64(i), size, true)
		}
	})
}

// BenchmarkValidateData benchmarks the ValidateData function to measure
// the overhead of data validation during reads.
//
// Expected Results (after optimization):
// - ValidateData adds ~2μs overhead per read operation (zero allocations)
// - Down from ~60μs with 8 allocations (27x faster)
// - Uses hardware-accelerated CRC32C and direct byte access
func BenchmarkValidateData(b *testing.B) {
	const size = 51200
	data, err := GenerateData(1, 2, size, true)
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ValidateData(1, 2, data, true)
	}
}
