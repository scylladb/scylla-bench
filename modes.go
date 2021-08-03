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
	. "github.com/scylladb/scylla-bench/pkg/rate_limiters"
	"github.com/scylladb/scylla-bench/pkg/results"
	. "github.com/scylladb/scylla-bench/pkg/workloads"
)

func RunConcurrently(maximumRate int, workload func(id int, testResult *results.TestThreadResult, rateLimiter RateLimiter)) *results.TestResults {
	var timeOffsetUnit int64
	if maximumRate != 0 {
		timeOffsetUnit = int64(time.Second) / int64(maximumRate)
		maximumRate /= arguments.Concurrency
	} else {
		timeOffsetUnit = 0
	}

	totalResults := results.TestResults{}
	totalResults.Init(arguments.Concurrency)
	totalResults.SetStartTime()
	totalResults.PrintResultsHeader()

	for i := 0; i < arguments.Concurrency; i++ {
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
		if ti.iteration+1 == arguments.Iterations {
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
			if rawLatency > arguments.ErrorToTimeoutCutoffTime {
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
		if arguments.MaxErrorsAtRow > 0 && errorsAtRow >= arguments.MaxErrorsAtRow {
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
	generatedDataMinSize          = generatedDataHeaderSize + 33
)

func GenerateData(pk int64, ck int64, size int64) []byte {
	if !arguments.ValidateData {
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
	if !arguments.ValidateData {
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
	query := session.Query("INSERT INTO " + arguments.KeyspaceName + "." + arguments.TableName + " (pk, ck, v) VALUES (?, ?, ?)")

	RunTest(threadResult, workload, rateLimiter, func(rb *results.TestThreadResult) (error, time.Duration) {
		pk := workload.NextPartitionKey()
		ck := workload.NextClusteringKey()
		value := GenerateData(pk, ck, arguments.ClusteringRowSizeDist.Generate())
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
	request := fmt.Sprintf("INSERT INTO %s.%s (pk, ck, v) VALUES (?, ?, ?)", arguments.KeyspaceName, arguments.TableName)

	RunTest(threadResult, workload, rateLimiter, func(rb *results.TestThreadResult) (error, time.Duration) {
		batch := session.NewBatch(gocql.UnloggedBatch)
		batchSize := 0

		currentPk := workload.NextPartitionKey()
		for !workload.IsPartitionDone() && atomic.LoadUint32(&stopAll) == 0 && batchSize < arguments.RowsPerRequest {
			ck := workload.NextClusteringKey()
			batchSize++
			value := GenerateData(currentPk, ck, arguments.ClusteringRowSizeDist.Generate())
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
	query := session.Query("UPDATE " + arguments.KeyspaceName + "." + counterTableName +
		" SET c1 = c1 + ?, c2 = c2 + ?, c3 = c3 + ?, c4 = c4 + ?, c5 = c5 + ? WHERE pk = ? AND ck = ?")

	RunTest(threadResult, workload, rateLimiter, func(rb *results.TestThreadResult) (error, time.Duration) {
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
	DoReadsFromTable(arguments.TableName, session, threadResult, workload, rateLimiter)
}

func DoCounterReads(session *gocql.Session, threadResult *results.TestThreadResult, workload WorkloadGenerator, rateLimiter RateLimiter) {
	DoReadsFromTable(counterTableName, session, threadResult, workload, rateLimiter)
}

func DoReadsFromTable(table string, session *gocql.Session, threadResult *results.TestThreadResult, workload WorkloadGenerator, rateLimiter RateLimiter) {
	var request string
	var selectFields string
	if table == arguments.TableName {
		selectFields = "pk, ck, v"
	} else {
		selectFields = "pk, ck, c1, c2, c3, c4, c5"
	}
	switch {
	case arguments.InRestriction:
		arr := make([]string, arguments.RowsPerRequest)
		for i := 0; i < arguments.RowsPerRequest; i++ {
			arr[i] = "?"
		}
		request = fmt.Sprintf("SELECT %s FROM %s.%s WHERE pk = ? AND ck IN (%s)", selectFields, arguments.KeyspaceName, table, strings.Join(arr, ", "))
	case arguments.ProvideUpperBound:
		request = fmt.Sprintf("SELECT %s FROM %s.%s WHERE pk = ? AND ck >= ? AND ck < ?", selectFields, arguments.KeyspaceName, table)
	case arguments.NoLowerBound:
		request = fmt.Sprintf("SELECT %s FROM %s.%s WHERE pk = ? LIMIT %d", selectFields, arguments.KeyspaceName, table, arguments.RowsPerRequest)
	default:
		request = fmt.Sprintf("SELECT %s FROM %s.%s WHERE pk = ? AND ck >= ? LIMIT %d", selectFields, arguments.KeyspaceName, table, arguments.RowsPerRequest)
	}
	query := session.Query(request)

	RunTest(threadResult, workload, rateLimiter, func(rb *results.TestThreadResult) (error, time.Duration) {
		pk := workload.NextPartitionKey()

		var bound *gocql.Query
		switch {
		case arguments.InRestriction:
			args := make([]interface{}, 1, arguments.RowsPerRequest+1)
			args[0] = pk
			for i := 0; i < arguments.RowsPerRequest; i++ {
				if workload.IsPartitionDone() {
					args = append(args, 0)
				} else {
					args = append(args, workload.NextClusteringKey())
				}
			}
			bound = query.Bind(args...)
		case arguments.NoLowerBound:
			bound = query.Bind(pk)
		default:
			ck := workload.NextClusteringKey()
			if arguments.ProvideUpperBound {
				bound = query.Bind(pk, ck, ck+int64(arguments.RowsPerRequest))
			} else {
				bound = query.Bind(pk, ck)
			}
		}

		var resPk, resCk int64
		var value []byte
		requestStart := time.Now()
		iter := bound.Iter()
		if table == arguments.TableName {
			for iter.Scan(&resPk, &resCk, &value) {
				rb.IncRows()
				if arguments.ValidateData {
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
				if arguments.ValidateData {
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
	request := fmt.Sprintf("SELECT * FROM %s.%s WHERE token(pk) >= ? AND token(pk) <= ?", arguments.KeyspaceName, arguments.TableName)
	query := session.Query(request)

	RunTest(threadResult, workload, rateLimiter, func(rb *results.TestThreadResult) (error, time.Duration) {
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
