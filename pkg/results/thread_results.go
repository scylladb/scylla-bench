package results

import (
	"fmt"
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
