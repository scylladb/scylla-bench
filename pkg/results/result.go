package results

import (
	"fmt"
	"github.com/HdrHistogram/hdrhistogram-go"
	"github.com/scylladb/scylla-bench/pkg/config"
	"github.com/scylladb/scylla-bench/pkg/stack"
	"github.com/scylladb/scylla-bench/pkg/tools"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"
)

const (
	withLatencyLineFmt    = "\n%5v %7v %7v %6v %-6v %-6v %-6v %-6v %-6v %-6v %-6v"
	withoutLatencyLineFmt = "\n%5v %7v %7v %6v"
)

type PartialResult struct {
	hdrLogWriter		*hdrhistogram.HistogramLogWriter
	histogramStartTime 	int64
	Operations          uint64
	ClusteringRows      uint64
	Errors              uint64
	RawLatencyStack     *stack.Stack
	CoFixedLatencyStack *stack.Stack
	LatencyToPrint      *hdrhistogram.Histogram
	RawLatency          *hdrhistogram.Histogram
	CoFixedLatency      *hdrhistogram.Histogram
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
	var RawLatencyStack, CoFixedLatencyStack *stack.Stack
	var RawLatency, CoFixedLatency, LatencyToPrint *hdrhistogram.Histogram
	if config.GetGlobalMeasureLatency() {
		RawLatency = NewHistogram(config.GetGlobalHistogramConfiguration(), "raw")
		CoFixedLatency = NewHistogram(config.GetGlobalHistogramConfiguration(), "co-fixed")

		// TODO: Link histograms to stacks via callbacks
		RawLatencyStack = initStack()
		CoFixedLatencyStack = initStack()
		if config.GetGlobalLatencyType() == config.LatencyTypeRaw {
			LatencyToPrint = RawLatency
		} else if config.GetGlobalLatencyType() == config.LatencyTypeCoordinatedOmissionFixed {
			LatencyToPrint = CoFixedLatency
		}
		if config.GetGlobalHdrLatencyFile() != "" {
			// We need this rounding since hdr histogram rounding up baseTime dividing it by 1000
			//  before reducing it from start time, which is divided by 1000000000 before applied to histogram
			// Which gives small chance that rounded baseTime would be greater than histogram start time, which will lead to
			//  negative time in the histogram log
			hdrLogWriter = InitHdrLogWriter(
				config.GetGlobalHdrLatencyFile(),
				(time.Now().UnixNano() / 1000000000) * 1000000000)
		}
	}
	result := &PartialResult{
		hdrLogWriter: hdrLogWriter,
		Operations: 0,
		ClusteringRows: 0,
		Errors: 0,
		RawLatencyStack: RawLatencyStack,
		CoFixedLatencyStack: CoFixedLatencyStack,
		RawLatency: RawLatency,
		CoFixedLatency: CoFixedLatency,
		LatencyToPrint: LatencyToPrint,
	}
	if RawLatencyStack != nil {
		RawLatencyStack.SetDataCallBack(func(data *[]int64, dataLength uint64){
			dataResolved := *data
			for i := uint64(0); i < dataLength; i ++ {
				RawLatency.RecordValue(dataResolved[i])
			}
		})
	}
	if CoFixedLatencyStack != nil {
		result.CoFixedLatencyStack.SetDataCallBack(func(data *[]int64, dataLength uint64) {
			dataResolved := *data
			for i := uint64(0); i < dataLength; i++ {
				CoFixedLatency.RecordValue(dataResolved[i])
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


func NewTotalResult(concurrency int) *TotalResult {
	return &TotalResult{
		*NewPartialResult(),
		make([]error, concurrency * 100),
		0,
	}
}

func (tr *PartialResult) Reset() {
	tr.histogramStartTime = time.Now().UnixNano()
	tr.Operations = 0
	tr.ClusteringRows = 0
	tr.CoFixedLatency.Reset()
	tr.RawLatency.Reset()
}

func (tr *PartialResult) FlushDataToHistogram() bool {
	isThereNewData := tr.RawLatencyStack.Swap(false)
	isThereNewCoFixedData := tr.CoFixedLatencyStack.Swap(false)
	return isThereNewData || isThereNewCoFixedData
}

func (tr *PartialResult) SaveLatenciesToHdrHistogram() {
	if tr.hdrLogWriter == nil {
		return
	}
	startTimeMs := tr.histogramStartTime / 1000000000
	endTimeMs := time.Now().UnixNano() / 1000000000
	tr.CoFixedLatency.SetStartTimeMs(startTimeMs)
	tr.CoFixedLatency.SetEndTimeMs(endTimeMs)
	if err := tr.hdrLogWriter.OutputIntervalHistogram(tr.CoFixedLatency); err != nil {
		fmt.Printf("Failed to write co-fixed hdr histogram: %s\n", err.Error())
	}
	tr.RawLatency.SetStartTimeMs(startTimeMs)
	tr.RawLatency.SetEndTimeMs(endTimeMs)
	if err := tr.hdrLogWriter.OutputIntervalHistogram(tr.RawLatency); err != nil {
		fmt.Printf("Failed to write raw hdr histogram: %s\n", err.Error())
	}
}

func (tr *PartialResult) PrintPartialResult(printTime time.Duration) {
	if tr.LatencyToPrint == nil {
		fmt.Printf(withoutLatencyLineFmt, tools.Round(printTime), tr.Operations, tr.ClusteringRows, tr.Errors)
		return
	}
	scale := config.GetGlobalHdrLatencyScale()
	var latencyHist = tr.LatencyToPrint
	fmt.Printf(
		withLatencyLineFmt,
		tools.Round(printTime), tr.Operations, tr.ClusteringRows, tr.Errors,
		tools.Round(time.Duration(latencyHist.Max() * scale)),
		tools.Round(time.Duration(latencyHist.ValueAtQuantile(99.9) * scale)),
		tools.Round(time.Duration(latencyHist.ValueAtQuantile(99) * scale)),
		tools.Round(time.Duration(latencyHist.ValueAtQuantile(95) * scale)),
		tools.Round(time.Duration(latencyHist.ValueAtQuantile(90) * scale)),
		tools.Round(time.Duration(latencyHist.ValueAtQuantile(50) * scale)),
		tools.Round(time.Duration(latencyHist.Mean() * float64(scale))),
		)
}

func (tr *PartialResult) PrintPartialResultHeader() {
	if tr.LatencyToPrint == nil {
		fmt.Printf(withoutLatencyLineFmt, "time", "ops/s", "rows/s", "errors")
	} else {
		fmt.Printf(withLatencyLineFmt, "time", "ops/s", "rows/s", "errors", "max", "99.9th", "99th", "95th", "90th", "median", "mean")
	}
}

func (tr *TotalResult) SubmitCriticalError(err *error) {
	idx := atomic.AddUint32(&tr.criticalNum, 1)
	tr.criticalErrors[idx] = *err
}

func (tr *TotalResult) PrintCriticalErrors() {
	if ! tr.IsCriticalErrorsFound() {
		return
	}
	fmt.Printf("\nFollowing critical errors where caught during the run:\n")
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

func NewHistogram(config *config.HistogramConfiguration, name string) *hdrhistogram.Histogram {
	histogram := hdrhistogram.New(config.MinValue, config.MaxValue, config.SigFig)
	histogram.SetTag(name)
	return histogram
}


func GetHdrMemoryConsumption() int {
	// Size of four HDR Histograms
	hdrSize := NewHistogram(config.GetGlobalHistogramConfiguration(), "example_hdr").ByteSize() * 4
	// Size of four Stacks
	stackSize := int(initStack().GetMemoryConsumption()) * 4
	return hdrSize + stackSize
}
