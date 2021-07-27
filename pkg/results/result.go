package results

import (
	"errors"
	"fmt"
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
	"raw": LatencyTypeRaw,
	"fixed-coordinated-omission": LatencyTypeCoordinatedOmissionFixed,
}

type Configuration struct {
	concurrency                   int
	measureLatency                bool
	latencyTypeToPrint            int
	latencyHistogramConfiguration histogramConfiguration
}

type Result struct {
	Final          bool
	ElapsedTime    time.Duration
	Operations     int
	ClusteringRows int
	Errors         int
	CriticalErrors []error
	RawLatency     *hdrhistogram.Histogram
	CoFixedLatency *hdrhistogram.Histogram
}

func SetGlobalHistogramConfiguration(minValue int64, maxValue int64, sigFig int) {
	globalResultConfiguration.latencyHistogramConfiguration.minValue = minValue
	globalResultConfiguration.latencyHistogramConfiguration.maxValue = maxValue
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

func SetGlobalLatencyTypeFromString(latencyType string){
	SetGlobalLatencyType(LatencyTypes[latencyType])
}

func ValidateGlobalLatencyType(latencyType string) error {
	_, ok := LatencyTypes[latencyType]
	if ! ok {
		return errors.New(fmt.Sprintf("unkown value %s, supported values are: raw, fixed-coordinated-omission", latencyType))
	}
	return nil
}


func SetGlobalMeasureLatency(value bool) {
	globalResultConfiguration.measureLatency = value
}

func GetGlobalMeasureLatency() bool {
	return globalResultConfiguration.measureLatency
}

func SetGlobalConcurrency(value int) {
	globalResultConfiguration.concurrency = value
}

func GetGlobalConcurrency() int {
	return globalResultConfiguration.concurrency
}

func NewHistogram(config *histogramConfiguration) *hdrhistogram.Histogram {
	return hdrhistogram.New(config.minValue, config.maxValue, config.sigFig)
}

var globalResultConfiguration Configuration

func init() {
	globalResultConfiguration = Configuration{
		measureLatency: false,
		latencyHistogramConfiguration: histogramConfiguration{
			minValue: 0,
			maxValue: 2 ^ 63 - 1,
			sigFig:   3,
		},
		latencyTypeToPrint: LatencyTypeRaw,
	}
}
