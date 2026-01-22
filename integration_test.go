package main

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/gocql/gocql"

	"github.com/scylladb/scylla-bench/pkg/results"
	"github.com/scylladb/scylla-bench/pkg/testutil"
	"github.com/scylladb/scylla-bench/pkg/workloads"
	"github.com/scylladb/scylla-bench/random"
)

// initTestGlobals initializes the global variables needed by the benchmark functions
func initTestGlobals() {
	// Initialize global variables used by modes.go functions
	keyspaceName = "scylla_bench"
	tableName = "test"
	counterTableName = "test_counters"

	// Initialize clustering row size distribution (default is Fixed{Value: 4})
	clusteringRowSizeDist = random.Fixed{Value: 4}

	// Initialize read-related variables
	rowsPerRequest = 1
	provideUpperBound = false
	inRestriction = false
	noLowerBound = false
	selectOrderByParsed = []string{""} // Default is "none" which maps to empty string
}

// TestIntegration runs integration tests against ScyllaDB
// These tests validate that all workload types and modes execute successfully
// Run with: RUN_CONTAINER_TESTS=true go test -v -run TestIntegration
func TestIntegration(t *testing.T) {
	// Skip if not explicitly enabled
	if os.Getenv("RUN_CONTAINER_TESTS") != "true" {
		t.Skip("Skipping integration tests. Set RUN_CONTAINER_TESTS=true to run")
	}

	// Note: Not using t.Parallel() because tests modify global state
	// Initialize global variables
	initTestGlobals()

	// Create a context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	// Start a ScyllaDB container
	container, err := testutil.NewScyllaDBContainer(ctx)
	if err != nil {
		t.Fatalf("Failed to start ScyllaDB container: %v", err)
	}
	defer func() {
		if err = container.Close(ctx); err != nil {
			t.Logf("Failed to close container: %v", err)
		}
	}()

	// Create test keyspace
	err = container.CreateKeyspace("scylla_bench", 1)
	if err != nil {
		t.Fatalf("Failed to create keyspace: %v", err)
	}

	// Create main table for write/read operations
	err = container.CreateTable("scylla_bench", "test")
	if err != nil {
		t.Fatalf("Failed to create table: %v", err)
	}

	// Create counter table for counter operations
	err = container.CreateCounterTable("scylla_bench", "test_counters")
	if err != nil {
		t.Fatalf("Failed to create counter table: %v", err)
	}

	session := container.Session

	// Run sub-tests for each workload and mode combination
	t.Run("SequentialWorkload", func(t *testing.T) {
		testSequentialWorkload(t, session)
	})

	t.Run("UniformWorkload", func(t *testing.T) {
		testUniformWorkload(t, session)
	})

	t.Run("TimeSeriesWorkload", func(t *testing.T) {
		testTimeSeriesWorkload(t, session)
	})

	t.Run("CounterOperations", func(t *testing.T) {
		testCounterOperations(t, session)
	})

	t.Run("ScanOperations", func(t *testing.T) {
		testScanOperations(t, session)
	})

	t.Run("MixedMode", func(t *testing.T) {
		testMixedMode(t, session)
	})
}

// testSequentialWorkload tests the sequential workload with write and read operations
func testSequentialWorkload(t *testing.T, session *gocql.Session) {
	t.Helper()
	// Note: Not using t.Parallel() due to shared global state and result access patterns

	// Test writes first
	t.Run("Write", func(t *testing.T) {
		workload := workloads.NewSequentialVisitAll(0, 1000, 10)
		testResult := results.NewTestThreadResult()

		// Run writes for 5 seconds
		done := make(chan bool)
		go func() {
			DoWrites(session, testResult, workload, &UnlimitedRateLimiter{}, false)
			done <- true
		}()

		select {
		case <-time.After(5 * time.Second):
			// Time's up, stop the test
		case <-done:
			// Completed early
		}

		// Verify some operations were completed
		// Allow goroutine to finish current operation
		time.Sleep(100 * time.Millisecond)

		if testResult.FullResult.Operations == 0 {
			t.Error("Sequential write: no operations completed")
		}

		if testResult.FullResult.Errors > 0 {
			t.Errorf("Sequential write: encountered %d errors", testResult.FullResult.Errors)
		}

		t.Logf("Sequential write: completed %d operations with %d errors",
			testResult.FullResult.Operations, testResult.FullResult.Errors)
	})

	// Test reads
	t.Run("Read", func(t *testing.T) {
		workload := workloads.NewSequentialVisitAll(0, 1000, 10)
		testResult := results.NewTestThreadResult()

		// Run reads for 5 seconds
		done := make(chan bool)
		go func() {
			DoReads(session, testResult, workload, &UnlimitedRateLimiter{}, false)
			done <- true
		}()

		select {
		case <-time.After(5 * time.Second):
			// Time's up
		case <-done:
			// Completed early
		}

		// Verify some operations were completed
		// Allow goroutine to finish current operation
		time.Sleep(100 * time.Millisecond)

		if testResult.FullResult.Operations == 0 {
			t.Error("Sequential read: no operations completed")
		}

		if testResult.FullResult.Errors > 0 {
			t.Errorf("Sequential read: encountered %d errors", testResult.FullResult.Errors)
		}

		t.Logf("Sequential read: completed %d operations with %d errors",
			testResult.FullResult.Operations, testResult.FullResult.Errors)
	})
}

// testUniformWorkload tests the uniform random workload
func testUniformWorkload(t *testing.T, session *gocql.Session) {
	t.Helper()
	// Note: Not using t.Parallel() due to shared global state and result access patterns

	// Test writes
	t.Run("Write", func(t *testing.T) {
		workload := workloads.NewRandomUniform(0, 1000, 0, 10)
		testResult := results.NewTestThreadResult()

		done := make(chan bool)
		go func() {
			DoWrites(session, testResult, workload, &UnlimitedRateLimiter{}, false)
			done <- true
		}()

		select {
		case <-time.After(5 * time.Second):
		case <-done:
		}

		// Allow goroutine to finish current operation
		time.Sleep(100 * time.Millisecond)

		if testResult.FullResult.Operations == 0 {
			t.Error("Uniform write: no operations completed")
		}

		if testResult.FullResult.Errors > 0 {
			t.Errorf("Uniform write: encountered %d errors", testResult.FullResult.Errors)
		}

		t.Logf("Uniform write: completed %d operations with %d errors",
			testResult.FullResult.Operations, testResult.FullResult.Errors)
	})

	// Test reads
	t.Run("Read", func(t *testing.T) {
		workload := workloads.NewRandomUniform(0, 1000, 0, 10)
		testResult := results.NewTestThreadResult()

		done := make(chan bool)
		go func() {
			DoReads(session, testResult, workload, &UnlimitedRateLimiter{}, false)
			done <- true
		}()

		select {
		case <-time.After(5 * time.Second):
		case <-done:
		}

		// Allow goroutine to finish current operation
		time.Sleep(100 * time.Millisecond)

		if testResult.FullResult.Operations == 0 {
			t.Error("Uniform read: no operations completed")
		}

		if testResult.FullResult.Errors > 0 {
			t.Errorf("Uniform read: encountered %d errors", testResult.FullResult.Errors)
		}

		t.Logf("Uniform read: completed %d operations with %d errors",
			testResult.FullResult.Operations, testResult.FullResult.Errors)
	})
}

// testTimeSeriesWorkload tests the timeseries workload
func testTimeSeriesWorkload(t *testing.T, session *gocql.Session) {
	t.Helper()
	// Note: Not using t.Parallel() due to shared global state and result access patterns

	// Test timeseries writes
	t.Run("Write", func(t *testing.T) {
		writeStartTime := time.Now()
		workload := workloads.NewTimeSeriesWriter(0, 1, 1000, 0, 100, writeStartTime, 1000)
		testResult := results.NewTestThreadResult()

		done := make(chan bool)
		go func() {
			DoWrites(session, testResult, workload, &UnlimitedRateLimiter{}, false)
			done <- true
		}()

		select {
		case <-time.After(5 * time.Second):
		case <-done:
		}

		// Allow goroutine to finish current operation
		time.Sleep(100 * time.Millisecond)

		if testResult.FullResult.Operations == 0 {
			t.Error("TimeSeries write: no operations completed")
		}

		if testResult.FullResult.Errors > 0 {
			t.Errorf("TimeSeries write: encountered %d errors", testResult.FullResult.Errors)
		}

		t.Logf("TimeSeries write: completed %d operations with %d errors",
			testResult.FullResult.Operations, testResult.FullResult.Errors)
	})

	// Test timeseries reads
	t.Run("Read", func(t *testing.T) {
		readStartTime := time.Now()
		workload := workloads.NewTimeSeriesReader(0, 1, 1000, 0, 100, 1000, "uniform", readStartTime)
		testResult := results.NewTestThreadResult()

		done := make(chan bool)
		go func() {
			DoReads(session, testResult, workload, &UnlimitedRateLimiter{}, false)
			done <- true
		}()

		select {
		case <-time.After(5 * time.Second):
		case <-done:
		}

		// Allow goroutine to finish current operation
		time.Sleep(100 * time.Millisecond)

		if testResult.FullResult.Operations == 0 {
			t.Error("TimeSeries read: no operations completed")
		}

		if testResult.FullResult.Errors > 0 {
			t.Errorf("TimeSeries read: encountered %d errors", testResult.FullResult.Errors)
		}

		t.Logf("TimeSeries read: completed %d operations with %d errors",
			testResult.FullResult.Operations, testResult.FullResult.Errors)
	})
}

// testCounterOperations tests counter update and read operations
func testCounterOperations(t *testing.T, session *gocql.Session) {
	t.Helper()
	// Note: Not using t.Parallel() due to shared global state and result access patterns

	// Test counter updates
	t.Run("Update", func(t *testing.T) {
		workload := workloads.NewRandomUniform(0, 1000, 0, 10)
		testResult := results.NewTestThreadResult()

		done := make(chan bool)
		go func() {
			DoCounterUpdates(session, testResult, workload, &UnlimitedRateLimiter{}, false)
			done <- true
		}()

		select {
		case <-time.After(5 * time.Second):
		case <-done:
		}

		// Allow goroutine to finish current operation
		time.Sleep(100 * time.Millisecond)

		if testResult.FullResult.Operations == 0 {
			t.Error("Counter update: no operations completed")
		}

		if testResult.FullResult.Errors > 0 {
			t.Errorf("Counter update: encountered %d errors", testResult.FullResult.Errors)
		}

		t.Logf("Counter update: completed %d operations with %d errors",
			testResult.FullResult.Operations, testResult.FullResult.Errors)
	})

	// Test counter reads
	t.Run("Read", func(t *testing.T) {
		workload := workloads.NewRandomUniform(0, 1000, 0, 10)
		testResult := results.NewTestThreadResult()

		done := make(chan bool)
		go func() {
			DoCounterReads(session, testResult, workload, &UnlimitedRateLimiter{}, false)
			done <- true
		}()

		select {
		case <-time.After(5 * time.Second):
		case <-done:
		}

		// Allow goroutine to finish current operation
		time.Sleep(100 * time.Millisecond)

		if testResult.FullResult.Operations == 0 {
			t.Error("Counter read: no operations completed")
		}

		if testResult.FullResult.Errors > 0 {
			t.Errorf("Counter read: encountered %d errors", testResult.FullResult.Errors)
		}

		t.Logf("Counter read: completed %d operations with %d errors",
			testResult.FullResult.Operations, testResult.FullResult.Errors)
	})
}

// testScanOperations tests table scanning
func testScanOperations(t *testing.T, session *gocql.Session) {
	t.Helper()
	// Note: Not using t.Parallel() due to shared global state and result access patterns

	workload := workloads.NewRangeScan(300, 0, 300)
	testResult := results.NewTestThreadResult()

	done := make(chan bool)
	go func() {
		DoScanTable(session, testResult, workload, &UnlimitedRateLimiter{}, false)
		done <- true
	}()

	select {
	case <-time.After(10 * time.Second):
		// Allow more time for scan operations
	case <-done:
	}

	// Allow goroutine to finish current operation
	time.Sleep(100 * time.Millisecond)

	if testResult.FullResult.Operations == 0 {
		t.Error("Scan: no operations completed")
	}

	if testResult.FullResult.Errors > 0 {
		t.Errorf("Scan: encountered %d errors", testResult.FullResult.Errors)
	}

	t.Logf("Scan: completed %d operations with %d errors",
		testResult.FullResult.Operations, testResult.FullResult.Errors)
}

// testMixedMode tests the mixed read/write mode
func testMixedMode(t *testing.T, session *gocql.Session) {
	t.Helper()
	// Note: Not using t.Parallel() due to shared global state and result access patterns

	// Reset global counter for mixed mode
	globalMixedOperationCount.Store(0)

	workload := workloads.NewRandomUniform(0, 1000, 0, 10)
	testResult := results.NewTestThreadResult()

	done := make(chan bool)
	go func() {
		DoMixed(session, testResult, workload, &UnlimitedRateLimiter{}, false)
		done <- true
	}()

	select {
	case <-time.After(10 * time.Second):
	case <-done:
	}

	// Allow goroutine to finish current operation
	time.Sleep(100 * time.Millisecond)

	if testResult.FullResult.Operations == 0 {
		t.Error("Mixed mode: no operations completed")
	}

	if testResult.FullResult.Errors > 0 {
		t.Errorf("Mixed mode: encountered %d errors", testResult.FullResult.Errors)
	}

	t.Logf("Mixed mode: completed %d operations with %d errors",
		testResult.FullResult.Operations, testResult.FullResult.Errors)
}

// TestIntegrationWithDataValidation tests data validation functionality
func TestIntegrationWithDataValidation(t *testing.T) {
	// Skip if not explicitly enabled
	if os.Getenv("RUN_CONTAINER_TESTS") != "true" {
		t.Skip("Skipping integration tests. Set RUN_CONTAINER_TESTS=true to run")
	}

	// Note: Not using t.Parallel() because tests modify global state
	// Initialize global variables
	initTestGlobals()

	// Create a context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	// Start a ScyllaDB container
	container, err := testutil.NewScyllaDBContainer(ctx)
	if err != nil {
		t.Fatalf("Failed to start ScyllaDB container: %v", err)
	}
	defer func() {
		if err = container.Close(ctx); err != nil {
			t.Logf("Failed to close container: %v", err)
		}
	}()

	// Create test keyspace and table
	err = container.CreateKeyspace("scylla_bench", 1)
	if err != nil {
		t.Fatalf("Failed to create keyspace: %v", err)
	}

	err = container.CreateTable("scylla_bench", "test")
	if err != nil {
		t.Fatalf("Failed to create table: %v", err)
	}

	session := container.Session

	// Test data validation with sequential workload
	t.Run("DataValidation", func(t *testing.T) {
		workload := workloads.NewSequentialVisitAll(0, 100, 5)
		writeResult := results.NewTestThreadResult()

		// First, write data with validation enabled
		done := make(chan bool)
		go func() {
			DoWrites(session, writeResult, workload, &UnlimitedRateLimiter{}, true)
			done <- true
		}()

		select {
		case <-time.After(5 * time.Second):
		case <-done:
		}

		// Allow goroutine to finish current operation
		time.Sleep(100 * time.Millisecond)

		if writeResult.FullResult.Operations == 0 {
			t.Fatal("Data validation write: no operations completed")
		}

		if writeResult.FullResult.Errors > 0 {
			t.Fatalf("Data validation write: encountered %d errors", writeResult.FullResult.Errors)
		}

		t.Logf("Data validation write: completed %d operations", writeResult.FullResult.Operations)

		// Now read the data back with validation enabled
		readWorkload := workloads.NewSequentialVisitAll(0, 100, 5)
		readResult := results.NewTestThreadResult()

		done = make(chan bool)
		go func() {
			DoReads(session, readResult, readWorkload, &UnlimitedRateLimiter{}, true)
			done <- true
		}()

		select {
		case <-time.After(5 * time.Second):
		case <-done:
		}

		// Allow goroutine to finish current operation
		time.Sleep(100 * time.Millisecond)

		if readResult.FullResult.Operations == 0 {
			t.Error("Data validation read: no operations completed")
		}

		if readResult.FullResult.Errors > 0 {
			t.Errorf("Data validation read: encountered %d errors (validation may have detected corruption)", readResult.FullResult.Errors)
		}

		t.Logf("Data validation read: completed %d operations with %d errors",
			readResult.FullResult.Operations, readResult.FullResult.Errors)
	})
}

// TestIntegrationQuickSmoke is a fast smoke test that runs all modes quickly
// This can be used for quick validation during development
func TestIntegrationQuickSmoke(t *testing.T) {
	// Skip if not explicitly enabled
	if os.Getenv("RUN_CONTAINER_TESTS") != "true" {
		t.Skip("Skipping integration tests. Set RUN_CONTAINER_TESTS=true to run")
	}

	// Note: Not using t.Parallel() because tests modify global state
	// Initialize global variables
	initTestGlobals()

	// Create a context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	// Start a ScyllaDB container
	container, err := testutil.NewScyllaDBContainer(ctx)
	if err != nil {
		t.Fatalf("Failed to start ScyllaDB container: %v", err)
	}
	defer func() {
		if err = container.Close(ctx); err != nil {
			t.Logf("Failed to close container: %v", err)
		}
	}()

	// Create test keyspace
	err = container.CreateKeyspace("scylla_bench", 1)
	if err != nil {
		t.Fatalf("Failed to create keyspace: %v", err)
	}

	err = container.CreateTable("scylla_bench", "test")
	if err != nil {
		t.Fatalf("Failed to create table: %v", err)
	}

	err = container.CreateCounterTable("scylla_bench", "test_counters")
	if err != nil {
		t.Fatalf("Failed to create counter table: %v", err)
	}

	session := container.Session

	// Quick test of each mode (2 seconds each)
	testCases := []struct {
		workload workloads.Generator
		mode     ModeFunc
		name     string
	}{
		{name: "Sequential-Write", workload: workloads.NewSequentialVisitAll(0, 100, 5), mode: DoWrites},
		{name: "Sequential-Read", workload: workloads.NewSequentialVisitAll(0, 100, 5), mode: DoReads},
		{name: "Uniform-Write", workload: workloads.NewRandomUniform(0, 100, 0, 5), mode: DoWrites},
		{name: "Uniform-Read", workload: workloads.NewRandomUniform(0, 100, 0, 5), mode: DoReads},
		{name: "Counter-Update", workload: workloads.NewRandomUniform(0, 100, 0, 5), mode: DoCounterUpdates},
		{name: "Counter-Read", workload: workloads.NewRandomUniform(0, 100, 0, 5), mode: DoCounterReads},
		{name: "Scan", workload: workloads.NewRangeScan(300, 0, 300), mode: DoScanTable},
		{name: "Mixed", workload: workloads.NewRandomUniform(0, 100, 0, 5), mode: DoMixed},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			testResult := results.NewTestThreadResult()

			done := make(chan bool)
			go func() {
				tc.mode(session, testResult, tc.workload, &UnlimitedRateLimiter{}, false)
				done <- true
			}()

			select {
			case <-time.After(2 * time.Second):
			case <-done:
			}

			// Allow goroutine to finish current operation
			time.Sleep(100 * time.Millisecond)

			if testResult.FullResult.Operations == 0 {
				t.Errorf("%s: no operations completed", tc.name)
			}

			if testResult.FullResult.Errors > 0 {
				t.Errorf("%s: encountered %d errors", tc.name, testResult.FullResult.Errors)
			}

			fmt.Printf("%s: %d ops, %d errors\n", tc.name, testResult.FullResult.Operations, testResult.FullResult.Errors)
		})
	}
}
