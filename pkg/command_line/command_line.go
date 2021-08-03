package command_line

import (
	"crypto/tls"
	"flag"
	"fmt"
	"github.com/gocql/gocql"
	"github.com/pkg/errors"
	"github.com/scylladb/scylla-bench/pkg/results"
	"github.com/scylladb/scylla-bench/random"
	"log"
	"os"
	"strings"
	"sync/atomic"
	"time"
)

type CommandLineArguments struct {
	HostVerification      bool
	CaCertFile            string
	ClientCertFile        string
	ClientCompression     bool
	ClientKeyFile         string
	ClusteringRowCount    int64
	ClusteringRowSizeDist random.Distribution
	Concurrency           int
	ConnectionCount       int
	ConsistencyLevel      string
	Distribution          string
	// Any error response that comes with delay greater than errorToTimeoutCutoffTime
	// to be considered as timeout error and recorded to histogram as such
	ErrorToTimeoutCutoffTime time.Duration
	HostSelectionPolicy      string
	InRestriction            bool
	Iterations               uint
	KeyspaceName             string
	LatencyType              string
	MaxErrorsAtRow           int
	MaximumRate              int
	MeasureLatency           bool
	Mode                     string
	Nodes                    string
	NoLowerBound             bool
	PageSize                 int
	PartitionCount           int64
	Password                 string
	PartitionOffset          int64
	ProvideUpperBound        bool
	RangeCount               int
	ReplicationFactor        int
	RowsPerRequest           int
	ServerName               string
	StartTimestamp			 int64
	TableName                string
	TestDuration             time.Duration
	Timeout                  time.Duration
	TLSEncryption            bool
	Username                 string
	ValidateData             bool
	Workload                 string
	WriteRate                int64
}

func (args *CommandLineArguments) PrepareCommandLineParser() {
	flag.StringVar(&args.Mode, "mode", "", "operating mode: write, read, counter_update, counter_read, scan")
	flag.StringVar(&args.Workload, "workload", "", "workload: sequential, uniform, timeseries")
	flag.StringVar(&args.ConsistencyLevel, "consistency-level", "quorum", "consistency level")
	flag.IntVar(&args.ReplicationFactor, "replication-factor", 1, "replication factor")
	flag.DurationVar(&args.Timeout, "timeout", 5*time.Second, "request timeout")

	flag.StringVar(&args.Nodes, "nodes", "127.0.0.1", "nodes")
	flag.BoolVar(&args.ClientCompression, "client-compression", true, "use compression for client-coordinator communication")
	flag.IntVar(&args.Concurrency, "concurrency", 16, "number of used goroutines")
	flag.IntVar(&args.ConnectionCount, "connection-count", 4, "number of connections")
	flag.IntVar(&args.MaximumRate, "max-rate", 0, "the maximum rate of outbound requests in op/s (0 for unlimited)")
	flag.IntVar(&args.PageSize, "page-size", 1000, "page size")

	flag.Int64Var(&args.PartitionCount, "partition-count", 10000, "number of partitions")
	flag.Int64Var(&args.ClusteringRowCount, "clustering-row-count", 100, "number of clustering rows in a partition")
	flag.Var(MakeDistributionValue(&args.ClusteringRowSizeDist, random.Fixed{Value: 4}), "clustering-row-size", "size of a single clustering row, can use random values")

	flag.IntVar(&args.RowsPerRequest, "rows-per-request", 1, "clustering rows per single request")
	flag.BoolVar(&args.ProvideUpperBound, "provide-upper-bound", false, "whether read requests should provide an upper bound")
	flag.BoolVar(&args.InRestriction, "in-restriction", false, "use IN restriction in read requests")
	flag.BoolVar(&args.NoLowerBound, "no-lower-bound", false, "do not provide lower bound in read requests")
	flag.IntVar(&args.RangeCount, "range-count", 1, "number of ranges to split the token space into (relevant only for scan mode)")

	flag.DurationVar(&args.TestDuration, "duration", 0, "duration of the test in seconds (0 for unlimited)")
	flag.UintVar(&args.Iterations, "iterations", 1, "number of iterations to run (0 for unlimited, relevant only for workloads that have a defined number of ops to execute)")

	flag.Int64Var(&args.PartitionOffset, "partition-offset", 0, "start of the partition range (only for sequential workload)")

	flag.BoolVar(&args.MeasureLatency, "measure-latency", true, "measure request latency")
	flag.BoolVar(&args.ValidateData, "validate-data", false, "write meaningful data and validate while reading")

	flag.Int64Var(&args.WriteRate, "write-rate", 0, "rate of writes (relevant only for time series reads)")
	flag.Int64Var(&args.StartTimestamp, "start-timestamp", 0, "start timestamp of the write load (relevant only for time series reads)")
	flag.StringVar(&args.Distribution, "distribution", "uniform", "distribution of keys (relevant only for time series reads): uniform, hnormal")

	flag.StringVar(&args.LatencyType, "latency-type", "raw", "type of the latency to print during the run: raw, fixed-coordinated-omission")

	flag.StringVar(&args.KeyspaceName, "keyspace", "scylla_bench", "keyspace to use")
	flag.StringVar(&args.TableName, "table", "test", "table to use")
	flag.StringVar(&args.Username, "username", "", "cql username for authentication")
	flag.StringVar(&args.Password, "password", "", "cql password for authentication")

	flag.BoolVar(&args.TLSEncryption, "tls", false, "use TLS encryption")
	flag.StringVar(&args.ServerName, "tls-server-name", "", "TLS server hostname")
	flag.BoolVar(&args.HostVerification, "tls-host-verification", false, "verify server certificate")
	flag.StringVar(&args.CaCertFile, "tls-ca-cert-file", "", "path to CA certificate file, needed to enable encryption")
	flag.StringVar(&args.ClientCertFile, "tls-client-cert-file", "", "path to client certificate file, needed to enable client certificate authentication")
	flag.StringVar(&args.ClientKeyFile, "tls-client-key-file", "", "path to client key file, needed to enable client certificate authentication")

	flag.StringVar(&args.HostSelectionPolicy, "host-selection-policy", "token-aware", "set the driver host selection policy (round-robin,token-aware,dc-aware),default 'token-aware'")
	flag.IntVar(&args.MaxErrorsAtRow, "error-at-row-limit", 0, "set limit of errors caught by one thread at row after which workflow will be terminated and error reported. Set it to 0 if you want to haven no limit")

	flag.Usage = func() {
		fmt.Fprintf(os.Stdout, "Usage:\n%s [options]\n\n", os.Args[0])
		flag.PrintDefaults()
	}
}

func (args *CommandLineArguments) Parse() {
	flag.Parse()
}

func (args *CommandLineArguments) ValidateAndNormalize() {
	if args.Mode == "" {
		log.Fatal("test args.Mode needs to be specified")
	}

	if args.Mode == "scan" {
		if args.Workload != "" {
			log.Fatal("workload type cannot be scpecified for scan mode")
		}
		args.Workload = "scan"
		if args.Concurrency > args.RangeCount {
			args.Concurrency = args.RangeCount
			log.Printf("adjusting args.Concurrency to the highest useful value of %v", args.Concurrency)
		}
	} else if args.Workload == "" {
		log.Fatal("workload type needs to be specified")
	}

	if args.Workload == "uniform" && args.TestDuration == 0 {
		log.Fatal("uniform workload requires limited test duration")
	}

	if args.Iterations > 1 && args.Workload != "sequential" && args.Workload != "scan" {
		log.Fatal("iterations only supported for the sequential and scan workload")
	}

	if args.PartitionOffset != 0 && args.Workload != "sequential" {
		log.Fatal("partition-offset has a meaning only in sequential workloads")
	}

	readModeTweaks := toInt(args.InRestriction) + toInt(args.ProvideUpperBound) + toInt(args.NoLowerBound)
	if args.Mode != "read" && args.Mode != "counter_read" {
		if readModeTweaks != 0 {
			log.Fatal("in-restriction, no-lower-bound and provide-uppder-bound flags make sense only in read mode")
		}
	} else if readModeTweaks > 1 {
		log.Fatal("in-restriction, no-lower-bound and provide-uppder-bound flags are mutually exclusive")
	}

	if args.Workload == "timeseries" && args.Mode == "read" && args.WriteRate == 0 {
		log.Fatal("write rate must be provided for time series reads loads")
	}
	if args.Workload == "timeseries" && args.Mode == "read" && args.StartTimestamp == 0 {
		log.Fatal("start timestamp must be provided for time series reads loads")
	}
	if args.Workload == "timeseries" && args.Mode == "write" && int64(args.Concurrency) > args.PartitionCount {
		log.Fatal("time series writes require args.Concurrency less than or equal partition count")
	}
	if args.Workload == "timeseries" && args.Mode == "write" && args.MaximumRate == 0 {
		log.Fatal("max-rate must be provided for time series write loads")
	}

	if err := results.ValidateGlobalLatencyType(args.LatencyType); err != nil {
		log.Fatal(errors.Wrap(err, "Bad value for latency-type"))
	}

	if args.Timeout != 0 {
		args.ErrorToTimeoutCutoffTime = args.Timeout / 5
	} else {
		args.ErrorToTimeoutCutoffTime = time.Second
	}
}

func (args *CommandLineArguments) BuildCQLClusterConfig() *gocql.ClusterConfig {
	cluster := gocql.NewCluster(strings.Split(args.Nodes, ",")...)
	cluster.NumConns = args.ConnectionCount
	cluster.PageSize = args.PageSize
	cluster.Timeout = args.Timeout
	policy, err := buildHostSelectionPolicy(args.HostSelectionPolicy, strings.Split(args.Nodes, ","))
	if err != nil {
		log.Fatal(err)
	}
	cluster.PoolConfig.HostSelectionPolicy = policy

	switch args.ConsistencyLevel {
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
		log.Fatal("unknown consistency level: ", args.ConsistencyLevel)
	}
	if args.ClientCompression {
		cluster.Compressor = &gocql.SnappyCompressor{}
	}

	if args.Username != "" && args.Password != "" {
		cluster.Authenticator = gocql.PasswordAuthenticator{
			Username: args.Username,
			Password: args.Password,
		}
	}

	if args.TLSEncryption {
		sslOpts := &gocql.SslOptions{
			Config: &tls.Config{
				ServerName: args.ServerName,
			},
			EnableHostVerification: args.HostVerification,
		}

		if args.CaCertFile != "" {
			if _, err := os.Stat(args.CaCertFile); err != nil {
				log.Fatal(err)
			}
			sslOpts.CaPath = args.CaCertFile
		}

		if args.ClientKeyFile != "" {
			if _, err := os.Stat(args.ClientKeyFile); err != nil {
				log.Fatal(err)
			}
			sslOpts.KeyPath = args.ClientKeyFile
		}

		if args.ClientCertFile != "" {
			if _, err := os.Stat(args.ClientCertFile); err != nil {
				log.Fatal(err)
			}
			sslOpts.CertPath = args.ClientCertFile
		}

		if args.ClientKeyFile != "" && args.ClientCertFile == "" {
			log.Fatal("tls-client-cert-file is required when tls-client-key-file is provided")
		}
		if args.ClientCertFile != "" && args.ClientKeyFile == "" {
			log.Fatal("tls-client-key-file is required when tls-client-cert-file is provided")
		}

		if args.HostVerification {
			if args.ServerName == "" {
				log.Fatal("tls-server-name is required when tls-host-verification is enabled")
			}
		}

		cluster.SslOpts = sslOpts
	}
	return cluster
}

func (args *CommandLineArguments) PrintValues()  {
	fmt.Println("Configuration")
	fmt.Println("Keyspace:\t\t", args.KeyspaceName)
	fmt.Println("Tablename:\t\t", args.TableName)
	fmt.Println("Mode:\t\t\t", args.Mode)
	fmt.Println("Workload:\t\t", args.Workload)
	fmt.Println("Timeout:\t\t", args.Timeout)
	fmt.Println("Consistency level:\t", args.ConsistencyLevel)
	fmt.Println("Replication factor:\t", args.ReplicationFactor)
	fmt.Println("Partition count:\t", args.PartitionCount)
	if args.Workload == "sequential" && args.PartitionOffset != 0 {
		fmt.Println("Partition offset:\t", args.PartitionOffset)
	}
	fmt.Println("Clustering rows:\t", args.ClusteringRowCount)
	fmt.Println("Clustering row size:\t", args.ClusteringRowSizeDist)
	fmt.Println("Rows per request:\t", args.RowsPerRequest)
	if args.Mode == "read" {
		fmt.Println("Provide upper bound:\t", args.ProvideUpperBound)
		fmt.Println("IN queries:\t\t", args.InRestriction)
		fmt.Println("No lower bound:\t\t", args.NoLowerBound)
	}
	fmt.Println("Page size:\t\t", args.PageSize)
	fmt.Println("Concurrency:\t\t", args.Concurrency)
	fmt.Println("Connections:\t\t", args.ConnectionCount)
	if args.MaximumRate > 0 {
		fmt.Println("Maximum rate:\t\t", args.MaximumRate, "op/s")
	} else {
		fmt.Println("Maximum rate:\t\t unlimited")
	}
	fmt.Println("Client compression:\t", args.ClientCompression)
	if args.Workload == "timeseries" {
		fmt.Println("Start timestamp:\t", args.StartTimestamp)
		fmt.Println("Write rate:\t\t", int64(args.MaximumRate)/args.PartitionCount)
	}

}

func (args *CommandLineArguments) RunTimeoutHandler(stopSignal *uint32)  {
	if args.TestDuration > 0 {
		go func() {
			time.Sleep(args.TestDuration)
			atomic.StoreUint32(stopSignal, 1)
		}()
	}
}

func (args *CommandLineArguments) GetStartTime() time.Time {
	if args.StartTimestamp != 0 {
		return time.Unix(0, args.StartTimestamp)
	}
	return time.Now()
}

func (args *CommandLineArguments) SetResultsConfiguration() {
	results.SetGlobalHistogramConfiguration(
		time.Microsecond.Nanoseconds()*50,
		(args.Timeout*3).Nanoseconds(),
		3,
	)
	results.SetGlobalMeasureLatency(args.MeasureLatency)
	results.SetGlobalConcurrency(args.Concurrency)
	results.SetGlobalLatencyTypeFromString(args.LatencyType)
}
