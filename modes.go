package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	stdErrors "errors"
	"fmt"
	"log"
	"math"
	"math/rand/v2"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gocql/gocql"
	"github.com/pkg/errors"

	"github.com/scylladb/scylla-bench/pkg/results"
	"github.com/scylladb/scylla-bench/pkg/workloads"
	"github.com/scylladb/scylla-bench/random"
)

var reportInterval = 1 * time.Second

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
	StartTime           time.Time
	Period              time.Duration
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

func NewRateLimiter(maximumRate int, _ time.Duration) RateLimiter {
	if maximumRate == 0 {
		return &UnlimitedRateLimiter{}
	}
	period := time.Duration(int64(time.Second) / int64(maximumRate))
	return &MaximumRateLimiter{
		Period:              period,
		StartTime:           time.Now(),
		CompletedOperations: 0,
	}
}

func RunConcurrently(
	maximumRate int,
	workload func(id int, testResult *results.TestThreadResult, rateLimiter RateLimiter),
) *results.TestResults {
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
	workload  workloads.Generator
	iteration uint
}

func NewTestIterator(workload workloads.Generator) *TestIterator {
	return &TestIterator{workload: workload, iteration: 0}
}

func (ti *TestIterator) IsDone() bool {
	if atomic.LoadUint32(&stopAll) != 0 {
		return true
	}

	if ti.workload.IsDone() {
		if ti.iteration+1 == iterations {
			return true
		}
		ti.workload.Restart()
		ti.iteration++
		return false
	}

	return false
}

// used to calculate exponentially growing time
// copy-pasted from the private function in the 'gocql@v1.7.3/policies.go' library
func getExponentialTime(minimum, maximum time.Duration, attempts int) time.Duration {
	if minimum <= 0 {
		minimum = 100 * time.Millisecond
	}

	if maximum <= 0 {
		maximum = 10 * time.Second
	}

	minFloat := float64(minimum)
	napDuration := minFloat * math.Pow(2, float64(attempts-1))
	// add some jitter
	napDuration += rand.Float64()*minFloat - (minFloat / 2)
	if napDuration > float64(maximum) {
		return maximum
	}
	return time.Duration(napDuration)
}

var (
	totalErrors          int32
	totalErrorsPrintOnce sync.Once
	errDoNotRegister     = errors.New("do not register this test results")
)

//
//nolint:lll
func RunTest(
	threadResult *results.TestThreadResult,
	workload workloads.Generator,
	rateLimiter RateLimiter,
	test func(rb *results.TestThreadResult) (time.Duration, error),
) {
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

		rawLatency, err := test(threadResult)
		endTime := time.Now()
		switch {
		case err == nil:
			errorsAtRow = 0
			threadResult.RecordRawLatency(rawLatency)
			threadResult.RecordCoFixedLatency(endTime.Sub(expectedStartTime))
		case stdErrors.Is(err, errDoNotRegister):
			// Do not register test run results
		default:
			errorsAtRow++
			atomic.AddInt32(&totalErrors, 1)
			threadResult.IncErrors()
			log.Print(err)
			if rawLatency > errorToTimeoutCutoffTime {
				// Consider this error to be timeout error and register it in histogram
				threadResult.RecordRawLatency(rawLatency)
				threadResult.RecordCoFixedLatency(endTime.Sub(expectedStartTime))
			}
		}

		now := time.Now()
		if maxErrorsAtRow > 0 && errorsAtRow >= maxErrorsAtRow {
			threadResult.SubmitCriticalError(fmt.Errorf(
				"error limit (maxErrorsAtRow) of %d errors is reached", errorsAtRow))
		}
		if maxErrors > 0 && int(atomic.LoadInt32(&totalErrors)) >= maxErrors {
			totalErrorsPrintOnce.Do(func() {
				threadResult.SubmitCriticalError(fmt.Errorf(
					"error limit (maxErrors) of %d errors is reached", maxErrors))
			})
		}
		if results.GlobalErrorFlag {
			threadResult.ResultChannel <- *threadResult.PartialResult
			threadResult.ResetPartialResult()
			break
		}
		if now.Sub(partialStart) > reportInterval {
			threadResult.ResultChannel <- *threadResult.PartialResult
			threadResult.ResetPartialResult()
			partialStart = partialStart.Add(reportInterval)
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

func GenerateData(pk, ck, size int64, validateData bool) ([]byte, error) {
	if !validateData {
		return make([]byte, size), nil
	}

	buf := bytes.Buffer{}
	buf.Grow(int(size))

	if size < generatedDataHeaderSize {
		if err := binary.Write(&buf, binary.LittleEndian, int8(size)); err != nil {
			return nil, err
		}
		if err := binary.Write(&buf, binary.LittleEndian, pk^ck); err != nil {
			return nil, err
		}
	} else {
		if err := binary.Write(&buf, binary.LittleEndian, size); err != nil {
			return nil, err
		}
		if err := binary.Write(&buf, binary.LittleEndian, pk); err != nil {
			return nil, err
		}
		if err := binary.Write(&buf, binary.LittleEndian, ck); err != nil {
			return nil, err
		}
		if size < generatedDataMinSize {
			for i := generatedDataHeaderSize; i < size; i++ {
				if err := binary.Write(&buf, binary.LittleEndian, int8(0)); err != nil {
					return nil, err
				}
			}
		} else {
			payload := make([]byte, size-generatedDataHeaderSize-sha256.Size)
			_, _ = random.String(payload)
			if err := binary.Write(&buf, binary.LittleEndian, payload); err != nil {
				return nil, err
			}
			if err := binary.Write(&buf, binary.LittleEndian, sha256.Sum256(payload)); err != nil {
				return nil, err
			}
		}
	}

	value := make([]byte, size)
	copy(value, buf.Bytes())
	return value, nil
}

func ValidateData(pk, ck int64, data []byte, validateData bool) error {
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
		return errors.Errorf(
			"actual size of value (%d) doesn't match size stored in value (%d)",
			size,
			storedSize,
		)
	}

	// There is no random payload for sizes < minFullSize
	if size < generatedDataMinSize {
		expectedBuf, err := GenerateData(pk, ck, size, validateData)
		if err != nil {
			return errors.Wrap(err, "failed to generate expected data for validation")
		}

		if err = buf.UnreadByte(); err != nil {
			return err
		}

		if !bytes.Equal(buf.Bytes(), expectedBuf) {
			return errors.Errorf(
				"actual value doesn't match expected value:\nexpected: %x\nactual: %x",
				expectedBuf,
				buf.Bytes(),
			)
		}

		return nil
	}

	var storedPk, storedCk int64

	// Validate pk

	if err := binary.Read(buf, binary.LittleEndian, &storedPk); err != nil {
		return errors.Wrap(err, "failed to validate data, cannot read pk from value")
	}
	if storedPk != pk {
		return errors.Errorf("actual pk (%d) doesn't match pk stored in value (%d)", pk, storedPk)
	}

	// Validate ck
	if err := binary.Read(buf, binary.LittleEndian, &storedCk); err != nil {
		return errors.Wrap(err, "failed to validate data, cannot read pk from value")
	}

	if storedCk != ck {
		return errors.Errorf("actual ck (%d) doesn't match ck stored in value (%d)", ck, storedCk)
	}

	// Validate checksum over the payload
	payload := make([]byte, size-generatedDataHeaderSize-sha256.Size)

	if err := binary.Read(buf, binary.LittleEndian, payload); err != nil {
		return errors.Wrap(err, "failed to verify checksum, cannot read payload from value")
	}

	calculatedChecksumArray := sha256.Sum256(payload)
	calculatedChecksum := calculatedChecksumArray[0:]

	var storedChecksum [sha256.Size]byte

	if err := binary.Read(buf, binary.LittleEndian, storedChecksum[:]); err != nil {
		return errors.Wrap(err, "failed to verify checksum, cannot read checksum from value")
	}

	if !bytes.Equal(calculatedChecksum, storedChecksum[:]) {
		return fmt.Errorf(
			"corrupt checksum or data: calculated checksum (%x) doesn't match stored checksum (%x) over data\n%x",
			calculatedChecksum,
			storedChecksum,
			payload,
		)
	}

	return nil
}

func handleSbRetryError(queryStr string, err error, currentAttempts int) error {
	if retryNumber <= currentAttempts {
		if err != nil {
			return fmt.Errorf(
				"%s || ERROR: %w attempts applied: %d",
				queryStr,
				err,
				currentAttempts,
			)
		}
		return err
	}

	sleepTime := getExponentialTime(retryPolicy.Min, retryPolicy.Max, currentAttempts)
	log.Printf("%s || retry: attempt №%d, sleep for %s", queryStr, currentAttempts, sleepTime)
	time.Sleep(sleepTime)
	return nil
}

func DoWrites(
	session *gocql.Session,
	threadResult *results.TestThreadResult,
	workload workloads.Generator,
	rateLimiter RateLimiter,
	validateData bool,
) {
	RunTest(
		threadResult,
		workload,
		rateLimiter,
		func(rb *results.TestThreadResult) (time.Duration, error) {
			request := fmt.Sprintf(
				"INSERT INTO %s.%s (pk, ck, v) VALUES (?, ?, ?)",
				keyspaceName,
				tableName,
			)
			query := session.Query(request)
			defer query.Release()
			pk := workload.NextPartitionKey()
			ck := workload.NextClusteringKey()
			value, err := GenerateData(pk, ck, clusteringRowSizeDist.Generate(), validateData)
			if err != nil {
				panic(err)
			}
			bound := query.Bind(pk, ck, value)

			queryStr := ""
			currentAttempts := 0
			for {
				requestStart := time.Now()
				err = bound.Exec()
				requestEnd := time.Now()

				if err == nil {
					rb.IncOps()
					rb.IncRows()
					latency := requestEnd.Sub(requestStart)
					return latency, nil
				}
				if retryHandler == "sb" {
					if queryStr == "" {
						// NOTE: use custom query string instead of 'query.String()' to avoid huge values printings
						queryStr = fmt.Sprintf(
							"[query statement=%q values=%+v consistency=%s]",
							request,
							[]any{pk, ck, "<" + strconv.Itoa(len(value)) + "-bytes-value>"},
							query.GetConsistency(),
						)
					}
					err = handleSbRetryError(queryStr, err, currentAttempts)
				}
				if err != nil {
					return time.Duration(0), err
				}
				currentAttempts++
			}
		},
	)
}

func DoBatchedWrites(
	session *gocql.Session,
	threadResult *results.TestThreadResult,
	workload workloads.Generator,
	rateLimiter RateLimiter,
	validateData bool,
) {
	request := fmt.Sprintf(
		"INSERT INTO %s.%s (pk, ck, v) VALUES (?, ?, ?)",
		keyspaceName,
		tableName,
	)

	RunTest(
		threadResult,
		workload,
		rateLimiter,
		func(rb *results.TestThreadResult) (time.Duration, error) {
			batch := session.Batch(gocql.UnloggedBatch)
			batchSize := 0

			currentPk := workload.NextPartitionKey()
			var startingCk int64
			valuesSizesSummary := 0
			for !workload.IsPartitionDone() && batchSize < rowsPerRequest {
				if atomic.LoadUint32(&stopAll) != 0 {
					return 0, errDoNotRegister
				}
				ck := workload.NextClusteringKey()
				if batchSize == 0 {
					startingCk = ck
				}
				batchSize++

				value, err := GenerateData(
					currentPk,
					ck,
					clusteringRowSizeDist.Generate(),
					validateData,
				)
				if err != nil {
					log.Panic(err)
				}
				valuesSizesSummary += len(value)
				batch.Query(request, currentPk, ck, value)
			}
			if batchSize == 0 {
				return 0, errDoNotRegister
			}
			endingCk := int(startingCk) + batchSize - 1
			avgValueSize := valuesSizesSummary / batchSize

			queryStr := ""
			currentAttempts := 0
			for {
				requestStart := time.Now()
				err := session.ExecuteBatch(batch)
				requestEnd := time.Now()

				if err == nil {
					rb.IncOps()
					rb.AddRows(batchSize)
					latency := requestEnd.Sub(requestStart)
					return latency, nil
				}
				if retryHandler == "sb" {
					if queryStr == "" {
						queryStr = fmt.Sprintf(
							"BATCH >>> [query statement=%q pk=%v cks=%v..%v avgValueSize=%v consistency=%s]",
							request,
							currentPk,
							startingCk,
							endingCk,
							avgValueSize,
							batch.GetConsistency(),
						)
					}
					err = handleSbRetryError(queryStr, err, currentAttempts)
				}
				if err != nil {
					return time.Duration(0), err
				}
				currentAttempts++
			}
		},
	)
}

func DoCounterUpdates(
	session *gocql.Session,
	threadResult *results.TestThreadResult,
	workload workloads.Generator,
	rateLimiter RateLimiter,
	_ bool,
) {
	RunTest(
		threadResult,
		workload,
		rateLimiter,
		func(rb *results.TestThreadResult) (time.Duration, error) {
			pk := workload.NextPartitionKey()
			ck := workload.NextClusteringKey()

			query := session.Query("UPDATE "+keyspaceName+"."+counterTableName+
				" SET c1 = c1 + ?, c2 = c2 + ?, c3 = c3 + ?, c4 = c4 + ?, c5 = c5 + ? WHERE pk = ? AND ck = ?").
				Bind(ck, ck+1, ck+2, ck+3, ck+4, pk, ck)
			defer query.Release()
			queryStr := ""
			currentAttempts := 0
			for {
				requestStart := time.Now()
				err := query.Exec()
				requestEnd := time.Now()

				if err == nil {
					rb.IncOps()
					rb.IncRows()
					latency := requestEnd.Sub(requestStart)
					return latency, nil
				}
				if retryHandler == "sb" {
					if queryStr == "" {
						queryStr = query.String()
					}
					err = handleSbRetryError(queryStr, err, currentAttempts)
				}
				if err != nil {
					return time.Duration(0), err
				}
				currentAttempts++
			}
		},
	)
}

func DoReads(
	session *gocql.Session,
	threadResult *results.TestThreadResult,
	workload workloads.Generator,
	rateLimiter RateLimiter,
	validateData bool,
) {
	DoReadsFromTable(tableName, session, threadResult, workload, rateLimiter, validateData)
}

func DoCounterReads(
	session *gocql.Session,
	threadResult *results.TestThreadResult,
	workload workloads.Generator,
	rateLimiter RateLimiter,
	validateData bool,
) {
	DoReadsFromTable(counterTableName, session, threadResult, workload, rateLimiter, validateData)
}

func BuildReadQueryString(table, orderBy string) string {
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
		return fmt.Sprintf(
			"SELECT %s FROM %s.%s WHERE pk = ? AND ck IN (%s) %s %s",
			selectFields,
			keyspaceName,
			table,
			strings.TrimRight(strings.Repeat("?,", rowsPerRequest), ","),
			orderBy,
			bypassCacheClause,
		)
	case provideUpperBound:
		return fmt.Sprintf(
			"SELECT %s FROM %s.%s WHERE pk = ? AND ck >= ? AND ck < ? %s %s",
			selectFields,
			keyspaceName,
			table,
			orderBy,
			bypassCacheClause,
		)
	case noLowerBound:
		return fmt.Sprintf(
			"SELECT %s FROM %s.%s WHERE pk = ? %s LIMIT %d %s",
			selectFields,
			keyspaceName,
			table,
			orderBy,
			rowsPerRequest,
			bypassCacheClause,
		)
	default:
		return fmt.Sprintf(
			"SELECT %s FROM %s.%s WHERE pk = ? AND ck >= ? %s LIMIT %d %s",
			selectFields,
			keyspaceName,
			table,
			orderBy,
			rowsPerRequest,
			bypassCacheClause,
		)
	}
}

func executeReadsQuery(query *gocql.Query, table string, rb *results.TestThreadResult, validateData bool) error {
	iter := query.Iter()
	var err error
	defer func() {
		err = iter.Close()
	}()

	var (
		resPk, resCk int64
		value        []byte
	)

	if table == tableName {
		for iter.Scan(&resPk, &resCk, &value) {
			rb.IncRows()
			if !validateData {
				continue
			}

			scanErr := ValidateData(resPk, resCk, value, validateData)
			if scanErr != nil {
				rb.IncErrors()
				log.Printf("data corruption in pk(%d), ck(%d): %s", resPk, resCk, scanErr)
			}
		}

		return err
	}

	var c1, c2, c3, c4, c5 int64
	for iter.Scan(&resPk, &resCk, &c1, &c2, &c3, &c4, &c5) {
		rb.IncRows()
		if !validateData {
			continue
		}

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

	return err
}

func DoReadsFromTable(
	table string,
	session *gocql.Session,
	threadResult *results.TestThreadResult,
	workload workloads.Generator,
	rateLimiter RateLimiter,
	validateData bool,
) {
	counter, numOfOrderings := 0, len(selectOrderByParsed)
	RunTest(threadResult, workload, rateLimiter, func(rb *results.TestThreadResult) (time.Duration, error) {
		counter++
		pk := workload.NextPartitionKey()
		query := session.Query(BuildReadQueryString(table, selectOrderByParsed[counter%numOfOrderings]))
		defer query.Release()

		switch {
		case inRestriction:
			args := make([]any, 1, rowsPerRequest+1)
			args[0] = pk
			for range rowsPerRequest {
				if workload.IsPartitionDone() {
					args = append(args, 0)
				} else {
					args = append(args, workload.NextClusteringKey())
				}
			}
			query.Bind(args...)
		case noLowerBound:
			query.Bind(pk)
		default:
			ck := workload.NextClusteringKey()
			if provideUpperBound {
				query.Bind(pk, ck, ck+int64(rowsPerRequest))
			} else {
				query.Bind(pk, ck)
			}
		}

		queryStr := query.String()

		for currentAttempts := 0; ; currentAttempts++ {
			requestStart := time.Now()
			err := executeReadsQuery(query, table, rb, validateData)
			requestEnd := time.Now()

			if err == nil {
				rb.IncOps()
				return requestEnd.Sub(requestStart), nil
			}

			if retryHandler == "sb" {
				err = handleSbRetryError(queryStr, err, currentAttempts)
			}

			if err != nil {
				return time.Duration(0), err
			}
		}
	})
}

func DoScanTable(session *gocql.Session, threadResult *results.TestThreadResult, workload workloads.Generator, rateLimiter RateLimiter, _ bool) {
	request := fmt.Sprintf("SELECT * FROM %s.%s WHERE token(pk) >= ? AND token(pk) <= ?", keyspaceName, tableName)

	RunTest(threadResult, workload, rateLimiter, func(rb *results.TestThreadResult) (time.Duration, error) {
		query := session.Query(request)
		defer query.Release()
		currentRange := workload.NextTokenRange()

		queryStr := query.String()

		for currentAttempts := 0; ; currentAttempts++ {
			requestStart := time.Now()
			query.Bind(currentRange.Start, currentRange.End)
			iter := query.Iter()
			for iter.Scan(nil, nil, nil) {
				rb.IncRows()
			}
			requestEnd := time.Now()
			err := iter.Close()

			if err == nil {
				rb.IncOps()
				return requestEnd.Sub(requestStart), nil
			}

			if retryHandler == "sb" {
				if err = handleSbRetryError(queryStr, err, currentAttempts); err != nil {
					return time.Duration(0), err
				}
			}
		}
	})
}
