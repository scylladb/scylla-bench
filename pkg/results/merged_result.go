package results

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
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
	CriticalErrors          []error
	HistogramStartTime		int64
	RawLatency              *hdrhistogram.Histogram
	CoFixedLatency 			*hdrhistogram.Histogram
}

func NewMergedResult() *MergedResult {
	result := &MergedResult{}
	if globalResultConfiguration.measureLatency {
		result.HistogramStartTime = time.Now().UnixNano()
		result.RawLatency = NewHistogram(&globalResultConfiguration.latencyHistogramConfiguration, "raw")
		result.CoFixedLatency = NewHistogram(&globalResultConfiguration.latencyHistogramConfiguration, "co-fixed")
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
	if result.CriticalErrors != nil {
		if mr.CriticalErrors == nil {
			mr.CriticalErrors = result.CriticalErrors
		} else {
			for _, err := range result.CriticalErrors {
				mr.CriticalErrors = append(mr.CriticalErrors, err)
			}
		}
	}
	if globalResultConfiguration.measureLatency {
		dropped := mr.RawLatency.Merge(result.RawLatency)
		if dropped > 0 {
			log.Print("dropped: ", dropped)
		}
		dropped = mr.CoFixedLatency.Merge(result.CoFixedLatency)
		if dropped > 0 {
			log.Print("dropped: ", dropped)
		}
	}
}

func InitHdrLogWriter(fileName string, baseTime int64) *hdrhistogram.HistogramLogWriter {
	fileNameAbs, err := filepath.Abs(fileName)
	if err != nil {
		panic(err)
	}
	dirName := filepath.Dir(fileNameAbs)
	err = os.MkdirAll(dirName, os.ModePerm)
	if err != nil {
		if ! os.IsExist(err) {
			panic(err)
		}
	}

	file, err := os.OpenFile(fileName, os.O_WRONLY|os.O_CREATE, os.ModePerm)
	if err != nil {
		panic(err)
	}

	writer := hdrhistogram.NewHistogramLogWriter(file)
	if err = writer.OutputLogFormatVersion(); err != nil {
		panic(err)
	}

	if err = writer.OutputComment("Logging op latencies for Cassandra Stress"); err != nil {
		panic(err)
	}
	baseTimeMsec := baseTime / 1000000
	writer.SetBaseTime(baseTimeMsec)
	if err = writer.OutputBaseTime(baseTimeMsec); err != nil {
		panic(err)
	}
	if err = writer.OutputStartTime(baseTimeMsec); err != nil {
		panic(err)
	}
	if err = writer.OutputLegend(); err != nil {
		panic(err)
	}
	return writer
}

func (mr *MergedResult) getLatencyHistogram() *hdrhistogram.Histogram {
	if globalResultConfiguration.latencyTypeToPrint == LatencyTypeCoordinatedOmissionFixed {
		return mr.CoFixedLatency
	}
	return mr.RawLatency
}

func (mr *MergedResult) SaveLatenciesToHdrHistogram(hdrLogWriter *hdrhistogram.HistogramLogWriter) {
	startTimeMs := mr.HistogramStartTime / 1000000000
	endTimeMs := time.Now().UnixNano() / 1000000000
	mr.CoFixedLatency.SetStartTimeMs(startTimeMs)
	mr.CoFixedLatency.SetEndTimeMs(endTimeMs)
	if err := hdrLogWriter.OutputIntervalHistogram(mr.CoFixedLatency); err != nil {
		fmt.Printf("Failed to write co-fixed hdr histogram: %s\n", err.Error())
	}
	mr.RawLatency.SetStartTimeMs(startTimeMs)
	mr.RawLatency.SetEndTimeMs(endTimeMs)
	if err := hdrLogWriter.OutputIntervalHistogram(mr.RawLatency); err != nil {
		fmt.Printf("Failed to write raw hdr histogram: %s\n", err.Error())
	}
}

func (mr *MergedResult) PrintPartialResult() {
	latencyError := ""
	if globalResultConfiguration.measureLatency {
		scale := globalResultConfiguration.hdrLatencyScale
		var latencyHist = mr.getLatencyHistogram()
		fmt.Printf(withLatencyLineFmt, Round(mr.Time), mr.Operations, mr.ClusteringRows, mr.Errors,
			Round(time.Duration(latencyHist.Max() * scale)), Round(time.Duration(latencyHist.ValueAtQuantile(99.9) * scale)), Round(time.Duration(latencyHist.ValueAtQuantile(99) * scale)),
			Round(time.Duration(latencyHist.ValueAtQuantile(95) * scale)), Round(time.Duration(latencyHist.ValueAtQuantile(90) * scale)),
			Round(time.Duration(latencyHist.ValueAtQuantile(50) * scale)), Round(time.Duration(latencyHist.Mean() * float64(scale))),
			latencyError)
	} else {
		fmt.Printf(withoutLatencyLineFmt, Round(mr.Time), mr.Operations, mr.ClusteringRows, mr.Errors)
	}
}

func (mr *MergedResult) PrintCriticalErrors() {
	if mr.CriticalErrors != nil {
		fmt.Printf("\nFollowing critical errors were caught during the run:\n")
		for _, err := range mr.CriticalErrors {
			fmt.Printf("    %s\n", err.Error())
		}
	}
}
