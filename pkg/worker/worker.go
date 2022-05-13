package worker

import (
	"github.com/scylladb/scylla-bench/pkg/results"
	"sync"
	"sync/atomic"
	"time"
)

type Worker struct {
	partialResult *results.PartialResult
	totalResult   *results.TotalResult
	waitGroup     *sync.WaitGroup
	measureLatency bool
	hdrLatencyScale int64
	hdrLatencyMaxValue int64
}

var GlobalErrorFlag = false

func NewWorker(
	partialResult *results.PartialResult,
	totalResult *results.TotalResult,
	waitGroup *sync.WaitGroup,
	measureLatency bool,
	hdrLatencyScale int64,
	hdrLatencyMaxValue int64,
	) *Worker {
	return &Worker{
		measureLatency: measureLatency,
		partialResult: partialResult,
		totalResult: totalResult,
		waitGroup: waitGroup,
		hdrLatencyScale: hdrLatencyScale,
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
	GlobalErrorFlag = true
}

func (r *Worker) RecordRawLatency(latency time.Duration) {
	if !r.measureLatency {
		return
	}
	lv := latency.Nanoseconds() / r.hdrLatencyScale

	if lv >= r.hdrLatencyMaxValue {
		lv = r.hdrLatencyMaxValue
	}
	// Instead of submitting value to the Histogram directly we push it to temporary storage, i.e. Stack,
	//  which occasionally trigger flushing data to Histogram
	// It is done to avoid data corruption in Histogram (it is not tread safe), but at the same time to have no locking
	// Since locking here will lead to interruption of the load generation, which is not acceptable
	r.partialResult.RawLatencyStack.Push(lv)
	r.totalResult.RawLatencyStack.Push(lv)
}

func (r *Worker) RecordCoFixedLatency(latency time.Duration) {
	if !r.measureLatency {
		return
	}
	lv := latency.Nanoseconds() / r.hdrLatencyScale

	if lv >= r.hdrLatencyMaxValue {
		lv = r.hdrLatencyMaxValue
	}
	// Instead of submitting value to the Histogram directly we push it to temporary storage, i.e. Stack,
	//  which occasionally trigger flushing data to Histogram
	// It is done to avoid data corruption in Histogram (it is not tread safe), but at the same time to have no locking
	// Since locking here will lead to interruption of the load generation, which is not acceptable
	r.partialResult.CoFixedLatencyStack.Push(lv)
	r.totalResult.CoFixedLatencyStack.Push(lv)
}

func (r *Worker) StopReporting() {
	r.waitGroup.Done()
}
