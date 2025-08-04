package testutil

import (
	"context"
	"fmt"
	"time"

	"github.com/gocql/gocql"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/scylladb"
	"github.com/testcontainers/testcontainers-go/wait"
)

// ScyllaDBContainer represents a ScyllaDB container for testing
type ScyllaDBContainer struct {
	Container *scylladb.Container
	Session   *gocql.Session
	Host      string
	Port      int
}

// NewScyllaDBContainer creates and starts a new ScyllaDB container
func NewScyllaDBContainer(ctx context.Context) (*ScyllaDBContainer, error) {
	// Configure the ScyllaDB container
	container, err := scylladb.Run(ctx,
		"scylladb/scylla:2025.2",
		testcontainers.WithWaitStrategy(
			wait.ForLog("Scylla version").
				WithStartupTimeout(2*time.Minute)),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to start ScyllaDB container: %w", err)
	}

	// Get the host and port
	host, err := container.Host(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get container host: %w", err)
	}

	mappedPort, err := container.MappedPort(ctx, "9042/tcp")
	if err != nil {
		return nil, fmt.Errorf("failed to get container port: %w", err)
	}
	port := mappedPort.Int()

	// Wait a bit longer for ScyllaDB to fully initialize
	time.Sleep(5 * time.Second)

	// Create a session
	cluster := gocql.NewCluster(host)
	cluster.Port = port
	cluster.Consistency = gocql.Quorum
	cluster.Timeout = 30 * time.Second
	cluster.ConnectTimeout = 30 * time.Second
	cluster.NumConns = 1
	// Disable initial host lookup which can cause issues with containers
	cluster.DisableInitialHostLookup = true

	// Wait for the container to be ready
	var session *gocql.Session
	var sessionErr error

	// Retry connection a few times with increasing delay
	for i := 0; i < 10; i++ {
		session, sessionErr = cluster.CreateSession()
		if sessionErr == nil {
			break
		}
		fmt.Printf("Connection attempt %d failed: %v\n", i+1, sessionErr)
		// Exponential backoff
		time.Sleep(time.Duration(2<<uint(i)) * time.Second)
	}

	if sessionErr != nil {
		// Clean up the container if we can't connect
		_ = container.Terminate(ctx)
		return nil, fmt.Errorf("failed to create session: %w", sessionErr)
	}

	return &ScyllaDBContainer{
		Container: container,
		Host:      host,
		Port:      port,
		Session:   session,
	}, nil
}

// CreateKeyspace creates a keyspace with the given name and replication factor
func (c *ScyllaDBContainer) CreateKeyspace(keyspaceName string, replicationFactor int) error {
	query := fmt.Sprintf(
		"CREATE KEYSPACE IF NOT EXISTS %s WITH REPLICATION = { 'class' : 'SimpleStrategy', 'replication_factor' : %d }",
		keyspaceName, replicationFactor)

	return c.Session.Query(query).Exec()
}

// CreateTable creates a table with the given name in the given keyspace
func (c *ScyllaDBContainer) CreateTable(keyspaceName, tableName string) error {
	query := fmt.Sprintf(
		"CREATE TABLE IF NOT EXISTS %s.%s (pk bigint, ck bigint, v blob, PRIMARY KEY(pk, ck)) WITH compression = { }",
		keyspaceName, tableName)

	return c.Session.Query(query).Exec()
}

// CreateCounterTable creates a counter table with the given name in the given keyspace
func (c *ScyllaDBContainer) CreateCounterTable(keyspaceName, tableName string) error {
	query := fmt.Sprintf(
		"CREATE TABLE IF NOT EXISTS %s.%s (pk bigint, ck bigint, c1 counter, c2 counter, c3 counter, c4 counter, c5 counter, PRIMARY KEY(pk, ck)) WITH compression = { }",
		keyspaceName, tableName)

	return c.Session.Query(query).Exec()
}

// TruncateTable truncates the given table in the given keyspace
func (c *ScyllaDBContainer) TruncateTable(keyspaceName, tableName string) error {
	query := fmt.Sprintf("TRUNCATE TABLE %s.%s", keyspaceName, tableName)
	return c.Session.Query(query).Exec()
}

// Close closes the session and terminates the container
func (c *ScyllaDBContainer) Close(ctx context.Context) error {
	if c.Session != nil {
		c.Session.Close()
	}

	if c.Container != nil {
		return c.Container.Terminate(ctx)
	}

	return nil
}

// GetClusterConfig returns a gocql.ClusterConfig configured to connect to this container
func (c *ScyllaDBContainer) GetClusterConfig() *gocql.ClusterConfig {
	cluster := gocql.NewCluster(c.Host)
	cluster.Port = c.Port
	cluster.Consistency = gocql.Quorum
	cluster.Timeout = 10 * time.Second
	return cluster
}
