package results

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
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
	RawLatency               *hdrhistogram.Histogram
	CoFixedWriteLatencyStack *stack.Stack
	CoFixedWriteLatency      *hdrhistogram.Histogram
	RawWriteLatency          *hdrhistogram.Histogram
	CoFixedReadLatency       *hdrhistogram.Histogram
	RawLatencyStack          *stack.Stack
	CoFixedLatencyStack      *stack.Stack
	RawReadLatencyStack      *stack.Stack
	RawReadLatency           *hdrhistogram.Histogram
	CoFixedReadLatencyStack  *stack.Stack
	RawWriteLatencyStack     *stack.Stack
	LatencyToPrint           *hdrhistogram.Histogram
	hdrLogWriter             *hdrhistogram.HistogramLogWriter
	CoFixedLatency           *hdrhistogram.Histogram
	histogramStartTime       int64
	Errors                   uint64
	ClusteringRows           uint64
	Operations               uint64
}

type TotalResult struct {
	criticalErrors []error
	PartialResult
	criticalErrorsMu sync.Mutex
}

func initStack() *stack.Stack {
	return stack.New(config.NumberOfLatencyResultsInPartialReportCycle())
}

// newLatencyPair allocates a histogram and a stack and wires the stack's flush
// callback to record into the histogram.
func newLatencyPair(histCfg *config.HistogramConfiguration, name string) (*hdrhistogram.Histogram, *stack.Stack) {
	h := NewHistogram(histCfg, name)
	s := initStack()
	s.SetDataCallBack(func(data *[]int64, dataLength uint64) {
		d := *data
		for i := range dataLength {
			_ = h.RecordValue(d[i])
		}
	})
	return h, s
}

// NewPartialResult allocates the always-used raw/co-fixed histograms and
// stacks. The mixed-mode read/write histograms are only allocated when
// mixedMode is true to avoid the OOM regression that motivated #270.
func NewPartialResult(mixedMode bool) *PartialResult {
	result := &PartialResult{}

	if !config.GetGlobalMeasureLatency() {
		return result
	}

	histCfg := config.GetGlobalHistogramConfiguration()
	result.RawLatency, result.RawLatencyStack = newLatencyPair(histCfg, "raw")
	result.CoFixedLatency, result.CoFixedLatencyStack = newLatencyPair(histCfg, "co-fixed")

	if mixedMode {
		result.RawReadLatency, result.RawReadLatencyStack = newLatencyPair(histCfg, "raw-read")
		result.CoFixedReadLatency, result.CoFixedReadLatencyStack = newLatencyPair(histCfg, "co-fixed-read")
		result.RawWriteLatency, result.RawWriteLatencyStack = newLatencyPair(histCfg, "raw-write")
		result.CoFixedWriteLatency, result.CoFixedWriteLatencyStack = newLatencyPair(histCfg, "co-fixed-write")
	}

	switch config.GetGlobalLatencyType() {
	case config.LatencyTypeRaw:
		result.LatencyToPrint = result.RawLatency
	case config.LatencyTypeCoordinatedOmissionFixed:
		result.LatencyToPrint = result.CoFixedLatency
	}

	if config.GetGlobalHdrLatencyFile() != "" {
		result.hdrLogWriter = InitHdrLogWriter(
			config.GetGlobalHdrLatencyFile(),
			(time.Now().UnixNano()/1000000000)*1000000000)
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

func NewTotalResult(mixedMode bool) *TotalResult {
	return &TotalResult{
		PartialResult: *NewPartialResult(mixedMode),
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
	if err == nil || *err == nil {
		return
	}
	tr.criticalErrorsMu.Lock()
	tr.criticalErrors = append(tr.criticalErrors, *err)
	tr.criticalErrorsMu.Unlock()
}

func (tr *TotalResult) PrintCriticalErrors() {
	tr.criticalErrorsMu.Lock()
	defer tr.criticalErrorsMu.Unlock()
	if len(tr.criticalErrors) == 0 {
		return
	}
	fmt.Printf("\nFollowing critical errors were caught during the run:\n")
	for _, err := range tr.criticalErrors {
		fmt.Printf("    %s\n", err.Error())
	}
}

func (tr *TotalResult) IsCriticalErrorsFound() bool {
	tr.criticalErrorsMu.Lock()
	defer tr.criticalErrorsMu.Unlock()
	return len(tr.criticalErrors) > 0
}

func NewHistogram(cfg *config.HistogramConfiguration, name string) *hdrhistogram.Histogram {
	histogram := hdrhistogram.New(cfg.MinValue, cfg.MaxValue, cfg.SigFig)
	histogram.SetTag(name)
	return histogram
}

// GetHdrMemoryConsumption estimates the memory used by HDR histograms and
// latency stacks. Each TestRun keeps both a PartialResult (per-cycle) and a
// TotalResult (whole-run) — workers push every measurement into both — so each
// pair holds two sets of histograms+stacks. In mixed mode all six pairs are
// allocated; otherwise only raw and co-fixed.
func GetHdrMemoryConsumption(mixedMode bool) int {
	pairs := 2
	if mixedMode {
		pairs = 6
	}
	const partialAndTotal = 2
	hdrSize := NewHistogram(config.GetGlobalHistogramConfiguration(), "example_hdr").ByteSize() * pairs * partialAndTotal
	stackSize := int(initStack().GetMemoryConsumption()) * pairs * partialAndTotal
	return hdrSize + stackSize
}
