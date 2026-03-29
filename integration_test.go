package main

import (
	"context"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gocql/gocql"

	"github.com/scylladb/scylla-bench/pkg/rate_limiter"
	"github.com/scylladb/scylla-bench/pkg/results"
	"github.com/scylladb/scylla-bench/pkg/testutil"
	"github.com/scylladb/scylla-bench/pkg/workloads"
	"github.com/scylladb/scylla-bench/random"
)

type integrationHarness struct {
	ctx              context.Context
	container        *testutil.ScyllaDBContainer
	session          *gocql.Session
	keyspaceName     string
	tableName        string
	counterTableName string
	config           ExecutionConfig
}

func requireContainerTests(t *testing.T) {
	t.Helper()
	if os.Getenv("RUN_CONTAINER_TESTS") != "true" {
		t.Skip("Skipping integration tests. Set RUN_CONTAINER_TESTS=true to run")
	}
}

func newIntegrationHarness(t *testing.T, withCounters bool) *integrationHarness {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	t.Cleanup(cancel)

	container := testutil.SharedScyllaDBContainer(t)

	h := &integrationHarness{
		ctx:              ctx,
		container:        container,
		session:          container.Session,
		keyspaceName:     testutil.GenerateUniqueKeyspaceName(t),
		tableName:        "test",
		counterTableName: "test_counters",
	}

	if createErr := container.CreateKeyspace(h.keyspaceName, 1); createErr != nil {
		t.Fatalf("Failed to create keyspace: %v", createErr)
	}
	t.Cleanup(func() {
		if dropErr := container.DropKeyspace(h.keyspaceName); dropErr != nil {
			t.Logf("Failed to drop keyspace %s: %v", h.keyspaceName, dropErr)
		}
	})

	if createErr := container.CreateTable(h.keyspaceName, h.tableName); createErr != nil {
		t.Fatalf("Failed to create table: %v", createErr)
	}

	if withCounters {
		if createErr := container.CreateCounterTable(h.keyspaceName, h.counterTableName); createErr != nil {
			t.Fatalf("Failed to create counter table: %v", createErr)
		}
	}

	h.config = ExecutionConfig{
		KeyspaceName:          h.keyspaceName,
		TableName:             h.tableName,
		CounterTableName:      h.counterTableName,
		ClusteringRowSizeDist: random.Fixed{Value: 4},
		RowsPerRequest:        1,
		SelectOrderByParsed:   []string{""},
		Iterations:            1,
		RetryHandler:          retryHandler,
		RetryNumber:           retryNumber,
		RetryPolicy:           retryPolicy,
	}
	return h
}

func (h *integrationHarness) childConfig() ExecutionConfig {
	config := h.config
	config.StopAll = &atomic.Uint32{}
	config.CriticalErrorFlag = &atomic.Bool{}
	config.MixedOperationCounter = &atomic.Uint64{}
	config.TotalErrors = &atomic.Int32{}
	config.TotalErrorsPrintOnce = &sync.Once{}
	return config
}

func runModeForDuration(
	t *testing.T,
	config ExecutionConfig,
	mode ModeFunc,
	session *gocql.Session,
	workload workloads.Generator,
	validateData bool,
	duration time.Duration,
) *results.TestThreadResult {
	t.Helper()

	testResult := results.NewTestThreadResultWithCriticalErrorFlag(config.CriticalErrorFlag)
	done := make(chan struct{})

	go func() {
		defer close(done)
		modeWithConfig(t, config, mode, session, testResult, workload, validateData)
	}()

	select {
	case <-time.After(duration):
		config.StopAll.Store(1)
	case <-done:
	}

	<-done
	return testResult
}

func modeName(mode ModeFunc) string {
	return getFuncName(mode)
}

func modeWithConfig(
	t *testing.T,
	config ExecutionConfig,
	mode ModeFunc,
	session *gocql.Session,
	testResult *results.TestThreadResult,
	workload workloads.Generator,
	validateData bool,
) {
	t.Helper()

	switch modeName(mode) {
	case modeName(DoWrites):
		DoWritesWithConfig(config, session, testResult, workload, &rate_limiter.UnlimitedRateLimiter{}, validateData)
	case modeName(DoBatchedWrites):
		DoBatchedWritesWithConfig(config, session, testResult, workload, &rate_limiter.UnlimitedRateLimiter{}, validateData)
	case modeName(DoReads):
		DoReadsFromTableWithConfig(config, config.TableName, session, testResult, workload, &rate_limiter.UnlimitedRateLimiter{}, validateData)
	case modeName(DoCounterReads):
		DoReadsFromTableWithConfig(config, config.CounterTableName, session, testResult, workload, &rate_limiter.UnlimitedRateLimiter{}, validateData)
	case modeName(DoCounterUpdates):
		DoCounterUpdatesWithConfig(config, session, testResult, workload, &rate_limiter.UnlimitedRateLimiter{}, validateData)
	case modeName(DoScanTable):
		DoScanTableWithConfig(config, session, testResult, workload, &rate_limiter.UnlimitedRateLimiter{}, validateData)
	case modeName(DoMixed):
		DoMixedWithConfig(config, session, testResult, workload, &rate_limiter.UnlimitedRateLimiter{}, validateData)
	default:
		t.Fatalf("unsupported mode func")
	}
}

func assertSuccessfulRun(t *testing.T, name string, result *results.TestThreadResult) {
	t.Helper()
	if result.FullResult.Operations == 0 {
		t.Fatalf("%s: no operations completed", name)
	}
	if result.FullResult.Errors > 0 {
		t.Fatalf("%s: encountered %d errors", name, result.FullResult.Errors)
	}
	if len(result.FullResult.CriticalErrors) > 0 {
		t.Fatalf("%s: encountered critical errors: %v", name, result.FullResult.CriticalErrors)
	}
}

func TestIntegration(t *testing.T) {
	requireContainerTests(t)
	t.Parallel()

	h := newIntegrationHarness(t, true)

	tests := []struct {
		run  func(t *testing.T, h *integrationHarness)
		name string
	}{
		{name: "SequentialWorkload", run: testSequentialWorkload},
		{name: "UniformWorkload", run: testUniformWorkload},
		{name: "TimeSeriesWorkload", run: testTimeSeriesWorkload},
		{name: "CounterOperations", run: testCounterOperations},
		{name: "ScanOperations", run: testScanOperations},
		{name: "MixedMode", run: testMixedMode},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tc.run(t, h)
		})
	}
}

func mustNewSequentialVisitAll(t *testing.T, rowOffset, rowCount, clusteringRowCount int64) *workloads.SequentialVisitAll {
	t.Helper()
	w, err := workloads.NewSequentialVisitAll(rowOffset, rowCount, clusteringRowCount)
	if err != nil {
		t.Fatalf("NewSequentialVisitAll(%d, %d, %d): %v", rowOffset, rowCount, clusteringRowCount, err)
	}
	return w
}

func testSequentialWorkload(t *testing.T, h *integrationHarness) {
	t.Helper()
	t.Run("Write", func(t *testing.T) {
		t.Parallel()
		config := h.childConfig()
		result := runModeForDuration(t, config, DoWrites, h.session, mustNewSequentialVisitAll(t, 0, 1000, 10), false, 2*time.Second)
		assertSuccessfulRun(t, "Sequential write", result)
	})

	t.Run("Read", func(t *testing.T) {
		t.Parallel()
		config := h.childConfig()
		writeResult := runModeForDuration(t, config, DoWrites, h.session, mustNewSequentialVisitAll(t, 0, 1000, 10), false, 2*time.Second)
		assertSuccessfulRun(t, "Sequential read setup", writeResult)
		config = h.childConfig()
		result := runModeForDuration(t, config, DoReads, h.session, mustNewSequentialVisitAll(t, 0, 1000, 10), false, 2*time.Second)
		assertSuccessfulRun(t, "Sequential read", result)
	})
}

func testUniformWorkload(t *testing.T, h *integrationHarness) {
	t.Helper()
	t.Run("Write", func(t *testing.T) {
		t.Parallel()
		config := h.childConfig()
		result := runModeForDuration(t, config, DoWrites, h.session, workloads.NewRandomUniform(0, 1000, 0, 10), false, 2*time.Second)
		assertSuccessfulRun(t, "Uniform write", result)
	})

	t.Run("Read", func(t *testing.T) {
		t.Parallel()
		config := h.childConfig()
		writeResult := runModeForDuration(t, config, DoWrites, h.session, workloads.NewRandomUniform(0, 1000, 0, 10), false, 2*time.Second)
		assertSuccessfulRun(t, "Uniform read setup", writeResult)
		config = h.childConfig()
		result := runModeForDuration(t, config, DoReads, h.session, workloads.NewRandomUniform(0, 1000, 0, 10), false, 2*time.Second)
		assertSuccessfulRun(t, "Uniform read", result)
	})
}

func testTimeSeriesWorkload(t *testing.T, h *integrationHarness) {
	t.Helper()
	t.Run("Write", func(t *testing.T) {
		t.Parallel()
		config := h.childConfig()
		result := runModeForDuration(t, config, DoWrites, h.session, workloads.NewTimeSeriesWriter(0, 1, 1000, 0, 100, globalClock.Now(), 1000), false, 2*time.Second)
		assertSuccessfulRun(t, "TimeSeries write", result)
	})

	t.Run("Read", func(t *testing.T) {
		t.Parallel()
		start := globalClock.Now()
		config := h.childConfig()
		writeResult := runModeForDuration(t, config, DoWrites, h.session, workloads.NewTimeSeriesWriter(0, 1, 1000, 0, 100, start, 1000), false, 2*time.Second)
		assertSuccessfulRun(t, "TimeSeries read setup", writeResult)
		config = h.childConfig()
		result := runModeForDuration(t, config, DoReads, h.session, workloads.NewTimeSeriesReader(0, 1, 1000, 0, 100, 1000, "uniform", start, globalClock), false, 2*time.Second)
		assertSuccessfulRun(t, "TimeSeries read", result)
	})
}

func testCounterOperations(t *testing.T, h *integrationHarness) {
	t.Helper()
	t.Run("Update", func(t *testing.T) {
		t.Parallel()
		config := h.childConfig()
		result := runModeForDuration(t, config, DoCounterUpdates, h.session, workloads.NewRandomUniform(0, 1000, 0, 10), false, 2*time.Second)
		assertSuccessfulRun(t, "Counter update", result)
	})

	t.Run("Read", func(t *testing.T) {
		t.Parallel()
		config := h.childConfig()
		updateResult := runModeForDuration(t, config, DoCounterUpdates, h.session, workloads.NewRandomUniform(0, 1000, 0, 10), false, 2*time.Second)
		assertSuccessfulRun(t, "Counter read setup", updateResult)
		config = h.childConfig()
		result := runModeForDuration(t, config, DoCounterReads, h.session, workloads.NewRandomUniform(0, 1000, 0, 10), false, 2*time.Second)
		assertSuccessfulRun(t, "Counter read", result)
	})
}

func testScanOperations(t *testing.T, h *integrationHarness) {
	t.Helper()
	config := h.childConfig()
	writeResult := runModeForDuration(t, config, DoWrites, h.session, mustNewSequentialVisitAll(t, 0, 1200, 4), false, 2*time.Second)
	assertSuccessfulRun(t, "Scan setup", writeResult)
	config = h.childConfig()
	result := runModeForDuration(t, config, DoScanTable, h.session, workloads.NewRangeScan(300, 0, 300), false, 3*time.Second)
	assertSuccessfulRun(t, "Scan", result)
}

func testMixedMode(t *testing.T, h *integrationHarness) {
	t.Helper()
	config := h.childConfig()
	result := runModeForDuration(t, config, DoMixed, h.session, workloads.NewRandomUniform(0, 1000, 0, 10), false, 3*time.Second)
	assertSuccessfulRun(t, "Mixed mode", result)
}

func TestIntegrationWithDataValidation(t *testing.T) {
	requireContainerTests(t)
	t.Parallel()

	h := newIntegrationHarness(t, false)
	config := h.childConfig()
	writeResult := runModeForDuration(t, config, DoWrites, h.session, mustNewSequentialVisitAll(t, 0, 100, 5), true, 2*time.Second)
	assertSuccessfulRun(t, "Data validation write", writeResult)

	config = h.childConfig()
	readResult := runModeForDuration(t, config, DoReads, h.session, mustNewSequentialVisitAll(t, 0, 100, 5), true, 2*time.Second)
	assertSuccessfulRun(t, "Data validation read", readResult)
}

func TestIntegrationQuickSmoke(t *testing.T) {
	requireContainerTests(t)
	t.Parallel()

	h := newIntegrationHarness(t, true)
	testCases := []struct {
		prepare  func(t *testing.T)
		workload workloads.Generator
		mode     ModeFunc
		name     string
	}{
		{name: "Sequential-Write", workload: mustNewSequentialVisitAll(t, 0, 100, 5), mode: DoWrites},
		{name: "Sequential-Read", workload: mustNewSequentialVisitAll(t, 0, 100, 5), mode: DoReads, prepare: func(t *testing.T) {
			t.Helper()
			assertSuccessfulRun(
				t,
				"Quick smoke sequential read setup",
				runModeForDuration(t, h.childConfig(), DoWrites, h.session, mustNewSequentialVisitAll(t, 0, 100, 5), false, time.Second),
			)
		}},
		{name: "Uniform-Write", workload: workloads.NewRandomUniform(0, 100, 0, 5), mode: DoWrites},
		{name: "Uniform-Read", workload: workloads.NewRandomUniform(0, 100, 0, 5), mode: DoReads, prepare: func(t *testing.T) {
			t.Helper()
			assertSuccessfulRun(
				t,
				"Quick smoke uniform read setup",
				runModeForDuration(t, h.childConfig(), DoWrites, h.session, workloads.NewRandomUniform(0, 100, 0, 5), false, time.Second),
			)
		}},
		{name: "Counter-Update", workload: workloads.NewRandomUniform(0, 100, 0, 5), mode: DoCounterUpdates},
		{name: "Counter-Read", workload: workloads.NewRandomUniform(0, 100, 0, 5), mode: DoCounterReads, prepare: func(t *testing.T) {
			t.Helper()
			assertSuccessfulRun(
				t,
				"Quick smoke counter read setup",
				runModeForDuration(t, h.childConfig(), DoCounterUpdates, h.session, workloads.NewRandomUniform(0, 100, 0, 5), false, time.Second),
			)
		}},
		{name: "Scan", workload: workloads.NewRangeScan(300, 0, 300), mode: DoScanTable, prepare: func(t *testing.T) {
			t.Helper()
			assertSuccessfulRun(
				t,
				"Quick smoke scan setup",
				runModeForDuration(t, h.childConfig(), DoWrites, h.session, mustNewSequentialVisitAll(t, 0, 200, 5), false, time.Second),
			)
		}},
		{name: "Mixed", workload: workloads.NewRandomUniform(0, 100, 0, 5), mode: DoMixed},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if tc.prepare != nil {
				tc.prepare(t)
			}
			result := runModeForDuration(t, h.childConfig(), tc.mode, h.session, tc.workload, false, time.Second)
			assertSuccessfulRun(t, tc.name, result)
			fmt.Printf("%s: %d ops, %d errors\n", tc.name, result.FullResult.Operations, result.FullResult.Errors)
		})
	}
}
