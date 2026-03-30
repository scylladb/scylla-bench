package results

import (
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/HdrHistogram/hdrhistogram-go"

	"github.com/scylladb/scylla-bench/pkg/config"
	"github.com/scylladb/scylla-bench/pkg/stack"
	"github.com/scylladb/scylla-bench/pkg/tools"
)

const (
	withLatencyLineFmt    = "\n%8v %7v %7v %6v %-6v %-6v %-6v %-6v %-6v %-6v %-6v %v"
	withoutLatencyLineFmt = "\n%8v %7v %7v %6v"
)

type PartialResult struct {
	hdrLogWriter             *hdrhistogram.HistogramLogWriter
	histogramStartTime       int64
	Operations               uint64
	ClusteringRows           uint64
	Errors                   uint64
	RawLatencyStack          *stack.Stack
	CoFixedLatencyStack      *stack.Stack
	RawReadLatencyStack      *stack.Stack
	CoFixedReadLatencyStack  *stack.Stack
	RawWriteLatencyStack     *stack.Stack
	CoFixedWriteLatencyStack *stack.Stack
	LatencyToPrint           *hdrhistogram.Histogram
	RawLatency               *hdrhistogram.Histogram
	CoFixedLatency           *hdrhistogram.Histogram
	RawReadLatency           *hdrhistogram.Histogram
	CoFixedReadLatency       *hdrhistogram.Histogram
	RawWriteLatency          *hdrhistogram.Histogram
	CoFixedWriteLatency      *hdrhistogram.Histogram
}

type TotalResult struct {
	PartialResult
	criticalErrors []error
	criticalNum    uint32
}

func initStack() *stack.Stack {
	return stack.New(config.NumberOfLatencyResultsInPartialReportCycle())
}

func NewPartialResult() *PartialResult {
	var hdrLogWriter *hdrhistogram.HistogramLogWriter
	var rawLatencyStack, coFixedLatencyStack *stack.Stack
	var rawReadLatencyStack, coFixedReadLatencyStack *stack.Stack
	var rawWriteLatencyStack, coFixedWriteLatencyStack *stack.Stack
	var rawLatency, coFixedLatency, latencyToPrint *hdrhistogram.Histogram
	var rawReadLatency, coFixedReadLatency *hdrhistogram.Histogram
	var rawWriteLatency, coFixedWriteLatency *hdrhistogram.Histogram

	if config.GetGlobalMeasureLatency() {
		histCfg := config.GetGlobalHistogramConfiguration()
		rawLatency = NewHistogram(histCfg, "raw")
		coFixedLatency = NewHistogram(histCfg, "co-fixed")
		rawReadLatency = NewHistogram(histCfg, "raw-read")
		coFixedReadLatency = NewHistogram(histCfg, "co-fixed-read")
		rawWriteLatency = NewHistogram(histCfg, "raw-write")
		coFixedWriteLatency = NewHistogram(histCfg, "co-fixed-write")

		rawLatencyStack = initStack()
		coFixedLatencyStack = initStack()
		rawReadLatencyStack = initStack()
		coFixedReadLatencyStack = initStack()
		rawWriteLatencyStack = initStack()
		coFixedWriteLatencyStack = initStack()

		if config.GetGlobalLatencyType() == config.LatencyTypeRaw {
			latencyToPrint = rawLatency
		} else if config.GetGlobalLatencyType() == config.LatencyTypeCoordinatedOmissionFixed {
			latencyToPrint = coFixedLatency
		}

		if config.GetGlobalHdrLatencyFile() != "" {
			hdrLogWriter = InitHdrLogWriter(
				config.GetGlobalHdrLatencyFile(),
				(time.Now().UnixNano()/1000000000)*1000000000)
		}
	}

	result := &PartialResult{
		hdrLogWriter:             hdrLogWriter,
		Operations:               0,
		ClusteringRows:           0,
		Errors:                   0,
		RawLatencyStack:          rawLatencyStack,
		CoFixedLatencyStack:      coFixedLatencyStack,
		RawReadLatencyStack:      rawReadLatencyStack,
		CoFixedReadLatencyStack:  coFixedReadLatencyStack,
		RawWriteLatencyStack:     rawWriteLatencyStack,
		CoFixedWriteLatencyStack: coFixedWriteLatencyStack,
		RawLatency:               rawLatency,
		CoFixedLatency:           coFixedLatency,
		RawReadLatency:           rawReadLatency,
		CoFixedReadLatency:       coFixedReadLatency,
		RawWriteLatency:          rawWriteLatency,
		CoFixedWriteLatency:      coFixedWriteLatency,
		LatencyToPrint:           latencyToPrint,
	}

	if rawLatencyStack != nil {
		rawLatencyStack.SetDataCallBack(func(data *[]int64, dataLength uint64) {
			dataResolved := *data
			for i := uint64(0); i < dataLength; i++ {
				rawLatency.RecordValue(dataResolved[i])
			}
		})
	}
	if coFixedLatencyStack != nil {
		coFixedLatencyStack.SetDataCallBack(func(data *[]int64, dataLength uint64) {
			dataResolved := *data
			for i := uint64(0); i < dataLength; i++ {
				coFixedLatency.RecordValue(dataResolved[i])
			}
		})
	}
	if rawReadLatencyStack != nil {
		rawReadLatencyStack.SetDataCallBack(func(data *[]int64, dataLength uint64) {
			dataResolved := *data
			for i := uint64(0); i < dataLength; i++ {
				rawReadLatency.RecordValue(dataResolved[i])
			}
		})
	}
	if coFixedReadLatencyStack != nil {
		coFixedReadLatencyStack.SetDataCallBack(func(data *[]int64, dataLength uint64) {
			dataResolved := *data
			for i := uint64(0); i < dataLength; i++ {
				coFixedReadLatency.RecordValue(dataResolved[i])
			}
		})
	}
	if rawWriteLatencyStack != nil {
		rawWriteLatencyStack.SetDataCallBack(func(data *[]int64, dataLength uint64) {
			dataResolved := *data
			for i := uint64(0); i < dataLength; i++ {
				rawWriteLatency.RecordValue(dataResolved[i])
			}
		})
	}
	if coFixedWriteLatencyStack != nil {
		coFixedWriteLatencyStack.SetDataCallBack(func(data *[]int64, dataLength uint64) {
			dataResolved := *data
			for i := uint64(0); i < dataLength; i++ {
				coFixedWriteLatency.RecordValue(dataResolved[i])
			}
		})
	}

	return result
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

func NewTotalResult(concurrency int) *TotalResult {
	return &TotalResult{
		*NewPartialResult(),
		make([]error, concurrency*100),
		0,
	}
}

func (pr *PartialResult) Reset() {
	pr.histogramStartTime = time.Now().UnixNano()
	pr.Operations = 0
	pr.ClusteringRows = 0
	pr.Errors = 0
	if pr.RawLatency != nil {
		pr.RawLatency.Reset()
	}
	if pr.CoFixedLatency != nil {
		pr.CoFixedLatency.Reset()
	}
	if pr.RawReadLatency != nil {
		pr.RawReadLatency.Reset()
	}
	if pr.CoFixedReadLatency != nil {
		pr.CoFixedReadLatency.Reset()
	}
	if pr.RawWriteLatency != nil {
		pr.RawWriteLatency.Reset()
	}
	if pr.CoFixedWriteLatency != nil {
		pr.CoFixedWriteLatency.Reset()
	}
}

func (pr *PartialResult) FlushDataToHistogram() bool {
	isNew := false
	if pr.RawLatencyStack != nil {
		isNew = pr.RawLatencyStack.Swap(false) || isNew
	}
	if pr.CoFixedLatencyStack != nil {
		isNew = pr.CoFixedLatencyStack.Swap(false) || isNew
	}
	if pr.RawReadLatencyStack != nil {
		isNew = pr.RawReadLatencyStack.Swap(false) || isNew
	}
	if pr.CoFixedReadLatencyStack != nil {
		isNew = pr.CoFixedReadLatencyStack.Swap(false) || isNew
	}
	if pr.RawWriteLatencyStack != nil {
		isNew = pr.RawWriteLatencyStack.Swap(false) || isNew
	}
	if pr.CoFixedWriteLatencyStack != nil {
		isNew = pr.CoFixedWriteLatencyStack.Swap(false) || isNew
	}
	return isNew
}

func (pr *PartialResult) SaveLatenciesToHdrHistogram() {
	if pr.hdrLogWriter == nil {
		return
	}
	startTimeMs := pr.histogramStartTime / 1000000000
	endTimeMs := time.Now().UnixNano() / 1000000000
	if pr.CoFixedLatency != nil {
		pr.CoFixedLatency.SetStartTimeMs(startTimeMs)
		pr.CoFixedLatency.SetEndTimeMs(endTimeMs)
		if err := pr.hdrLogWriter.OutputIntervalHistogram(pr.CoFixedLatency); err != nil {
			fmt.Printf("Failed to write co-fixed hdr histogram: %s\n", err.Error())
		}
	}
	if pr.RawLatency != nil {
		pr.RawLatency.SetStartTimeMs(startTimeMs)
		pr.RawLatency.SetEndTimeMs(endTimeMs)
		if err := pr.hdrLogWriter.OutputIntervalHistogram(pr.RawLatency); err != nil {
			fmt.Printf("Failed to write raw hdr histogram: %s\n", err.Error())
		}
	}
}

func (pr *PartialResult) PrintPartialResult(printTime time.Duration) {
	if pr.LatencyToPrint == nil {
		fmt.Printf(withoutLatencyLineFmt, tools.Round(printTime), pr.Operations, pr.ClusteringRows, pr.Errors)
		return
	}
	scale := config.GetGlobalHdrLatencyScale()
	latencyHist := pr.LatencyToPrint
	fmt.Printf(
		withLatencyLineFmt,
		tools.Round(printTime), pr.Operations, pr.ClusteringRows, pr.Errors,
		tools.Round(time.Duration(latencyHist.Max()*scale)),
		tools.Round(time.Duration(latencyHist.ValueAtQuantile(99.9)*scale)),
		tools.Round(time.Duration(latencyHist.ValueAtQuantile(99)*scale)),
		tools.Round(time.Duration(latencyHist.ValueAtQuantile(95)*scale)),
		tools.Round(time.Duration(latencyHist.ValueAtQuantile(90)*scale)),
		tools.Round(time.Duration(latencyHist.ValueAtQuantile(50)*scale)),
		tools.Round(time.Duration(latencyHist.Mean()*float64(scale))),
		"",
	)
}

func (pr *PartialResult) PrintPartialResultHeader() {
	if pr.LatencyToPrint == nil {
		fmt.Printf(withoutLatencyLineFmt, "time", "ops/s", "rows/s", "errors")
	} else {
		fmt.Printf(withLatencyLineFmt, "time", "ops/s", "rows/s", "errors", "max", "99.9th", "99th", "95th", "90th", "median", "mean", "")
	}
}

func (tr *TotalResult) SubmitCriticalError(err *error) {
	idx := atomic.AddUint32(&tr.criticalNum, 1)
	tr.criticalErrors[idx] = *err
}

func (tr *TotalResult) PrintCriticalErrors() {
	if !tr.IsCriticalErrorsFound() {
		return
	}
	fmt.Printf("\nFollowing critical errors were caught during the run:\n")
	for _, err := range tr.criticalErrors {
		if err != nil {
			fmt.Printf("    %s\n", err.Error())
		}
	}
}

func (tr *TotalResult) IsCriticalErrorsFound() bool {
	if tr.criticalErrors == nil || len(tr.criticalErrors) == 0 {
		return false
	}
	for _, err := range tr.criticalErrors {
		if err != nil {
			return true
		}
	}
	return false
}

func NewHistogram(cfg *config.HistogramConfiguration, name string) *hdrhistogram.Histogram {
	histogram := hdrhistogram.New(cfg.MinValue, cfg.MaxValue, cfg.SigFig)
	histogram.SetTag(name)
	return histogram
}

func GetHdrMemoryConsumption() int {
	hdrSize := NewHistogram(config.GetGlobalHistogramConfiguration(), "example_hdr").ByteSize() * 6
	stackSize := int(initStack().GetMemoryConsumption()) * 6
	return hdrSize + stackSize
}
