package config

import (
	"errors"
	"fmt"
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
	reportingCycle				  time.Duration
}


var Config Configuration


func SetGlobalHistogramConfiguration(minValue int64, maxValue int64, sigFig int) {
	Config.latencyHistogramConfiguration.MinValue = minValue / Config.hdrLatencyScale
	Config.latencyHistogramConfiguration.MaxValue = maxValue / Config.hdrLatencyScale
	Config.latencyHistogramConfiguration.SigFig = sigFig
}

func GetGlobalHistogramConfiguration() *HistogramConfiguration {
	return &Config.latencyHistogramConfiguration
}

func SetGlobalLatencyType(latencyType LatencyType) {
	Config.latencyTypeToPrint = latencyType
}

func GetGlobalLatencyType() LatencyType {
	return Config.latencyTypeToPrint
}

func SetGlobalLatencyTypeFromString(latencyType string) {
	SetGlobalLatencyType(LatencyTypes[latencyType])
}

func ValidateGlobalLatencyType(latencyType string) error {
	_, ok := LatencyTypes[latencyType]
	if !ok {
		return errors.New(fmt.Sprintf("unkown value %s, supported values are: raw, fixed-coordinated-omission", latencyType))
	}
	return nil
}

func SetGlobalMeasureLatency(value bool) {
	Config.measureLatency = value
}

func GetGlobalMeasureLatency() bool {
	return Config.measureLatency
}

func SetGlobalHdrLatencyFile(value string) {
	Config.hdrLatencyFile = value
}

func GetGlobalHdrLatencyFile() string {
	return Config.hdrLatencyFile
}

func SetGlobalHdrLatencyUnits(value string) {
	switch value {
	case "ns":
		Config.hdrLatencyScale = 1
		break
	case "us":
		Config.hdrLatencyScale = 1000
		break
	case "ms":
		Config.hdrLatencyScale = 1000000
		break
	default:
		panic("Wrong value for hdr-latency-scale, only supported values are: ns, us and ms")
	}
}

func GetGlobalHdrLatencyScale() int64 {
	return Config.hdrLatencyScale
}

func SetGlobalConcurrency(value int) {
	Config.concurrency = value
}

func GetGlobalConcurrency() int {
	return Config.concurrency
}

func NumberOfLatencyResultsInPartialReportCycle() int64 {
	// Minimum delay is considered to be 0.5ms
	result := int64(Config.concurrency) * int64(Config.reportingCycle) / int64(time.Millisecond/2) * 2
	if result >= 10000000 {
		return 10000000
	}
	return result
}


func init() {
	Config = Configuration{
		measureLatency: false,
		latencyHistogramConfiguration: HistogramConfiguration{
			MinValue: 0,
			MaxValue: 2 ^ 63 - 1,
			SigFig:   3,
		},
		latencyTypeToPrint: LatencyTypeRaw,
		reportingCycle: time.Second,
	}
}
