package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gocql/gocql"
	"github.com/hailocab/go-hostpool"
)

var (
	keyspaceName     string
	tableName        string
	counterTableName string

	mode        string
	concurrency int
	maximumRate int

	testDuration time.Duration

	partitionCount     int64
	clusteringRowCount int64
	clusteringRowSize  int64

	rowsPerRequest    int
	provideUpperBound bool
	inRestriction     bool
	noLowerBound      bool

	ops         string
	profileFile string

	rangeCount int

	timeout    time.Duration
	iterations uint

	startTime time.Time

	stopAll uint32

	measureLatency bool
	validateData   bool
)

const (
	withLatencyLineFmt    = "\n%-15v  %15v  %7v  %7v  %-15v  %-15v  %-15v  %-15v  %-15v  %-15v  %-15v  %v"
	withoutLatencyLineFmt = "\n%-15v  %15v  %7v  %7v"
)

func Query(session *gocql.Session, request string) {
	err := session.Query(request).Exec()
	if err != nil {
		log.Fatal(err)
	}
}

func PrepareDatabase(session *gocql.Session, replicationFactor int) {
	Query(session, fmt.Sprintf("CREATE KEYSPACE IF NOT EXISTS %s WITH REPLICATION = { 'class' : 'SimpleStrategy', 'replication_factor' : %d }", keyspaceName, replicationFactor))

	Query(session, "CREATE TABLE IF NOT EXISTS "+keyspaceName+"."+tableName+" (pk bigint, ck bigint, v blob, PRIMARY KEY(pk, ck)) WITH compression = { }")

	Query(session, "CREATE TABLE IF NOT EXISTS "+keyspaceName+"."+counterTableName+
		" (pk bigint, ck bigint, c1 counter, c2 counter, c3 counter, c4 counter, c5 counter, PRIMARY KEY(pk, ck)) WITH compression = { }")

	if validateData {
		switch mode {
		case "write":
			Query(session, "TRUNCATE TABLE "+keyspaceName+"."+tableName)
		case "counter_update":
			Query(session, "TRUNCATE TABLE "+keyspaceName+"."+counterTableName)
		}
	}
}

func GetWorkload(name string, threadId int, partitionOffset int64, mode string, writeRate int64, distribution string) WorkloadGenerator {
	switch name {
	case "sequential":
		pksPerThread := partitionCount / int64(concurrency)
		thisOffset := pksPerThread * int64(threadId)
		var thisSize int64
		if threadId+1 == concurrency {
			thisSize = partitionCount - thisOffset
		} else {
			thisSize = pksPerThread
		}
		return NewSequentialVisitAll(thisOffset+partitionOffset, thisSize, clusteringRowCount)
	case "uniform":
		return NewRandomUniform(threadId, partitionCount, clusteringRowCount)
	case "timeseries":
		if mode == "read" {
			return NewTimeSeriesReader(threadId, concurrency, partitionCount, clusteringRowCount, writeRate, distribution, startTime)
		} else if mode == "write" {
			return NewTimeSeriesWriter(threadId, concurrency, partitionCount, clusteringRowCount, startTime, int64(maximumRate/concurrency))
		} else {
			log.Fatal("time series workload supports only write and read modes")
		}
	case "scan":
		rangesPerThread := rangeCount / concurrency
		thisOffset := rangesPerThread * threadId
		var thisCount int
		if threadId+1 == concurrency {
			thisCount = rangeCount - thisOffset
		} else {
			thisCount = rangesPerThread
		}
		return NewRangeScan(rangeCount, thisOffset, thisCount)
	default:
		if mode == "user" {
			return nil
		}
		log.Fatal("unknown workload: ", name)
	}
	panic("unreachable")
}

var user = NewUserMode()

func GetMode(name string) func(session *gocql.Session, resultChannel chan Result, workload WorkloadGenerator, rateLimiter RateLimiter) {
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
	case "counter_read":
		return DoCounterReads
	case "scan":
		return DoScanTable
	case "user":
		return user.Do
	default:
		log.Fatal("unknown mode: ", name)
	}
	panic("unreachable")
}

func PrintPartialResult(result *MergedResult) {
	latencyError := ""
	if errorRecordingLatency {
		latencyError = "latency measurement error"
	}
	if measureLatency {
		fmt.Printf(withLatencyLineFmt, result.Time, result.Operations, result.ClusteringRows, result.Errors,
			time.Duration(result.Latency.Max()), time.Duration(result.Latency.ValueAtQuantile(99.9)), time.Duration(result.Latency.ValueAtQuantile(99)),
			time.Duration(result.Latency.ValueAtQuantile(95)), time.Duration(result.Latency.ValueAtQuantile(90)),
			time.Duration(result.Latency.ValueAtQuantile(50)), time.Duration(result.Latency.Mean()),
			latencyError)
	} else {
		fmt.Printf(withoutLatencyLineFmt, result.Time, result.Operations, result.ClusteringRows, result.Errors)
	}
}

func toInt(value bool) int {
	if value {
		return 1
	} else {
		return 0
	}
}

func main() {
	var (
		workload          string
		consistencyLevel  string
		replicationFactor int

		nodes             string
		clientCompression bool
		connectionCount   int
		pageSize          int

		partitionOffset int64

		writeRate    int64
		distribution string

		hostSelectionPolicy string
	)

	flag.StringVar(&mode, "mode", "", "operating mode: write, read, counter_update, counter_read, scan")
	flag.StringVar(&workload, "workload", "", "workload: sequential, uniform, timeseries")
	flag.StringVar(&consistencyLevel, "consistency-level", "quorum", "consistency level")
	flag.IntVar(&replicationFactor, "replication-factor", 1, "replication factor")
	flag.DurationVar(&timeout, "timeout", 5*time.Second, "request timeout")

	flag.StringVar(&nodes, "nodes", "127.0.0.1", "nodes")
	flag.BoolVar(&clientCompression, "client-compression", true, "use compression for client-coordinator communication")
	flag.IntVar(&concurrency, "concurrency", runtime.NumCPU(), "number of used goroutines")
	flag.IntVar(&connectionCount, "connection-count", 4, "number of connections")
	flag.IntVar(&maximumRate, "max-rate", 0, "the maximum rate of outbound requests in op/s (0 for unlimited)")
	flag.IntVar(&pageSize, "page-size", 1000, "page size")

	flag.Int64Var(&partitionCount, "partition-count", 10000, "number of partitions")
	flag.Int64Var(&clusteringRowCount, "clustering-row-count", 100, "number of clustering rows in a partition")
	flag.Int64Var(&clusteringRowSize, "clustering-row-size", 4, "size of a single clustering row")

	flag.IntVar(&rowsPerRequest, "rows-per-request", 1, "clustering rows per single request")
	flag.BoolVar(&provideUpperBound, "provide-upper-bound", false, "whether read requests should provide an upper bound")
	flag.BoolVar(&inRestriction, "in-restriction", false, "use IN restriction in read requests")
	flag.BoolVar(&noLowerBound, "no-lower-bound", false, "do not provide lower bound in read requests")
	flag.IntVar(&rangeCount, "range-count", 1, "number of ranges to split the token space into (relevant only for scan mode)")

	flag.StringVar(&profileFile, "profile", "", "YAML profile to load in user mode")
	flag.StringVar(&ops, "ops", "", "Comma-separated list of operations to execite in user mode")

	flag.DurationVar(&testDuration, "duration", 0, "duration of the test in seconds (0 for unlimited)")
	flag.UintVar(&iterations, "iterations", 1, "number of iterations to run (0 for unlimited, relevant only for workloads that have a defined number of ops to execute)")
	flag.UintVar(&iterations, "n", 1, "number of iterations to run (short option)")

	flag.Int64Var(&partitionOffset, "partition-offset", 0, "start of the partition range (only for sequential workload)")

	flag.BoolVar(&measureLatency, "measure-latency", true, "measure request latency")
	flag.BoolVar(&validateData, "validate-data", false, "write meaningful data and validate while reading")

	var startTimestamp int64
	flag.Int64Var(&writeRate, "write-rate", 0, "rate of writes (relevant only for time series reads)")
	flag.Int64Var(&startTimestamp, "start-timestamp", 0, "start timestamp of the write load (relevant only for time series reads)")
	flag.StringVar(&distribution, "distribution", "uniform", "distribution of keys (relevant only for time series reads): uniform, hnormal")

	flag.StringVar(&keyspaceName, "keyspace", "scylla_bench", "keyspace to use")
	flag.StringVar(&tableName, "table", "test", "table to use")

	flag.StringVar(&hostSelectionPolicy, "host-selection-policy", "token-aware", "set the driver host selection policy (round-robin,token-aware,dc-aware),default 'token-aware'")
	flag.Parse()
	counterTableName = "test_counters"

	flag.Usage = func() {
		fmt.Fprintf(os.Stdout, "Usage:\n%s [options]\n\n", os.Args[0])
		flag.PrintDefaults()
	}

	if mode == "" {
		log.Fatal("test mode needs to be specified")
	}

	if mode == "scan" {
		if workload != "" {
			log.Fatal("workload type cannot be scpecified for scan mode")
		}
		workload = "scan"
		if concurrency > rangeCount {
			concurrency = rangeCount
			log.Printf("adjusting concurrency to the highest useful value of %v", concurrency)
		}
	} else {
		if workload == "" && mode != "user" {
			log.Fatal("workload type needs to be specified")
		}
	}

	if workload == "uniform" && testDuration == 0 {
		log.Fatal("uniform workload requires limited test duration")
	}

	if iterations > 1 && workload != "sequential" && workload != "scan" && mode != "user" {
		log.Fatal("iterations only supported for the sequential and scan workload")
	}
	if iterations == 0 && mode == "user" {
		log.Fatal("unlimited iterations are not supported for user mode")
	}

	if partitionOffset != 0 && workload != "sequential" {
		log.Fatal("partition-offset has a meaning only in sequential workloads")
	}

	readModeTweaks := toInt(inRestriction) + toInt(provideUpperBound) + toInt(noLowerBound)
	if mode != "read" && mode != "counter_read" {
		if readModeTweaks != 0 {
			log.Fatal("in-restriction, no-lower-bound and provide-uppder-bound flags make sense only in read mode")
		}
	} else if readModeTweaks > 1 {
		log.Fatal("in-restriction, no-lower-bound and provide-uppder-bound flags are mutually exclusive")
	}

	if workload == "timeseries" && mode == "read" && writeRate == 0 {
		log.Fatal("write rate must be provided for time series reads loads")
	}
	if workload == "timeseries" && mode == "read" && startTimestamp == 0 {
		log.Fatal("start timestamp must be provided for time series reads loads")
	}
	if workload == "timeseries" && mode == "write" && int64(concurrency) > partitionCount {
		log.Fatal("time series writes require concurrency less than or equal partition count")
	}
	if workload == "timeseries" && mode == "write" && maximumRate == 0 {
		log.Fatal("max-rate must be provided for time series write loads")
	}

	cluster := gocql.NewCluster(strings.Split(nodes, ",")...)
	cluster.NumConns = connectionCount
	cluster.PageSize = pageSize
	cluster.Timeout = timeout
	policy, err := newHostSelectionPolicy(hostSelectionPolicy, strings.Split(nodes, ","))
	if err != nil {
		log.Fatal(err)
	}
	cluster.PoolConfig.HostSelectionPolicy = policy

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

	if mode != "user" {
		PrepareDatabase(session, replicationFactor)
	}

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

	if startTimestamp != 0 {
		startTime = time.Unix(0, startTimestamp)
	} else {
		startTime = time.Now()
	}

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
	if mode == "read" {
		fmt.Println("Provide upper bound:\t", provideUpperBound)
		fmt.Println("IN queries:\t\t", inRestriction)
		fmt.Println("No lower bound:\t\t", noLowerBound)
	}
	fmt.Println("Page size:\t\t", pageSize)
	fmt.Println("Concurrency:\t\t", concurrency)
	fmt.Println("Connections:\t\t", connectionCount)
	if maximumRate > 0 {
		fmt.Println("Maximum rate:\t\t", maximumRate, "op/s")
	} else {
		fmt.Println("Maximum rate:\t\t unlimited")
	}
	fmt.Println("Client compression:\t", clientCompression)
	if workload == "timeseries" {
		fmt.Println("Start timestamp:\t", startTime.UnixNano())
		fmt.Println("Write rate:\t\t", int64(maximumRate)/partitionCount)
	}

	if measureLatency {
		fmt.Printf(withLatencyLineFmt, "time", "operations/s", "rows/s", "errors", "max", "99.9th", "99th", "95th", "90th", "median", "mean", "")
	} else {
		fmt.Printf(withoutLatencyLineFmt, "time", "operations/s", "rows/s", "errors")
	}

	result := RunConcurrently(maximumRate, func(i int, resultChannel chan Result, rateLimiter RateLimiter) {
		GetMode(mode)(session, resultChannel, GetWorkload(workload, i, partitionOffset, mode, writeRate, distribution), rateLimiter)
	})

	// When executing user mode ended with a fatal error (e.g. missing or
	// ill-formed profile file) op count is 0 - do not print results.
	if mode == "user" && result.Operations == 0 {
		return
	}

	fmt.Println("\nResults")
	fmt.Println("Time (avg):\t", result.Time)
	fmt.Println("Total ops:\t", result.Operations)
	fmt.Println("Total rows:\t", result.ClusteringRows)
	if result.Errors != 0 {
		fmt.Println("Total errors:\t", result.Errors)
	}
	fmt.Println("Operations/s:\t", result.OperationsPerSecond)
	fmt.Println("Rows/s:\t\t", result.ClusteringRowsPerSecond)
	if errorRecordingLatency {
		fmt.Println("Latency measurements may be inaccurate")
	}
	if measureLatency {
		fmt.Println("Latency:\n  max:\t\t", time.Duration(result.Latency.Max()),
			"\n  99.9th:\t", time.Duration(result.Latency.ValueAtQuantile(99.9)),
			"\n  99th:\t\t", time.Duration(result.Latency.ValueAtQuantile(99)),
			"\n  95th:\t\t", time.Duration(result.Latency.ValueAtQuantile(95)),
			"\n  90th:\t\t", time.Duration(result.Latency.ValueAtQuantile(90)),
			"\n  median:\t", time.Duration(result.Latency.ValueAtQuantile(50)),
			"\n  mean:\t\t", time.Duration(result.Latency.Mean()))
	}
	if mode == "user" {
		fmt.Print(user.Summary())
	}
}

func newHostSelectionPolicy(policy string, hosts []string) (gocql.HostSelectionPolicy, error) {
	switch policy {
	case "round-robin":
		return gocql.RoundRobinHostPolicy(), nil
	case "host-pool":
		return gocql.HostPoolHostPolicy(hostpool.New(hosts)), nil
	case "token-aware":
		return gocql.TokenAwareHostPolicy(gocql.RoundRobinHostPolicy()), nil
	default:
		return nil, fmt.Errorf("unknown host selection policy, %s", policy)
	}
}
