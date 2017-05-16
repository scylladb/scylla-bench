package main

import (
	"fmt"
	"log"
	"sync/atomic"
	"time"

	"github.com/codahale/hdrhistogram"
	"github.com/gocql/gocql"
)

type Result struct {
	ElapsedTime    time.Duration
	Operations     int
	ClusteringRows int
	Latency        *hdrhistogram.Histogram
}

type MergedResult struct {
	Time                    time.Duration
	Operations              int
	ClusteringRows          int
	OperationsPerSecond     float64
	ClusteringRowsPerSecond float64
	Latency                 *hdrhistogram.Histogram
}

func NewHistogram() *hdrhistogram.Histogram {
	return hdrhistogram.New(0, (2 * timeout).Nanoseconds(), 5)
}

var reportedError uint32

func HandleError(err error) {
	if atomic.SwapUint32(&stopAll, 1) == 0 {
		log.Print(err)
		fmt.Println("\nstopping")
		atomic.StoreUint32(&stopAll, 1)
	}
}

func RunConcurrently(workload func(id int) Result) MergedResult {
	results := make([]chan Result, concurrency)
	for i := range results {
		results[i] = make(chan Result, 1)
	}

	for i := 0; i < concurrency; i++ {
		go func(i int) {
			results[i] <- workload(i)
			close(results[i])
		}(i)
	}

	var result MergedResult
	result.Latency = NewHistogram()
	for _, ch := range results {
		res := <-ch
		result.Time += res.ElapsedTime
		result.Operations += res.Operations
		result.ClusteringRows += res.ClusteringRows
		result.OperationsPerSecond += float64(res.Operations) / res.ElapsedTime.Seconds()
		result.ClusteringRowsPerSecond += float64(res.ClusteringRows) / res.ElapsedTime.Seconds()
		dropped := result.Latency.Merge(res.Latency)
		if dropped > 0 {
			log.Print("dropped: ", dropped)
		}

	}
	result.Time /= time.Duration(concurrency)
	return result
}

func DoWrites(session *gocql.Session, workload WorkloadGenerator) Result {
	value := make([]byte, clusteringRowSize)
	query := session.Query("INSERT INTO " + keyspaceName + "." + tableName + " (pk, ck, v) VALUES (?, ?, ?)")

	var operations int
	latencyHistogram := NewHistogram()

	start := time.Now()
	for !workload.IsDone() && atomic.LoadUint32(&stopAll) == 0 {
		operations++
		pk := workload.NextPartitionKey()
		ck := workload.NextClusteringKey()
		bound := query.Bind(pk, ck, value)

		requestStart := time.Now()
		err := bound.Exec()
		requestEnd := time.Now()
		if err != nil {
			HandleError(err)
			break
		}

		latency := requestEnd.Sub(requestStart)
		err = latencyHistogram.RecordValue(latency.Nanoseconds())
		if err != nil {
			HandleError(err)
			break
		}
	}
	end := time.Now()

	return Result{end.Sub(start), operations, operations, latencyHistogram}
}

func DoBatchedWrites(session *gocql.Session, workload WorkloadGenerator) Result {
	value := make([]byte, clusteringRowSize)
	request := fmt.Sprintf("INSERT INTO %s.%s (pk, ck, v) VALUES (?, ?, ?)", keyspaceName, tableName)

	var result Result
	result.Latency = NewHistogram()

	start := time.Now()
	for !workload.IsDone() && atomic.LoadUint32(&stopAll) == 0 {
		batch := gocql.NewBatch(gocql.UnloggedBatch)
		batchSize := 0

		currentPk := workload.NextPartitionKey()
		for !workload.IsPartitionDone() && atomic.LoadUint32(&stopAll) == 0 && batchSize < rowsPerRequest {
			ck := workload.NextClusteringKey()
			batchSize++
			batch.Query(request, currentPk, ck, value)
		}

		result.Operations++
		result.ClusteringRows += batchSize

		requestStart := time.Now()
		err := session.ExecuteBatch(batch)
		requestEnd := time.Now()
		if err != nil {
			HandleError(err)
			break
		}

		latency := requestEnd.Sub(requestStart)
		err = result.Latency.RecordValue(latency.Nanoseconds())
		if err != nil {
			HandleError(err)
			break
		}
	}
	end := time.Now()

	result.ElapsedTime = end.Sub(start)
	return result
}

func DoCounterUpdates(session *gocql.Session, workload WorkloadGenerator) Result {
	query := session.Query("UPDATE " + keyspaceName + "." + counterTableName + " SET c1 = c1 + 1, c2 = c2 + 1, c3 = c3 + 1, c4 = c4 + 1, c5 = c5 + 1 WHERE pk = ? AND ck = ?")

	var operations int
	latencyHistogram := NewHistogram()

	start := time.Now()
	for !workload.IsDone() && atomic.LoadUint32(&stopAll) == 0 {
		operations++
		pk := workload.NextPartitionKey()
		ck := workload.NextClusteringKey()
		bound := query.Bind(pk, ck)

		requestStart := time.Now()
		err := bound.Exec()
		requestEnd := time.Now()
		if err != nil {
			HandleError(err)
			break
		}

		latency := requestEnd.Sub(requestStart)
		err = latencyHistogram.RecordValue(latency.Nanoseconds())
		if err != nil {
			HandleError(err)
			break
		}
	}
	end := time.Now()

	return Result{end.Sub(start), operations, operations, latencyHistogram}
}

func DoReads(session *gocql.Session, workload WorkloadGenerator) Result {
	request := fmt.Sprintf("SELECT * FROM %s.%s WHERE pk = ? AND ck >= ? LIMIT %d", keyspaceName, tableName, rowsPerRequest)
	query := session.Query(request)

	var result Result
	result.Latency = NewHistogram()

	start := time.Now()
	for !workload.IsDone() && atomic.LoadUint32(&stopAll) == 0 {
		result.Operations++
		pk := workload.NextPartitionKey()
		ck := workload.NextClusteringKey()
		bound := query.Bind(pk, ck)

		requestStart := time.Now()
		iter := bound.Iter()
		for iter.Scan(nil, nil, nil) {
			result.ClusteringRows++
		}
		requestEnd := time.Now()

		err := iter.Close()
		if err != nil {
			HandleError(err)
			break
		}

		latency := requestEnd.Sub(requestStart)
		err = result.Latency.RecordValue(latency.Nanoseconds())
		if err != nil {
			HandleError(err)
			break
		}
	}
	end := time.Now()

	result.ElapsedTime = end.Sub(start)
	return result
}
