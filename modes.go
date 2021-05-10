package main

import (
	"fmt"
	"log"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gocql/gocql"
	. "github.com/scylladb/scylla-bench/libs"
)

type RateLimiter interface {
	Wait()
	Expected() time.Time
}

type UnlimitedRateLimiter struct{}

func (*UnlimitedRateLimiter) Wait() {}

func (*UnlimitedRateLimiter) Expected() time.Time {
	return time.Time{}
}

type MaximumRateLimiter struct {
	Period              time.Duration
	StartTime           time.Time
	CompletedOperations int64
}

func (mxrl *MaximumRateLimiter) Wait() {
	mxrl.CompletedOperations++
	nextRequest := mxrl.StartTime.Add(mxrl.Period * time.Duration(mxrl.CompletedOperations))
	now := time.Now()
	if now.Before(nextRequest) {
		time.Sleep(nextRequest.Sub(now))
	}
}

func (mxrl *MaximumRateLimiter) Expected() time.Time {
	return mxrl.StartTime.Add(mxrl.Period * time.Duration(mxrl.CompletedOperations))
}

func NewRateLimiter(maximumRate int, timeOffset time.Duration) RateLimiter {
	if maximumRate == 0 {
		return &UnlimitedRateLimiter{}
	}
	period := time.Duration(int64(time.Second) / int64(maximumRate))
	return &MaximumRateLimiter{period, time.Now(), 0}
}

func HandleError(err error) {
	if atomic.SwapUint32(&stopAll, 1) == 0 {
		log.Print(err)
		fmt.Println("\nstopping")
		atomic.StoreUint32(&stopAll, 1)
	}
}

func RunConcurrently(maximumRate int, workload func(id int, resultChannel chan Result, rateLimiter RateLimiter)) *MergedResult {
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
			workload(i, results[i], NewRateLimiter(maximumRate, timeOffset))
			close(results[i])
		}(i)
	}

	final, result := MergeResults(results, timeout, measureLatency, concurrency)
	for !final {
		result.Time = time.Now().Sub(startTime)
		PrintPartialResult(result, errorRecordingLatency, measureLatency)
		final, result = MergeResults(results, timeout, measureLatency, concurrency)
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
	rb.FullResult.Latency = NewHistogram(timeout, measureLatency)
	rb.PartialResult.Latency = NewHistogram(timeout, measureLatency)
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

func (rb *ResultBuilder) IncErrors() {
	rb.FullResult.Errors++
	rb.PartialResult.Errors++
}

func (rb *ResultBuilder) ResetPartialResult() {
	rb.PartialResult = &Result{}
	rb.PartialResult.Latency = NewHistogram(timeout, measureLatency)
}

func (rb *ResultBuilder) RecordLatency(start time.Time, end time.Time) {
	if !measureLatency {
		return
	}

	value := end.Sub(start)

	_ = rb.FullResult.Latency.RecordValue(value.Nanoseconds())
	_ = rb.PartialResult.Latency.RecordValue(value.Nanoseconds())
}

var errorRecordingLatency bool

type TestIterator struct {
	iteration uint
	workload  WorkloadGenerator
}

func NewTestIterator(workload WorkloadGenerator) *TestIterator {
	return &TestIterator{0, workload}
}

func (ti *TestIterator) IsDone() bool {
	if atomic.LoadUint32(&stopAll) != 0 {
		return true;
	}

	if ti.workload.IsDone() {
		if ti.iteration + 1 == iterations {
			return true
		} else {
			ti.workload.Restart()
			ti.iteration++
			return false
		}
	} else {
		return false
	}
}

func RunTest(resultChannel chan Result, workload WorkloadGenerator, rateLimiter RateLimiter, test func(rb *ResultBuilder) (error, time.Duration)) {
	rb := NewResultBuilder()

	start := time.Now()
	partialStart := start
	iter := NewTestIterator(workload)
	for !iter.IsDone() {
		rateLimiter.Wait()

		start := rateLimiter.Expected()
		if start.IsZero() {
			start = time.Now()
		}

		err, _ := test(rb)
		if err != nil {
			log.Print(err)
			rb.IncErrors()
			continue
		}

		rb.RecordLatency(start, time.Now())

		now := time.Now()
		if now.Sub(partialStart) > time.Second {
			resultChannel <- *rb.PartialResult
			rb.ResetPartialResult()
			partialStart = now
		}
	}
	end := time.Now()

	rb.FullResult.ElapsedTime = end.Sub(start)
	resultChannel <- *rb.FullResult
}

func DoWrites(session *gocql.Session, resultChannel chan Result, workload WorkloadGenerator, rateLimiter RateLimiter) {
	query := session.Query("INSERT INTO " + keyspaceName + "." + tableName + " (pk, ck, v) VALUES (?, ?, ?)")

	RunTest(resultChannel, workload, rateLimiter, func(rb *ResultBuilder) (error, time.Duration) {
		pk := workload.NextPartitionKey()
		ck := workload.NextClusteringKey()
		value := GenerateData(pk, ck, clusteringRowSizeDist.Generate(), validateData)
		bound := query.Bind(pk, ck, value)

		requestStart := time.Now()
		err := bound.Exec()
		requestEnd := time.Now()
		if err != nil {
			return err, time.Duration(0)
		}

		rb.IncOps()
		rb.IncRows()

		latency := requestEnd.Sub(requestStart)
		return nil, latency
	})
}

func DoBatchedWrites(session *gocql.Session, resultChannel chan Result, workload WorkloadGenerator, rateLimiter RateLimiter) {
	request := fmt.Sprintf("INSERT INTO %s.%s (pk, ck, v) VALUES (?, ?, ?)", keyspaceName, tableName)

	RunTest(resultChannel, workload, rateLimiter, func(rb *ResultBuilder) (error, time.Duration) {
		batch := session.NewBatch(gocql.UnloggedBatch)
		batchSize := 0

		currentPk := workload.NextPartitionKey()
		for !workload.IsPartitionDone() && atomic.LoadUint32(&stopAll) == 0 && batchSize < rowsPerRequest {
			ck := workload.NextClusteringKey()
			batchSize++
			value := GenerateData(currentPk, ck, clusteringRowSizeDist.Generate(), validateData)
			batch.Query(request, currentPk, ck, value)
		}

		requestStart := time.Now()
		err := session.ExecuteBatch(batch)
		requestEnd := time.Now()
		if err != nil {
			return err, time.Duration(0)
		}

		rb.IncOps()
		rb.AddRows(batchSize)

		latency := requestEnd.Sub(requestStart)
		return nil, latency
	})
}

func DoCounterUpdates(session *gocql.Session, resultChannel chan Result, workload WorkloadGenerator, rateLimiter RateLimiter) {
	query := session.Query("UPDATE " + keyspaceName + "." + counterTableName +
		" SET c1 = c1 + ?, c2 = c2 + ?, c3 = c3 + ?, c4 = c4 + ?, c5 = c5 + ? WHERE pk = ? AND ck = ?")

	RunTest(resultChannel, workload, rateLimiter, func(rb *ResultBuilder) (error, time.Duration) {
		pk := workload.NextPartitionKey()
		ck := workload.NextClusteringKey()
		bound := query.Bind(ck, ck+1, ck+2, ck+3, ck+4, pk, ck)

		requestStart := time.Now()
		err := bound.Exec()
		requestEnd := time.Now()
		if err != nil {
			return err, time.Duration(0)
		}

		rb.IncOps()
		rb.IncRows()

		latency := requestEnd.Sub(requestStart)
		return nil, latency
	})
}

func DoReads(session *gocql.Session, resultChannel chan Result, workload WorkloadGenerator, rateLimiter RateLimiter) {
	DoReadsFromTable(tableName, session, resultChannel, workload, rateLimiter)
}

func DoCounterReads(session *gocql.Session, resultChannel chan Result, workload WorkloadGenerator, rateLimiter RateLimiter) {
	DoReadsFromTable(counterTableName, session, resultChannel, workload, rateLimiter)
}

func DoReadsFromTable(table string, session *gocql.Session, resultChannel chan Result, workload WorkloadGenerator, rateLimiter RateLimiter) {
	var request string
	if inRestriction {
		arr := make([]string, rowsPerRequest)
		for i := 0; i < rowsPerRequest; i++ {
			arr[i] = "?"
		}
		request = fmt.Sprintf("SELECT * from %s.%s WHERE pk = ? AND ck IN (%s)", keyspaceName, table, strings.Join(arr, ", "))
	} else if provideUpperBound {
		request = fmt.Sprintf("SELECT * FROM %s.%s WHERE pk = ? AND ck >= ? AND ck < ?", keyspaceName, table)
	} else if noLowerBound {
		request = fmt.Sprintf("SELECT * FROM %s.%s WHERE pk = ? LIMIT %d", keyspaceName, table, rowsPerRequest)
	} else {
		request = fmt.Sprintf("SELECT * FROM %s.%s WHERE pk = ? AND ck >= ? LIMIT %d", keyspaceName, table, rowsPerRequest)
	}
	query := session.Query(request)

	RunTest(resultChannel, workload, rateLimiter, func(rb *ResultBuilder) (error, time.Duration) {
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
		} else if noLowerBound {
			bound = query.Bind(pk)
		} else {
			ck := workload.NextClusteringKey()
			if provideUpperBound {
				bound = query.Bind(pk, ck, ck+int64(rowsPerRequest))
			} else {
				bound = query.Bind(pk, ck)
			}
		}

		var resPk, resCk int64
		var value []byte
		requestStart := time.Now()
		iter := bound.Iter()
		if table == tableName {
			for iter.Scan(&resPk, &resCk, &value) {
				rb.IncRows()
				if validateData {
					err := ValidateData(resPk, resCk, value, validateData)
					if err != nil {
						rb.IncErrors()
						log.Printf("data corruption in pk(%d), ck(%d): %s", resPk, resCk, err)
					}
				}
			}
		} else {
			var c1, c2, c3, c4, c5 int64
			for iter.Scan(&resPk, &resCk, &c1, &c2, &c3, &c4, &c5) {
				rb.IncRows()
				if validateData {
					// in case of uniform workload the same row can be updated number of times
					var updateNum int64
					if resCk == 0 {
						updateNum = c2
					} else {
						updateNum = c1 / resCk
					}
					if c1 != resCk*updateNum || c2 != c1+updateNum || c3 != c1+updateNum*2 || c4 != c1+updateNum*3 || c5 != c1+updateNum*4 {
						rb.IncErrors()
						log.Print("counter data corruption:", resPk, resCk, c1, c2, c3, c4, c5)
					}
				}
			}
		}
		requestEnd := time.Now()

		err := iter.Close()
		if err != nil {
			return err, time.Duration(0)
		}

		rb.IncOps()

		latency := requestEnd.Sub(requestStart)
		return nil, latency
	})
}

func DoScanTable(session *gocql.Session, resultChannel chan Result, workload WorkloadGenerator, rateLimiter RateLimiter) {
	request := fmt.Sprintf("SELECT * FROM %s.%s WHERE token(pk) >= ? AND token(pk) <= ?", keyspaceName, tableName)
	query := session.Query(request)

	RunTest(resultChannel, workload, rateLimiter, func(rb *ResultBuilder) (error, time.Duration) {
		requestStart := time.Now()
		currentRange := workload.NextTokenRange()
		bound := query.Bind(currentRange.Start, currentRange.End)
		iter := bound.Iter()
		for iter.Scan(nil, nil, nil) {
			rb.IncRows()
		}
		requestEnd := time.Now()

		err := iter.Close()
		if err != nil {
			return err, time.Duration(0)
		}

		rb.IncOps()

		latency := requestEnd.Sub(requestStart)
		return nil, latency
	})
}
