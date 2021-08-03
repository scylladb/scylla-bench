package main

import (
	"fmt"
	"github.com/scylladb/scylla-bench/pkg/command_line"
	"log"
	"os"
	"os/signal"
	"sync/atomic"
	"time"

	"github.com/scylladb/scylla-bench/pkg/results"

	"github.com/gocql/gocql"
	"github.com/scylladb/scylla-bench/pkg/rate_limiters"
	. "github.com/scylladb/scylla-bench/pkg/workloads"
)


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

func main() {

	arguments.PrepareCommandLineParser()
	arguments.Parse()
	arguments.ValidateAndNormalize()
	arguments.PrintValues()
	startTime = arguments.GetStartTime()
	arguments.RunTimeoutHandler(&stopAll)
	RunOsSignalHandler()
	session := CreateSession()
	defer session.Close()
	PrepareDatabase(session, arguments.ReplicationFactor)
	arguments.SetResultsConfiguration()
	testResult := RunConcurrently(arguments.MaximumRate, func(i int, testResult *results.TestThreadResult, rateLimiter rate_limiters.RateLimiter) {
		GetMode(arguments.Mode)(session, testResult, GetWorkload(arguments.Workload, i, arguments.PartitionOffset, arguments.Mode, arguments.WriteRate, arguments.Distribution), rateLimiter)
	})

	testResult.GetTotalResults()
	testResult.PrintTotalResults()
	os.Exit(testResult.GetFinalStatus())
}

func CreateSession() *gocql.Session {
	session, err := arguments.BuildCQLClusterConfig().CreateSession()
	if err != nil {
		log.Fatal(err)
	}
	return session
}

func RunOsSignalHandler()  {
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
}