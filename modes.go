package main

import (
	"fmt"
	"log"
	"strings"
	"sync/atomic"
	"time"

	"github.com/codahale/hdrhistogram"
	"github.com/gocql/gocql"
)

type RateLimiter interface {
	Wait()
	ExpectedInterval() int64
}

type UnlimitedRateLimiter struct{}

func (*UnlimitedRateLimiter) Wait() {}

func (*UnlimitedRateLimiter) ExpectedInterval() int64 {
	return 0
}

type MaximumRateLimiter struct {
	Period      time.Duration
	LastRequest time.Time
}

func (mxrl *MaximumRateLimiter) Wait() {
	nextRequest := mxrl.LastRequest.Add(mxrl.Period)
	now := time.Now()
	if now.Before(nextRequest) {
		time.Sleep(nextRequest.Sub(now))
	}
	mxrl.LastRequest = time.Now()
}

func (mxrl *MaximumRateLimiter) ExpectedInterval() int64 {
	return mxrl.Period.Nanoseconds()
}

func NewRateLimiter(maximumRate int, timeOffset time.Duration) RateLimiter {
	if maximumRate == 0 {
		return &UnlimitedRateLimiter{}
	}
	period := time.Duration(int64(time.Second) / int64(maximumRate))
	lastRequest := time.Now().Add(timeOffset)
	return &MaximumRateLimiter{period, lastRequest}
}

type Result struct {
	Final          bool
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

func NewMergedResult() *MergedResult {
	result := &MergedResult{}
	result.Latency = NewHistogram()
	return result
}

func (mr *MergedResult) AddResult(result Result) {
	mr.Time += result.ElapsedTime
	mr.Operations += result.Operations
	mr.ClusteringRows += result.ClusteringRows
	mr.OperationsPerSecond += float64(result.Operations) / result.ElapsedTime.Seconds()
	mr.ClusteringRowsPerSecond += float64(result.ClusteringRows) / result.ElapsedTime.Seconds()
	dropped := mr.Latency.Merge(result.Latency)
	if dropped > 0 {
		log.Print("dropped: ", dropped)
	}
}

func NewHistogram() *hdrhistogram.Histogram {
	return hdrhistogram.New(time.Microsecond.Nanoseconds()*50, (timeout + timeout*2).Nanoseconds(), 3)
}

var reportedError uint32

func HandleError(err error) {
	if atomic.SwapUint32(&stopAll, 1) == 0 {
		log.Print(err)
		fmt.Println("\nstopping")
		atomic.StoreUint32(&stopAll, 1)
	}
}

func MergeResults(results []chan Result) (bool, *MergedResult) {
	result := NewMergedResult()
	final := false
	for i, ch := range results {
		res := <-ch
		if !final && res.Final {
			final = true
			result = NewMergedResult()
			for _, ch2 := range results[0:i] {
				res = <-ch2
				for !res.Final {
					res = <-ch2
				}
				result.AddResult(res)
			}
		} else if final && !res.Final {
			for !res.Final {
				res = <-ch
			}
		}
		result.AddResult(res)
	}
	result.Time /= time.Duration(concurrency)
	return final, result
}

func RunConcurrently(maximumRate int, workload func(id int, resultChannel chan Result, rateLimiter RateLimiter) Result) *MergedResult {
	var timeOffsetUnit int64
	if maximumRate != 0 {
		timeOffsetUnit = int64(time.Second) / int64(maximumRate)
		maximumRate /= concurrency
	} else {
		timeOffsetUnit = 0
	}

	results := make([]chan Result, concurrency)
	for i := range results {
		results[i] = make(chan Result, 1)
	}

	startTime := time.Now()
	for i := 0; i < concurrency; i++ {
		go func(i int) {
			timeOffset := time.Duration(timeOffsetUnit * int64(i))
			results[i] <- workload(i, results[i], NewRateLimiter(maximumRate, timeOffset))
			close(results[i])
		}(i)
	}

	final, result := MergeResults(results)
	for !final {
		result.Time = time.Now().Sub(startTime)
		PrintPartialResult(result)
		final, result = MergeResults(results)
	}
	return result
}

type ResultBuilder struct {
	FullResult    *Result
	PartialResult *Result
}

func NewResultBuilder() *ResultBuilder {
	rb := &ResultBuilder{}
	rb.FullResult = &Result{}
	rb.PartialResult = &Result{}
	rb.FullResult.Final = true
	rb.FullResult.Latency = NewHistogram()
	rb.PartialResult.Latency = NewHistogram()
	return rb
}

func (rb *ResultBuilder) IncOps() {
	rb.FullResult.Operations++
	rb.PartialResult.Operations++
}

func (rb *ResultBuilder) IncRows() {
	rb.FullResult.ClusteringRows++
	rb.PartialResult.ClusteringRows++
}

func (rb *ResultBuilder) AddRows(n int) {
	rb.FullResult.ClusteringRows += n
	rb.PartialResult.ClusteringRows += n
}

func (rb *ResultBuilder) ResetPartialResult() {
	rb.PartialResult = &Result{}
	rb.PartialResult.Latency = NewHistogram()
}

func (rb *ResultBuilder) RecordLatency(latency time.Duration, rateLimiter RateLimiter) error {
	err := rb.FullResult.Latency.RecordCorrectedValue(latency.Nanoseconds(), rateLimiter.ExpectedInterval())
	if err != nil {
		return err
	}

	err = rb.PartialResult.Latency.RecordCorrectedValue(latency.Nanoseconds(), rateLimiter.ExpectedInterval())
	if err != nil {
		return err
	}

	return nil
}

func DoWrites(session *gocql.Session, resultChannel chan Result, workload WorkloadGenerator, rateLimiter RateLimiter) Result {
	value := make([]byte, clusteringRowSize)
	query := session.Query("INSERT INTO " + keyspaceName + "." + tableName + " (pk, ck, v) VALUES (?, ?, ?)")

	rb := NewResultBuilder()

	start := time.Now()
	partialStart := start
	for !workload.IsDone() && atomic.LoadUint32(&stopAll) == 0 {
		rateLimiter.Wait()

		rb.IncOps()
		rb.IncRows()

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
		err = rb.RecordLatency(latency, rateLimiter)
		if err != nil {
			HandleError(err)
			break
		}

		now := time.Now()
		if now.Sub(partialStart) > time.Second {
			resultChannel <- *rb.PartialResult
			rb.ResetPartialResult()
			partialStart = now
		}
	}
	end := time.Now()

	rb.FullResult.ElapsedTime = end.Sub(start)
	return *rb.FullResult
}

func DoBatchedWrites(session *gocql.Session, resultChannel chan Result, workload WorkloadGenerator, rateLimiter RateLimiter) Result {
	value := make([]byte, clusteringRowSize)
	request := fmt.Sprintf("INSERT INTO %s.%s (pk, ck, v) VALUES (?, ?, ?)", keyspaceName, tableName)

	rb := NewResultBuilder()

	start := time.Now()
	partialStart := start
	for !workload.IsDone() && atomic.LoadUint32(&stopAll) == 0 {
		rateLimiter.Wait()

		batch := gocql.NewBatch(gocql.UnloggedBatch)
		batchSize := 0

		currentPk := workload.NextPartitionKey()
		for !workload.IsPartitionDone() && atomic.LoadUint32(&stopAll) == 0 && batchSize < rowsPerRequest {
			ck := workload.NextClusteringKey()
			batchSize++
			batch.Query(request, currentPk, ck, value)
		}

		rb.IncOps()
		rb.AddRows(batchSize)

		requestStart := time.Now()
		err := session.ExecuteBatch(batch)
		requestEnd := time.Now()
		if err != nil {
			HandleError(err)
			break
		}

		latency := requestEnd.Sub(requestStart)
		rb.RecordLatency(latency, rateLimiter)
		if err != nil {
			HandleError(err)
			break
		}

		now := time.Now()
		if now.Sub(partialStart) > time.Second {
			resultChannel <- *rb.PartialResult
			rb.ResetPartialResult()
			partialStart = now
		}
	}
	end := time.Now()

	rb.FullResult.ElapsedTime = end.Sub(start)
	return *rb.FullResult
}

func DoCounterUpdates(session *gocql.Session, resultChannel chan Result, workload WorkloadGenerator, rateLimiter RateLimiter) Result {
	query := session.Query("UPDATE " + keyspaceName + "." + counterTableName + " SET c1 = c1 + 1, c2 = c2 + 1, c3 = c3 + 1, c4 = c4 + 1, c5 = c5 + 1 WHERE pk = ? AND ck = ?")

	rb := NewResultBuilder()

	start := time.Now()
	partialStart := start
	for !workload.IsDone() && atomic.LoadUint32(&stopAll) == 0 {
		rateLimiter.Wait()

		rb.IncOps()
		rb.IncRows()

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
		rb.RecordLatency(latency, rateLimiter)
		if err != nil {
			HandleError(err)
			break
		}

		now := time.Now()
		if now.Sub(partialStart) > time.Second {
			resultChannel <- *rb.PartialResult
			rb.ResetPartialResult()
			partialStart = now
		}
	}
	end := time.Now()

	rb.FullResult.ElapsedTime = end.Sub(start)
	return *rb.FullResult
}

func DoReads(session *gocql.Session, resultChannel chan Result, workload WorkloadGenerator, rateLimiter RateLimiter) Result {
	var request string
	if inRestriction {
		arr := make([]string, rowsPerRequest)
		for i := 0; i < rowsPerRequest; i++ {
			arr[i] = "?"
		}
		request = fmt.Sprintf("SELECT * from %s.%s WHERE pk = ? AND ck IN (%s)", keyspaceName, tableName, strings.Join(arr, ", "))
	} else if provideUpperBound {
		request = fmt.Sprintf("SELECT * FROM %s.%s WHERE pk = ? AND ck >= ? AND ck < ?", keyspaceName, tableName)
	} else {
		request = fmt.Sprintf("SELECT * FROM %s.%s WHERE pk = ? AND ck >= ? LIMIT %d", keyspaceName, tableName, rowsPerRequest)
	}
	query := session.Query(request)

	rb := NewResultBuilder()

	start := time.Now()
	partialStart := start
	for !workload.IsDone() && atomic.LoadUint32(&stopAll) == 0 {
		rateLimiter.Wait()

		rb.IncOps()
		pk := workload.NextPartitionKey()

		var bound *gocql.Query
		if inRestriction {
			args := make([]interface{}, 1, rowsPerRequest+1)
			args[0] = pk
			for i := 0; i < rowsPerRequest; i++ {
				if workload.IsPartitionDone() {
					args = append(args, 0)
				} else {
					args = append(args, workload.NextClusteringKey())
				}
			}
			bound = query.Bind(args...)
		} else {
			ck := workload.NextClusteringKey()
			if provideUpperBound {
				bound = query.Bind(pk, ck, ck+rowsPerRequest)
			} else {
				bound = query.Bind(pk, ck)
			}
		}

		requestStart := time.Now()
		iter := bound.Iter()
		for iter.Scan(nil, nil, nil) {
			rb.IncRows()
		}
		requestEnd := time.Now()

		err := iter.Close()
		if err != nil {
			HandleError(err)
			break
		}

		latency := requestEnd.Sub(requestStart)
		rb.RecordLatency(latency, rateLimiter)
		if err != nil {
			HandleError(err)
			break
		}

		now := time.Now()
		if now.Sub(partialStart) > time.Second {
			resultChannel <- *rb.PartialResult
			rb.ResetPartialResult()
			partialStart = now
		}
	}
	end := time.Now()

	rb.FullResult.ElapsedTime = end.Sub(start)
	return *rb.FullResult
}
