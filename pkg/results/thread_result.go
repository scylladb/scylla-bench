package results

import "time"

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
		r.FullResult.RawLatency = NewHistogram(&globalResultConfiguration.latencyHistogramConfiguration)
		r.FullResult.CoFixedLatency = NewHistogram(&globalResultConfiguration.latencyHistogramConfiguration)
		r.PartialResult.RawLatency = NewHistogram(&globalResultConfiguration.latencyHistogramConfiguration)
		r.PartialResult.CoFixedLatency = NewHistogram(&globalResultConfiguration.latencyHistogramConfiguration)
	}
	r.ResultChannel = make(chan Result, 1)
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
		r.PartialResult.RawLatency = NewHistogram(&globalResultConfiguration.latencyHistogramConfiguration)
		r.PartialResult.CoFixedLatency = NewHistogram(&globalResultConfiguration.latencyHistogramConfiguration)
	}
}

func (r *TestThreadResult) RecordRawLatency(latency time.Duration) {
	if ! globalResultConfiguration.measureLatency {
		return
	}
	lv := latency.Nanoseconds()

	if lv >= globalResultConfiguration.latencyHistogramConfiguration.maxValue {
		lv = globalResultConfiguration.latencyHistogramConfiguration.maxValue
	}

	_ = r.FullResult.RawLatency.RecordValue(lv)
	_ = r.PartialResult.RawLatency.RecordValue(lv)
}

func (r *TestThreadResult) RecordCoFixedLatency(latency time.Duration) {
	if ! globalResultConfiguration.measureLatency {
		return
	}
	lv := latency.Nanoseconds()

	if lv >= globalResultConfiguration.latencyHistogramConfiguration.maxValue {
		lv = globalResultConfiguration.latencyHistogramConfiguration.maxValue
	}
	_ = r.FullResult.CoFixedLatency.RecordValue(lv)
	_ = r.PartialResult.CoFixedLatency.RecordValue(lv)
}

func (r *TestThreadResult) SubmitResult() {
	now := time.Now()
	if now.Sub(r.partialStart) > time.Second {
		r.ResultChannel <- *r.PartialResult
		r.ResetPartialResult()
		r.partialStart = now
	}

}

func (r *TestThreadResult) StopReporting() {
	close(r.ResultChannel)
}
