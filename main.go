package main

import (
	"crypto/tls"
	"flag"
	"fmt"
	"github.com/scylladb/scylla-bench/pkg"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/scylladb/scylla-bench/pkg/results"

	"github.com/gocql/gocql"
	"github.com/hailocab/go-hostpool"
	"github.com/pkg/errors"
	"github.com/scylladb/scylla-bench/pkg/rate_limiters"
	. "github.com/scylladb/scylla-bench/pkg/workloads"
	"github.com/scylladb/scylla-bench/random"
)

type DistributionValue struct {
	Dist *random.Distribution
}

func MakeDistributionValue(dist *random.Distribution, defaultDist random.Distribution) *DistributionValue {
	*dist = defaultDist
	return &DistributionValue{dist}
}

func (v DistributionValue) String() string {
	if v.Dist == nil {
		return ""
	}
	return fmt.Sprintf("%s", *v.Dist)
}

func (v *DistributionValue) Set(s string) error {
	i, err := strconv.ParseInt(s, 10, 64)
	if err == nil {
		if i < 1 {
			return errors.New("value for fixed distribution is invalid: value has to be positive")
		}
		*v.Dist = random.Fixed{Value: i}
		return nil
	}

	dist, err := random.ParseDistribution(s)
	if err == nil {
		*v.Dist = dist
		return nil
	} else {
		return err
	}
}

var arguments = command_line.CommandLineArguments{}
var (
	startTime time.Time
	stopAll uint32
	counterTableName = "test_counters"
)


func Query(session *gocql.Session, request string) {
	err := session.Query(request).Exec()
	if err != nil {
		log.Fatal(err)
	}
}

func PrepareDatabase(session *gocql.Session, replicationFactor int) {
	Query(session, fmt.Sprintf("CREATE KEYSPACE IF NOT EXISTS %s WITH REPLICATION = { 'class' : 'SimpleStrategy', 'replication_factor' : %d }", arguments.KeyspaceName, replicationFactor))

	Query(session, "CREATE TABLE IF NOT EXISTS "+arguments.KeyspaceName+"."+arguments.TableName+" (pk bigint, ck bigint, v blob, PRIMARY KEY(pk, ck)) WITH compression = { }")

	Query(session, "CREATE TABLE IF NOT EXISTS "+arguments.KeyspaceName+"."+counterTableName+
		" (pk bigint, ck bigint, c1 counter, c2 counter, c3 counter, c4 counter, c5 counter, PRIMARY KEY(pk, ck)) WITH compression = { }")

	if arguments.ValidateData {
		switch arguments.Mode {
		case "write":
			Query(session, "TRUNCATE TABLE "+arguments.KeyspaceName+"."+arguments.TableName)
		case "counter_update":
			Query(session, "TRUNCATE TABLE "+arguments.KeyspaceName+"."+counterTableName)
		}
	}
}

func GetWorkload(name string, threadId int, partitionOffset int64, mode string, writeRate int64, distribution string) WorkloadGenerator {
	switch name {
	case "sequential":
		pksPerThread := arguments.PartitionCount / int64(arguments.Concurrency)
		thisOffset := pksPerThread * int64(threadId)
		var thisSize int64
		if threadId+1 == arguments.Concurrency {
			thisSize = arguments.PartitionCount - thisOffset
		} else {
			thisSize = pksPerThread
		}
		return NewSequentialVisitAll(thisOffset+partitionOffset, thisSize, arguments.ClusteringRowCount)
	case "uniform":
		return NewRandomUniform(threadId, arguments.PartitionCount, arguments.ClusteringRowCount)
	case "timeseries":
		switch mode {
		case "read":
			return NewTimeSeriesReader(threadId, arguments.Concurrency, arguments.PartitionCount, arguments.ClusteringRowCount, writeRate, distribution, startTime)
		case "write":
			return NewTimeSeriesWriter(threadId, arguments.Concurrency, arguments.PartitionCount, arguments.ClusteringRowCount, startTime, int64(arguments.MaximumRate/arguments.Concurrency))
		default:
			log.Fatal("time series workload supports only write and read modes")
		}
	case "scan":
		rangesPerThread := arguments.RangeCount / arguments.Concurrency
		thisOffset := rangesPerThread * threadId
		var thisCount int
		if threadId+1 == arguments.Concurrency {
			thisCount = arguments.RangeCount - thisOffset
		} else {
			thisCount = rangesPerThread
		}
		return NewRangeScan(arguments.RangeCount, thisOffset, thisCount)
	default:
		log.Fatal("unknown workload: ", name)
	}
	panic("unreachable")
}

func GetMode(name string) func(session *gocql.Session, testResult *results.TestThreadResult, workload WorkloadGenerator, rateLimiter rate_limiters.RateLimiter) {
	switch name {
	case "write":
		if arguments.RowsPerRequest == 1 {
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
	default:
		log.Fatal("unknown mode: ", name)
	}
	panic("unreachable")
}

func toInt(value bool) int {
	if value {
		return 1
	} else {
		return 0
	}
}

func main() {
	flag.StringVar(&arguments.Mode, "mode", "", "operating mode: write, read, counter_update, counter_read, scan")
	flag.StringVar(&arguments.Workload, "workload", "", "workload: sequential, uniform, timeseries")
	flag.StringVar(&arguments.ConsistencyLevel, "consistency-level", "quorum", "consistency level")
	flag.IntVar(&arguments.ReplicationFactor, "replication-factor", 1, "replication factor")
	flag.DurationVar(&arguments.Timeout, "timeout", 5*time.Second, "request timeout")

	flag.StringVar(&arguments.Nodes, "nodes", "127.0.0.1", "nodes")
	flag.BoolVar(&arguments.ClientCompression, "client-compression", true, "use compression for client-coordinator communication")
	flag.IntVar(&arguments.Concurrency, "concurrency", 16, "number of used goroutines")
	flag.IntVar(&arguments.ConnectionCount, "connection-count", 4, "number of connections")
	flag.IntVar(&arguments.MaximumRate, "max-rate", 0, "the maximum rate of outbound requests in op/s (0 for unlimited)")
	flag.IntVar(&arguments.PageSize, "page-size", 1000, "page size")

	flag.Int64Var(&arguments.PartitionCount, "partition-count", 10000, "number of partitions")
	flag.Int64Var(&arguments.ClusteringRowCount, "clustering-row-count", 100, "number of clustering rows in a partition")
	flag.Var(MakeDistributionValue(&arguments.ClusteringRowSizeDist, random.Fixed{Value: 4}), "clustering-row-size", "size of a single clustering row, can use random values")

	flag.IntVar(&arguments.RowsPerRequest, "rows-per-request", 1, "clustering rows per single request")
	flag.BoolVar(&arguments.ProvideUpperBound, "provide-upper-bound", false, "whether read requests should provide an upper bound")
	flag.BoolVar(&arguments.InRestriction, "in-restriction", false, "use IN restriction in read requests")
	flag.BoolVar(&arguments.NoLowerBound, "no-lower-bound", false, "do not provide lower bound in read requests")
	flag.IntVar(&arguments.RangeCount, "range-count", 1, "number of ranges to split the token space into (relevant only for scan mode)")

	flag.DurationVar(&arguments.TestDuration, "duration", 0, "duration of the test in seconds (0 for unlimited)")
	flag.UintVar(&arguments.Iterations, "iterations", 1, "number of iterations to run (0 for unlimited, relevant only for workloads that have a defined number of ops to execute)")

	flag.Int64Var(&arguments.PartitionOffset, "partition-offset", 0, "start of the partition range (only for sequential workload)")

	flag.BoolVar(&arguments.MeasureLatency, "measure-latency", true, "measure request latency")
	flag.BoolVar(&arguments.ValidateData, "validate-data", false, "write meaningful data and validate while reading")

	var startTimestamp int64
	flag.Int64Var(&arguments.WriteRate, "write-rate", 0, "rate of writes (relevant only for time series reads)")
	flag.Int64Var(&startTimestamp, "start-timestamp", 0, "start timestamp of the write load (relevant only for time series reads)")
	flag.StringVar(&arguments.Distribution, "distribution", "uniform", "distribution of keys (relevant only for time series reads): uniform, hnormal")

	flag.StringVar(&arguments.LatencyType, "latency-type", "raw", "type of the latency to print during the run: raw, fixed-coordinated-omission")

	flag.StringVar(&arguments.KeyspaceName, "keyspace", "scylla_bench", "keyspace to use")
	flag.StringVar(&arguments.TableName, "table", "test", "table to use")
	flag.StringVar(&arguments.Username, "username", "", "cql username for authentication")
	flag.StringVar(&arguments.Password, "password", "", "cql password for authentication")

	flag.BoolVar(&arguments.TLSEncryption, "tls", false, "use TLS encryption")
	flag.StringVar(&arguments.ServerName, "tls-server-name", "", "TLS server hostname")
	flag.BoolVar(&arguments.HostVerification, "tls-host-verification", false, "verify server certificate")
	flag.StringVar(&arguments.CaCertFile, "tls-ca-cert-file", "", "path to CA certificate file, needed to enable encryption")
	flag.StringVar(&arguments.ClientCertFile, "tls-client-cert-file", "", "path to client certificate file, needed to enable client certificate authentication")
	flag.StringVar(&arguments.ClientKeyFile, "tls-client-key-file", "", "path to client key file, needed to enable client certificate authentication")

	flag.StringVar(&arguments.HostSelectionPolicy, "host-selection-policy", "token-aware", "set the driver host selection policy (round-robin,token-aware,dc-aware),default 'token-aware'")
	flag.IntVar(&arguments.MaxErrorsAtRow, "error-at-row-limit", 0, "set limit of errors caught by one thread at row after which workflow will be terminated and error reported. Set it to 0 if you want to haven no limit")

	flag.Parse()

	flag.Usage = func() {
		fmt.Fprintf(os.Stdout, "Usage:\n%s [options]\n\n", os.Args[0])
		flag.PrintDefaults()
	}

	if arguments.Mode == "" {
		log.Fatal("test arguments.Mode needs to be specified")
	}

	if arguments.Mode == "scan" {
		if arguments.Workload != "" {
			log.Fatal("workload type cannot be scpecified for scan mode")
		}
		arguments.Workload = "scan"
		if arguments.Concurrency > arguments.RangeCount {
			arguments.Concurrency = arguments.RangeCount
			log.Printf("adjusting arguments.Concurrency to the highest useful value of %v", arguments.Concurrency)
		}
	} else if arguments.Workload == "" {
		log.Fatal("workload type needs to be specified")
	}

	if arguments.Workload == "uniform" && arguments.TestDuration == 0 {
		log.Fatal("uniform workload requires limited test duration")
	}

	if arguments.Iterations > 1 && arguments.Workload != "sequential" && arguments.Workload != "scan" {
		log.Fatal("iterations only supported for the sequential and scan workload")
	}

	if arguments.PartitionOffset != 0 && arguments.Workload != "sequential" {
		log.Fatal("partition-offset has a meaning only in sequential workloads")
	}

	readModeTweaks := toInt(arguments.InRestriction) + toInt(arguments.ProvideUpperBound) + toInt(arguments.NoLowerBound)
	if arguments.Mode != "read" && arguments.Mode != "counter_read" {
		if readModeTweaks != 0 {
			log.Fatal("in-restriction, no-lower-bound and provide-uppder-bound flags make sense only in read mode")
		}
	} else if readModeTweaks > 1 {
		log.Fatal("in-restriction, no-lower-bound and provide-uppder-bound flags are mutually exclusive")
	}

	if arguments.Workload == "timeseries" && arguments.Mode == "read" && arguments.WriteRate == 0 {
		log.Fatal("write rate must be provided for time series reads loads")
	}
	if arguments.Workload == "timeseries" && arguments.Mode == "read" && startTimestamp == 0 {
		log.Fatal("start timestamp must be provided for time series reads loads")
	}
	if arguments.Workload == "timeseries" && arguments.Mode == "write" && int64(arguments.Concurrency) > arguments.PartitionCount {
		log.Fatal("time series writes require arguments.Concurrency less than or equal partition count")
	}
	if arguments.Workload == "timeseries" && arguments.Mode == "write" && arguments.MaximumRate == 0 {
		log.Fatal("max-rate must be provided for time series write loads")
	}

	if arguments.Timeout != 0 {
		arguments.ErrorToTimeoutCutoffTime = arguments.Timeout / 5
	} else {
		arguments.ErrorToTimeoutCutoffTime = time.Second
	}

	if err := results.ValidateGlobalLatencyType(arguments.LatencyType); err != nil {
		log.Fatal(errors.Wrap(err, "Bad value for latency-type"))
	}

	cluster := gocql.NewCluster(strings.Split(arguments.Nodes, ",")...)
	cluster.NumConns = arguments.ConnectionCount
	cluster.PageSize = arguments.PageSize
	cluster.Timeout = arguments.Timeout
	policy, err := newHostSelectionPolicy(arguments.HostSelectionPolicy, strings.Split(arguments.Nodes, ","))
	if err != nil {
		log.Fatal(err)
	}
	cluster.PoolConfig.HostSelectionPolicy = policy

	switch arguments.ConsistencyLevel {
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
		log.Fatal("unknown consistency level: ", arguments.ConsistencyLevel)
	}
	if arguments.ClientCompression {
		cluster.Compressor = &gocql.SnappyCompressor{}
	}

	if arguments.Username != "" && arguments.Password != "" {
		cluster.Authenticator = gocql.PasswordAuthenticator{
			Username: arguments.Username,
			Password: arguments.Password,
		}
	}

	if arguments.TLSEncryption {
		sslOpts := &gocql.SslOptions{
			Config: &tls.Config{
				ServerName: arguments.ServerName,
			},
			EnableHostVerification: arguments.HostVerification,
		}

		if arguments.CaCertFile != "" {
			if _, err := os.Stat(arguments.CaCertFile); err != nil {
				log.Fatal(err)
			}
			sslOpts.CaPath = arguments.CaCertFile
		}

		if arguments.ClientKeyFile != "" {
			if _, err := os.Stat(arguments.ClientKeyFile); err != nil {
				log.Fatal(err)
			}
			sslOpts.KeyPath = arguments.ClientKeyFile
		}

		if arguments.ClientCertFile != "" {
			if _, err := os.Stat(arguments.ClientCertFile); err != nil {
				log.Fatal(err)
			}
			sslOpts.CertPath = arguments.ClientCertFile
		}

		if arguments.ClientKeyFile != "" && arguments.ClientCertFile == "" {
			log.Fatal("tls-client-cert-file is required when tls-client-key-file is provided")
		}
		if arguments.ClientCertFile != "" && arguments.ClientKeyFile == "" {
			log.Fatal("tls-client-key-file is required when tls-client-cert-file is provided")
		}

		if arguments.HostVerification {
			if arguments.ServerName == "" {
				log.Fatal("tls-server-name is required when tls-host-verification is enabled")
			}
		}

		cluster.SslOpts = sslOpts
	}

	session, err := cluster.CreateSession()
	if err != nil {
		log.Fatal(err)
	}
	defer session.Close()

	interrupted := make(chan os.Signal, 1)
	signal.Notify(interrupted, os.Interrupt)
	go func() {
		<-interrupted
		fmt.Println("\ninterrupted")
		atomic.StoreUint32(&stopAll, 1)

		<-interrupted
		fmt.Println("\nkilled")
		os.Exit(1)
	}()

	if arguments.TestDuration > 0 {
		go func() {
			time.Sleep(arguments.TestDuration)
			atomic.StoreUint32(&stopAll, 1)
		}()
	}

	if startTimestamp != 0 {
		startTime = time.Unix(0, startTimestamp)
	} else {
		startTime = time.Now()
	}

	fmt.Println("Configuration")
	fmt.Println("Keyspace:\t\t", arguments.KeyspaceName)
	fmt.Println("Tablename:\t\t", arguments.TableName)
	fmt.Println("Mode:\t\t\t", arguments.Mode)
	fmt.Println("Workload:\t\t", arguments.Workload)
	fmt.Println("Timeout:\t\t", arguments.Timeout)
	fmt.Println("Consistency level:\t", arguments.ConsistencyLevel)
	fmt.Println("Replication factor:\t", arguments.ReplicationFactor)
	fmt.Println("Partition count:\t", arguments.PartitionCount)
	if arguments.Workload == "sequential" && arguments.PartitionOffset != 0 {
		fmt.Println("Partition offset:\t", arguments.PartitionOffset)
	}
	fmt.Println("Clustering rows:\t", arguments.ClusteringRowCount)
	fmt.Println("Clustering row size:\t", arguments.ClusteringRowSizeDist)
	fmt.Println("Rows per request:\t", arguments.RowsPerRequest)
	if arguments.Mode == "read" {
		fmt.Println("Provide upper bound:\t", arguments.ProvideUpperBound)
		fmt.Println("IN queries:\t\t", arguments.InRestriction)
		fmt.Println("No lower bound:\t\t", arguments.NoLowerBound)
	}
	fmt.Println("Page size:\t\t", arguments.PageSize)
	fmt.Println("Concurrency:\t\t", arguments.Concurrency)
	fmt.Println("Connections:\t\t", arguments.ConnectionCount)
	if arguments.MaximumRate > 0 {
		fmt.Println("Maximum rate:\t\t", arguments.MaximumRate, "op/s")
	} else {
		fmt.Println("Maximum rate:\t\t unlimited")
	}
	fmt.Println("Client compression:\t", arguments.ClientCompression)
	if arguments.Workload == "timeseries" {
		fmt.Println("Start timestamp:\t", startTime.UnixNano())
		fmt.Println("Write rate:\t\t", int64(arguments.MaximumRate)/arguments.PartitionCount)
	}

	PrepareDatabase(session, arguments.ReplicationFactor)

	setResultsConfiguration()
	testResult := RunConcurrently(arguments.MaximumRate, func(i int, testResult *results.TestThreadResult, rateLimiter rate_limiters.RateLimiter) {
		GetMode(arguments.Mode)(session, testResult, GetWorkload(arguments.Workload, i, arguments.PartitionOffset, arguments.Mode, arguments.WriteRate, arguments.Distribution), rateLimiter)
	})

	testResult.GetTotalResults()
	testResult.PrintTotalResults()
	os.Exit(testResult.GetFinalStatus())
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

func setResultsConfiguration() {
	results.SetGlobalHistogramConfiguration(
		time.Microsecond.Nanoseconds()*50,
		(arguments.Timeout*3).Nanoseconds(),
		3,
	)
	results.SetGlobalMeasureLatency(arguments.MeasureLatency)
	results.SetGlobalConcurrency(arguments.Concurrency)
	results.SetGlobalLatencyTypeFromString(arguments.LatencyType)
}
