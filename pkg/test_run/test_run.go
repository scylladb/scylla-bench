package test_run

import (
	"fmt"
	"sync"
	"time"

	"github.com/HdrHistogram/hdrhistogram-go"

	"github.com/scylladb/scylla-bench/internal/clock"
	"github.com/scylladb/scylla-bench/pkg/config"
	"github.com/scylladb/scylla-bench/pkg/rate_limiter"
	"github.com/scylladb/scylla-bench/pkg/results"
	"github.com/scylladb/scylla-bench/pkg/tools"
	"github.com/scylladb/scylla-bench/pkg/worker"
	"github.com/scylladb/scylla-bench/pkg/workloads"
)

type TestRun struct {
	clk                clock.Clock
	workers            []*worker.Worker
	numberOfThreads    int
	startTime          time.Time
	stopTime           *time.Time
	partialResult      results.PartialResult
	totalResult        results.TotalResult
	waitGroup          sync.WaitGroup
	measureLatency     bool
	hdrLatencyScale    int64
	hdrLatencyMaxValue int64
	timeOffsetUnit     int64
	maximumRate        int
}

func NewTestRun(clk clock.Clock, concurrency int, maximumRate int) *TestRun {
	if clk == nil {
		panic("test_run: clock must not be nil")
	}
	var timeOffsetUnit int64
	if maximumRate != 0 {
		timeOffsetUnit = int64(time.Second) / int64(maximumRate)
		maximumRate /= concurrency
	} else {
		timeOffsetUnit = 0
	}
	tr := &TestRun{
		clk:                clk,
		timeOffsetUnit:     timeOffsetUnit,
		maximumRate:        maximumRate,
		workers:            make([]*worker.Worker, concurrency),
		numberOfThreads:    concurrency,
		partialResult:      *results.NewPartialResult(),
		totalResult:        *results.NewTotalResult(concurrency),
		measureLatency:     config.GetGlobalMeasureLatency(),
		hdrLatencyScale:    config.GetGlobalHdrLatencyScale(),
		hdrLatencyMaxValue: config.GetGlobalHistogramConfiguration().MaxValue,
	}
	tr.waitGroup.Add(concurrency)
	for i := 0; i < concurrency; i++ {
		tr.workers[i] = worker.NewWorker(
			&tr.partialResult,
			&tr.totalResult,
			&tr.waitGroup,
			tr.measureLatency,
			tr.hdrLatencyScale,
			tr.hdrLatencyMaxValue,
		)
	}
	return tr
}

func (tr *TestRun) SetStartTime() {
	tr.startTime = tr.clk.Now()
}

func (tr *TestRun) GetTestResult(idx int) *worker.Worker {
	return tr.workers[idx]
}

func (tr *TestRun) GetTestResults() []*worker.Worker {
	return tr.workers
}

func (tr *TestRun) GetElapsedTime() time.Duration {
	return tools.Round(tr.stopTime.Sub(tr.startTime))
}

func (tr *TestRun) GetTotalResults() {
	tr.waitGroup.Wait()
	timeNow := tr.clk.Now()
	tr.stopTime = &timeNow
	tr.partialResult.FlushDataToHistogram()
	tr.totalResult.FlushDataToHistogram()
}

func (tr *TestRun) StartPrintingPartialResult() {
	go func() {
		for range time.Tick(time.Second) {
			tr.partialResult.FlushDataToHistogram()
			tr.partialResult.PrintPartialResult(tr.clk.Now().Sub(tr.startTime))
			tr.partialResult.Reset()
			if tr.stopTime != nil {
				if tr.partialResult.FlushDataToHistogram() {
					tr.partialResult.PrintPartialResult(tr.clk.Now().Sub(tr.startTime))
				}
				break
			}
		}
	}()
}

func (tr *TestRun) PrintResultsHeader() {
	tr.partialResult.PrintPartialResultHeader()
}

func (tr *TestRun) RunTest(workload workloads.WorkloadFunction) {
	for i := 0; i < tr.numberOfThreads; i++ {
		testResult := tr.GetTestResult(i)
		i := i
		go func() {
			timeOffset := time.Duration(tr.timeOffsetUnit * int64(i))
			workload(i, testResult, rate_limiter.NewRateLimiter(tr.clk, tr.maximumRate, timeOffset))
		}()
	}
}

func (tr *TestRun) PrintTotalResults() {
	fmt.Println("\nResults")
	fmt.Println("Time (avg):\t", tr.GetElapsedTime())
	fmt.Println("Total ops:\t", tr.totalResult.Operations)
	fmt.Println("Total rows:\t", tr.totalResult.ClusteringRows)
	if tr.totalResult.Errors != 0 {
		fmt.Println("Total errors:\t", tr.totalResult.Errors)
	}
	elapsed := tr.GetElapsedTime().Seconds()
	fmt.Println("Operations/s:\t", uint64(float64(tr.totalResult.Operations)/elapsed))
	fmt.Println("Rows/s:\t\t", uint64(float64(tr.totalResult.ClusteringRows)/elapsed))
	if tr.measureLatency {
		printLatencyResults("raw latency", tr.totalResult.RawLatency, tr.hdrLatencyScale)
		printLatencyResults("c-o fixed latency", tr.totalResult.CoFixedLatency, tr.hdrLatencyScale)
		if tr.totalResult.RawReadLatency != nil && tr.totalResult.RawReadLatency.TotalCount() > 0 {
			printLatencyResults("raw read latency", tr.totalResult.RawReadLatency, tr.hdrLatencyScale)
		}
		if tr.totalResult.CoFixedReadLatency != nil && tr.totalResult.CoFixedReadLatency.TotalCount() > 0 {
			printLatencyResults("c-o fixed read latency", tr.totalResult.CoFixedReadLatency, tr.hdrLatencyScale)
		}
		if tr.totalResult.RawWriteLatency != nil && tr.totalResult.RawWriteLatency.TotalCount() > 0 {
			printLatencyResults("raw write latency", tr.totalResult.RawWriteLatency, tr.hdrLatencyScale)
		}
		if tr.totalResult.CoFixedWriteLatency != nil && tr.totalResult.CoFixedWriteLatency.TotalCount() > 0 {
			printLatencyResults("c-o fixed write latency", tr.totalResult.CoFixedWriteLatency, tr.hdrLatencyScale)
		}
	}
	tr.totalResult.PrintCriticalErrors()
}

func printLatencyResults(name string, latency *hdrhistogram.Histogram, scale int64) {
	fmt.Println(name, ":\n  max:\t\t", time.Duration(latency.Max()*scale),
		"\n  99.9th:\t", time.Duration(latency.ValueAtQuantile(99.9)*scale),
		"\n  99th:\t\t", time.Duration(latency.ValueAtQuantile(99)*scale),
		"\n  95th:\t\t", time.Duration(latency.ValueAtQuantile(95)*scale),
		"\n  90th:\t\t", time.Duration(latency.ValueAtQuantile(90)*scale),
		"\n  median:\t", time.Duration(latency.ValueAtQuantile(50)*scale),
		"\n  mean:\t\t", time.Duration(latency.Mean()*float64(scale)))
}

func (tr *TestRun) GetFinalStatus() int {
	if tr.totalResult.IsCriticalErrorsFound() {
		return 1
	}
	return 0
}
