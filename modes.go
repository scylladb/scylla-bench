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

	"github.com/scylladb/scylla-bench/internal/clock"
	"github.com/scylladb/scylla-bench/pkg/rate_limiter"
	"github.com/scylladb/scylla-bench/pkg/worker"
	"github.com/scylladb/scylla-bench/pkg/workloads"
	"github.com/scylladb/scylla-bench/random"
)

// globalClock is the process-wide real clock, initialized at package load time.
// It is used as the default for DefaultExecutionConfig() and normalized().
var globalClock clock.Clock = clock.New()

type ExecutionConfig struct {
	Clock                 clock.Clock
	ClusteringRowSizeDist random.Distribution
	RetryPolicy           *gocql.ExponentialBackoffRetryPolicy
	StopAll               *atomic.Uint32
	CriticalErrorFlag     *atomic.Bool
	MixedOperationCounter *atomic.Uint64
	TotalErrors           *atomic.Int32
	TotalErrorsPrintOnce  *sync.Once
	TableName             string
	CounterTableName      string
	KeyspaceName          string
	RetryHandler          string
	SelectOrderByParsed   []string
	RowsPerRequest        int
	MaxErrorsAtRow        int
	MaxErrors             int
	RetryNumber           int
	Iterations            uint
	InRestriction         bool
	NoLowerBound          bool
	BypassCache           bool
	ProvideUpperBound     bool
}

func DefaultExecutionConfig() ExecutionConfig {
	return ExecutionConfig{
		Clock:                 globalClock,
		KeyspaceName:          keyspaceName,
		TableName:             tableName,
		CounterTableName:      counterTableName,
		ClusteringRowSizeDist: clusteringRowSizeDist,
		RowsPerRequest:        rowsPerRequest,
		ProvideUpperBound:     provideUpperBound,
		InRestriction:         inRestriction,
		SelectOrderByParsed:   append([]string(nil), selectOrderByParsed...),
		NoLowerBound:          noLowerBound,
		BypassCache:           bypassCache,
		MaxErrorsAtRow:        maxErrorsAtRow,
		MaxErrors:             maxErrors,
		RetryHandler:          retryHandler,
		RetryNumber:           retryNumber,
		RetryPolicy:           retryPolicy,
		Iterations:            iterations,
		StopAll:               &stopAll,
		CriticalErrorFlag:     &resultsGlobalCriticalErrorFlag,
		MixedOperationCounter: &globalMixedOperationCount,
		TotalErrors:           &globalTotalErrors,
		TotalErrorsPrintOnce:  &globalTotalErrorsPrintOnce,
	}
}

func (cfg ExecutionConfig) normalized() ExecutionConfig {
	if cfg.Clock == nil {
		cfg.Clock = globalClock
	}
	if cfg.KeyspaceName == "" {
		cfg.KeyspaceName = keyspaceName
	}
	if cfg.TableName == "" {
		cfg.TableName = tableName
	}
	if cfg.CounterTableName == "" {
		cfg.CounterTableName = counterTableName
	}
	if cfg.ClusteringRowSizeDist == nil {
		cfg.ClusteringRowSizeDist = clusteringRowSizeDist
	}
	if cfg.RowsPerRequest == 0 {
		cfg.RowsPerRequest = rowsPerRequest
	}
	if len(cfg.SelectOrderByParsed) == 0 {
		cfg.SelectOrderByParsed = append([]string(nil), selectOrderByParsed...)
	}
	if cfg.Iterations == 0 {
		cfg.Iterations = iterations
	}
	if cfg.StopAll == nil {
		cfg.StopAll = &stopAll
	}
	if cfg.CriticalErrorFlag == nil {
		cfg.CriticalErrorFlag = &resultsGlobalCriticalErrorFlag
	}
	if cfg.MixedOperationCounter == nil {
		cfg.MixedOperationCounter = &globalMixedOperationCount
	}
	if cfg.TotalErrors == nil {
		cfg.TotalErrors = &globalTotalErrors
	}
	if cfg.TotalErrorsPrintOnce == nil {
		cfg.TotalErrorsPrintOnce = &globalTotalErrorsPrintOnce
	}
	if cfg.RetryHandler == "" {
		cfg.RetryHandler = retryHandler
	}
	if cfg.RetryNumber == 0 && retryNumber != 0 {
		cfg.RetryNumber = retryNumber
	}
	if cfg.RetryPolicy == nil {
		cfg.RetryPolicy = retryPolicy
	}
	return cfg
}

var resultsGlobalCriticalErrorFlag atomic.Bool

// Global atomic counter for mixed mode operations to ensure true 50/50 distribution across threads
var globalMixedOperationCount atomic.Uint64

var (
	globalTotalErrors          atomic.Int32
	globalTotalErrorsPrintOnce sync.Once
)

type TestIterator struct {
	workload  workloads.Generator
	config    ExecutionConfig
	iteration uint
}

func NewTestIterator(workload workloads.Generator, config ExecutionConfig) *TestIterator {
	return &TestIterator{workload: workload, config: config.normalized(), iteration: 0}
}

func (ti *TestIterator) IsDone() bool {
	if ti.config.StopAll.Load() != 0 {
		return true
	}

	if ti.workload.IsDone() {
		if ti.iteration+1 == ti.config.Iterations {
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

var errDoNotRegister = errors.New("do not register this test results")

func RunTest(
	config ExecutionConfig,
	w *worker.Worker,
	workload workloads.Generator,
	rateLimiter rate_limiter.RateLimiter,
	test func(w *worker.Worker) (time.Duration, error),
) {
	config = config.normalized()
	iter := NewTestIterator(workload, config)
	errorsAtRow := 0
	for !iter.IsDone() {
		rateLimiter.Wait()

		expectedStartTime := rateLimiter.Expected()
		if expectedStartTime.IsZero() {
			expectedStartTime = config.Clock.Now()
		}

		rawLatency, err := test(w)
		endTime := config.Clock.Now()
		switch {
		case err == nil:
			errorsAtRow = 0
			w.RecordRawLatency(rawLatency)
			w.RecordCoFixedLatency(endTime.Sub(expectedStartTime))
		case stdErrors.Is(err, errDoNotRegister):
			// Do not register test run results
		default:
			errorsAtRow++
			config.TotalErrors.Add(1)
			w.IncErrors()
			log.Print(err)
			if rawLatency > errorToTimeoutCutoffTime {
				// Consider this error to be timeout error and register it in histogram
				w.RecordRawLatency(rawLatency)
				w.RecordCoFixedLatency(endTime.Sub(expectedStartTime))
			}
		}

		if config.MaxErrorsAtRow > 0 && errorsAtRow >= config.MaxErrorsAtRow {
			w.SubmitCriticalError(fmt.Errorf(
				"error limit (maxErrorsAtRow) of %d errors is reached", errorsAtRow))
		}
		if config.MaxErrors > 0 && int(config.TotalErrors.Load()) >= config.MaxErrors {
			config.TotalErrorsPrintOnce.Do(func() {
				w.SubmitCriticalError(fmt.Errorf(
					"error limit (maxErrors) of %d errors is reached", config.MaxErrors))
			})
		}
		if worker.GlobalErrorFlag.Load() {
			break
		}
	}
	w.StopReporting()
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

		// Compare the original data slice directly, not the buffer's remaining bytes.
		// After reading the size field (either int8 or int64), the buffer position has advanced,
		// and buf.Bytes() would return incomplete data. We need to compare the full original data.
		if !bytes.Equal(data, expectedBuf) {
			return errors.Errorf(
				"actual value doesn't match expected value:\nexpected: %x\nactual: %x",
				expectedBuf,
				data,
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

func handleSbRetryErrorWithConfig(config ExecutionConfig, queryStr string, err error, currentAttempts int) error {
	if config.RetryNumber <= currentAttempts {
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

	sleepTime := getExponentialTime(config.RetryPolicy.Min, config.RetryPolicy.Max, currentAttempts)
	log.Printf("%s || retry: attempt №%d, sleep for %s", queryStr, currentAttempts, sleepTime)
	config.Clock.Sleep(sleepTime)
	return nil
}

// createWriteTestFunc creates a test function for write operations that can be reused
func createWriteTestFuncWithConfig(
	config ExecutionConfig,
	session *gocql.Session,
	workload workloads.Generator,
	validateData bool,
) func(w *worker.Worker) (time.Duration, error) {
	config = config.normalized()
	return func(w *worker.Worker) (time.Duration, error) {
		request := fmt.Sprintf(
			"INSERT INTO %s.%s (pk, ck, v) VALUES (?, ?, ?)",
			config.KeyspaceName,
			config.TableName,
		)
		query := session.Query(request)
		defer query.Release()
		pk := workload.NextPartitionKey()
		ck := workload.NextClusteringKey()
		value, err := GenerateData(pk, ck, config.ClusteringRowSizeDist.Generate(), validateData)
		if err != nil {
			panic(err)
		}
		bound := query.Bind(pk, ck, value)

		queryStr := ""
		currentAttempts := 0
		for {
			requestStart := config.Clock.Now()
			err = bound.Exec()
			requestEnd := config.Clock.Now()

			if err == nil {
				w.IncOps()
				w.IncRows()
				latency := requestEnd.Sub(requestStart)
				return latency, nil
			}
			if config.RetryHandler == "sb" {
				if queryStr == "" {
					// NOTE: use custom query string instead of 'query.String()' to avoid huge values printings
					queryStr = fmt.Sprintf(
						"[query statement=%q values=%+v consistency=%s]",
						request,
						[]any{pk, ck, "<" + strconv.Itoa(len(value)) + "-bytes-value>"},
						query.GetConsistency(),
					)
				}
				err = handleSbRetryErrorWithConfig(config, queryStr, err, currentAttempts)
			}
			if err != nil {
				return time.Duration(0), err
			}
			currentAttempts++
		}
	}
}

func DoWrites(
	session *gocql.Session,
	w *worker.Worker,
	workload workloads.Generator,
	rateLimiter rate_limiter.RateLimiter,
	validateData bool,
) {
	RunTest(
		DefaultExecutionConfig(),
		w,
		workload,
		rateLimiter,
		createWriteTestFuncWithConfig(DefaultExecutionConfig(), session, workload, validateData),
	)
}

func DoWritesWithConfig(
	config ExecutionConfig,
	session *gocql.Session,
	w *worker.Worker,
	workload workloads.Generator,
	rateLimiter rate_limiter.RateLimiter,
	validateData bool,
) {
	RunTest(
		config,
		w,
		workload,
		rateLimiter,
		createWriteTestFuncWithConfig(config, session, workload, validateData),
	)
}

func DoBatchedWritesWithConfig(
	config ExecutionConfig,
	session *gocql.Session,
	w *worker.Worker,
	workload workloads.Generator,
	rateLimiter rate_limiter.RateLimiter,
	validateData bool,
) {
	config = config.normalized()
	request := fmt.Sprintf(
		"INSERT INTO %s.%s (pk, ck, v) VALUES (?, ?, ?)",
		config.KeyspaceName,
		config.TableName,
	)

	RunTest(
		config,
		w,
		workload,
		rateLimiter,
		func(rb *worker.Worker) (time.Duration, error) {
			batch := session.Batch(gocql.UnloggedBatch)
			batchSize := 0

			currentPk := workload.NextPartitionKey()
			var startingCk int64
			valuesSizesSummary := 0
			for !workload.IsPartitionDone() && batchSize < config.RowsPerRequest {
				if config.StopAll.Load() != 0 {
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
					config.ClusteringRowSizeDist.Generate(),
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
				requestStart := config.Clock.Now()
				err := session.ExecuteBatch(batch)
				requestEnd := config.Clock.Now()

				if err == nil {
					rb.IncOps()
					rb.AddRows(uint64(batchSize))
					latency := requestEnd.Sub(requestStart)
					return latency, nil
				}
				if config.RetryHandler == "sb" {
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
					err = handleSbRetryErrorWithConfig(config, queryStr, err, currentAttempts)
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
	w *worker.Worker,
	workload workloads.Generator,
	rateLimiter rate_limiter.RateLimiter,
	validateData bool,
) {
	DoBatchedWritesWithConfig(DefaultExecutionConfig(), session, w, workload, rateLimiter, validateData)
}

func DoCounterUpdates(
	session *gocql.Session,
	w *worker.Worker,
	workload workloads.Generator,
	rateLimiter rate_limiter.RateLimiter,
	_ bool,
) {
	DoCounterUpdatesWithConfig(DefaultExecutionConfig(), session, w, workload, rateLimiter, false)
}

func DoCounterUpdatesWithConfig(
	config ExecutionConfig,
	session *gocql.Session,
	w *worker.Worker,
	workload workloads.Generator,
	rateLimiter rate_limiter.RateLimiter,
	_ bool,
) {
	config = config.normalized()
	RunTest(
		config,
		w,
		workload,
		rateLimiter,
		func(rb *worker.Worker) (time.Duration, error) {
			pk := workload.NextPartitionKey()
			ck := workload.NextClusteringKey()

			query := session.Query("UPDATE "+config.KeyspaceName+"."+config.CounterTableName+
				" SET c1 = c1 + ?, c2 = c2 + ?, c3 = c3 + ?, c4 = c4 + ?, c5 = c5 + ? WHERE pk = ? AND ck = ?").
				Bind(ck, ck+1, ck+2, ck+3, ck+4, pk, ck)
			defer query.Release()
			queryStr := ""
			currentAttempts := 0
			for {
				requestStart := config.Clock.Now()
				err := query.Exec()
				requestEnd := config.Clock.Now()

				if err == nil {
					rb.IncOps()
					rb.IncRows()
					latency := requestEnd.Sub(requestStart)
					return latency, nil
				}
				if config.RetryHandler == "sb" {
					if queryStr == "" {
						queryStr = query.String()
					}
					err = handleSbRetryErrorWithConfig(config, queryStr, err, currentAttempts)
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
	w *worker.Worker,
	workload workloads.Generator,
	rateLimiter rate_limiter.RateLimiter,
	validateData bool,
) {
	config := DefaultExecutionConfig()
	DoReadsFromTableWithConfig(config, config.TableName, session, w, workload, rateLimiter, validateData)
}

func DoCounterReads(
	session *gocql.Session,
	w *worker.Worker,
	workload workloads.Generator,
	rateLimiter rate_limiter.RateLimiter,
	validateData bool,
) {
	config := DefaultExecutionConfig()
	DoReadsFromTableWithConfig(config, config.CounterTableName, session, w, workload, rateLimiter, validateData)
}

func BuildReadQueryString(table, orderBy string) string {
	return BuildReadQueryStringWithConfig(DefaultExecutionConfig(), table, orderBy)
}

func BuildReadQueryStringWithConfig(config ExecutionConfig, table, orderBy string) string {
	config = config.normalized()
	var selectFields string
	if table == config.TableName {
		selectFields = "pk, ck, v"
	} else {
		selectFields = "pk, ck, c1, c2, c3, c4, c5"
	}

	var bypassCacheClause string
	if config.BypassCache {
		bypassCacheClause = "BYPASS CACHE"
	}

	switch {
	case config.InRestriction:
		return fmt.Sprintf(
			"SELECT %s FROM %s.%s WHERE pk = ? AND ck IN (%s) %s %s",
			selectFields,
			config.KeyspaceName,
			table,
			strings.TrimRight(strings.Repeat("?,", config.RowsPerRequest), ","),
			orderBy,
			bypassCacheClause,
		)
	case config.ProvideUpperBound:
		return fmt.Sprintf(
			"SELECT %s FROM %s.%s WHERE pk = ? AND ck >= ? AND ck < ? %s %s",
			selectFields,
			config.KeyspaceName,
			table,
			orderBy,
			bypassCacheClause,
		)
	case config.NoLowerBound:
		return fmt.Sprintf(
			"SELECT %s FROM %s.%s WHERE pk = ? %s LIMIT %d %s",
			selectFields,
			config.KeyspaceName,
			table,
			orderBy,
			config.RowsPerRequest,
			bypassCacheClause,
		)
	default:
		return fmt.Sprintf(
			"SELECT %s FROM %s.%s WHERE pk = ? AND ck >= ? %s LIMIT %d %s",
			selectFields,
			config.KeyspaceName,
			table,
			orderBy,
			config.RowsPerRequest,
			bypassCacheClause,
		)
	}
}

func executeReadsQuery(config ExecutionConfig, query *gocql.Query, table string, rb *worker.Worker, validateData bool) error {
	config = config.normalized()
	iter := query.Iter()
	var err error
	defer func() {
		err = iter.Close()
	}()

	var (
		resPk, resCk int64
		value        []byte
	)

	if table == config.TableName {
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

// createReadTestFunc creates a test function for read operations that can be reused
func createReadTestFuncWithConfig(
	config ExecutionConfig,
	table string,
	session *gocql.Session,
	workload workloads.Generator,
	validateData bool,
) func(w *worker.Worker) (time.Duration, error) {
	config = config.normalized()
	counter, numOfOrderings := 0, len(config.SelectOrderByParsed)
	return func(rb *worker.Worker) (time.Duration, error) {
		counter++
		pk := workload.NextPartitionKey()
		query := session.Query(
			BuildReadQueryStringWithConfig(
				config,
				table,
				config.SelectOrderByParsed[counter%numOfOrderings],
			),
		)
		defer query.Release()

		switch {
		case config.InRestriction:
			args := make([]any, 1, config.RowsPerRequest+1)
			args[0] = pk
			for range config.RowsPerRequest {
				if workload.IsPartitionDone() {
					args = append(args, 0)
				} else {
					args = append(args, workload.NextClusteringKey())
				}
			}
			query.Bind(args...)
		case config.NoLowerBound:
			query.Bind(pk)
		default:
			ck := workload.NextClusteringKey()
			if config.ProvideUpperBound {
				query.Bind(pk, ck, ck+int64(config.RowsPerRequest))
			} else {
				query.Bind(pk, ck)
			}
		}

		queryStr := query.String()

		for currentAttempts := 0; ; currentAttempts++ {
			requestStart := config.Clock.Now()
			err := executeReadsQuery(config, query, table, rb, validateData)
			requestEnd := config.Clock.Now()

			if err == nil {
				rb.IncOps()
				return requestEnd.Sub(requestStart), nil
			}

			if config.RetryHandler == "sb" {
				err = handleSbRetryErrorWithConfig(config, queryStr, err, currentAttempts)
			}

			if err != nil {
				return time.Duration(0), err
			}
		}
	}
}

func DoReadsFromTableWithConfig(
	config ExecutionConfig,
	table string,
	session *gocql.Session,
	w *worker.Worker,
	workload workloads.Generator,
	rateLimiter rate_limiter.RateLimiter,
	validateData bool,
) {
	RunTest(config, w, workload, rateLimiter, createReadTestFuncWithConfig(config, table, session, workload, validateData))
}

func DoReadsFromTable(
	table string,
	session *gocql.Session,
	w *worker.Worker,
	workload workloads.Generator,
	rateLimiter rate_limiter.RateLimiter,
	validateData bool,
) {
	DoReadsFromTableWithConfig(DefaultExecutionConfig(), table, session, w, workload, rateLimiter, validateData)
}

func DoScanTableWithConfig(config ExecutionConfig, session *gocql.Session, w *worker.Worker, workload workloads.Generator, rateLimiter rate_limiter.RateLimiter, _ bool) {
	config = config.normalized()
	request := fmt.Sprintf("SELECT * FROM %s.%s WHERE token(pk) >= ? AND token(pk) <= ?", config.KeyspaceName, config.TableName)

	RunTest(config, w, workload, rateLimiter, func(rb *worker.Worker) (time.Duration, error) {
		query := session.Query(request)
		defer query.Release()
		currentRange := workload.NextTokenRange()

		queryStr := query.String()

		for currentAttempts := 0; ; currentAttempts++ {
			requestStart := config.Clock.Now()
			query.Bind(currentRange.Start, currentRange.End)
			iter := query.Iter()
			for iter.Scan(nil, nil, nil) {
				rb.IncRows()
			}
			requestEnd := config.Clock.Now()
			err := iter.Close()

			if err == nil {
				rb.IncOps()
				return requestEnd.Sub(requestStart), nil
			}

			if config.RetryHandler == "sb" {
				if err = handleSbRetryErrorWithConfig(config, queryStr, err, currentAttempts); err != nil {
					return time.Duration(0), err
				}
			}
		}
	})
}

func DoScanTable(session *gocql.Session, w *worker.Worker, workload workloads.Generator, rateLimiter rate_limiter.RateLimiter, validateData bool) {
	DoScanTableWithConfig(DefaultExecutionConfig(), session, w, workload, rateLimiter, validateData)
}

func DoMixedWithConfig(
	config ExecutionConfig,
	session *gocql.Session,
	w *worker.Worker,
	workload workloads.Generator,
	rateLimiter rate_limiter.RateLimiter,
	validateData bool,
) {
	config = config.normalized()
	// Create reusable test functions for write and read operations with separate latency recording
	writeTestFunc := createMixedWriteTestFuncWithConfig(config, session, workload, validateData)
	readTestFunc := createMixedReadTestFuncWithConfig(config, config.TableName, session, workload, validateData)

	RunTest(config, w, workload, rateLimiter, func(rb *worker.Worker) (time.Duration, error) {
		// Use global atomic counter to ensure true 50/50 distribution across all threads
		opCount := config.MixedOperationCounter.Add(1)

		expectedStartTime := rateLimiter.Expected()
		if expectedStartTime.IsZero() {
			expectedStartTime = config.Clock.Now()
		}

		// Perform write on even operations, read on odd operations
		// This gives us 50% reads and 50% writes globally across all threads
		if opCount%2 == 0 {
			// Perform write operation using existing write logic
			rawLatency, err := writeTestFunc(rb)
			if err == nil {
				// Record coordinated omission fixed latency for write operations
				endTime := config.Clock.Now()
				rb.RecordWriteCoFixedLatency(endTime.Sub(expectedStartTime))
			}
			return rawLatency, err
		}
		// Perform read operation using existing read logic
		rawLatency, err := readTestFunc(rb)
		if err == nil {
			// Record coordinated omission fixed latency for read operations
			endTime := config.Clock.Now()
			rb.RecordReadCoFixedLatency(endTime.Sub(expectedStartTime))
		}
		return rawLatency, err
	})
}

func DoMixed(
	session *gocql.Session,
	w *worker.Worker,
	workload workloads.Generator,
	rateLimiter rate_limiter.RateLimiter,
	validateData bool,
) {
	DoMixedWithConfig(DefaultExecutionConfig(), session, w, workload, rateLimiter, validateData)
}

// createMixedWriteTestFunc creates a test function for write operations in mixed mode with separate latency recording
func createMixedWriteTestFuncWithConfig(
	config ExecutionConfig,
	session *gocql.Session,
	workload workloads.Generator,
	validateData bool,
) func(rb *worker.Worker) (time.Duration, error) {
	config = config.normalized()
	return func(rb *worker.Worker) (time.Duration, error) {
		request := fmt.Sprintf(
			"INSERT INTO %s.%s (pk, ck, v) VALUES (?, ?, ?)",
			config.KeyspaceName,
			config.TableName,
		)
		query := session.Query(request)
		defer query.Release()
		pk := workload.NextPartitionKey()
		ck := workload.NextClusteringKey()
		value, err := GenerateData(pk, ck, config.ClusteringRowSizeDist.Generate(), validateData)
		if err != nil {
			panic(err)
		}
		bound := query.Bind(pk, ck, value)

		queryStr := ""
		currentAttempts := 0
		for {
			requestStart := config.Clock.Now()
			err = bound.Exec()
			requestEnd := config.Clock.Now()

			if err == nil {
				rb.IncOps()
				rb.IncRows()
				latency := requestEnd.Sub(requestStart)
				// Record latency in write-specific histograms for mixed mode
				rb.RecordWriteRawLatency(latency)

				// Also record in general histograms for compatibility
				rb.RecordRawLatency(latency)
				return latency, nil
			}
			if config.RetryHandler == "sb" {
				if queryStr == "" {
					// NOTE: use custom query string instead of 'query.String()' to avoid huge values printings
					queryStr = fmt.Sprintf(
						"[query statement=%q values=%+v consistency=%s]",
						request,
						[]any{pk, ck, "<" + strconv.Itoa(len(value)) + "-bytes-value>"},
						query.GetConsistency(),
					)
				}
				err = handleSbRetryErrorWithConfig(config, queryStr, err, currentAttempts)
			}
			if err != nil {
				return time.Duration(0), err
			}
			currentAttempts++
		}
	}
}

// createMixedReadTestFunc creates a test function for read operations in mixed mode with separate latency recording
func createMixedReadTestFuncWithConfig(
	config ExecutionConfig,
	table string,
	session *gocql.Session,
	workload workloads.Generator,
	validateData bool,
) func(rb *worker.Worker) (time.Duration, error) {
	config = config.normalized()
	counter, numOfOrderings := 0, len(config.SelectOrderByParsed)
	return func(rb *worker.Worker) (time.Duration, error) {
		counter++
		pk := workload.NextPartitionKey()
		query := session.Query(
			BuildReadQueryStringWithConfig(
				config,
				table,
				config.SelectOrderByParsed[counter%numOfOrderings],
			),
		)
		defer query.Release()

		switch {
		case config.InRestriction:
			args := make([]any, 1, config.RowsPerRequest+1)
			args[0] = pk
			for range config.RowsPerRequest {
				if workload.IsPartitionDone() {
					args = append(args, 0)
				} else {
					args = append(args, workload.NextClusteringKey())
				}
			}
			query.Bind(args...)
		case config.NoLowerBound:
			query.Bind(pk)
		case config.ProvideUpperBound:
			ck := workload.NextClusteringKey()
			query.Bind(pk, ck, ck+int64(config.RowsPerRequest))
		default:
			query.Bind(pk, workload.NextClusteringKey())
		}

		queryStr := ""
		currentAttempts := 0
		for {
			requestStart := config.Clock.Now()
			iter := query.Iter()

			var (
				resPk, resCk int64
				value        []byte
			)

			if table == config.TableName {
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
			} else {
				// Counter table
				var c1, c2, c3, c4, c5 int64
				for iter.Scan(&c1, &c2, &c3, &c4, &c5) {
					rb.IncRows()
				}
			}

			requestEnd := config.Clock.Now()
			err := iter.Close()
			if err == nil {
				rb.IncOps()
				latency := requestEnd.Sub(requestStart)
				// Record latency in read-specific histograms for mixed mode
				rb.RecordReadRawLatency(latency)

				// Also record in general histograms for compatibility
				rb.RecordRawLatency(latency)
				return latency, nil
			}

			if config.RetryHandler == "sb" {
				if queryStr == "" {
					queryStr = query.String()
				}

				err = handleSbRetryErrorWithConfig(config, queryStr, err, currentAttempts)
			}

			if err != nil {
				return time.Duration(0), err
			}

			currentAttempts++
		}
	}
}
