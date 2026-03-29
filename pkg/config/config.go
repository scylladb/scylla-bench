package config

import (
	"fmt"
	"math"
	"time"
)

type LatencyType int

const (
	LatencyTypeCoordinatedOmissionFixed LatencyType = iota
	LatencyTypeRaw                      LatencyType = iota
)

var LatencyTypes = map[string]LatencyType{
	"raw":                        LatencyTypeRaw,
	"fixed-coordinated-omission": LatencyTypeCoordinatedOmissionFixed,
}

type HistogramConfiguration struct {
	MinValue int64
	MaxValue int64
	SigFig   int
}

type Configuration struct {
	concurrency                   int
	measureLatency                bool
	hdrLatencyFile                string
	hdrLatencyScale               int64
	latencyTypeToPrint            LatencyType
	latencyHistogramConfiguration HistogramConfiguration
	reportingCycle                time.Duration
}

var globalConfig Configuration

func SetGlobalHistogramConfiguration(minValue, maxValue int64, sigFig int) {
	globalConfig.latencyHistogramConfiguration.MinValue = minValue / globalConfig.hdrLatencyScale
	globalConfig.latencyHistogramConfiguration.MaxValue = maxValue / globalConfig.hdrLatencyScale
	globalConfig.latencyHistogramConfiguration.SigFig = sigFig
}

func GetGlobalHistogramConfiguration() *HistogramConfiguration {
	return &globalConfig.latencyHistogramConfiguration
}

func SetGlobalLatencyType(latencyType LatencyType) {
	globalConfig.latencyTypeToPrint = latencyType
}

func GetGlobalLatencyType() LatencyType {
	return globalConfig.latencyTypeToPrint
}

func SetGlobalLatencyTypeFromString(latencyType string) {
	SetGlobalLatencyType(LatencyTypes[latencyType])
}

func ValidateGlobalLatencyType(latencyType string) error {
	_, ok := LatencyTypes[latencyType]
	if !ok {
		return fmt.Errorf("unknown value %s, supported values are: raw, fixed-coordinated-omission", latencyType)
	}
	return nil
}

func SetGlobalMeasureLatency(value bool) {
	globalConfig.measureLatency = value
}

func GetGlobalMeasureLatency() bool {
	return globalConfig.measureLatency
}

func SetGlobalHdrLatencyFile(value string) {
	globalConfig.hdrLatencyFile = value
}

func GetGlobalHdrLatencyFile() string {
	return globalConfig.hdrLatencyFile
}

func SetGlobalHdrLatencyUnits(value string) {
	switch value {
	case "ns":
		globalConfig.hdrLatencyScale = 1
	case "us":
		globalConfig.hdrLatencyScale = 1000
	case "ms":
		globalConfig.hdrLatencyScale = 1000000
	default:
		panic("Wrong value for hdr-latency-scale, only supported values are: ns, us and ms")
	}
}

func GetGlobalHdrLatencyScale() int64 {
	return globalConfig.hdrLatencyScale
}

func SetGlobalConcurrency(value int) {
	globalConfig.concurrency = value
}

func GetGlobalConcurrency() int {
	return globalConfig.concurrency
}

func NumberOfLatencyResultsInPartialReportCycle() int64 {
	nsInReportCycleTime := int64(globalConfig.reportingCycle)
	nsPerDataPoint := int64(1000)
	return nsInReportCycleTime / nsPerDataPoint
}

func init() {
	globalConfig = Configuration{
		measureLatency: false,
		latencyHistogramConfiguration: HistogramConfiguration{
			MinValue: 0,
			MaxValue: math.MaxInt64,
			SigFig:   3,
		},
		latencyTypeToPrint: LatencyTypeRaw,
		reportingCycle:     time.Second,
	}
}
