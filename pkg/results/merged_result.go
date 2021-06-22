package results

import (
	"fmt"
	"log"
	"time"

	"github.com/HdrHistogram/hdrhistogram-go"
)

type MergedResult struct {
	Time                    time.Duration
	Operations              int
	ClusteringRows          int
	OperationsPerSecond     float64
	ClusteringRowsPerSecond float64
	Errors                  int
	Latency                 *hdrhistogram.Histogram
}

func NewMergedResult() *MergedResult {
	result := &MergedResult{}
	if globalResultConfiguration.measureLatency {
		result.Latency = NewHistogram(&globalResultConfiguration.latencyHistogramConfiguration)
	}
	return result
}

func (mr *MergedResult) AddResult(result Result) {
	mr.Time += result.ElapsedTime
	mr.Operations += result.Operations
	mr.ClusteringRows += result.ClusteringRows
	mr.OperationsPerSecond += float64(result.Operations) / result.ElapsedTime.Seconds()
	mr.ClusteringRowsPerSecond += float64(result.ClusteringRows) / result.ElapsedTime.Seconds()
	mr.Errors += result.Errors
	if globalResultConfiguration.measureLatency {
		dropped := mr.Latency.Merge(result.Latency)
		if dropped > 0 {
			log.Print("dropped: ", dropped)
		}
	}
}

func (mr *MergedResult) PrintPartialResult() {
	latencyError := ""
	if globalResultConfiguration.measureLatency {
		fmt.Printf(withLatencyLineFmt, Round(mr.Time), mr.Operations, mr.ClusteringRows, mr.Errors,
			Round(time.Duration(mr.Latency.Max())), Round(time.Duration(mr.Latency.ValueAtQuantile(99.9))), Round(time.Duration(mr.Latency.ValueAtQuantile(99))),
			Round(time.Duration(mr.Latency.ValueAtQuantile(95))), Round(time.Duration(mr.Latency.ValueAtQuantile(90))),
			Round(time.Duration(mr.Latency.ValueAtQuantile(50))), Round(time.Duration(mr.Latency.Mean())),
			latencyError)
	} else {
		fmt.Printf(withoutLatencyLineFmt, Round(mr.Time), mr.Operations, mr.ClusteringRows, mr.Errors)
	}
}
