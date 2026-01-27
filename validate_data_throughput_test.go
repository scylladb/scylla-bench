package main

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/scylladb/scylla-bench/pkg/testutil"
	"github.com/scylladb/scylla-bench/pkg/workloads"
)

// TestValidateDataThroughputImpact tests if the -validate-data parameter
// significantly impacts throughput as reported in the issue.
// This test requires Docker and is skipped by default.
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
		partitionCount      = 100   // Smaller than issue (3000) for faster test
		clusteringRowCount  = 100   // Smaller than issue (1000) for faster test
		clusteringRowSize   = 51200 // Same as issue
		testConcurrency     = 4     // Smaller than issue (400) for faster test
		testDuration        = 10 * time.Second
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
	throughputRatio := float64(throughputWithValidation) / float64(throughputWithoutValidation)
	throughputDecrease := (1.0 - throughputRatio) * 100.0

	// Log results
	t.Logf("\n=== Throughput Test Results ===")
	t.Logf("Without -validate-data: %.2f ops/sec", throughputWithoutValidation)
	t.Logf("With -validate-data:    %.2f ops/sec", throughputWithValidation)
	t.Logf("Throughput ratio:       %.2f%%", throughputRatio*100)
	t.Logf("Throughput decrease:    %.2f%%", throughputDecrease)

	// The issue reported ~90% decrease (533 ops -> 50 ops)
	// We consider the issue still present if throughput decreases by more than 50%
	const maxAcceptableDecrease = 50.0

	if throughputDecrease > maxAcceptableDecrease {
		t.Logf("\n⚠️  ISSUE STILL PRESENT: -validate-data causes %.2f%% throughput decrease (threshold: %.2f%%)",
			throughputDecrease, maxAcceptableDecrease)
		t.Logf("This is consistent with the reported issue where throughput decreased from ~533 ops to ~50 ops")
	} else {
		t.Logf("\n✅ ISSUE RESOLVED: -validate-data causes only %.2f%% throughput decrease (threshold: %.2f%%)",
			throughputDecrease, maxAcceptableDecrease)
	}

	// This test is informational - we don't fail it, just report the findings
	// If you want to enforce a threshold, uncomment the following:
	// if throughputDecrease > maxAcceptableDecrease {
	//     t.Errorf("Throughput decrease of %.2f%% exceeds threshold of %.2f%%",
	//         throughputDecrease, maxAcceptableDecrease)
	// }
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
		0,                      // rowOffset
		totalRowsInWorkload,    // rowCount
		clusteringRowCount,     // clusteringRowCount
	)

	// Create a done channel to signal when to stop
	done := make(chan struct{})
	totalOps := int64(0)
	totalRows := int64(0)

	// Run workload concurrently
	type threadStats struct {
		ops  int64
		rows int64
	}
	stats := make([]threadStats, testConcurrency)

	for i := 0; i < testConcurrency; i++ {
		go func(threadID int) {
			request := fmt.Sprintf(
				"INSERT INTO %s.%s (pk, ck, v) VALUES (?, ?, ?)",
				keyspace,
				table,
			)

			ops := int64(0)
			rows := int64(0)

			for {
				select {
				case <-done:
					stats[threadID].ops = ops
					stats[threadID].rows = rows
					return
				default:
					// Generate next operation
					pk := workload.NextPartitionKey()
					if workload.IsPartitionDone() {
						stats[threadID].ops = ops
						stats[threadID].rows = rows
						return
					}
					ck := workload.NextClusteringKey()

					// Generate data with or without validation
					value, err := GenerateData(pk, ck, clusteringRowSize, validateData)
					if err != nil {
						t.Errorf("Failed to generate data: %v", err)
						stats[threadID].ops = ops
						stats[threadID].rows = rows
						return
					}

					// Execute write
					err = session.Query(request, pk, ck, value).Exec()

					if err != nil {
						// Don't fail on errors, just continue
						continue
					}

					// Record successful operation
					ops++
					rows++
				}
			}
		}(i)
	}

	// Wait for the test duration
	time.Sleep(duration)

	// Signal all goroutines to stop
	close(done)

	// Wait a bit for goroutines to finish
	time.Sleep(500 * time.Millisecond)

	// Calculate throughput
	for i := 0; i < testConcurrency; i++ {
		totalOps += stats[i].ops
		totalRows += stats[i].rows
	}

	// Calculate operations per second
	actualDuration := duration.Seconds()
	throughput := float64(totalOps) / actualDuration

	t.Logf("Completed %d operations (%d rows) in %.2fs with validateData=%v (throughput: %.2f ops/sec)",
		totalOps, totalRows, actualDuration, validateData, throughput)

	return throughput
}

// BenchmarkGenerateDataWithValidation benchmarks the GenerateData function
// to understand the overhead of data validation
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

// BenchmarkValidateData benchmarks the ValidateData function
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
