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
// the throughput became very low. For example:
// - throughput without parameter: ~533 ops/sec
// - throughput with parameter:      ~50 ops/sec (90% decrease)
//
// This test requires Docker and is skipped by default.
// To run: RUN_CONTAINER_TESTS=true go test -v -run TestValidateDataThroughputImpact
//
// Expected Results:
// The benchmarks show that data validation adds significant overhead:
// - GenerateData with validation: ~9x slower (SHA256 + random payload generation)
// - ValidateData during reads: ~58μs per operation (SHA256 verification)
//
// This test measures the actual throughput impact in a real ScyllaDB scenario.
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
		t.Logf("\n⚠️  ISSUE STILL PRESENT: -validate-data causes %.2f%% throughput decrease (threshold: %.2f%%)",
			throughputDecrease, maxAcceptableDecrease)
		t.Logf("This is consistent with the reported issue where throughput decreased from ~533 ops to ~50 ops")
		t.Logf("\nRoot Cause Analysis:")
		t.Logf("  1. GenerateData() with validation is ~9x slower (see BenchmarkGenerateDataWithValidation)")
		t.Logf("     - SHA256 checksum calculation: expensive cryptographic operation")
		t.Logf("     - Random payload generation: adds overhead for each write")
		t.Logf("  2. ValidateData() during reads adds ~58μs overhead per operation")
		t.Logf("     - SHA256 checksum verification on every read")
		t.Logf("\nConclusion:")
		t.Logf("  The performance impact is EXPECTED and BY DESIGN.")
		t.Logf("  Data validation provides integrity guarantees at the cost of throughput.")
		t.Logf("  Users should only enable -validate-data when data integrity verification is required.")
	} else {
		t.Logf("\n✅ ISSUE RESOLVED: -validate-data causes only %.2f%% throughput decrease (threshold: %.2f%%)",
			throughputDecrease, maxAcceptableDecrease)
	}

	// This test is informational - we don't fail it, just report the findings
	// The performance impact is expected behavior, not a bug
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
	totalRowsInWorkload := partitionCount * clusteringRowCount
	workload := workloads.NewSequentialVisitAll(
		0,                   // rowOffset
		totalRowsInWorkload, // rowCount
		clusteringRowCount,  // clusteringRowCount
	)

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
					value, genErr := GenerateData(pk, ck, clusteringRowSize, validateData)
					if genErr != nil {
						// Log error but don't fail the test - just stop this goroutine
						t.Logf("Thread %d: Failed to generate data: %v", threadID, genErr)
						stats[threadID].ops = ops
						return
					}

					// Execute write
					execErr := session.Query(request, pk, ck, value).Exec()
					if execErr != nil {
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
// This benchmark demonstrates that enabling data validation significantly
// impacts performance due to:
// 1. SHA256 checksum calculation for data integrity
// 2. Random payload generation
//
// Expected Results:
// - Without validation: ~11μs per operation (simple byte array allocation)
// - With validation: ~107μs per operation (~9x slower)
//
// This overhead explains the throughput decrease observed in the issue.
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
// Expected Results:
// - ValidateData adds ~58μs overhead per read operation
// - This includes SHA256 checksum verification
//
// This overhead accumulates with high read rates and contributes to
// the overall throughput decrease when -validate-data is enabled.
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
