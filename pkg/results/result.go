package results

import (
	"time"

	"github.com/HdrHistogram/hdrhistogram-go"

	"github.com/scylladb/scylla-bench/pkg/config"
)

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

func NewHistogram(cfg *config.HistogramConfiguration, name string) *hdrhistogram.Histogram {
	histogram := hdrhistogram.New(cfg.MinValue, cfg.MaxValue, cfg.SigFig)
	histogram.SetTag(name)
	return histogram
}

func ensureHistogram(h **hdrhistogram.Histogram, name string) *hdrhistogram.Histogram {
	if *h == nil {
		*h = NewHistogram(config.GetGlobalHistogramConfiguration(), name)
	}
	return *h
}

func GetHdrMemoryConsumption(concurrency int) int {
	hdr := NewHistogram(config.GetGlobalHistogramConfiguration(), "example_hdr")
	return hdr.ByteSize() * concurrency * 4
}
