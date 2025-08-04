package testutil

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestScyllaDBContainer tests the ScyllaDB container helper
// This test is skipped by default because it requires Docker
func TestScyllaDBContainer(t *testing.T) {
	// Skip if not explicitly enabled
	if os.Getenv("RUN_CONTAINER_TESTS") != "true" {
		t.Skip("Skipping container test. Set RUN_CONTAINER_TESTS=true to run")
	}

	// Create a context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Create a ScyllaDB container
	container, err := NewScyllaDBContainer(ctx)
	if err != nil {
		t.Fatalf("Failed to create ScyllaDB container: %v", err)
	}
	defer func() {
		if err = container.Close(ctx); err != nil {
			t.Logf("Failed to close container: %v", err)
		}
	}()

	// Test creating a keyspace
	err = container.CreateKeyspace("test_keyspace", 1)
	if err != nil {
		t.Fatalf("Failed to create keyspace: %v", err)
	}

	// Test creating a table
	err = container.CreateTable("test_keyspace", "test_table")
	if err != nil {
		t.Fatalf("Failed to create table: %v", err)
	}

	// Test creating a counter table
	err = container.CreateCounterTable("test_keyspace", "test_counter_table")
	if err != nil {
		t.Fatalf("Failed to create counter table: %v", err)
	}

	// Test truncating a table
	err = container.TruncateTable("test_keyspace", "test_table")
	if err != nil {
		t.Fatalf("Failed to truncate table: %v", err)
	}

	// Test getting a cluster config
	cluster := container.GetClusterConfig()
	if cluster == nil {
		t.Fatal("GetClusterConfig returned nil")
	}

	// Verify the cluster config
	if cluster.Hosts[0] != container.Host {
		t.Errorf("Expected host %s, got %s", container.Host, cluster.Hosts[0])
	}
	if cluster.Port != container.Port {
		t.Errorf("Expected port %d, got %d", container.Port, cluster.Port)
	}

	// Create a session with the cluster config
	session, err := cluster.CreateSession()
	if err != nil {
		t.Fatalf("Failed to create session with cluster config: %v", err)
	}
	defer session.Close()

	// Execute a simple query
	if err = session.Query("SELECT * FROM test_keyspace.test_table LIMIT 1").Exec(); err != nil {
		t.Fatalf("Failed to execute query: %v", err)
	}

	t.Log("ScyllaDB container test passed")
}
