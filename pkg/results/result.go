package results

import (
	"time"

	"github.com/HdrHistogram/hdrhistogram-go"
)

type histogramConfiguration struct {
	minValue int64
	maxValue int64
	sigFig   int
}

type Configuration struct {
	concurrency                   int
	measureLatency                bool
	latencyHistogramConfiguration histogramConfiguration
}

type Result struct {
	Final          bool
	ElapsedTime    time.Duration
	Operations     int
	ClusteringRows int
	Errors         int
	Latency        *hdrhistogram.Histogram
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
	}
}
