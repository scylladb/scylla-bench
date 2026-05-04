package results

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/HdrHistogram/hdrhistogram-go"

	"github.com/scylladb/scylla-bench/internal/clock"
	"github.com/scylladb/scylla-bench/pkg/config"
	"github.com/scylladb/scylla-bench/pkg/tools"
)

type MergedResult struct {
	clk            clock.Clock
	RawLatency     *hdrhistogram.Histogram
	CoFixedLatency *hdrhistogram.Histogram
	// Separate histograms for mixed mode read/write operations
	RawReadLatency          *hdrhistogram.Histogram
	CoFixedReadLatency      *hdrhistogram.Histogram
	RawWriteLatency         *hdrhistogram.Histogram
	CoFixedWriteLatency     *hdrhistogram.Histogram
	CriticalErrors          []error
	Time                    time.Duration
	Operations              int
	ClusteringRows          int
	Errors                  int
	OperationsPerSecond     float64
	ClusteringRowsPerSecond float64
	HistogramStartTime      int64
}

func NewMergedResult(clk clock.Clock) *MergedResult {
	result := &MergedResult{clk: clk}
	if config.GetGlobalMeasureLatency() {
		histCfg := config.GetGlobalHistogramConfiguration()
		result.HistogramStartTime = clk.NowUnixNano()
		result.RawLatency = NewHistogram(histCfg, "raw")
		result.CoFixedLatency = NewHistogram(histCfg, "co-fixed")
		// Read/write specific histograms are allocated lazily on first merge.
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
			mr.CriticalErrors = append(mr.CriticalErrors, result.CriticalErrors...)
		}
	}
	if config.GetGlobalMeasureLatency() {
		if result.RawLatency != nil {
			droppedRaw := mr.RawLatency.Merge(result.RawLatency)
			if droppedRaw > 0 {
				log.Print("dropped: ", droppedRaw)
			}
		}
		if result.CoFixedLatency != nil {
			droppedCofixed := mr.CoFixedLatency.Merge(result.CoFixedLatency)
			if droppedCofixed > 0 {
				log.Print("dropped: ", droppedCofixed)
			}
		}
		// Merge read/write specific histograms for mixed mode
		if result.RawReadLatency != nil {
			droppedRawRead := ensureHistogram(&mr.RawReadLatency, "raw-read").Merge(result.RawReadLatency)
			if droppedRawRead > 0 {
				log.Print("dropped raw read: ", droppedRawRead)
			}
		}
		if result.CoFixedReadLatency != nil {
			droppedCoFixedRead := ensureHistogram(&mr.CoFixedReadLatency, "co-fixed-read").Merge(result.CoFixedReadLatency)
			if droppedCoFixedRead > 0 {
				log.Print("dropped co-fixed read: ", droppedCoFixedRead)
			}
		}
		if result.RawWriteLatency != nil {
			droppedRawWrite := ensureHistogram(&mr.RawWriteLatency, "raw-write").Merge(result.RawWriteLatency)
			if droppedRawWrite > 0 {
				log.Print("dropped raw write: ", droppedRawWrite)
			}
		}
		if result.CoFixedWriteLatency != nil {
			droppedCoFixedWrite := ensureHistogram(&mr.CoFixedWriteLatency, "co-fixed-write").Merge(result.CoFixedWriteLatency)
			if droppedCoFixedWrite > 0 {
				log.Print("dropped co-fixed write: ", droppedCoFixedWrite)
			}
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
		if !os.IsExist(err) {
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
	if config.GetGlobalLatencyType() == config.LatencyTypeCoordinatedOmissionFixed {
		return mr.CoFixedLatency
	}
	return mr.RawLatency
}

func (mr *MergedResult) SaveLatenciesToHdrHistogram(hdrLogWriter *hdrhistogram.HistogramLogWriter) {
	startTimeMs := mr.HistogramStartTime / 1000000
	endTimeMs := mr.clk.NowUnixNano() / 1000000

	// Save standard histograms
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

	// Save read/write specific histograms for mixed mode
	if mr.RawReadLatency != nil && mr.RawReadLatency.TotalCount() > 0 {
		mr.RawReadLatency.SetStartTimeMs(startTimeMs)
		mr.RawReadLatency.SetEndTimeMs(endTimeMs)
		if err := hdrLogWriter.OutputIntervalHistogram(mr.RawReadLatency); err != nil {
			fmt.Printf("Failed to write raw read hdr histogram: %s\n", err.Error())
		}
	}
	if mr.CoFixedReadLatency != nil && mr.CoFixedReadLatency.TotalCount() > 0 {
		mr.CoFixedReadLatency.SetStartTimeMs(startTimeMs)
		mr.CoFixedReadLatency.SetEndTimeMs(endTimeMs)
		if err := hdrLogWriter.OutputIntervalHistogram(mr.CoFixedReadLatency); err != nil {
			fmt.Printf("Failed to write co-fixed read hdr histogram: %s\n", err.Error())
		}
	}
	if mr.RawWriteLatency != nil && mr.RawWriteLatency.TotalCount() > 0 {
		mr.RawWriteLatency.SetStartTimeMs(startTimeMs)
		mr.RawWriteLatency.SetEndTimeMs(endTimeMs)
		if err := hdrLogWriter.OutputIntervalHistogram(mr.RawWriteLatency); err != nil {
			fmt.Printf("Failed to write raw write hdr histogram: %s\n", err.Error())
		}
	}
	if mr.CoFixedWriteLatency != nil && mr.CoFixedWriteLatency.TotalCount() > 0 {
		mr.CoFixedWriteLatency.SetStartTimeMs(startTimeMs)
		mr.CoFixedWriteLatency.SetEndTimeMs(endTimeMs)
		if err := hdrLogWriter.OutputIntervalHistogram(mr.CoFixedWriteLatency); err != nil {
			fmt.Printf("Failed to write co-fixed write hdr histogram: %s\n", err.Error())
		}
	}
}

func (mr *MergedResult) PrintPartialResult() {
	latencyError := ""
	if config.GetGlobalMeasureLatency() {
		scale := config.GetGlobalHdrLatencyScale()
		latencyHist := mr.getLatencyHistogram()
		fmt.Printf(
			withLatencyLineFmt,
			tools.Round(mr.Time),
			mr.Operations,
			mr.ClusteringRows,
			mr.Errors,
			tools.Round(
				time.Duration(latencyHist.Max()*scale),
			),
			tools.Round(time.Duration(latencyHist.ValueAtQuantile(99.9)*scale)),
			tools.Round(time.Duration(latencyHist.ValueAtQuantile(99)*scale)),
			tools.Round(
				time.Duration(latencyHist.ValueAtQuantile(95)*scale),
			),
			tools.Round(time.Duration(latencyHist.ValueAtQuantile(90)*scale)),
			tools.Round(
				time.Duration(latencyHist.ValueAtQuantile(50)*scale),
			),
			tools.Round(time.Duration(latencyHist.Mean()*float64(scale))),
			latencyError,
		)
	} else {
		fmt.Printf(withoutLatencyLineFmt, tools.Round(mr.Time), mr.Operations, mr.ClusteringRows, mr.Errors)
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
