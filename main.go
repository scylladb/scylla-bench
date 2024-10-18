package main

import (
	"crypto/tls"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/scylladb/scylla-bench/pkg/results"

	"github.com/gocql/gocql"
	"github.com/gocql/gocql/scyllacloud"
	"github.com/hailocab/go-hostpool"
	"github.com/pkg/errors"
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

var (
	keyspaceName     string
	tableName        string
	counterTableName string
	username         string
	password         string

	mode           string
	latencyType    string
	maxErrorsAtRow int
	maxErrors      int
	concurrency    int
	maximumRate    int

	testDuration time.Duration

	partitionCount        int64
	clusteringRowCount    int64
	clusteringRowSizeDist random.Distribution

	rowsPerRequest      int
	provideUpperBound   bool
	inRestriction       bool
	selectOrderBy       string
	selectOrderByParsed []string
	noLowerBound        bool
	bypassCache         bool

	rangeCount int

	timeout    time.Duration
	iterations uint

	retryNumber   int
	retryInterval string
	retryHandler  string
	retryPolicy   *gocql.ExponentialBackoffRetryPolicy

	// Any error response that comes with delay greater than errorToTimeoutCutoffTime
	// to be considered as timeout error and recorded to histogram as such
	errorToTimeoutCutoffTime time.Duration
	startTime                time.Time
	stopAll                  uint32
	measureLatency           bool
	hdrLatencyFile           string
	hdrLatencyUnits          string
	hdrLatencySigFig         int
	validateData             bool
)

func Query(session *gocql.Session, request string) {
	err := session.Query(request).Exec()
	if err != nil {
		log.Fatal(err)
	}
}

func PrepareDatabase(session *gocql.Session, replicationFactor int) {
	Query(session, fmt.Sprintf("CREATE KEYSPACE IF NOT EXISTS %s WITH REPLICATION = { 'class' : 'SimpleStrategy', 'replication_factor' : %d }", keyspaceName, replicationFactor))

	switch mode {
		case "counter_update":
			fallthrough
		case "counter_read":
			Query(session, "CREATE TABLE IF NOT EXISTS "+keyspaceName+"."+counterTableName+
				" (pk bigint, ck bigint, c1 counter, c2 counter, c3 counter, c4 counter, c5 counter, PRIMARY KEY(pk, ck)) WITH compression = { }")
		default:
			Query(session, "CREATE TABLE IF NOT EXISTS "+keyspaceName+"."+tableName+" (pk bigint, ck bigint, v blob, PRIMARY KEY(pk, ck)) WITH compression = { }")

	}
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
		return NewRandomUniform(threadId, partitionCount, partitionOffset, clusteringRowCount)
	case "timeseries":
		switch mode {
		case "read":
			return NewTimeSeriesReader(threadId, concurrency, partitionCount, partitionOffset, clusteringRowCount, writeRate, distribution, startTime)
		case "write":
			return NewTimeSeriesWriter(threadId, concurrency, partitionCount, partitionOffset, clusteringRowCount, startTime, int64(maximumRate/concurrency))
		default:
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
		log.Fatal("unknown workload: ", name, ". Available workloads: sequential, uniform, timeseries, scan")
	}
	panic("unreachable")
}

func GetMode(name string) func(session *gocql.Session, testResult *results.TestThreadResult, workload WorkloadGenerator, rateLimiter RateLimiter) {
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
	default:
		log.Fatal("unknown mode: ", name, ". Available modes: write, counter_update, read, counter_read, scan")
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

func getRetryPolicy() *gocql.ExponentialBackoffRetryPolicy {
	var retryMinIntervalMillisecond, retryMaxIntervalMillisecond int
	var err error

	retryInterval = strings.Replace(retryInterval, " ", "", -1)
	values := strings.Split(retryInterval, ",")
	len_values := len(values)
	if (len_values == 0) || (len_values > 2) {
		log.Fatal("Wrong value for retry interval: '", retryInterval,
			"'. Only 1 or 2 values are expected.")
	}
	for i := range values {
		if _, err := strconv.Atoi(values[i]); err == nil {
			values[i] = values[i] + "000"
		}
		values[i] = strings.Replace(values[i], "ms", "", -1)
		values[i] = strings.Replace(values[i], "s", "000", -1)
	}
	retryMinIntervalMillisecond, err = strconv.Atoi(values[0])
	if err != nil {
		log.Fatal("Wrong value for retry minimum interval: '", values[0], "'")
	}
	retryMaxIntervalMillisecond, err = strconv.Atoi(values[len_values-1])
	if err != nil {
		log.Fatal("Wrong value for retry maximum interval: '", values[len_values-1], "'")
	}
	if retryMinIntervalMillisecond > retryMaxIntervalMillisecond {
		log.Fatal("Wrong retry interval values provided: 'min' (",
			values[0], "ms) interval is bigger than 'max' ("+
				values[len_values-1]+"ms)")
	}

	return &gocql.ExponentialBackoffRetryPolicy{
		NumRetries: retryNumber,
		Min:        time.Duration(retryMinIntervalMillisecond) * time.Millisecond,
		Max:        time.Duration(retryMaxIntervalMillisecond) * time.Millisecond,
	}
}

func main() {
	var (
		workload          string
		consistencyLevel  string
		replicationFactor int

		nodes             string
		caCertFile        string
		clientCertFile    string
		clientKeyFile     string
		serverName        string
		hostVerification  bool
		clientCompression bool
		connectionCount   int
		pageSize          int

		partitionOffset int64

		writeRate    int64
		distribution string

		hostSelectionPolicy string
		tlsEncryption       bool

		cloudConfigPath string
	)

	flag.StringVar(&mode, "mode", "", "operating mode: write, read, counter_update, counter_read, scan")
	flag.StringVar(&workload, "workload", "", "workload: sequential, uniform, timeseries")
	flag.StringVar(&consistencyLevel, "consistency-level", "quorum", "consistency level")
	flag.IntVar(&replicationFactor, "replication-factor", 1, "replication factor")
	flag.DurationVar(&timeout, "timeout", 5*time.Second, "request timeout")

	flag.IntVar(&retryNumber, "retry-number", 10, "number of retries (default 10)")
	flag.StringVar(
		&retryInterval, "retry-interval", "80ms,1s",
		"interval between retries. linear - '1s', exponential - '100ms,5s'")
	flag.StringVar(
		&retryHandler, "retry-handler", "sb",
		"Name of the query retries handler. Allowed values are 'sb' and 'gocql'. "+
			"Where 'sb' means 'scylla-bench' level of retries and 'gocql' means driver itself.")

	flag.StringVar(&nodes, "nodes", "127.0.0.1", "nodes")
	flag.BoolVar(&clientCompression, "client-compression", true, "use compression for client-coordinator communication")
	flag.IntVar(&concurrency, "concurrency", 16, "number of used goroutines")
	flag.IntVar(&connectionCount, "connection-count", 4, "number of connections")
	flag.IntVar(&maximumRate, "max-rate", 0, "the maximum rate of outbound requests in op/s (0 for unlimited)")
	flag.IntVar(&pageSize, "page-size", 1000, "page size")

	flag.Int64Var(&partitionCount, "partition-count", 10000, "number of partitions")
	flag.Int64Var(&clusteringRowCount, "clustering-row-count", 100, "number of clustering rows in a partition")
	flag.Var(MakeDistributionValue(&clusteringRowSizeDist, random.Fixed{Value: 4}), "clustering-row-size", "size of a single clustering row, can use random values")

	flag.IntVar(&rowsPerRequest, "rows-per-request", 1, "clustering rows per single request")
	flag.BoolVar(&provideUpperBound, "provide-upper-bound", false, "whether read requests should provide an upper bound")
	flag.BoolVar(&inRestriction, "in-restriction", false, "use IN restriction in read requests")
	flag.StringVar(&selectOrderBy, "select-order-by", "none", "controls order part 'order by ck asc/desc' of the read query, you can set it to: none,asc,desc or to the list of them, i.e. 'none,asc', in such case it will run queries with these orders one by one")
	flag.BoolVar(&noLowerBound, "no-lower-bound", false, "do not provide lower bound in read requests")
	flag.BoolVar(&bypassCache, "bypass-cache", false, "Execute queries with the \"BYPASS CACHE\" CQL clause")
	flag.IntVar(&rangeCount, "range-count", 1, "number of ranges to split the token space into (relevant only for scan mode)")

	flag.DurationVar(&testDuration, "duration", 0, "duration of the test in seconds (0 for unlimited)")
	flag.UintVar(&iterations, "iterations", 1, "number of iterations to run (0 for unlimited, relevant only for workloads that have a defined number of ops to execute)")

	flag.Int64Var(&partitionOffset, "partition-offset", 0, "start of the partition range (not applicable to the 'scan' workload)")

	flag.BoolVar(&measureLatency, "measure-latency", true, "measure request latency")
	flag.StringVar(&hdrLatencyFile, "hdr-latency-file", "", "log co-fixed and raw latency hdr histograms into a file")
	flag.StringVar(&hdrLatencyUnits, "hdr-latency-units", "ns", "ns (nano seconds), us (microseconds), ms (milliseconds)")
	flag.IntVar(&hdrLatencySigFig, "hdr-latency-sig", 3, "significant figures of the hdr histogram, number from 1 to 5 (default: 3)")

	flag.BoolVar(&validateData, "validate-data", false, "write meaningful data and validate while reading")

	var startTimestamp int64
	flag.Int64Var(&writeRate, "write-rate", 0, "rate of writes (relevant only for time series reads)")
	flag.Int64Var(&startTimestamp, "start-timestamp", 0, "start timestamp of the write load (relevant only for time series reads)")
	flag.StringVar(&distribution, "distribution", "uniform", "distribution of keys (relevant only for time series reads): uniform, hnormal")

	flag.StringVar(&latencyType, "latency-type", "raw", "type of the latency to print during the run: raw, fixed-coordinated-omission")

	flag.StringVar(&keyspaceName, "keyspace", "scylla_bench", "keyspace to use")
	flag.StringVar(&tableName, "table", "test", "table to use")
	flag.StringVar(&username, "username", "", "cql username for authentication")
	flag.StringVar(&password, "password", "", "cql password for authentication")

	flag.BoolVar(&tlsEncryption, "tls", false, "use TLS encryption")
	flag.StringVar(&serverName, "tls-server-name", "", "TLS server hostname")
	flag.BoolVar(&hostVerification, "tls-host-verification", false, "verify server certificate")
	flag.StringVar(&caCertFile, "tls-ca-cert-file", "", "path to CA certificate file, needed to enable encryption")
	flag.StringVar(&clientCertFile, "tls-client-cert-file", "", "path to client certificate file, needed to enable client certificate authentication")
	flag.StringVar(&clientKeyFile, "tls-client-key-file", "", "path to client key file, needed to enable client certificate authentication")

	flag.StringVar(&hostSelectionPolicy, "host-selection-policy", "token-aware", "set the driver host selection policy (round-robin,token-aware,dc-aware),default 'token-aware'")
	flag.IntVar(&maxErrorsAtRow, "error-at-row-limit", 0, "set limit of errors caught by one thread at row after which workflow will be terminated and error reported. Set it to 0 if you want to haven no limit")
	flag.IntVar(
		&maxErrors,
		"error-limit", 0,
		"Number of errors in summary to appear before stopping the execution of a workflow. "+
			"If it is set to '0' then no limit for number of errors is applied.")

	flag.StringVar(&cloudConfigPath, "cloud-config-path", "", "set the cloud config bundle")

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
	} else if workload == "" {
		log.Fatal("workload type needs to be specified")
	}

	if workload == "uniform" && testDuration == 0 {
		log.Fatal("uniform workload requires limited test duration")
	}

	if iterations > 1 && workload != "sequential" && workload != "scan" {
		log.Fatal("iterations only supported for the sequential and scan workload")
	}

	if partitionOffset != 0 && workload == "scan" {
		log.Fatal("partition-offset is not supported by the 'scan' workload")
	}

	if selectOrderBy == "" {
		selectOrderBy = "none"
	}

	for idx, chunk := range strings.Split(selectOrderBy, ",") {
		switch strings.ToLower(chunk) {
		case "none":
			selectOrderByParsed = append(selectOrderByParsed, "")
		case "asc":
			selectOrderByParsed = append(selectOrderByParsed, "ORDER BY ck ASC")
		case "desc":
			selectOrderByParsed = append(selectOrderByParsed, "ORDER BY ck DESC")
		default:
			log.Fatal(fmt.Sprintf("value in -select-order-by[%d] is neither of none,asc,desc", idx))
		}
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

	if 0 >= hdrLatencySigFig || hdrLatencySigFig > 5 {
		log.Fatal("hdr-latency-sig should be int from 1 to 5")
	}

	if timeout != 0 {
		errorToTimeoutCutoffTime = timeout / 5
	} else {
		errorToTimeoutCutoffTime = time.Second
	}

	if err := results.ValidateGlobalLatencyType(latencyType); err != nil {
		log.Fatal(errors.Wrap(err, "Bad value for latency-type"))
	}
	var cluster *gocql.ClusterConfig
	var err error

	if cloudConfigPath == "" {
		cluster = gocql.NewCluster(strings.Split(nodes, ",")...)
	} else if !tlsEncryption && username == "" && password == "" {
		cluster, err = scyllacloud.NewCloudCluster(cloudConfigPath)
		if err != nil {
			log.Fatal(err)
		}
	} else {
		log.Fatal("can't use -tls/-username/-password and -cloud-config-path at the same time")
	}

	retryPolicy = getRetryPolicy()
	if retryHandler == "gocql" {
		cluster.RetryPolicy = retryPolicy
	} else if retryHandler != "sb" {
		log.Fatal("'-retry-handler' option accepts only 'sb' and 'gocql' values.")
	}
	cluster.DefaultIdempotence = true
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

	if username != "" && password != "" {
		cluster.Authenticator = gocql.PasswordAuthenticator{
			Username: username,
			Password: password,
		}
	}

	if tlsEncryption {
		sslOpts := &gocql.SslOptions{
			Config: &tls.Config{
				ServerName:         serverName,
				InsecureSkipVerify: !hostVerification,
			},
			EnableHostVerification: false,
		}

		if caCertFile != "" {
			if _, err := os.Stat(caCertFile); err != nil {
				log.Fatal(err)
			}
			sslOpts.CaPath = caCertFile
		}

		if clientKeyFile != "" {
			if _, err := os.Stat(clientKeyFile); err != nil {
				log.Fatal(err)
			}
			sslOpts.KeyPath = clientKeyFile
		}

		if clientCertFile != "" {
			if _, err := os.Stat(clientCertFile); err != nil {
				log.Fatal(err)
			}
			sslOpts.CertPath = clientCertFile
		}

		if clientKeyFile != "" && clientCertFile == "" {
			log.Fatal("tls-client-cert-file is required when tls-client-key-file is provided")
		}
		if clientCertFile != "" && clientKeyFile == "" {
			log.Fatal("tls-client-key-file is required when tls-client-cert-file is provided")
		}

		if hostVerification {
			if serverName == "" {
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

	PrepareDatabase(session, replicationFactor)

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
	if maxErrorsAtRow == 0 {
		fmt.Println("Max error number at row: unlimited")
	} else {
		fmt.Println("Max error number at row:", maxErrorsAtRow)
	}
	if maxErrors == 0 {
		fmt.Println("Max error number:\t unlimited")
	} else {
		fmt.Println("Max error number:\t", maxErrors)
	}
	fmt.Println("Retries:\t\t")
	fmt.Println("  number:\t\t", retryPolicy.NumRetries)
	fmt.Println("  min interval:\t\t", retryPolicy.Min)
	fmt.Println("  max interval:\t\t", retryPolicy.Max)
	fmt.Println("  handler:\t\t", retryHandler)
	fmt.Println("Consistency level:\t", consistencyLevel)
	fmt.Println("Partition count:\t", partitionCount)
	if workload == "sequential" && partitionOffset != 0 {
		fmt.Println("Partition offset:\t", partitionOffset)
	}
	fmt.Println("Clustering rows:\t", clusteringRowCount)
	fmt.Println("Clustering row size:\t", clusteringRowSizeDist)
	fmt.Println("Rows per request:\t", rowsPerRequest)
	if mode == "read" {
		fmt.Println("Provide upper bound:\t", provideUpperBound)
		fmt.Println("IN queries:\t\t", inRestriction)
		fmt.Println("Order by:\t\t", selectOrderBy)
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
	setResultsConfiguration()

	fmt.Println("Hdr memory consumption:\t", results.GetHdrMemoryConsumption(concurrency), "bytes")

	testResult := RunConcurrently(maximumRate, func(i int, testResult *results.TestThreadResult, rateLimiter RateLimiter) {
		GetMode(mode)(session, testResult, GetWorkload(workload, i, partitionOffset, mode, writeRate, distribution), rateLimiter)
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
	results.SetGlobalMeasureLatency(measureLatency)
	results.SetGlobalHdrLatencyFile(hdrLatencyFile)
	results.SetGlobalHdrLatencyUnits(hdrLatencyUnits)
	results.SetGlobalHistogramConfiguration(
		time.Microsecond.Nanoseconds()*50,
		(timeout * 3).Nanoseconds(),
		hdrLatencySigFig,
	)
	results.SetGlobalConcurrency(concurrency)
	results.SetGlobalLatencyTypeFromString(latencyType)
}
