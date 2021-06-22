package results

import "time"

type TestThreadResult struct {
	FullResult    *Result
	PartialResult *Result
	ResultChannel chan Result
	partialStart  time.Time
}

func NewTestThreadResult() *TestThreadResult {
	r := &TestThreadResult{}
	r.FullResult = &Result{}
	r.PartialResult = &Result{}
	r.FullResult.Final = true
	if globalResultConfiguration.measureLatency {
		r.FullResult.Latency = NewHistogram(&globalResultConfiguration.latencyHistogramConfiguration)
		r.PartialResult.Latency = NewHistogram(&globalResultConfiguration.latencyHistogramConfiguration)
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

func (r *TestThreadResult) ResetPartialResult() {
	r.PartialResult = &Result{}
	if globalResultConfiguration.measureLatency {
		r.PartialResult.Latency = NewHistogram(&globalResultConfiguration.latencyHistogramConfiguration)
	}
}

func (r *TestThreadResult) RecordLatency(start time.Time, end time.Time) {
	value := end.Sub(start)
	if r.FullResult.Latency != nil {
		_ = r.FullResult.Latency.RecordValue(value.Nanoseconds())
	}
	if r.PartialResult.Latency != nil {
		_ = r.PartialResult.Latency.RecordValue(value.Nanoseconds())
	}
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
