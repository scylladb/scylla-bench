package libs

import (
	"fmt"
	"time"
)

const (
	withLatencyLineFmt    = "\n%5v %7v %7v %6v %-6v %-6v %-6v %-6v %-6v %-6v %-6v %v"
	withoutLatencyLineFmt = "\n%5v %7v %7v %6v"
)


func PrintLatencyHeader(){
	fmt.Printf(withLatencyLineFmt, "time", "ops/s", "rows/s", "errors", "max", "99.9th", "99th", "95th", "90th", "median", "mean", "")
}

func PrintWithoutLatencyHeader() {
	fmt.Printf(withoutLatencyLineFmt, "time", "ops/s", "rows/s", "errors")
}

func PrintPartialResult(result *MergedResult, errorRecordingLatency bool, measureLatency bool) {
	latencyError := ""
	if errorRecordingLatency {
		latencyError = "latency measurement error"
	}
	if measureLatency {
		fmt.Printf(withLatencyLineFmt, Round(result.Time), result.Operations, result.ClusteringRows, result.Errors,
			Round(time.Duration(result.Latency.Max())), Round(time.Duration(result.Latency.ValueAtQuantile(99.9))), Round(time.Duration(result.Latency.ValueAtQuantile(99))),
			Round(time.Duration(result.Latency.ValueAtQuantile(95))), Round(time.Duration(result.Latency.ValueAtQuantile(90))),
			Round(time.Duration(result.Latency.ValueAtQuantile(50))), Round(time.Duration(result.Latency.Mean())),
			latencyError)
	} else {
		fmt.Printf(withoutLatencyLineFmt, Round(result.Time), result.Operations, result.ClusteringRows, result.Errors)
	}
}
