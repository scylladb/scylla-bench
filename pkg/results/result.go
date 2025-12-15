package results

import (
	"fmt"
	"math"
	"time"

	"github.com/HdrHistogram/hdrhistogram-go"
)

type histogramConfiguration struct {
	minValue int64
	maxValue int64
	sigFig   int
}

const (
	LatencyTypeCoordinatedOmissionFixed = iota
	LatencyTypeRaw                      = iota
)

var LatencyTypes = map[string]int{
	"raw":                        LatencyTypeRaw,
	"fixed-coordinated-omission": LatencyTypeCoordinatedOmissionFixed,
}

type Configuration struct {
	hdrLatencyFile                string
	latencyHistogramConfiguration histogramConfiguration
	concurrency                   int
	hdrLatencyScale               int64
	latencyTypeToPrint            int
	measureLatency                bool
}

type Result struct {
	RawLatency     *hdrhistogram.Histogram
	CoFixedLatency *hdrhistogram.Histogram
	// Separate histograms for mixed mode read/write operations
	RawReadLatency      *hdrhistogram.Histogram
	CoFixedReadLatency  *hdrhistogram.Histogram
	RawWriteLatency     *hdrhistogram.Histogram
	CoFixedWriteLatency *hdrhistogram.Histogram
	CriticalErrors      []error
	ElapsedTime         time.Duration
	Operations          int
	ClusteringRows      int
	Errors              int
	Final               bool
}

func SetGlobalHistogramConfiguration(minValue, maxValue int64, sigFig int) {
	globalResultConfiguration.latencyHistogramConfiguration.minValue = minValue / globalResultConfiguration.hdrLatencyScale
	globalResultConfiguration.latencyHistogramConfiguration.maxValue = maxValue / globalResultConfiguration.hdrLatencyScale
	globalResultConfiguration.latencyHistogramConfiguration.sigFig = sigFig
}

func GetGlobalHistogramConfiguration() (int64, int64, int) {
	return globalResultConfiguration.latencyHistogramConfiguration.minValue,
		globalResultConfiguration.latencyHistogramConfiguration.maxValue,
		globalResultConfiguration.latencyHistogramConfiguration.sigFig
}

func SetGlobalLatencyType(latencyType int) {
	globalResultConfiguration.latencyTypeToPrint = latencyType
}

func GetGlobalLatencyType(latencyType int) {
	globalResultConfiguration.latencyTypeToPrint = latencyType
}

func SetGlobalLatencyTypeFromString(latencyType string) {
	SetGlobalLatencyType(LatencyTypes[latencyType])
}

func ValidateGlobalLatencyType(latencyType string) error {
	_, ok := LatencyTypes[latencyType]
	if !ok {
		//nolint:revive,lll,simple
		return fmt.Errorf(
			"unknown value %s, supported values are: raw, fixed-coordinated-omission",
			latencyType,
		)
	}
	return nil
}

func SetGlobalMeasureLatency(value bool) {
	globalResultConfiguration.measureLatency = value
}

func GetGlobalMeasureLatency() bool {
	return globalResultConfiguration.measureLatency
}

func SetGlobalHdrLatencyFile(value string) {
	globalResultConfiguration.hdrLatencyFile = value
}

func SetGlobalHdrLatencyUnits(value string) {
	switch value {
	case "ns":
		globalResultConfiguration.hdrLatencyScale = 1
	case "us":
		globalResultConfiguration.hdrLatencyScale = 1000
	case "ms":
		globalResultConfiguration.hdrLatencyScale = 1000000
	default:
		panic("Wrong value for hdr-latency-scale, only supported values are: ns, us and ms")
	}
}

func SetGlobalConcurrency(value int) {
	globalResultConfiguration.concurrency = value
}

func GetGlobalConcurrency() int {
	return globalResultConfiguration.concurrency
}

func NewHistogram(config *histogramConfiguration, name string) *hdrhistogram.Histogram {
	histogram := hdrhistogram.New(config.minValue, config.maxValue, config.sigFig)
	histogram.SetTag(name)
	return histogram
}

var globalResultConfiguration Configuration

func GetHdrMemoryConsumption(concurrency int) int {
	hdr := NewHistogram(&globalResultConfiguration.latencyHistogramConfiguration, "example_hdr")
	return hdr.ByteSize() * concurrency * 4
}

func init() {
	globalResultConfiguration = Configuration{
		measureLatency: false,
		latencyHistogramConfiguration: histogramConfiguration{
			minValue: 0,
			maxValue: math.MaxInt64,
			sigFig:   3,
		},
		latencyTypeToPrint: LatencyTypeRaw,
	}
}
