package results

import (
	"fmt"
	"github.com/HdrHistogram/hdrhistogram-go"
	"time"
)

const (
	withLatencyLineFmt    = "\n%5v %7v %7v %6v %-6v %-6v %-6v %-6v %-6v %-6v %-6v %v"
	withoutLatencyLineFmt = "\n%5v %7v %7v %6v"
)

type TestResults struct {
	threadResults   []*TestThreadResult
	numberOfThreads int
	startTime       time.Time
}

func (tr *TestResults) Init(concurrency int) {
	tr.threadResults = make([]*TestThreadResult, concurrency)
	tr.numberOfThreads = concurrency
	for i := range tr.threadResults {
		tr.threadResults[i] = NewTestThreadResult()
	}
}

func (tr *TestResults) SetStartTime() {
	tr.startTime = time.Now()
}

func (tr *TestResults) GetTestResult(idx int) *TestThreadResult {
	return tr.threadResults[idx]
}

func (tr *TestResults) GetTestResults() []*TestThreadResult {
	return tr.threadResults
}

func (tr *TestResults) GetResultsFromThreadsAndMerge() (bool, *MergedResult) {
	result := NewMergedResult()
	final := false
	for i, ch := range tr.threadResults {
		res := <-ch.ResultChannel
		if !final && res.Final {
			final = true
			result = NewMergedResult()
			for _, ch2 := range tr.threadResults[0:i] {
				res = <-ch2.ResultChannel
				for !res.Final {
					res = <-ch2.ResultChannel
				}
				result.AddResult(res)
			}
		} else if final && !res.Final {
			for !res.Final {
				res = <-ch.ResultChannel
			}
		}
		result.AddResult(res)
	}
	result.Time /= time.Duration(globalResultConfiguration.concurrency)
	return final, result
}

func (tr *TestResults) GetTotalResults() *MergedResult {
	final, result := tr.GetResultsFromThreadsAndMerge()
	for !final {
		result.Time = time.Since(tr.startTime)
		result.PrintPartialResult()
		final, result = tr.GetResultsFromThreadsAndMerge()
	}
	return result
}

func (tr *TestResults) PrintResultsHeader() {
	if globalResultConfiguration.measureLatency {
		fmt.Printf(withLatencyLineFmt, "time", "ops/s", "rows/s", "errors", "max", "99.9th", "99th", "95th", "90th", "median", "mean", "")
	} else {
		fmt.Printf(withoutLatencyLineFmt, "time", "ops/s", "rows/s", "errors")
	}
}

func (tr *TestResults) PrintTotalResults(result *MergedResult) {
	fmt.Println("\nResults")
	fmt.Println("Time (avg):\t", result.Time)
	fmt.Println("Total ops:\t", result.Operations)
	fmt.Println("Total rows:\t", result.ClusteringRows)
	if result.Errors != 0 {
		fmt.Println("Total errors:\t", result.Errors)
	}
	fmt.Println("Operations/s:\t", result.OperationsPerSecond)
	fmt.Println("Rows/s:\t\t", result.ClusteringRowsPerSecond)
	if globalResultConfiguration.measureLatency {
		printLatencyResults("raw latency", result.RawLatency)
		printLatencyResults("c-o fixed latency", result.CoFixedLatency)
	}
}

func printLatencyResults(name string, latency *hdrhistogram.Histogram) {
	fmt.Println(name, ":\n  max:\t\t", time.Duration(latency.Max()),
		"\n  99.9th:\t", time.Duration(latency.ValueAtQuantile(99.9)),
		"\n  99th:\t\t", time.Duration(latency.ValueAtQuantile(99)),
		"\n  95th:\t\t", time.Duration(latency.ValueAtQuantile(95)),
		"\n  90th:\t\t", time.Duration(latency.ValueAtQuantile(90)),
		"\n  median:\t", time.Duration(latency.ValueAtQuantile(50)),
		"\n  mean:\t\t", time.Duration(latency.Mean()))
}