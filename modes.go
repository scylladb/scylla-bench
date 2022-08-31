package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"log"
	"math/rand"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gocql/gocql"
	"github.com/pkg/errors"
	"github.com/scylladb/scylla-bench/pkg/results"
	. "github.com/scylladb/scylla-bench/pkg/workloads"
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

func RunConcurrently(maximumRate int, workload func(id int, testResult *results.TestThreadResult, rateLimiter RateLimiter)) *results.TestResults {
	var timeOffsetUnit int64
	if maximumRate != 0 {
		timeOffsetUnit = int64(time.Second) / int64(maximumRate)
		maximumRate /= concurrency
	} else {
		timeOffsetUnit = 0
	}

	totalResults := results.TestResults{}
	totalResults.Init(concurrency)
	totalResults.SetStartTime()
	totalResults.PrintResultsHeader()

	for i := 0; i < concurrency; i++ {
		testResult := totalResults.GetTestResult(i)
		go func(i int) {
			timeOffset := time.Duration(timeOffsetUnit * int64(i))
			workload(i, testResult, NewRateLimiter(maximumRate, timeOffset))
		}(i)
	}

	return &totalResults
}

type TestIterator struct {
	iteration uint
	workload  WorkloadGenerator
}

func NewTestIterator(workload WorkloadGenerator) *TestIterator {
	return &TestIterator{0, workload}
}

func (ti *TestIterator) IsDone() bool {
	if atomic.LoadUint32(&stopAll) != 0 {
		return true
	}

	if ti.workload.IsDone() {
		if ti.iteration+1 == iterations {
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

func RunTest(threadResult *results.TestThreadResult, workload WorkloadGenerator, rateLimiter RateLimiter, test func(rb *results.TestThreadResult) (error, time.Duration)) {

	start := time.Now()
	partialStart := start
	iter := NewTestIterator(workload)
	errorsAtRow := 0
	for !iter.IsDone() {
		rateLimiter.Wait()

		expectedStartTime := rateLimiter.Expected()
		if expectedStartTime.IsZero() {
			expectedStartTime = time.Now()
		}

		err, rawLatency := test(threadResult)
		endTime := time.Now()
		if err != nil {
			errorsAtRow += 1
			threadResult.IncErrors()
			log.Print(err)
			if rawLatency > errorToTimeoutCutoffTime {
				// Consider this error to be timeout error and register it in histogram
				threadResult.RecordRawLatency(rawLatency)
				threadResult.RecordCoFixedLatency(endTime.Sub(expectedStartTime))
			}
		} else {
			errorsAtRow = 0
			threadResult.RecordRawLatency(rawLatency)
			threadResult.RecordCoFixedLatency(endTime.Sub(expectedStartTime))
		}

		now := time.Now()
		if maxErrorsAtRow > 0 && errorsAtRow >= maxErrorsAtRow {
			threadResult.SubmitCriticalError(errors.New(fmt.Sprintf("Error limit (maxErrorsAtRow) of %d errors is reached", errorsAtRow)))
		}
		if results.GlobalErrorFlag {
			threadResult.ResultChannel <- *threadResult.PartialResult
			threadResult.ResetPartialResult()
			break
		}
		if now.Sub(partialStart) > time.Second {
			threadResult.ResultChannel <- *threadResult.PartialResult
			threadResult.ResetPartialResult()
			partialStart = now
		}
	}
	end := time.Now()

	threadResult.FullResult.ElapsedTime = end.Sub(start)
	threadResult.ResultChannel <- *threadResult.FullResult
	threadResult.StopReporting()
}

const (
	generatedDataHeaderSize int64 = 24
	generatedDataMinSize    int64 = generatedDataHeaderSize + 33
)

func GenerateData(pk int64, ck int64, size int64) []byte {
	if !validateData {
		return make([]byte, size)
	}

	buf := new(bytes.Buffer)

	if size < generatedDataHeaderSize {
		if err := binary.Write(buf, binary.LittleEndian, int8(size)); err != nil {
			panic(err)
		}
		if err := binary.Write(buf, binary.LittleEndian, pk^ck); err != nil {
			panic(err)
		}
	} else {
		if err := binary.Write(buf, binary.LittleEndian, size); err != nil {
			panic(err)
		}
		if err := binary.Write(buf, binary.LittleEndian, pk); err != nil {
			panic(err)
		}
		if err := binary.Write(buf, binary.LittleEndian, ck); err != nil {
			panic(err)
		}
		if size < generatedDataMinSize {
			for i := generatedDataHeaderSize; i < size; i++ {
				if err := binary.Write(buf, binary.LittleEndian, int8(0)); err != nil {
					panic(err)
				}
			}
		} else {
			payload := make([]byte, size-generatedDataHeaderSize-sha256.Size)
			rand.Read(payload)
			csum := sha256.Sum256(payload)
			if err := binary.Write(buf, binary.LittleEndian, payload); err != nil {
				panic(err)
			}
			if err := binary.Write(buf, binary.LittleEndian, csum); err != nil {
				panic(err)
			}
		}
	}

	value := make([]byte, size)
	copy(value, buf.Bytes())
	return value
}

func ValidateData(pk int64, ck int64, data []byte) error {
	if !validateData {
		return nil
	}

	buf := bytes.NewBuffer(data)
	size := int64(buf.Len())

	var storedSize int64
	if size < generatedDataHeaderSize {
		var storedSizeCompact int8
		err := binary.Read(buf, binary.LittleEndian, &storedSizeCompact)
		if err != nil {
			return errors.Wrap(err, "failed to validate data, cannot read size from value")
		}
		storedSize = int64(storedSizeCompact)
	} else {
		err := binary.Read(buf, binary.LittleEndian, &storedSize)
		if err != nil {
			return errors.Wrap(err, "failed to validate data, cannot read size from value")
		}
	}

	if size != storedSize {
		return errors.Errorf("actual size of value (%d) doesn't match size stored in value (%d)", size, storedSize)
	}

	// There is no random payload for sizes < minFullSize
	if size < generatedDataMinSize {
		expectedBuf := GenerateData(pk, ck, size)
		if !bytes.Equal(buf.Bytes(), expectedBuf) {
			return errors.Errorf("actual value doesn't match expected value:\nexpected: %x\nactual: %x", expectedBuf, buf.Bytes())
		}
		return nil
	}

	var storedPk, storedCk int64
	var err error

	// Validate pk
	err = binary.Read(buf, binary.LittleEndian, &storedPk)
	if err != nil {
		return errors.Wrap(err, "failed to validate data, cannot read pk from value")
	}
	if storedPk != pk {
		return errors.Errorf("actual pk (%d) doesn't match pk stored in value (%d)", pk, storedPk)
	}

	// Validate ck
	err = binary.Read(buf, binary.LittleEndian, &storedCk)
	if err != nil {
		return errors.Wrap(err, "failed to validate data, cannot read pk from value")
	}
	if storedCk != ck {
		return errors.Errorf("actual ck (%d) doesn't match ck stored in value (%d)", ck, storedCk)
	}

	// Validate checksum over the payload
	payload := make([]byte, size-generatedDataHeaderSize-sha256.Size)
	err = binary.Read(buf, binary.LittleEndian, payload)
	if err != nil {
		return errors.Wrap(err, "failed to verify checksum, cannot read payload from value")
	}

	calculatedChecksumArray := sha256.Sum256(payload)
	calculatedChecksum := calculatedChecksumArray[0:]

	storedChecksum := make([]byte, 32)
	err = binary.Read(buf, binary.LittleEndian, storedChecksum)
	if err != nil {
		return errors.Wrap(err, "failed to verify checksum, cannot read checksum from value")
	}

	if !bytes.Equal(calculatedChecksum, storedChecksum) {
		return errors.New(fmt.Sprintf(
			"corrupt checksum or data: calculated checksum (%x) doesn't match stored checksum (%x) over data\n%x",
			calculatedChecksum,
			storedChecksum,
			payload))
	}
	return nil
}

func DoWrites(session *gocql.Session, threadResult *results.TestThreadResult, workload WorkloadGenerator, rateLimiter RateLimiter) {
	RunTest(threadResult, workload, rateLimiter, func(rb *results.TestThreadResult) (error, time.Duration) {
		query := session.Query("INSERT INTO " + keyspaceName + "." + tableName + " (pk, ck, v) VALUES (?, ?, ?)")
		pk := workload.NextPartitionKey()
		ck := workload.NextClusteringKey()
		value := GenerateData(pk, ck, clusteringRowSizeDist.Generate())
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

func DoBatchedWrites(session *gocql.Session, threadResult *results.TestThreadResult, workload WorkloadGenerator, rateLimiter RateLimiter) {
	request := fmt.Sprintf("INSERT INTO %s.%s (pk, ck, v) VALUES (?, ?, ?)", keyspaceName, tableName)

	RunTest(threadResult, workload, rateLimiter, func(rb *results.TestThreadResult) (error, time.Duration) {
		batch := session.NewBatch(gocql.UnloggedBatch)
		batchSize := 0

		currentPk := workload.NextPartitionKey()
		for !workload.IsPartitionDone() && atomic.LoadUint32(&stopAll) == 0 && batchSize < rowsPerRequest {
			ck := workload.NextClusteringKey()
			batchSize++
			value := GenerateData(currentPk, ck, clusteringRowSizeDist.Generate())
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

func DoCounterUpdates(session *gocql.Session, threadResult *results.TestThreadResult, workload WorkloadGenerator, rateLimiter RateLimiter) {
	RunTest(threadResult, workload, rateLimiter, func(rb *results.TestThreadResult) (error, time.Duration) {
		query := session.Query("UPDATE " + keyspaceName + "." + counterTableName +
			" SET c1 = c1 + ?, c2 = c2 + ?, c3 = c3 + ?, c4 = c4 + ?, c5 = c5 + ? WHERE pk = ? AND ck = ?")
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

func DoReads(session *gocql.Session, threadResult *results.TestThreadResult, workload WorkloadGenerator, rateLimiter RateLimiter) {
	DoReadsFromTable(tableName, session, threadResult, workload, rateLimiter)
}

func DoCounterReads(session *gocql.Session, threadResult *results.TestThreadResult, workload WorkloadGenerator, rateLimiter RateLimiter) {
	DoReadsFromTable(counterTableName, session, threadResult, workload, rateLimiter)
}

func BuildReadQuery(table string, orderBy string, session *gocql.Session) *gocql.Query {
	var request string
	var selectFields string
	if table == tableName {
		selectFields = "pk, ck, v"
	} else {
		selectFields = "pk, ck, c1, c2, c3, c4, c5"
	}

	var bypassCacheClause string
	if bypassCache {
		bypassCacheClause = "BYPASS CACHE"
	}

	switch {
	case inRestriction:
		arr := make([]string, rowsPerRequest)
		for i := 0; i < rowsPerRequest; i++ {
			arr[i] = "?"
		}
		request = fmt.Sprintf("SELECT %s FROM %s.%s WHERE pk = ? AND ck IN (%s) %s %s", selectFields, keyspaceName, table, strings.Join(arr, ", "), orderBy, bypassCacheClause)
	case provideUpperBound:
		request = fmt.Sprintf("SELECT %s FROM %s.%s WHERE pk = ? AND ck >= ? AND ck < ? %s %s", selectFields, keyspaceName, table, orderBy, bypassCacheClause)
	case noLowerBound:
		request = fmt.Sprintf("SELECT %s FROM %s.%s WHERE pk = ? %s LIMIT %d %s", selectFields, keyspaceName, table, orderBy, rowsPerRequest, bypassCacheClause)
	default:
		request = fmt.Sprintf("SELECT %s FROM %s.%s WHERE pk = ? AND ck >= ? %s LIMIT %d %s", selectFields, keyspaceName, table, orderBy, rowsPerRequest, bypassCacheClause)
	}
	return session.Query(request)
}

func DoReadsFromTable(table string, session *gocql.Session, threadResult *results.TestThreadResult, workload WorkloadGenerator, rateLimiter RateLimiter) {
	counter, num_of_orderings := 0, len(selectOrderByParsed)
	RunTest(threadResult, workload, rateLimiter, func(rb *results.TestThreadResult) (error, time.Duration) {
		counter++
		pk := workload.NextPartitionKey()
		query := BuildReadQuery(table, selectOrderByParsed[counter%num_of_orderings], session)

		var bound *gocql.Query
		switch {
		case inRestriction:
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
		case noLowerBound:
			bound = query.Bind(pk)
		default:
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
					err := ValidateData(resPk, resCk, value)
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

func DoScanTable(session *gocql.Session, threadResult *results.TestThreadResult, workload WorkloadGenerator, rateLimiter RateLimiter) {
	request := fmt.Sprintf("SELECT * FROM %s.%s WHERE token(pk) >= ? AND token(pk) <= ?", keyspaceName, tableName)
	RunTest(threadResult, workload, rateLimiter, func(rb *results.TestThreadResult) (error, time.Duration) {
		query := session.Query(request)
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
