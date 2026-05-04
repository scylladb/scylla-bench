package worker

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/scylladb/scylla-bench/pkg/results"
)

var GlobalErrorFlag atomic.Bool

type Worker struct {
	partialResult      *results.PartialResult
	totalResult        *results.TotalResult
	waitGroup          *sync.WaitGroup
	measureLatency     bool
	hdrLatencyScale    int64
	hdrLatencyMaxValue int64
}

func NewWorker(
	partialResult *results.PartialResult,
	totalResult *results.TotalResult,
	waitGroup *sync.WaitGroup,
	measureLatency bool,
	hdrLatencyScale int64,
	hdrLatencyMaxValue int64,
) *Worker {
	return &Worker{
		measureLatency:     measureLatency,
		partialResult:      partialResult,
		totalResult:        totalResult,
		waitGroup:          waitGroup,
		hdrLatencyScale:    hdrLatencyScale,
		hdrLatencyMaxValue: hdrLatencyMaxValue,
	}
}

func (r *Worker) IncOps() {
	atomic.AddUint64(&r.partialResult.Operations, 1)
	atomic.AddUint64(&r.totalResult.Operations, 1)
}

func (r *Worker) IncRows() {
	atomic.AddUint64(&r.partialResult.ClusteringRows, 1)
	atomic.AddUint64(&r.totalResult.ClusteringRows, 1)
}

func (r *Worker) AddRows(n uint64) {
	atomic.AddUint64(&r.partialResult.ClusteringRows, n)
	atomic.AddUint64(&r.totalResult.ClusteringRows, n)
}

func (r *Worker) IncErrors() {
	atomic.AddUint64(&r.partialResult.Errors, 1)
	atomic.AddUint64(&r.totalResult.Errors, 1)
}

func (r *Worker) SubmitCriticalError(err error) {
	r.totalResult.SubmitCriticalError(&err)
	GlobalErrorFlag.Store(true)
}

func (r *Worker) clampLatency(latency time.Duration) int64 {
	return min(latency.Nanoseconds()/r.hdrLatencyScale, r.hdrLatencyMaxValue)
}

func (r *Worker) RecordRawLatency(latency time.Duration) {
	if !r.measureLatency {
		return
	}
	lv := r.clampLatency(latency)
	r.partialResult.RawLatencyStack.Push(lv)
	r.totalResult.RawLatencyStack.Push(lv)
}

func (r *Worker) RecordCoFixedLatency(latency time.Duration) {
	if !r.measureLatency {
		return
	}
	lv := r.clampLatency(latency)
	r.partialResult.CoFixedLatencyStack.Push(lv)
	r.totalResult.CoFixedLatencyStack.Push(lv)
}

// The mixed-mode read/write stacks are only allocated when the test runs in
// mixed mode (see results.NewPartialResult). Outside mixed mode these record
// methods are not called; the nil checks make calls in any other context a
// silent no-op rather than a nil-deref.
func (r *Worker) RecordReadRawLatency(latency time.Duration) {
	if !r.measureLatency || r.partialResult.RawReadLatencyStack == nil {
		return
	}
	lv := r.clampLatency(latency)
	r.partialResult.RawReadLatencyStack.Push(lv)
	r.totalResult.RawReadLatencyStack.Push(lv)
}

func (r *Worker) RecordReadCoFixedLatency(latency time.Duration) {
	if !r.measureLatency || r.partialResult.CoFixedReadLatencyStack == nil {
		return
	}
	lv := r.clampLatency(latency)
	r.partialResult.CoFixedReadLatencyStack.Push(lv)
	r.totalResult.CoFixedReadLatencyStack.Push(lv)
}

func (r *Worker) RecordWriteRawLatency(latency time.Duration) {
	if !r.measureLatency || r.partialResult.RawWriteLatencyStack == nil {
		return
	}
	lv := r.clampLatency(latency)
	r.partialResult.RawWriteLatencyStack.Push(lv)
	r.totalResult.RawWriteLatencyStack.Push(lv)
}

func (r *Worker) RecordWriteCoFixedLatency(latency time.Duration) {
	if !r.measureLatency || r.partialResult.CoFixedWriteLatencyStack == nil {
		return
	}
	lv := r.clampLatency(latency)
	r.partialResult.CoFixedWriteLatencyStack.Push(lv)
	r.totalResult.CoFixedWriteLatencyStack.Push(lv)
}

func (r *Worker) StopReporting() {
	r.waitGroup.Done()
}
