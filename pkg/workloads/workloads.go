package workloads

import (
	"log"
	"math"
	"math/rand"
	"time"
)

func MinInt64(a int64, b int64) int64 {
	if a < b {
		return a
	} else {
		return b
	}
}

const (
	minToken int64 = -(1 << 63)
	maxToken int64 = (1 << 63) - 1
)

// Bounds are inclusive
type TokenRange struct {
	Start int64
	End   int64
}

type WorkloadGenerator interface {
	NextTokenRange() TokenRange
	NextPartitionKey() int64
	NextClusteringKey() int64
	IsPartitionDone() bool
	IsDone() bool
	Restart()
}

type SequentialVisitAll struct {
	PartitionOffset    int64
	PartitionCount     int64
	ClusteringRowCount int64
	NextPartition      int64
	NextClusteringRow  int64
}

func NewSequentialVisitAll(partitionOffset int64, partitionCount int64, clusteringRowCount int64) *SequentialVisitAll {
	return &SequentialVisitAll{partitionOffset, partitionOffset + partitionCount, clusteringRowCount, partitionOffset, 0}
}

func (sva *SequentialVisitAll) NextTokenRange() TokenRange {
	panic("SequentialVisitAll does not support NextTokenRange()")
}

func (sva *SequentialVisitAll) NextPartitionKey() int64 {
	if sva.NextClusteringRow < sva.ClusteringRowCount {
		return sva.NextPartition
	}
	sva.NextClusteringRow = 0
	sva.NextPartition++
	pk := sva.NextPartition
	return pk
}

func (sva *SequentialVisitAll) NextClusteringKey() int64 {
	ck := sva.NextClusteringRow
	sva.NextClusteringRow++
	return ck
}

func (sva *SequentialVisitAll) IsDone() bool {
	return sva.NextPartition >= sva.PartitionCount || (sva.NextPartition+1 == sva.PartitionCount && sva.NextClusteringRow >= sva.ClusteringRowCount)
}

func (sva *SequentialVisitAll) Restart() {
	sva.NextClusteringRow = 0
	sva.NextPartition = sva.PartitionOffset
}

func (sva *SequentialVisitAll) IsPartitionDone() bool {
	return sva.NextClusteringRow == sva.ClusteringRowCount
}

type RandomUniform struct {
	Generator          *rand.Rand
	PartitionCount     int64
	ClusteringRowCount int64
}

func NewRandomUniform(i int, partitionCount int64, clusteringRowCount int64) *RandomUniform {
	generator := rand.New(rand.NewSource(int64(time.Now().Nanosecond() * (i + 1))))
	return &RandomUniform{generator, int64(partitionCount), int64(clusteringRowCount)}
}

func (ru *RandomUniform) NextTokenRange() TokenRange {
	panic("RandomUniform does not support NextTokenRange()")
}

func (ru *RandomUniform) NextPartitionKey() int64 {
	return ru.Generator.Int63n(ru.PartitionCount)
}

func (ru *RandomUniform) NextClusteringKey() int64 {
	return ru.Generator.Int63n(ru.ClusteringRowCount)
}

func (ru *RandomUniform) IsDone() bool {
	return false
}

func (ru *RandomUniform) IsPartitionDone() bool {
	return false
}

func (ru *RandomUniform) Restart() {
}

type TimeSeriesWrite struct {
	PkStride            int64
	PkOffset            int64
	PkCount             int64
	PkPosition          int64
	PkGeneration        int64
	CkCount             int64
	CkPosition          int64
	StartTime           time.Time
	Period              time.Duration
	MoveToNextPartition bool
}

func NewTimeSeriesWriter(threadId int, threadCount int, pkCount int64, ckCount int64, startTime time.Time, rate int64) *TimeSeriesWrite {
	period := time.Duration(int64(time.Second.Nanoseconds()) * (pkCount / int64(threadCount)) / rate)
	pkStride := int64(threadCount)
	pkOffset := int64(threadId)
	return &TimeSeriesWrite{pkStride, pkOffset, pkCount, pkOffset - pkStride, 0,
		ckCount, 0, startTime, period, false}
}

func (tsw *TimeSeriesWrite) NextTokenRange() TokenRange {
	panic("TimeSeriesWrite does not support NextTokenRange()")
}

func (tsw *TimeSeriesWrite) NextPartitionKey() int64 {
	tsw.PkPosition += tsw.PkStride
	if tsw.PkPosition >= tsw.PkCount {
		tsw.PkPosition = tsw.PkOffset
		tsw.CkPosition++
		if tsw.CkPosition >= tsw.CkCount {
			tsw.PkGeneration++
			tsw.CkPosition = 0
		}
	}
	tsw.MoveToNextPartition = false
	return tsw.PkPosition<<32 | tsw.PkGeneration
}

func (tsw *TimeSeriesWrite) NextClusteringKey() int64 {
	tsw.MoveToNextPartition = true
	position := tsw.CkPosition + tsw.PkGeneration*tsw.CkCount
	return -(tsw.StartTime.UnixNano() + tsw.Period.Nanoseconds()*position)
}

func (*TimeSeriesWrite) IsDone() bool {
	return false
}

func (tsw *TimeSeriesWrite) IsPartitionDone() bool {
	return tsw.MoveToNextPartition
}

func (*TimeSeriesWrite) Restart() {
}

type TimeSeriesRead struct {
	Generator         *rand.Rand
	HalfNormalDist    bool
	PkStride          int64
	PkOffset          int64
	PkCount           int64
	PkPosition        int64
	StartTimestamp    int64
	CkCount           int64
	CurrentGeneration int64
	Period            int64
}

func NewTimeSeriesReader(threadId int, threadCount int, pkCount int64, ckCount int64, writeRate int64, distribution string, startTime time.Time) *TimeSeriesRead {
	var halfNormalDist bool
	switch distribution {
	case "uniform":
		halfNormalDist = false
	case "hnormal":
		halfNormalDist = true
	default:
		log.Fatal("unknown distribution", distribution)
	}
	generator := rand.New(rand.NewSource(int64(time.Now().Nanosecond() * (threadId + 1))))
	pkStride := int64(threadCount)
	pkOffset := int64(threadId) % pkCount
	period := time.Second.Nanoseconds() / writeRate
	return &TimeSeriesRead{generator, halfNormalDist, pkStride, pkOffset, pkCount, pkOffset - pkStride,
		startTime.UnixNano(), ckCount, 0, period}
}

func RandomInt64(generator *rand.Rand, halfNormalDist bool, maxValue int64) int64 {
	if halfNormalDist {
		value := 1. - math.Min(math.Abs(generator.NormFloat64()), 4.)/4.
		return int64(float64(maxValue) * value)
	} else {
		return generator.Int63n(maxValue)
	}
}

func (tsw *TimeSeriesRead) NextTokenRange() TokenRange {
	panic("TimeSeriesRead does not support NextTokenRange()")
}

func (tsw *TimeSeriesRead) NextPartitionKey() int64 {
	tsw.PkPosition += tsw.PkStride
	if tsw.PkPosition >= tsw.PkCount {
		tsw.PkPosition = tsw.PkOffset
	}
	maxGeneration := (time.Now().UnixNano()-tsw.StartTimestamp)/(tsw.Period*tsw.CkCount) + 1
	tsw.CurrentGeneration = RandomInt64(tsw.Generator, tsw.HalfNormalDist, maxGeneration)
	return tsw.PkPosition<<32 | tsw.CurrentGeneration
}

func (tsw *TimeSeriesRead) NextClusteringKey() int64 {
	maxRange := (time.Now().UnixNano()-tsw.StartTimestamp)/tsw.Period - tsw.CurrentGeneration*tsw.CkCount + 1
	maxRange = MinInt64(tsw.CkCount, maxRange)
	timestampDelta := (tsw.CurrentGeneration*tsw.CkCount + RandomInt64(tsw.Generator, tsw.HalfNormalDist, maxRange)) * tsw.Period
	return -(timestampDelta + tsw.StartTimestamp)
}

func (*TimeSeriesRead) IsDone() bool {
	return false
}

func (tsw *TimeSeriesRead) IsPartitionDone() bool {
	return false
}

func (tsw *TimeSeriesRead) Restart() {
}

type RangeScan struct {
	TotalRangeCount int
	RangeOffset     int
	RangeCount      int
	NextRange       int
}

func NewRangeScan(totalRangeCount int, rangeOffset int, rangeCount int) *RangeScan {
	return &RangeScan{totalRangeCount, rangeOffset, rangeOffset + rangeCount, rangeOffset}
}

func (rs *RangeScan) NextTokenRange() TokenRange {
	// Special case, no range splitting
	if rs.TotalRangeCount == 1 {
		rs.NextRange++
		return TokenRange{minToken, maxToken}
	}

	// This is in fact -1 compared to the real number of tokens, which
	// is 2**64. But this is fine, as the worst that can happen is that
	// due to the inprecise calculation of tokensPerRange more tokens
	// will be in the very last range than should be, which is
	// tolerable.
	const tokenCount uint64 = ^uint64(0)
	// Due to the special handling of TotalRangeCount == 1 above, this
	// is guaranteed to safely fit into an int64
	tokensPerRange := int64(tokenCount / uint64(rs.TotalRangeCount))

	currentRange := rs.NextRange
	rs.NextRange++

	firstToken := minToken + int64(currentRange)*tokensPerRange
	var lastToken int64
	// Make sure the very last range streches all the way to maxToken.
	if rs.NextRange == rs.TotalRangeCount {
		lastToken = maxToken
	} else {
		lastToken = firstToken + tokensPerRange - 1
	}

	return TokenRange{firstToken, lastToken}
}

func (*RangeScan) NextPartitionKey() int64 {
	return 0
}

func (*RangeScan) NextClusteringKey() int64 {
	return 0
}

func (*RangeScan) IsPartitionDone() bool {
	return false
}

func (rs *RangeScan) IsDone() bool {
	return rs.NextRange >= rs.RangeCount
}

func (rs *RangeScan) Restart() {
	rs.NextRange = rs.RangeOffset
}
