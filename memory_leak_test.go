package main

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"runtime/pprof"
	"testing"
	"time"

	"github.com/scylladb/gocql"

	"github.com/scylladb/scylla-bench/pkg/testutil"
)

// TestMemoryLeakWithRetries is an integration test that can be run manually
// to verify that the fixes for memory leaks are working properly.
//
// This test uses testcontainers to create a ScyllaDB container for testing.
// Docker must be running on the system to use this test.
//
// Run with: go test -v -run TestMemoryLeakWithRetries
//
// The test will:
// 1. Start a ScyllaDB container
// 2. Create a test keyspace and table
// 3. Run a series of operations with forced retries
// 4. Monitor memory usage to detect leaks
func TestMemoryLeakWithRetries(t *testing.T) {
	// Skip in normal test runs
	if os.Getenv("RUN_MEMORY_LEAK_TEST") != "true" {
		t.Skip("Skipping memory leak test. Set RUN_MEMORY_LEAK_TEST=true to run")
	}

	// Create a context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	// Start a ScyllaDB container
	container, err := testutil.NewScyllaDBContainer(ctx)
	if err != nil {
		t.Fatalf("Failed to start ScyllaDB container: %v", err)
	}
	defer container.Close(ctx)

	// Use the container's session
	session := container.Session

	// Create test keyspace and table
	err = container.CreateKeyspace("memory_leak_test", 1)
	if err != nil {
		t.Fatalf("Failed to create keyspace: %v", err)
	}

	err = container.CreateTable("memory_leak_test", "test")
	if err != nil {
		t.Fatalf("Failed to create table: %v", err)
	}

	// Set up test parameters
	originalRetryHandler := retryHandler
	originalRetryNumber := retryNumber

	retryHandler = "sb"
	retryNumber = 30
	retryPolicy = &gocql.ExponentialBackoffRetryPolicy{
		NumRetries: retryNumber,
		Min:        time.Millisecond * 80,
		Max:        time.Second,
	}

	// Create a memory profile before the test
	beforeFile, err := os.Create("memory_before.pprof")
	if err != nil {
		t.Fatalf("Failed to create memory profile: %v", err)
	}
	runtime.GC()
	if err = pprof.WriteHeapProfile(beforeFile); err != nil {
		t.Fatalf("Failed to write memory profile: %v", err)
	}
	beforeFile.Close()

	// Record initial memory stats
	var m1 runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&m1)

	// Run a series of operations with retries
	fmt.Println("Running operations with retries...")

	// Number of operations to perform
	const numOperations = 1000

	// Create a value to insert
	value := make([]byte, 1024) // 1KB value

	// Perform write operations
	for i := 0; i < numOperations; i++ {
		// Use a query that will likely need to be retried
		query := session.Query("INSERT INTO memory_leak_test.test (pk, ck, v) VALUES (?, ?, ?) IF NOT EXISTS", i, i, value)

		// Execute with retry logic similar to DoWrites
		var currentAttempts int
		for {
			if err = query.Exec(); err == nil {
				break
			}

			// Simulate the retry logic in modes.go
			if retryHandler == "sb" {
				if currentAttempts >= retryNumber {
					break
				}

				sleepTime := getExponentialTime(retryPolicy.Min, retryPolicy.Max, currentAttempts)
				time.Sleep(sleepTime)
				currentAttempts++
			} else {
				break
			}
		}

		// Important: Release the query to prevent memory leaks
		query.Release()

		// Print progress
		if i%100 == 0 {
			fmt.Printf("Completed %d/%d operations\n", i, numOperations)

			// Check memory usage periodically
			var mCurrent runtime.MemStats
			runtime.GC()
			runtime.ReadMemStats(&mCurrent)
			fmt.Printf("Current memory usage: %d MB\n", mCurrent.Alloc/1024/1024)
		}
	}

	// Record final memory stats
	runtime.GC()
	var m2 runtime.MemStats
	runtime.ReadMemStats(&m2)

	// Create a memory profile after the test
	afterFile, err := os.Create("memory_after.pprof")
	if err != nil {
		t.Fatalf("Failed to create memory profile: %v", err)
	}
	if err = pprof.WriteHeapProfile(afterFile); err != nil {
		t.Fatalf("Failed to write memory profile: %v", err)
	}
	afterFile.Close()

	// Restore original settings
	retryHandler = originalRetryHandler
	retryNumber = originalRetryNumber

	// Report memory usage
	fmt.Printf("\nMemory usage before: %d MB\n", m1.Alloc/1024/1024)
	fmt.Printf("Memory usage after: %d MB\n", m2.Alloc/1024/1024)
	fmt.Printf("Memory difference: %d MB\n", (m2.Alloc-m1.Alloc)/1024/1024)

	// Check for significant memory increase
	// Note: Some increase is expected due to normal Go runtime behavior
	// The key is to look for unbounded growth over time
	if m2.Alloc > m1.Alloc*2 {
		t.Logf("Warning: Memory usage more than doubled from %d to %d bytes",
			m1.Alloc, m2.Alloc)
	}

	fmt.Println("\nMemory profiles written to memory_before.pprof and memory_after.pprof")
	fmt.Println("To analyze, run: go tool pprof memory_before.pprof memory_after.pprof")
	fmt.Println("Or visualize with: go tool pprof -http=:8080 memory_after.pprof")
}

// TestMemoryLeakWithHighConcurrency tests memory usage under high concurrency
// This test simulates the conditions that caused the original memory leak
func TestMemoryLeakWithHighConcurrency(t *testing.T) {
	// Skip in normal test runs
	if os.Getenv("RUN_MEMORY_LEAK_TEST") != "true" {
		t.Skip("Skipping memory leak test. Set RUN_MEMORY_LEAK_TEST=true to run")
	}

	// Create a context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	// Start a ScyllaDB container
	container, err := testutil.NewScyllaDBContainer(ctx)
	if err != nil {
		t.Fatalf("Failed to start ScyllaDB container: %v", err)
	}
	defer container.Close(ctx)

	// Use the container's session
	session := container.Session

	// Create test keyspace and table
	if err = container.CreateKeyspace("memory_leak_test", 1); err != nil {
		t.Fatalf("Failed to create keyspace: %v", err)
	}

	if err = container.CreateTable("memory_leak_test", "test"); err != nil {
		t.Fatalf("Failed to create table: %v", err)
	}

	// Set up test parameters to match the longevity test configuration
	originalRetryHandler := retryHandler
	originalRetryNumber := retryNumber

	retryHandler = "sb"
	retryNumber = 30
	retryPolicy = &gocql.ExponentialBackoffRetryPolicy{
		NumRetries: retryNumber,
		Min:        time.Millisecond * 80,
		Max:        time.Second,
	}

	// Create a memory profile before the test
	beforeFile, err := os.Create("memory_before_concurrency.pprof")
	if err != nil {
		t.Fatalf("Failed to create memory profile: %v", err)
	}
	runtime.GC()
	if err = pprof.WriteHeapProfile(beforeFile); err != nil {
		t.Fatalf("Failed to write memory profile: %v", err)
	}
	beforeFile.Close()

	// Record initial memory stats
	var m1, m2 runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&m1)

	// Number of goroutines to use
	const numGoroutines = 50
	const opsPerGoroutine = 200

	// Create a channel to wait for all goroutines to complete
	done := make(chan bool)

	// Start the goroutines
	for g := 0; g < numGoroutines; g++ {
		go func(goroutineID int) {
			value := make([]byte, 1024)

			for i := range opsPerGoroutine {
				pk := goroutineID*opsPerGoroutine + i

				query := session.Query("INSERT INTO memory_leak_test.test (pk, ck, v) VALUES (?, ?, ?) IF NOT EXISTS", pk, i, value)

				var currentAttempts int
				for {
					// Use a local error variable to avoid data race
					localErr := query.Exec()
					if localErr == nil {
						break
					}

					// Simulate the retry logic in modes.go
					if retryHandler != "sb" {
						break
					}

					if currentAttempts >= retryNumber {
						break
					}

					sleepTime := getExponentialTime(retryPolicy.Min, retryPolicy.Max, currentAttempts)
					time.Sleep(sleepTime)
					currentAttempts++
				}

				// Important: Release the query to prevent memory leaks
				query.Release()
			}

			// Signal completion
			done <- true
		}(g)
	}

	// Wait for all goroutines to complete
	for g := range numGoroutines {
		<-done

		// Check memory usage periodically
		if (g+1)%(numGoroutines/5) == 0 {
			var mCurrent runtime.MemStats
			runtime.GC()
			runtime.ReadMemStats(&mCurrent)
			fmt.Printf("Current memory usage: %d MB\n", mCurrent.Alloc/1024/1024)
		}
	}

	// Record final memory stats
	runtime.GC()
	runtime.ReadMemStats(&m2)

	// Create a memory profile after the test
	afterFile, err := os.Create("memory_after_concurrency.pprof")
	if err != nil {
		t.Fatalf("Failed to create memory profile: %v", err)
	}

	if err = pprof.WriteHeapProfile(afterFile); err != nil {
		t.Fatalf("Failed to write memory profile: %v", err)
	}
	afterFile.Close()

	// Restore original settings
	retryHandler = originalRetryHandler
	retryNumber = originalRetryNumber

	// Report memory usage
	fmt.Printf("\nMemory usage before: %d MB\n", m1.Alloc/1024/1024)
	fmt.Printf("Memory usage after: %d MB\n", m2.Alloc/1024/1024)
	fmt.Printf("Memory difference: %d MB\n", (m2.Alloc-m1.Alloc)/1024/1024)

	// Check for significant memory increase
	if m2.Alloc > m1.Alloc*2 {
		t.Logf("Warning: Memory usage more than doubled from %d to %d bytes",
			m1.Alloc, m2.Alloc)
	}

	fmt.Println("\nMemory profiles written to memory_before_concurrency.pprof and memory_after_concurrency.pprof")
	fmt.Println("To analyze, run: go tool pprof memory_before_concurrency.pprof memory_after_concurrency.pprof")
	fmt.Println("Or visualize with: go tool pprof -http=:8080 memory_after_concurrency.pprof")
}
