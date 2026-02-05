# scylla-bench

scylla-bench is a benchmarking tool for [Scylla](https://github.com/scylladb/scylla) written in Go. It aims at minimising the client overhead and provide a wide range of test scenarios.

## Install

The recommended way to install scylla-bench is to download the repository and then install it from source:

```
git clone https://github.com/scylladb/scylla-bench
cd scylla-bench/
go install .
go build .
```

__It is not recommended to download and install the tool directly using `go get` or `go install`__.
If you do that, a scylla-bench binary will be built __without using ScyllaDB's fork of the gocql driver__, and the shard-awareness __won't work__.

```bash
# If you use those commands, shard-awareness won't work!
# go get github.com/scylladb/scylla-bench
# go install github.com/scylladb/scylla-bench
```

This is due to the `go` tool not honoring replace directives in the `go.mod` file: https://github.com/golang/go/issues/30354

## Docker

scylla-bench can be built and run using Docker, which is useful for local development, testing, and deployment scenarios.

### Building a Local Docker Image

To build a local Docker image for development:

```bash
# Clone the repository
git clone https://github.com/scylladb/scylla-bench
cd scylla-bench/

# Build the production Docker image
make build-docker-image

# Or build with a custom tag
DOCKER_IMAGE_TAG=my-scylla-bench make build-docker-image
```

The Dockerfile supports multiple build targets:

- **`production`** (default): Minimal image with the static binary (~8.6MB)
- **`debug`**: Development image with debugging tools (gdb, delve debugger)
- **`production-sct`**: Alternative production build for SCT (Scylla Cluster Tests)

### Building Specific Targets

```bash
# Build debug image with debugging tools
docker build --target debug -t scylla-bench:debug .

# Build production image (same as make build-docker-image)
docker build --target production -t scylla-bench:latest .

# Build SCT-specific image
make build-sct-docker-image
```

### Running with Docker

```bash
# Run scylla-bench from Docker container
docker run --rm scylla-bench:latest --help

# Run mixed mode benchmark against local ScyllaDB
docker run --rm --network=host scylla-bench:latest \
  -workload uniform -mode mixed -nodes 127.0.0.1 \
  -concurrency 64 -duration 30s

# Run with custom settings
docker run --rm --network=host scylla-bench:latest \
  -workload sequential -mode write -nodes scylla-node1,scylla-node2 \
  -partition-count 10000 -clustering-row-count 100
```

### Debug Mode with Docker

The debug image includes delve debugger and development tools:

```bash
# Build and run debug image
docker build --target debug -t scylla-bench:debug .

# Run with debugger (exposes port 2345 for delve)
docker run --rm -p 2345:2345 -p 6060:6060 --network=host \
  scylla-bench:debug -workload uniform -mode mixed -nodes 127.0.0.1

# Connect with delve client
dlv connect localhost:2345
```

### Docker Environment Variables

The Docker images support several environment variables:

- **`GODEBUG`**: Go runtime debugging options (pre-configured for optimal performance)
- **`PATH`**: Binary path (automatically configured)
- **`TZ`**: Timezone (defaults to UTC)

### Docker Image Variants

| Image Target | Size | Use Case | Debugging Tools |
|--------------|------|----------|----------------|
| `production` | ~8.6MB | Production, CI/CD | ❌ |
| `debug` | ~500MB | Development, troubleshooting | ✅ (gdb, delve, vim) |
| `production-sct` | ~8.6MB | SCT integration testing | ❌ |

### Development Workflow with Docker

```bash
# 1. Make code changes
# 2. Build new image
make build-docker-image

# 3. Test your changes
docker run --rm --network=host scylla-bench:latest \
  -workload uniform -mode mixed -nodes 127.0.0.1 -duration 10s

# 4. Debug if needed
docker build --target debug -t scylla-bench:debug .
docker run --rm -p 2345:2345 --network=host scylla-bench:debug \
  -workload uniform -mode mixed -nodes 127.0.0.1
```

## Usage

### Schema adjustments

The default scylla-bench schema for regular columns looks like this:

```
CREATE TABLE IF NOT EXISTS scylla_bench.test (
    pk bigint,
    ck bigint,
    v blob,
    PRIMARY KEY(pk, ck)
) WITH compression = { }
```

scylla-bench allows configuring the number of partitions, number of rows in a partition and the size of a single row. This is done using flags `-partition-count`, `-clustering-row-count` and `-clustering-row-size` respectively.

### Modes

scylla-bench can operate in several modes (flag `-mode`) which basically determine what kind of requests are sent to the server. Some of the modes allow additional, further configuration.

#### Write mode (`-mode write`)

The behaviour in this mode differs depending on the configured number of rows per requests. If `-rows-per-request` is set to 1 (default) scylla-bench sends simple INSERT requests like this:

```
INSERT INTO scylla_bench.test (pk, ck, v) VALUES (?, ?, ?)
```

Otherwise, writes are sent in unlogged batches each containing at most `rows-per-request` insertions. All writes in a single batch refer to the same partition. The consequence of this is that in some configuration the number of rows written in a single requests can be actually smaller than the set value (e.g. `-clustering-row-count 2 -rows-per-request 4`).

#### Counter update mode (`-mode counter_update`)

Counter updates are written to a separate column family:
```
CREATE TABLE IF NOT EXISTS scylla_bench.test_counters (
    pk bigint,
    ck bigint,
    c1 counter,
    c2 counter,
    c3 counter,
    c4 counter,
    c5 counter,
    PRIMARY KEY(pk, ck)
) WITH compression = { }
```

Each request updates all five counters in a row and only one row per request is supported:

```
UPDATE scylla_bench.test_counters SET c1 = c1 + 1, c2 = c2 + 1, c3 = c3 + 1, c4 = c4 + 1, c5 = c5 + 1 WHERE pk = ? AND ck = ?
```

#### Read mode (`-mode read`)

Read mode is essentially split into four sub-modes and offers most configurability. The default requests resemble single partition paging queries, there is a lower bound of clustering keys and a limit which is can be adjusted using flag `rows-per-request`:

```
SELECT * FROM scylla_bench.test WHERE pk = ? AND ck >= ? LIMIT ?
```

It is possible to send request without a lower bound if flag `-no-lower-bound` is set:

```
SELECT * FROM %s.%s WHERE pk = ? LIMIT ?
```

Limit can be replaced by upper bound (flag `-provide-upper-bound`). In this case scylla-bench will choose the upper bound so that the expected number of rows equals the one specified by `rows-per-request`.

```
SELECT * FROM %s.%s WHERE pk = ? AND ck >= ? AND ck < ?
```

Finally, scylla-bench can send a request with an IN restriction (flag `-in-restriction`). Again, the number of requested clustering keys will equal `rows-per-request`.

```
SELECT * from %s.%s WHERE pk = ? AND ck IN (?, ...)
```

#### Counter read mode (`-mode counter_read`)

Counter read mode works in exactly the same as regular read mode (with the same configuration flags available) except that it reads data from the counter table `scylla_bench.test_counters`.

#### Scan mode (`-mode scan`)

Scan the entire table. This mode does not allow the `workload` to be configured (it has its own workload called `scan`). The scan mode allows for the token-space to be split into a user configurable sub-ranges and for querying these sub-ranges concurrently. The algorithm used is that descibed by [Avi's efficient range scans blog post](https://www.scylladb.com/2017/02/13/efficient-full-table-scans-with-scylla-1-6/).
The amount of sub-ranges that the token-space will be split into can be set by the `-range-count` flag. The recommended number to set this to is:

    -range-count = (nodes in cluster) ✕ (cores in node) ✕ 300

The number of sub-ranges to be read concurrency can be set by the `-concurrency` flag as usual. The recommended concurrency is:

    -concurrency = range-count/100

For more details on these numbers see the above mentioned blog post.

Essentially the following query is executed:

    SELECT * FROM scylla_bench.test WHERE token(pk) >= ? AND token(pk) <= ?

The number of iterations to run can be specified with the `-iterations` flag. The default is 1.

#### Mixed mode (`-mode mixed`)

Mixed mode performs a combination of read and write operations in a single benchmark run, providing a 50% read and 50% write workload similar to `cassandra-stress`. This mode alternates between write and read operations:

- Write operations are performed on even operation counts
- Read operations are performed on odd operation counts
- Uses the existing write and read logic for consistency
- Compatible with all workloads: sequential, uniform, and timeseries

Mixed mode requires specifying a duration (e.g., `-duration 1h`) and works with all the same configuration options as the individual read and write modes.

### Workloads

The second very important part of scylla-bench configuration is the workload. While mode chooses what kind of requests are to be sent to the cluster the workload decides which partitions and rows should be the target of these requests.

#### Sequential workload (`-workload sequential`)

This workload sequentially visits all partitions and rows in them. If the concurrency is larger than one then the whole population is split evenly between goroutines. Sequential workload allows specifying the offset of the first partition in the population (flag `-partition-offset`) to enable sequential population of the database by multiple clients. For example, if we have three simultaneously running scylla-bench processes:

1. `scylla-bench -workload sequential -mode write -partition-count 5`
1. `scylla-bench -workload sequential -mode write -partition-count 5 -partition-offset 5`
1. `scylla-bench -workload sequential -mode write -partition-count 5 -partition-offset 10`

The first loader will write partitions [0, 5), the second [5, 10) and the third [10, 15).

The sequential workload is useful for initial population of the database (in write mode) or warming up the cache for in-memory tests (in read mode).
The number of iterations to run can be specified with the `-iterations` flag. The default is 1.

#### Uniform workload (`-workload unifrom`)

Uniform workload chooses the partition key and clustering key randomly with a uniform distribution.

scylla-bench requires that the maximum duration of the test is specified when running with uniform workload (e.g. `-duration 1h`).

#### Time series workload (`-workload timeseries`)

Time series workload is the most complex one and behaves differently depending whether scylla-bench is run in write or read mode.

##### Write mode

In write mode time series workload divides the set of partitions between all goroutines (it is required that `-partition-count` >= `-concurrency`). Then each goroutine prepends to its partitions (partitions are chosen in a round-robind manner) new rows. Newer rows have smaller clustering keys than the older ones.

Once the partition reches `clustering-row-count` rows the goroutine will switch to a new partiton key. This means that the total partition count will be larger than `-partition-count`, since in time series workload that flag specifies only the number of partitions to which data is concurrently written.

The rate at which rows depends on `-max-rate` flag which must be specified in this workload. Since `-max-rate` sets the total maximum request rate of the whole client the rate at which rows will be appended to a single partition may be lower. The acutal per-partition is printed in scylla-bench configuration as `Write rate` (it is `concurrency / partition-count`). scylla-bench also prints "Start timestamp" which is necessary if there is a time series read load running.

##### Read mode

Time series workload in read mode is supposed to be run simultanously with time series writes. It requires specifying the start timestamp `-start-timestamp` and per-partition write rate `-write-rate` both of which are printed by scylla-bench running in write mode.

The time series workload in read mode chooses partition and clustering keys randomly from the range that has been written up to this point (using start timestamp and write rate). The distribution can be either uniform (flag `-distribution uniform`) or half-normal with the latest rows being most likely (`-distribution hnormal`).

Note that if the effective write rate is lower than the specified one the reader may attempt to read rows that are not yet present in the database. However, because `-max-rate` doesn't just limit the rate but tries to make the average op/s equal the specified values it will be able to recover from small periodic dips in write throughput.

### Other notable options

* `-concurrency` sets the number of goroutines used by the benchmark. The higher concurrency the higher internal client overheads, in some cases it may be better to use more than one client process instead of further increasing the concurrency.
* `-max-rate` set the expected rate of requests. The benchmark will try to reach this average which means that it may actually send more request per second if there was a period during which the throughput was lower than expected.
* `-connection-count` sets the number of connections.
* `-replication-factor` sets the replication factor of scylla-bench keyspace (default: 1).
* `-timeout` sets client timeout (default: 5s).
* `-metadata-schema-timeout` sets the timeout for schema and metadata queries (default: 60s). This can be helpful when running tests that involve schema changes (e.g., ALTER TABLE operations) on clusters that may experience temporary slowdowns.
* `-client-compression` enables or disables client compression (default: enabled).

* `-validate-data` defines data integrity verification. If set then some none-zero data will be written in such a way that it can be validated during read operation.
Note that this option should be set for both write and read (counter_update and counter_read) modes.

* `-iterations` sets the Number of iterations to run the given workloads. This is only relevant for workloads that have a finite number of steps. Currently the only such workloads are [sequential](#sequential-workload--workload-sequential) and [scan](#scan-mode--mode-scan). Can be combined with `-duration` to limit a run by both number of iterations and time. Set to 0 for infinite iterations. Defaults to 1.

* `keyspace` defines keyspace name to use
* `table` defines table name to work with
* `username` - cql username for authentication
* `password` - cql password for authentication
* `tls` - use TLS encryption

### Private Link / Client Routes

scylla-bench supports connecting to ScyllaDB clusters via Private Link endpoints using the client routes feature. 
This is useful when connecting to ScyllaDB Cloud clusters through AWS PrivateLink or similar private connectivity solutions when nodes are exposed via tcp proxy.

#### Configuration Flags

* `-client-routes-connection-ids` - comma-separated list of Private Link connection IDs to use for routing. When specified, scylla-bench will query the `system.client_routes` table to determine the appropriate endpoints for each node.
* `-client-routes-table` - the table containing node IP/port mapping (default: `system.client_routes`), to be used only for testing purposes when you want to target regular table to emulate scenarios.

#### Example Usage

```bash
# Connect using Private Link with a single connection ID
./build/scylla-bench -workload uniform -mode read -nodes private-endpoint.example.com \
  -client-routes-connection-ids "plcon-abc123" \
  -duration 15m -concurrency 64

# Connect using multiple Private Link connection IDs
./build/scylla-bench -workload uniform -mode mixed -nodes private-endpoint.example.com \
  -client-routes-connection-ids "plcon-abc123,plcon-def456" \
  -duration 30m -concurrency 128

# Use a custom client routes table
./build/scylla-bench -workload uniform -mode read -nodes private-endpoint.example.com \
  -client-routes-connection-ids "plcon-abc123" \
  -client-routes-table "my_keyspace.custom_routes" \
  -duration 15m
```

**Note:** The `-client-routes-connection-ids` flag cannot be used together with `-cloud-config-path`.

### Random value distributions

scylla-bench supports random values for certain command line arguments. The list of these arguments is:
* `-clustering-row-size`

There are three distributions supported:
* `fixed:VALUE`, always generates `VALUE`.
* `uniform:MIN..MAX`, generates a uniformly distributed value in the interval `[MIN, MAX)`.

Example: `-clustering-row-size=uniform:100..1000`

All command line arguments that accept a random distribution, also accept a single number, in which case a Fixed distribution will be used. This ensures backward compatibility.

## Testing with TestContainers

scylla-bench includes support for testing with [TestContainers](https://testcontainers.com/), which allows running tests against a real ScyllaDB instance in a Docker container without requiring a pre-existing installation.

### Setting up TestContainers

To use TestContainers with scylla-bench:

1. Make sure Docker is installed and running on your system
2. Use the `pkg/testutil` package which provides a `ScyllaDBContainer` helper

Example usage:

```go
package mytest

import (
    "context"
    "testing"
    "time"
    
    "github.com/scylladb/scylla-bench/pkg/testutil"
)

func TestWithScyllaDB(t *testing.T) {
    // Create a context with timeout
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
    defer cancel()

    // Start a ScyllaDB container
    container, err := testutil.NewScyllaDBContainer(ctx)
    if err != nil {
        t.Fatalf("Failed to start ScyllaDB container: %v", err)
    }
    defer container.Close(ctx)

    // Use the container's session
    session := container.Session

    // Create keyspace and table
    err = container.CreateKeyspace("test_keyspace", 1)
    if err != nil {
        t.Fatalf("Failed to create keyspace: %v", err)
    }

    err = container.CreateTable("test_keyspace", "test_table")
    if err != nil {
        t.Fatalf("Failed to create table: %v", err)
    }

    // Run your tests...
}
```

### Memory Leak Tests

scylla-bench includes memory leak tests that use TestContainers to verify that resources are properly released, especially under high retry pressure. To run these tests:

```bash
RUN_MEMORY_LEAK_TEST=true go test -v -run TestMemoryLeak
```

### Integration Tests

scylla-bench includes comprehensive integration tests that validate all workload types and modes against a real ScyllaDB instance. These tests ensure that no workload is critically broken and are automatically run on CI/CD.

#### Running Integration Tests Locally

To run the integration tests locally (requires Docker):

```bash
# Run all integration tests
RUN_CONTAINER_TESTS=true go test -v -race -timeout 25m -run "^TestIntegration"

# Run a specific integration test
RUN_CONTAINER_TESTS=true go test -v -race -run TestIntegrationQuickSmoke

# Run integration tests with data validation
RUN_CONTAINER_TESTS=true go test -v -race -run TestIntegrationWithDataValidation
```

#### Integration Test Coverage

The integration test suite covers:

- **Workload types**: Sequential, Uniform, TimeSeries
- **Operation modes**: Write, Read, Mixed (50/50 read/write), Counter Update/Read, Scan
- **Data validation**: Write and read operations with checksum verification
- **Quick smoke tests**: Fast validation of all modes (useful for development)

Each test runs for a short duration (2-10 seconds) to provide fast feedback while ensuring the workload executes without critical errors.

#### CI/CD Integration

Integration tests are automatically run on:
- Pull requests to the `master` branch
- Pushes to the `master` branch
- Manual workflow dispatch

The tests use TestContainers with ScyllaDB 2025.2 and complete in approximately 8-10 minutes.

## Examples

1. Sequential write to populate the database: `scylla-bench -workload sequential -mode write -nodes 127.0.0.1`
2. Read test: `scylla-bench -workload uniform -mode read -concurrency 128 -duration 15m -nodes some_node`
3. Read latency test: `scylla-bench -workload uniform -mode read -duration 15m -concurrency 32 -max-rate 32000 -nodes 192.168.8.4`
4. Counter write test: `scylla-bench -workload uniform -mode counter_update -duration 30m -concurrency 128`
5. Mixed read/write test: `scylla-bench -workload uniform -mode mixed -concurrency 64 -duration 30m -nodes 127.0.0.1`
6. Full table scan test: `scylla-bench -mode scan -timeout 5m -concurrency 1`
7. Write to populate database with non-zero data: `scylla-bench -workload sequential -mode write -nodes 127.0.0.1 -clustering-row-size 16 -validate-data`
8. Read with data verification: `scylla-bench -workload uniform -mode write -nodes 127.0.0.1 -clustering-row-size 16 -validate-data  -duration 10m`
9. Test with increased schema timeout (for clusters with frequent ALTER TABLE operations): `scylla-bench -workload uniform -mode mixed -nodes 127.0.0.1 -duration 30m -metadata-schema-timeout 120s`
