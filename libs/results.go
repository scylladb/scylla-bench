package libs

import (
	"github.com/codahale/hdrhistogram"
	"log"
	"time"
)

func NewHistogram(timeout time.Duration, measureLatency bool) *hdrhistogram.Histogram {
	if !measureLatency {
		return nil
	}
	return hdrhistogram.New(time.Microsecond.Nanoseconds()*50, (timeout + timeout*2).Nanoseconds(), 3)
}

func NewMergedResult(timeout time.Duration, measureLatency bool) *MergedResult {
	result := &MergedResult{}
	result.Latency = NewHistogram(timeout, measureLatency)
	return result
}

func MergeResults(results []chan Result, timeout time.Duration, measureLatency bool, concurrency int) (bool, *MergedResult) {
	result := NewMergedResult(timeout, measureLatency)
	final := false
	for i, ch := range results {
		res := <-ch
		if !final && res.Final {
			final = true
			result = NewMergedResult(timeout, measureLatency)
			for _, ch2 := range results[0:i] {
				res = <-ch2
				for !res.Final {
					res = <-ch2
				}
				result.AddResult(res, measureLatency)
			}
		} else if final && !res.Final {
			for !res.Final {
				res = <-ch
			}
		}
		result.AddResult(res, measureLatency)
	}
	result.Time /= time.Duration(concurrency)
	return final, result
}

type Result struct {
	Final          bool
	ElapsedTime    time.Duration
	Operations     int
	ClusteringRows int
	Errors         int
	Latency        *hdrhistogram.Histogram
}


func (mr *MergedResult) AddResult(result Result, measureLatency bool) {
	mr.Time += result.ElapsedTime
	mr.Operations += result.Operations
	mr.ClusteringRows += result.ClusteringRows
	mr.OperationsPerSecond += float64(result.Operations) / result.ElapsedTime.Seconds()
	mr.ClusteringRowsPerSecond += float64(result.ClusteringRows) / result.ElapsedTime.Seconds()
	mr.Errors += result.Errors
	if measureLatency {
		dropped := mr.Latency.Merge(result.Latency)
		if dropped > 0 {
			log.Print("dropped: ", dropped)
		}
	}
}
