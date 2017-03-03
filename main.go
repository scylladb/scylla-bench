package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sync/atomic"
	"time"

	"github.com/gocql/gocql"
)

var keyspaceName string
var tableName string
var counterTableName string

var concurrency int

var testDuration time.Duration

var partitionCount int
var clusteringRowCount int
var clusteringRowSize int

var rowsPerRequest int

var timeout time.Duration

var stopAll uint32

func PrepareDatabase(session *gocql.Session, replicationFactor int) {
	request := fmt.Sprintf("CREATE KEYSPACE IF NOT EXISTS %s WITH REPLICATION = { 'class' : 'SimpleStrategy', 'replication_factor' : %d }", keyspaceName, replicationFactor)
	err := session.Query(request).Exec()
	if err != nil {
		log.Fatal(err)
	}

	err = session.Query("CREATE TABLE IF NOT EXISTS " + keyspaceName + "." + tableName + " (pk bigint, ck bigint, v blob, PRIMARY KEY(pk, ck)) WITH compression = { }").Exec()
	if err != nil {
		log.Fatal(err)
	}

	err = session.Query("CREATE TABLE IF NOT EXISTS " + keyspaceName + "." + counterTableName + " (pk bigint, ck bigint, c1 counter, c2 counter, c3 counter, c4 counter, c5 counter, PRIMARY KEY(pk, ck)) WITH compression = { }").Exec()
	if err != nil {
		log.Fatal(err)
	}
}

func GetWorkload(name string, threadId int, partitionOffset int) WorkloadGenerator {
	switch name {
	case "sequential":
		pksPerThread := partitionCount / concurrency
		thisOffset := pksPerThread * threadId
		var thisSize int
		if threadId+1 == concurrency {
			thisSize = partitionCount - thisOffset
		} else {
			thisSize = pksPerThread
		}
		return NewSequentialVisitAll(thisOffset+partitionOffset, thisSize, clusteringRowCount)
	case "uniform":
		return NewRandomUniform(threadId, partitionCount, clusteringRowCount)
	default:
		log.Fatal("unknown workload: ", name)
	}
	panic("unreachable")
}

func GetMode(name string) func(session *gocql.Session, workload WorkloadGenerator) Result {
	switch name {
	case "write":
		if rowsPerRequest == 1 {
			return DoWrites
		}
		return DoBatchedWrites
	case "counter_update":
		return DoCounterUpdates
	case "read":
		return DoReads
	default:
		log.Fatal("unknown mode: ", name)
	}
	panic("unreachable")
}

func main() {
	var mode string
	var workload string
	var consistencyLevel string
	var replicationFactor int

	var nodes string
	var clientCompression bool
	var connectionCount int
	var pageSize int

	var partitionOffset int

	flag.StringVar(&mode, "mode", "", "operating mode: write, read")
	flag.StringVar(&workload, "workload", "", "workload: sequential, uniform")
	flag.StringVar(&consistencyLevel, "consistency-level", "quorum", "consistency level")
	flag.IntVar(&replicationFactor, "replication-factor", 1, "replication factor")
	flag.DurationVar(&timeout, "timeout", 5*time.Second, "request timeout")

	flag.StringVar(&nodes, "nodes", "127.0.0.1", "nodes")
	flag.BoolVar(&clientCompression, "client-compression", true, "use compression for client-coordinator communication")
	flag.IntVar(&concurrency, "concurrency", 16, "number of used goroutines")
	flag.IntVar(&connectionCount, "connection-count", 4, "number of connections")
	flag.IntVar(&pageSize, "page-size", 1000, "page size")

	flag.IntVar(&partitionCount, "partition-count", 10000, "number of partitions")
	flag.IntVar(&clusteringRowCount, "clustering-row-count", 100, "number of clustering rows in a partition")
	flag.IntVar(&clusteringRowSize, "clustering-row-size", 4, "size of a single clustering row")

	flag.IntVar(&rowsPerRequest, "rows-per-request", 1, "clustering rows per single request")
	flag.DurationVar(&testDuration, "duration", 0, "duration of the test in seconds (0 for unlimited)")

	flag.IntVar(&partitionOffset, "partition-offset", 0, "start of the partition range (only for sequential workload)")

	flag.StringVar(&keyspaceName, "keyspace", "scylla_bench", "keyspace to use")
	flag.StringVar(&tableName, "table", "test", "table to use")
	flag.Parse()
	counterTableName = "test_counters"

	flag.Usage = func() {
		fmt.Fprintf(os.Stdout, "Usage:\n%s [options]\n\n", os.Args[0])
		flag.PrintDefaults()
	}

	if workload == "" {
		log.Fatal("workload type needs to be specified")
	}

	if mode == "" {
		log.Fatal("test mode needs to be specified")
	}

	if workload == "uniform" && testDuration == 0 {
		log.Fatal("uniform workload requires limited test duration")
	}

	if partitionOffset != 0 && workload != "sequential" {
		log.Fatal("partition-offset has a meaning only in sequential workloads")
	}

	cluster := gocql.NewCluster(nodes)
	cluster.NumConns = connectionCount
	cluster.PageSize = pageSize
	cluster.Timeout = timeout
	switch consistencyLevel {
	case "any":
		cluster.Consistency = gocql.Any
	case "one":
		cluster.Consistency = gocql.One
	case "two":
		cluster.Consistency = gocql.Two
	case "three":
		cluster.Consistency = gocql.Three
	case "quorum":
		cluster.Consistency = gocql.Quorum
	case "all":
		cluster.Consistency = gocql.All
	case "local_quorum":
		cluster.Consistency = gocql.LocalQuorum
	case "each_quorum":
		cluster.Consistency = gocql.EachQuorum
	case "local_one":
		cluster.Consistency = gocql.LocalOne
	default:
		log.Fatal("unknown consistency level: ", consistencyLevel)
	}
	if clientCompression {
		cluster.Compressor = &gocql.SnappyCompressor{}
	}

	session, err := cluster.CreateSession()
	if err != nil {
		log.Fatal(err)
	}
	defer session.Close()

	PrepareDatabase(session, replicationFactor)

	interrupted := make(chan os.Signal, 1)
	signal.Notify(interrupted, os.Interrupt)
	go func() {
		<-interrupted
		fmt.Println("\ninterrupted")
		atomic.StoreUint32(&stopAll, 1)
	}()

	if testDuration > 0 {
		go func() {
			time.Sleep(testDuration)
			atomic.StoreUint32(&stopAll, 1)
		}()
	}

	result := RunConcurrently(func(i int) Result {
		return GetMode(mode)(session, GetWorkload(workload, i, partitionOffset))
	})

	fmt.Println("Configuration")
	fmt.Println("Mode:\t\t\t", mode)
	fmt.Println("Workload:\t\t", workload)
	fmt.Println("Timeout:\t\t", timeout)
	fmt.Println("Consistency level:\t", consistencyLevel)
	fmt.Println("Partition count:\t", partitionCount)
	if workload == "sequential" && partitionOffset != 0 {
		fmt.Println("Partition offset:\t", partitionOffset)
	}
	fmt.Println("Clustering rows:\t", clusteringRowCount)
	fmt.Println("Clustering row size:\t", clusteringRowSize)
	fmt.Println("Rows per request:\t", rowsPerRequest)
	fmt.Println("Page size:\t\t", pageSize)
	fmt.Println("Concurrency:\t\t", concurrency)
	fmt.Println("Connections:\t\t", connectionCount)
	fmt.Println("Client compression:\t", clientCompression)

	fmt.Println("\nResults")
	fmt.Println("Time (avg):\t", result.Time)
	fmt.Println("Total ops:\t", result.Operations)
	fmt.Println("Total rows:\t", result.ClusteringRows)
	fmt.Println("Operations/s:\t", result.OperationsPerSecond)
	fmt.Println("Rows/s:\t\t", result.ClusteringRowsPerSecond)
	fmt.Println("Latency:\n  max:\t\t", time.Duration(result.Latency.Max()),
		"\n  99.9th:\t", time.Duration(result.Latency.ValueAtQuantile(99.9)),
		"\n  99th:\t\t", time.Duration(result.Latency.ValueAtQuantile(99)),
		"\n  95th:\t\t", time.Duration(result.Latency.ValueAtQuantile(95)),
		"\n  90th:\t\t", time.Duration(result.Latency.ValueAtQuantile(90)),
		"\n  mean:\t\t", time.Duration(result.Latency.Mean()))
}
