package results

import (
	"sync/atomic"
	"time"

	"github.com/scylladb/scylla-bench/internal/clock"
)

type TestThreadResult struct {
	FullResult        *Result
	PartialResult     *Result
	ResultChannel     chan Result
	clk               clock.Clock
	criticalErrorFlag *atomic.Bool
	partialStart      time.Time
}

var globalCriticalErrorFlag atomic.Bool

// globalResultClock is the default clock for result-reporting code. It is used
// when NewTestThreadResult() is called without an explicit clock. Call
// SetGlobalResultClock to override (e.g. from main, to share a single clock
// instance with the rest of the program).
var globalResultClock clock.Clock = clock.New()

// SetGlobalResultClock replaces the package-level default clock. Call this
// once from main() before creating any TestThreadResult instances.
func SetGlobalResultClock(clk clock.Clock) {
	globalResultClock = clk
}

func NewTestThreadResult() *TestThreadResult {
	return NewTestThreadResultWithCriticalErrorFlag(&globalCriticalErrorFlag)
}

func NewTestThreadResultWithCriticalErrorFlag(flag *atomic.Bool) *TestThreadResult {
	return NewTestThreadResultWithClockAndFlag(globalResultClock, flag)
}

func NewTestThreadResultWithClockAndFlag(clk clock.Clock, flag *atomic.Bool) *TestThreadResult {
	if clk == nil {
		panic("results: clock must not be nil; use clock.New() for production or clock.NewManual() for tests")
	}
	r := &TestThreadResult{clk: clk}
	r.FullResult = &Result{}
	r.PartialResult = &Result{}
	r.FullResult.Final = true
	r.criticalErrorFlag = flag
	if globalResultConfiguration.measureLatency {
		r.FullResult.RawLatency = NewHistogram(
			&globalResultConfiguration.latencyHistogramConfiguration,
			"raw-latency",
		)
		r.FullResult.CoFixedLatency = NewHistogram(
			&globalResultConfiguration.latencyHistogramConfiguration,
			"co-fixed-lantecy",
		)
		r.PartialResult.RawLatency = NewHistogram(
			&globalResultConfiguration.latencyHistogramConfiguration,
			"raw-latency",
		)
		r.PartialResult.CoFixedLatency = NewHistogram(
			&globalResultConfiguration.latencyHistogramConfiguration,
			"co-fixed-lantecy",
		)
		// Read/write specific histograms are allocated lazily on first use
		// (only in mixed mode) to avoid unnecessary memory consumption.
	}
	r.ResultChannel = make(chan Result, 10000)
	return r
}

func ResetGlobalCriticalErrorFlag() {
	globalCriticalErrorFlag.Store(false)
}

func (r *TestThreadResult) HasCriticalError() bool {
	return r.criticalErrorFlag != nil && r.criticalErrorFlag.Load()
}

func (r *TestThreadResult) IncOps() {
	r.FullResult.Operations++
	r.PartialResult.Operations++
}

func (r *TestThreadResult) IncRows() {
	r.FullResult.ClusteringRows++
	r.PartialResult.ClusteringRows++
}

func (r *TestThreadResult) AddRows(n int) {
	r.FullResult.ClusteringRows += n
	r.PartialResult.ClusteringRows += n
}

func (r *TestThreadResult) IncErrors() {
	r.FullResult.Errors++
	r.PartialResult.Errors++
}

func (r *TestThreadResult) SubmitCriticalError(err error) {
	if r.FullResult.CriticalErrors == nil {
		r.FullResult.CriticalErrors = []error{err}
	} else {
		r.FullResult.CriticalErrors = append(r.FullResult.CriticalErrors, err)
	}
	if r.criticalErrorFlag != nil {
		r.criticalErrorFlag.Store(true)
	}
}

func (r *TestThreadResult) ResetPartialResult() {
	r.PartialResult = &Result{}
	if globalResultConfiguration.measureLatency {
		r.PartialResult.RawLatency = NewHistogram(
			&globalResultConfiguration.latencyHistogramConfiguration,
			"raw-latency",
		)
		r.PartialResult.CoFixedLatency = NewHistogram(
			&globalResultConfiguration.latencyHistogramConfiguration,
			"co-fixed-lantecy",
		)
		// Read/write specific histograms are allocated lazily on first use.
	}
}

func (r *TestThreadResult) RecordRawLatency(latency time.Duration) {
	if !globalResultConfiguration.measureLatency {
		return
	}
	lv := latency.Nanoseconds() / globalResultConfiguration.hdrLatencyScale

	if lv >= globalResultConfiguration.latencyHistogramConfiguration.maxValue {
		lv = globalResultConfiguration.latencyHistogramConfiguration.maxValue
	}
	_ = r.FullResult.RawLatency.RecordValue(lv)
	_ = r.PartialResult.RawLatency.RecordValue(lv)
}

func (r *TestThreadResult) RecordCoFixedLatency(latency time.Duration) {
	if !globalResultConfiguration.measureLatency {
		return
	}
	lv := latency.Nanoseconds() / globalResultConfiguration.hdrLatencyScale

	if lv >= globalResultConfiguration.latencyHistogramConfiguration.maxValue {
		lv = globalResultConfiguration.latencyHistogramConfiguration.maxValue
	}
	_ = r.FullResult.CoFixedLatency.RecordValue(lv)
	_ = r.PartialResult.CoFixedLatency.RecordValue(lv)
}

// RecordReadRawLatency records raw latency for read operations in mixed mode
func (r *TestThreadResult) RecordReadRawLatency(latency time.Duration) {
	if !globalResultConfiguration.measureLatency {
		return
	}
	lv := latency.Nanoseconds() / globalResultConfiguration.hdrLatencyScale

	if lv >= globalResultConfiguration.latencyHistogramConfiguration.maxValue {
		lv = globalResultConfiguration.latencyHistogramConfiguration.maxValue
	}
	_ = ensureHistogram(&r.FullResult.RawReadLatency, "raw-read-latency").RecordValue(lv)
	_ = ensureHistogram(&r.PartialResult.RawReadLatency, "raw-read-latency").RecordValue(lv)
}

// RecordReadCoFixedLatency records coordinated omission fixed latency for read operations in mixed mode
func (r *TestThreadResult) RecordReadCoFixedLatency(latency time.Duration) {
	if !globalResultConfiguration.measureLatency {
		return
	}
	lv := latency.Nanoseconds() / globalResultConfiguration.hdrLatencyScale

	if lv >= globalResultConfiguration.latencyHistogramConfiguration.maxValue {
		lv = globalResultConfiguration.latencyHistogramConfiguration.maxValue
	}
	_ = ensureHistogram(&r.FullResult.CoFixedReadLatency, "co-fixed-read-latency").RecordValue(lv)
	_ = ensureHistogram(&r.PartialResult.CoFixedReadLatency, "co-fixed-read-latency").RecordValue(lv)
}

// RecordWriteRawLatency records raw latency for write operations in mixed mode
func (r *TestThreadResult) RecordWriteRawLatency(latency time.Duration) {
	if !globalResultConfiguration.measureLatency {
		return
	}
	lv := latency.Nanoseconds() / globalResultConfiguration.hdrLatencyScale

	if lv >= globalResultConfiguration.latencyHistogramConfiguration.maxValue {
		lv = globalResultConfiguration.latencyHistogramConfiguration.maxValue
	}
	_ = ensureHistogram(&r.FullResult.RawWriteLatency, "raw-write-latency").RecordValue(lv)
	_ = ensureHistogram(&r.PartialResult.RawWriteLatency, "raw-write-latency").RecordValue(lv)
}

// RecordWriteCoFixedLatency records coordinated omission fixed latency for write operations in mixed mode
func (r *TestThreadResult) RecordWriteCoFixedLatency(latency time.Duration) {
	if !globalResultConfiguration.measureLatency {
		return
	}
	lv := latency.Nanoseconds() / globalResultConfiguration.hdrLatencyScale

	if lv >= globalResultConfiguration.latencyHistogramConfiguration.maxValue {
		lv = globalResultConfiguration.latencyHistogramConfiguration.maxValue
	}
	_ = ensureHistogram(&r.FullResult.CoFixedWriteLatency, "co-fixed-write-latency").RecordValue(lv)
	_ = ensureHistogram(&r.PartialResult.CoFixedWriteLatency, "co-fixed-write-latency").RecordValue(lv)
}

func (r *TestThreadResult) SubmitResult() {
	now := r.clk.Now()
	if now.Sub(r.partialStart) > time.Second {
		r.ResultChannel <- *r.PartialResult
		r.ResetPartialResult()
		r.partialStart = now
	}
}

func (r *TestThreadResult) StopReporting() {
	close(r.ResultChannel)
}
