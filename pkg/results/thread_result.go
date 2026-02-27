package results

import (
	"time"
)

type TestThreadResult struct {
	FullResult    *Result
	PartialResult *Result
	ResultChannel chan Result
	partialStart  time.Time
}

var GlobalErrorFlag = false

func NewTestThreadResult() *TestThreadResult {
	r := &TestThreadResult{}
	r.FullResult = &Result{}
	r.PartialResult = &Result{}
	r.FullResult.Final = true
	if globalResultConfiguration.measureLatency {
		r.FullResult.RawLatency = NewHistogram(
			&globalResultConfiguration.latencyHistogramConfiguration,
			"raw-latency",
		)
		r.FullResult.CoFixedLatency = NewHistogram(
			&globalResultConfiguration.latencyHistogramConfiguration,
			"co-fixed-lantecy",
		)
		// Create separate histograms for mixed mode read/write operations
		r.FullResult.RawReadLatency = NewHistogram(
			&globalResultConfiguration.latencyHistogramConfiguration,
			"raw-read-latency",
		)
		r.FullResult.CoFixedReadLatency = NewHistogram(
			&globalResultConfiguration.latencyHistogramConfiguration,
			"co-fixed-read-latency",
		)
		r.FullResult.RawWriteLatency = NewHistogram(
			&globalResultConfiguration.latencyHistogramConfiguration,
			"raw-write-latency",
		)
		r.FullResult.CoFixedWriteLatency = NewHistogram(
			&globalResultConfiguration.latencyHistogramConfiguration,
			"co-fixed-write-latency",
		)
		r.PartialResult.RawLatency = NewHistogram(
			&globalResultConfiguration.latencyHistogramConfiguration,
			"raw-latency",
		)
		r.PartialResult.CoFixedLatency = NewHistogram(
			&globalResultConfiguration.latencyHistogramConfiguration,
			"co-fixed-lantecy",
		)
		// Create separate histograms for mixed mode read/write operations in partial results
		r.PartialResult.RawReadLatency = NewHistogram(
			&globalResultConfiguration.latencyHistogramConfiguration,
			"raw-read-latency",
		)
		r.PartialResult.CoFixedReadLatency = NewHistogram(
			&globalResultConfiguration.latencyHistogramConfiguration,
			"co-fixed-read-latency",
		)
		r.PartialResult.RawWriteLatency = NewHistogram(
			&globalResultConfiguration.latencyHistogramConfiguration,
			"raw-write-latency",
		)
		r.PartialResult.CoFixedWriteLatency = NewHistogram(
			&globalResultConfiguration.latencyHistogramConfiguration,
			"co-fixed-write-latency",
		)
	}
	r.ResultChannel = make(chan Result, 10000)
	return r
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
	GlobalErrorFlag = true
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
		// Create separate histograms for mixed mode read/write operations in partial results
		r.PartialResult.RawReadLatency = NewHistogram(
			&globalResultConfiguration.latencyHistogramConfiguration,
			"raw-read-latency",
		)
		r.PartialResult.CoFixedReadLatency = NewHistogram(
			&globalResultConfiguration.latencyHistogramConfiguration,
			"co-fixed-read-latency",
		)
		r.PartialResult.RawWriteLatency = NewHistogram(
			&globalResultConfiguration.latencyHistogramConfiguration,
			"raw-write-latency",
		)
		r.PartialResult.CoFixedWriteLatency = NewHistogram(
			&globalResultConfiguration.latencyHistogramConfiguration,
			"co-fixed-write-latency",
		)
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
	_ = r.FullResult.RawReadLatency.RecordValue(lv)
	_ = r.PartialResult.RawReadLatency.RecordValue(lv)
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
	_ = r.FullResult.CoFixedReadLatency.RecordValue(lv)
	_ = r.PartialResult.CoFixedReadLatency.RecordValue(lv)
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
	_ = r.FullResult.RawWriteLatency.RecordValue(lv)
	_ = r.PartialResult.RawWriteLatency.RecordValue(lv)
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
	_ = r.FullResult.CoFixedWriteLatency.RecordValue(lv)
	_ = r.PartialResult.CoFixedWriteLatency.RecordValue(lv)
}

func (r *TestThreadResult) SubmitResult() {
	now := time.Now().UTC()
	if now.Sub(r.partialStart) > time.Second {
		r.ResultChannel <- *r.PartialResult
		r.ResetPartialResult()
		r.partialStart = now
	}
}

func (r *TestThreadResult) StopReporting() {
	close(r.ResultChannel)
}
